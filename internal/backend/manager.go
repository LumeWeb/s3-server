package backend

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/SiaFoundation/s3d/s3"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/status"
	"go.lumeweb.com/s3-server/internal/store"
	"go.sia.tech/core/types"
	sdk "go.sia.tech/siastorage"
	"go.uber.org/zap"
)

// Backend wraps the s3d Sia backend with the methods the panel needs.
type Backend interface {
	S3Backend() s3.Backend
	Close() error

	ListBuckets(ctx context.Context, accessKeyID string) ([]s3.BucketInfo, error)
	ListAllBuckets(ctx context.Context) ([]BucketInfo, error)
	BucketStats(ctx context.Context, bucketName string) (count int, size int64, err error)
	BucketOwner(ctx context.Context, bucketName string) (owner string, err error)
	BucketVersioning(ctx context.Context, bucketName string) (status string, err error)
	BucketCountForUser(ctx context.Context, userName string) (int, error)
	CreateBucket(ctx context.Context, accessKeyID, name string) error
	DeleteBucket(ctx context.Context, accessKeyID, name string) error
	FlushObjects(ctx context.Context) error
	PutBucketVersioning(ctx context.Context, accessKeyID, bucket, status string) error
	GetBucketVersioning(ctx context.Context, accessKeyID, bucket string) (string, error)

	PutBucketLifecycleConfiguration(ctx context.Context, accessKeyID, bucket string, config s3.LifecycleConfiguration) error
	GetBucketLifecycleConfiguration(ctx context.Context, accessKeyID, bucket string) (s3.LifecycleConfiguration, error)
	DeleteBucketLifecycleConfiguration(ctx context.Context, accessKeyID, bucket string) error
	BackupSQLite3(ctx context.Context, destPath string) error
}

// S3DStore wraps the s3d SQLite store for user and access key operations.
type S3DStore interface {
	AppKey() (types.PrivateKey, string, error)
	SetAppKey(key types.PrivateKey, indexerURL string) error
	CreateUser(name string) error
	DeleteUser(name string) error
	ListUsers() ([]string, error)
	CreateAccessKey(userName, accessKeyID, secretKey string) error
	DeleteAccessKey(accessKeyID string) error
	ListAccessKeys(userName *string) ([]AccessKeyInfo, error)
	Close() error
}

// AccessKeyInfo matches s3d's sia.AccessKeyInfo while keeping S3DStore interface-local.
type AccessKeyInfo struct {
	AccessKeyID string
	SecretKey   string
	UserName    string
}

// BucketInfo is a bucket with its owner name, for admin-panel listings
// that span all users.
type BucketInfo struct {
	Name      string
	Owner     string
	CreatedAt time.Time
}

// S3Swapper is the interface for a thread-safe swappable http.Handler.
type S3Swapper interface {
	Swap(handler http.Handler)
}

// Factory initializes the s3d backend and returns the components.
type Factory interface {
	Init(ctx context.Context, s3Cfg config.S3Config, sqliteStore S3DStore) (Backend, http.Handler, func(), error)
	OpenDatabase(dbPath string) (S3DStore, error)
}

// Manager owns the s3d backend lifecycle. It initializes s3d when onboarding
// completes and provides cleanup on shutdown.
type Manager struct {
	store   store.Store
	factory Factory
	s3      S3Swapper
	log     *zap.Logger

	mu          sync.RWMutex
	backend     Backend
	sqliteStore S3DStore
	cleanup     func()
	accountCli  AccountClient
	initError   string // non-empty when InitFromConfig failed at startup
	starting    bool  // true while async init is in progress

	// onStatusChange is called (under mu) when the status transitions.
	// Used by the SSE broker to publish an immediate status update.
	// Receives the current status string and initError so the callback
	// does not need to re-acquire m.mu (which would deadlock).
	onStatusChange func(statusStr, initErr string)

	restartMu sync.Mutex // serializes concurrent Restart calls
	initWg    sync.WaitGroup // tracks the InitFromConfigAsync goroutine
}

// NewManager creates a backend manager.
func NewManager(s store.Store, factory Factory, s3 S3Swapper, log *zap.Logger) *Manager {
	return &Manager{
		store:   s,
		factory: factory,
		s3:      s3,
		log:     log,
	}
}

// swapBackend tears down any existing backend via teardownBackend/closeResources,
// then swaps in the new backend under m.mu and swaps the S3 handler outside the lock.
func (m *Manager) swapBackend(backend Backend, sqliteStore S3DStore, cleanup func(), accountCli AccountClient, s3Handler http.Handler) {
	oldCleanup, oldAccountCli := m.teardownBackend()
	m.closeResources(oldCleanup, oldAccountCli)

	m.mu.Lock()
	m.backend = backend
	m.sqliteStore = sqliteStore
	m.cleanup = cleanup
	m.accountCli = accountCli
	m.initError = ""
	m.starting = false
	m.notifyStatusChange()
	m.mu.Unlock()

	m.s3.Swap(s3Handler)
}

// teardownBackend nils out the backend fields under m.mu and returns the
// old cleanup and accountClient for I/O outside the lock. Used by Restart
// and Cleanup.
func (m *Manager) teardownBackend() (cleanup func(), accountCli AccountClient) {
	m.mu.Lock()
	cleanup = m.cleanup
	accountCli = m.accountCli
	m.cleanup = nil
	m.sqliteStore = nil
	m.backend = nil
	m.accountCli = nil
	m.mu.Unlock()
	return
}

// closeResources runs cleanup and closes the account client outside the lock.
func (m *Manager) closeResources(cleanup func(), accountCli AccountClient) {
	if cleanup != nil {
		cleanup()
	}
	if accountCli != nil {
		if err := accountCli.Close(); err != nil {
			m.log.Error("failed to close account client", zap.Error(err))
		}
	}
}

// initResult holds the output of initCore.
type initResult struct {
	backend    Backend
	s3Handler  http.Handler
	cleanup    func()
	accountCli AccountClient
}

// initCore validates the store, calls factory.Init, and builds the account
// client. Does not close the store or call failInit — the caller owns both.
func (m *Manager) initCore(ctx context.Context, sqliteStore S3DStore, s3Cfg config.S3Config, preValidate func(S3DStore) error) (initResult, error) {
	if preValidate != nil {
		if vErr := preValidate(sqliteStore); vErr != nil {
			return initResult{}, vErr
		}
	}

	backend, s3Handler, cleanup, initErr := m.factory.Init(ctx, s3Cfg, sqliteStore)
	if initErr != nil {
		return initResult{}, initErr
	}

	accountCli, acctErr := newAccountClient(sqliteStore, s3Cfg, m.log)
	if acctErr != nil {
		m.log.Warn("failed to create account client, using noop", zap.Error(acctErr))
		accountCli = NewNoopAccountClient(m.log)
	}

	return initResult{
		backend:    backend,
		s3Handler:  s3Handler,
		cleanup:    cleanup,
		accountCli: accountCli,
	}, nil
}

// closeStore ignores close errors but logs them. Used by withStore on
// error paths where the store must be released but the error is
// secondary to the init failure.
func (m *Manager) closeStore(store S3DStore, reason string) {
	if store == nil {
		return
	}
	if err := store.Close(); err != nil {
		m.log.Error("failed to close database "+reason, zap.Error(err))
	}
}

// withStore owns the store lifecycle for all init paths. It opens the store
// via openStore, runs initCore, and transfers ownership to swapBackend on
// success. On error or panic it closes the store and calls failInit with
// the provided onFailure callback.
func (m *Manager) withStore(
	ctx context.Context,
	openStore func() (S3DStore, error),
	s3Cfg config.S3Config,
	preValidate func(S3DStore) error,
	onFailure func(),
) (retErr error) {
	var sqliteStore S3DStore

	// Registered before openStore so panics during open are caught.
	defer func() {
		if r := recover(); r != nil {
			m.closeStore(sqliteStore, "after panic")
			m.failInit(fmt.Sprintf("panic in backend init: %v", r), onFailure)
			panic(r)
		}
	}()

	sqliteStore, retErr = openStore()
	if retErr != nil {
		m.failInit(userFriendlyInitError(retErr), onFailure)
		return retErr
	}

	// Context may have been cancelled while we waited for the lock.
	// Don't fire onFailure — a shutdown cancellation is not a genuine
	// init failure that warrants resetting onboarding state.
	if ctx.Err() != nil {
		m.closeStore(sqliteStore, "on context cancel")
		m.failInit("backend init cancelled", nil)
		return ctx.Err()
	}

	result, initErr := m.initCore(ctx, sqliteStore, s3Cfg, preValidate)
	if initErr != nil {
		m.closeStore(sqliteStore, "after init error")
		retErr = initErr
		m.failInit(userFriendlyInitError(retErr), onFailure)
		return retErr
	}

	// swapBackend takes ownership on success.
	m.swapBackend(result.backend, sqliteStore, result.cleanup, result.accountCli, result.s3Handler)
	return nil
}

// validateAccessKeysExist is the preValidate fn for onboarding init paths.
func validateAccessKeysExist(s S3DStore) error {
	keys, err := s.ListAccessKeys(nil)
	if err != nil {
		return fmt.Errorf("failed to list access keys: %w", err)
	}
	if len(keys) == 0 {
		return fmt.Errorf("no access keys")
	}
	return nil
}

// InitFromConfig initializes the s3d backend from an already-completed
// onboarding config. Called at startup when onboarding is already complete.
func (m *Manager) InitFromConfig(ctx context.Context) error {
	m.restartMu.Lock()
	defer m.restartMu.Unlock()

	s3Cfg := m.store.S3Config()
	return m.withStore(ctx, func() (S3DStore, error) {
		return m.factory.OpenDatabase(s3Cfg.Directory + "/s3d.db")
	}, s3Cfg, nil, nil)
}

// InitAfterOnboarding initializes the s3d backend after onboarding completes.
// Called by the onboarding flow once app key + access keys are both set.
func (m *Manager) InitAfterOnboarding(ctx context.Context, sqliteStore S3DStore) error {
	m.restartMu.Lock()
	defer m.restartMu.Unlock()

	s3Cfg := m.store.S3Config()
	return m.withStore(ctx, func() (S3DStore, error) {
		return sqliteStore, nil
	}, s3Cfg, validateAccessKeysExist, nil)
}

// failInit sets the error state, notifies SSE, and fires the onFailure
// callback. The caller owns the callback — no shared Manager state.
func (m *Manager) failInit(initErr string, onFailure func()) {
	m.mu.Lock()
	m.starting = false
	m.initError = initErr
	m.notifyStatusChange()
	m.mu.Unlock()
	if onFailure != nil {
		onFailure()
	}
}

// OpenDatabase opens the s3d SQLite database. Used by onboarding to store the app key.
func (m *Manager) OpenDatabase(dbPath string) (S3DStore, error) {
	return m.factory.OpenDatabase(dbPath)
}

// AccountClient returns the current account client, or nil if not initialized.
func (m *Manager) AccountClient() AccountClient {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.accountCli
}

// newAccountClient builds an AccountClient from the app key and indexer URL
// stored in the SQLite store. Returns a noop client if the app key is not set
// or the indexer URL is empty.
func newAccountClient(sqliteStore S3DStore, s3Cfg config.S3Config, log *zap.Logger) (AccountClient, error) {
	appKey, indexerURL, err := sqliteStore.AppKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get app key: %w", err)
	}
	if len(appKey) == 0 {
		return NewNoopAccountClient(log), nil
	}
	if indexerURL == "" {
		indexerURL = s3Cfg.IndexerURL
	}
	if indexerURL == "" {
		return NewNoopAccountClient(log), nil
	}

	builder := sdk.NewBuilder(indexerURL, sdk.AppMetadata{
		ID:          types.HashBytes([]byte("s3d")),
		Name:        "s3-server-panel",
		Description: "S3 Server panel account monitoring",
	})
	sdkClient, err := builder.SDK(appKey, sdk.WithLogger(log.Named("account")))
	if err != nil {
		return nil, fmt.Errorf("failed to create account SDK client: %w", err)
	}
	return NewAccountClient(sdkClient), nil
}

// KeyStore returns the current s3d SQLite store, or nil if the backend is not initialized.
func (m *Manager) KeyStore() S3DStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sqliteStore
}

// Cleanup shuts down the s3d backend. Called on server shutdown.
// Waits for any in-flight async init goroutine to finish before
// tearing down resources.
func (m *Manager) Cleanup() {
	m.initWg.Wait()
	cleanup, accountCli := m.teardownBackend()
	m.closeResources(cleanup, accountCli)
}

// Restart tears down the running backend and re-initializes it from the current config.
// Called after config changes (e.g. access key changes) that require a backend restart.
func (m *Manager) Restart(ctx context.Context) error {
	m.restartMu.Lock()
	defer m.restartMu.Unlock()

	m.log.Info("restarting s3d backend")

	cleanup, oldAccountCli := m.teardownBackend()
	m.closeResources(cleanup, oldAccountCli)

	m.s3.Swap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("s3-server restarting...")) //nolint:errcheck
	}))

	s3Cfg := m.store.S3Config()
	err := m.withStore(ctx, func() (S3DStore, error) {
		return m.factory.OpenDatabase(s3Cfg.Directory + "/s3d.db")
	}, s3Cfg, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to restart backend: %w", err)
	}
	m.log.Info("s3d backend restarted")
	return nil
}

// Backend returns the current s3d backend, or nil if not initialized.
func (m *Manager) Backend() Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.backend
}

// currentStatus derives the backend status from the current field values.
// Caller must hold m.mu.
func (m *Manager) currentStatus() status.Status {
	switch {
	case m.backend != nil:
		return status.Running
	case m.initError != "":
		return status.Error
	case m.starting:
		return status.Starting
	default:
		return status.Stopped
	}
}

// Status returns the current backend status.
func (m *Manager) Status() status.Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentStatus()
}

// InitError returns the error message from a failed InitFromConfig, or empty.
func (m *Manager) InitError() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initError
}

// SetOnStatusChange sets a callback invoked whenever the backend status
// transitions (e.g. stopped -> starting -> running). The SSE broker uses
// this to push an immediate dashboard event instead of waiting for the
// next status tick.
func (m *Manager) SetOnStatusChange(fn func(statusStr, initErr string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStatusChange = fn
}

// notifyStatusChange invokes onStatusChange outside m.mu to avoid deadlock
// if the callback re-enters the manager. Must be called with m.mu held.
func (m *Manager) notifyStatusChange() {
	if m.onStatusChange == nil {
		return
	}
	statusStr := m.currentStatus().String()
	initErr := m.initError
	callback := m.onStatusChange
	m.mu.Unlock()
	callback(statusStr, initErr)
	m.mu.Lock()
}

// runAsyncInit launches doInit in a goroutine. Sets up starting state,
// initWg tracking, restartMu serialization, context cancellation, and
// a panic safety net. doInit receives onFailure to thread through to
// withStore/failInit — no shared callback state on the Manager.
func (m *Manager) runAsyncInit(ctx context.Context, onFailure func(), doInit func(context.Context, func()) error) {
	m.mu.Lock()
	m.starting = true
	m.initError = ""
	m.initWg.Add(1) // track goroutine before releasing lock — Cleanup() may
	// observe a zero WaitGroup and return immediately if Add is called after Unlock.
	m.notifyStatusChange()
	m.mu.Unlock()

	go func() {
		defer m.initWg.Done()
		defer func() {
			if r := recover(); r != nil {
				m.log.Error("panic recovered in async init", zap.Any("panic", r))
				m.mu.Lock()
				stillStarting := m.starting
				m.mu.Unlock()
				if stillStarting {
					m.failInit(fmt.Sprintf("panic in async init: %v", r), onFailure)
				}
			}
		}()

		m.restartMu.Lock()
		defer m.restartMu.Unlock()

		if err := doInit(ctx, onFailure); err != nil {
			m.log.Error("async init failed", zap.Error(err))
		} else {
			m.log.Info("async init succeeded")
		}
	}()
}

// InitFromConfigAsync starts InitFromConfig in a background goroutine.
// The HTTP server can start immediately while the backend initializes.
func (m *Manager) InitFromConfigAsync(ctx context.Context, onFailure func()) {
	m.runAsyncInit(ctx, onFailure, func(ctx context.Context, onFailure func()) error {
		s3Cfg := m.store.S3Config()
		return m.withStore(ctx, func() (S3DStore, error) {
			return m.factory.OpenDatabase(s3Cfg.Directory + "/s3d.db")
		}, s3Cfg, nil, onFailure)
	})
}

// InitAfterOnboardingAsync launches InitAfterOnboarding in a background
// goroutine with the same lifecycle guarantees.
func (m *Manager) InitAfterOnboardingAsync(ctx context.Context, sqliteStore S3DStore, onFailure func()) {
	m.runAsyncInit(ctx, onFailure, func(ctx context.Context, onFailure func()) error {
		s3Cfg := m.store.S3Config()
		return m.withStore(ctx, func() (S3DStore, error) {
			return sqliteStore, nil
		}, s3Cfg, validateAccessKeysExist, onFailure)
	})
}

// userFriendlyInitError translates internal init errors into messages
// suitable for display in the panel UI.
func userFriendlyInitError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no app key set"):
		return "No Sia app key configured. Complete onboarding to set up the backend."
	case strings.Contains(msg, "no access keys"):
		return "No access keys found. Complete onboarding to create keys."
	case strings.Contains(msg, "failed to open database"):
		return "Failed to open the S3 database. Check the data directory."
	default:
		return "Backend failed to start: " + msg
	}
}
