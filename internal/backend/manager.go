package backend

import (
	"context"
	"errors"
	"fmt"
	"net"
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
	starting    bool   // true while async init is in progress
	restarting  bool   // true between the start and end of a concurrent Restart

	// onStatusChange is called (under mu) when the status transitions.
	// Used by the SSE broker to publish an immediate status update.
	// Receives the current status string and initError so the callback
	// does not need to re-acquire m.mu (which would deadlock).
	onStatusChange func(statusStr, initErr string)

	restartMu sync.Mutex     // serializes concurrent Restart calls
	initWg    sync.WaitGroup // tracks the InitFromConfigAsync goroutine

	// retryDelay returns the backoff delay before retry attempt n. It is
	// injectable for testing; production uses retryBackoff.
	retryDelay func(attempt int) time.Duration
}

// NewManager creates a backend manager.
func NewManager(s store.Store, factory Factory, s3 S3Swapper, log *zap.Logger) *Manager {
	return &Manager{
		store:      s,
		factory:    factory,
		s3:         s3,
		log:        log,
		retryDelay: retryBackoff,
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
// client. Does not close the store or call recordInitFailure — the caller owns both.
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

// closeStore ignores close errors but logs them. Used by attemptInit and
// InitAfterOnboardingAsync on error/transfer paths where the store must be
// released but the error is secondary to the init failure.
func (m *Manager) closeStore(store S3DStore, reason string) {
	if store == nil {
		return
	}
	if err := store.Close(); err != nil {
		m.log.Error("failed to close database "+reason, zap.Error(err))
	}
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
	err := m.syncInit(ctx, func() (S3DStore, error) {
		return m.factory.OpenDatabase(s3Cfg.Directory + "/s3d.db")
	}, s3Cfg, nil)
	if err != nil {
		m.recordInitFailure(userFriendlyInitError(err), nil)
	}
	return err
}

// InitAfterOnboarding initializes the s3d backend after onboarding completes.
// Called by the onboarding flow once app key + access keys are both set.
func (m *Manager) InitAfterOnboarding(ctx context.Context, sqliteStore S3DStore) error {
	m.restartMu.Lock()
	defer m.restartMu.Unlock()

	s3Cfg := m.store.S3Config()
	err := m.syncInit(ctx, func() (S3DStore, error) {
		return sqliteStore, nil
	}, s3Cfg, validateAccessKeysExist)
	if err != nil {
		m.recordInitFailure(userFriendlyInitError(err), nil)
	}
	return err
}

// syncInit is a thin wrapper around attemptInit with panic recovery for
// synchronous (non-async) callers. On error it closes the store already;
// the caller calls recordInitFailure with the appropriate onFailure.
func (m *Manager) syncInit(
	ctx context.Context,
	openStore func() (S3DStore, error),
	s3Cfg config.S3Config,
	preValidate func(S3DStore) error,
) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in backend init: %v", r)
		}
	}()
	return m.attemptInit(ctx, openStore, s3Cfg, preValidate)
}

// recordInitFailure sets the error state, notifies SSE, and conditionally
// fires the onFailure callback. It is the single failure path for both sync
// and async init.
//
// The guard is simple: onFailure is never fired when a live backend exists or
// a concurrent Restart is in progress — those are the only conditions where
// firing the (potentially destructive) onboarding callback would be wrong.
// Unlike the previous failInitIfStarting, this does NOT require starting==true,
// so a terminal failure after a concurrent Restart failed (clearing starting)
// still surfaces the error and resets onboarding instead of being swallowed.
//
// The RLock is held across the onFailure invocation so a concurrent Restart
// cannot bring the backend to Running between the check and the destructive
// callback. The callback must NOT re-acquire manager locks (e.g.
// ResetToAppKeySet must not block on m.mu).
func (m *Manager) recordInitFailure(initErr string, onFailure func()) {
	m.mu.Lock()
	if m.backend != nil || m.restarting {
		// A concurrent Restart won or is in flight — don't overwrite its
		// state (it may be about to bring the backend to Running).
		m.mu.Unlock()
		return
	}
	m.starting = false
	m.initError = initErr
	m.notifyStatusChange()
	m.mu.Unlock()

	if onFailure == nil {
		return
	}
	// Hold the RLock across onFailure so a concurrent Restart cannot bring
	// the backend to Running between the check and the destructive callback.
	// This is safe because onFailure (ResetToAppKeySet) does NOT re-acquire
	// m.mu — it operates on the onboarding FSM, panel store, and SQLite store
	// via the factory, none of which touch the manager lock.
	m.mu.RLock()
	stillFailed := m.backend == nil && !m.restarting
	if stillFailed {
		onFailure()
	}
	m.mu.RUnlock()
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

	builder := sdk.NewBuilder(indexerURL, SiaAppMetadata())
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
	// Mark a concurrent Restart as in-progress before acquiring restartMu so
	// the async retry loop's destructive onFailure (onboarding key wipe) can
	// never race a queued/in-flight Restart. Cleared on every exit path.
	m.mu.Lock()
	m.restarting = true
	m.mu.Unlock()
	m.restartMu.Lock()
	defer func() {
		m.restartMu.Unlock()
		m.mu.Lock()
		m.restarting = false
		m.mu.Unlock()
	}()

	m.log.Info("restarting s3d backend")

	cleanup, oldAccountCli := m.teardownBackend()
	m.closeResources(cleanup, oldAccountCli)

	m.s3.Swap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("s3-server restarting...")) //nolint:errcheck
	}))

	s3Cfg := m.store.S3Config()
	err := m.syncInit(ctx, func() (S3DStore, error) {
		return m.factory.OpenDatabase(s3Cfg.Directory + "/s3d.db")
	}, s3Cfg, nil)
	if err != nil {
		// Clear restarting before recording the failure so the guard in
		// recordInitFailure doesn't suppress it. Safe: Restart still holds
		// restartMu, so no concurrent Restart or attemptInit can run.
		m.mu.Lock()
		m.restarting = false
		m.mu.Unlock()
		m.recordInitFailure(userFriendlyInitError(err), nil)
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

// initRetryBaseDelay is the initial delay before the first retry of a
// failed async backend init. Subsequent retries back off exponentially up to
// initRetryMaxDelay. The loop keeps retrying until the backend initializes,
// a terminal (non-retryable) error occurs, the maxInitRetries budget is
// exhausted, or the context is cancelled — so a transient indexer connection
// failure self-heals without a manual restart, while a persistent failure is
// bounded and surfaced instead of retrying forever.
const (
	initRetryBaseDelay = 5 * time.Second
	initRetryMaxDelay  = 5 * time.Minute
	// maxInitRetries bounds the retry loop so a persistent non-terminal
	// failure is eventually surfaced (rather than retrying forever) and so
	// Cleanup's initWg.Wait() always returns on shutdown. At base 5s doubled
	// and capped at 5min this is roughly 40 minutes of total retry time.
	maxInitRetries = 12
)

// retryBackoff returns the backoff delay before retry attempt n (0-based).
// It doubles from initRetryBaseDelay on each retry, capped at initRetryMaxDelay.
func retryBackoff(attempt int) time.Duration {
	d := initRetryBaseDelay
	for i := 0; i < attempt && d < initRetryMaxDelay; i++ {
		d *= 2
		if d > initRetryMaxDelay {
			d = initRetryMaxDelay
		}
	}
	return d
}

// isTerminalInitError reports whether an init failure is a config or
// precondition problem that retrying cannot fix (and therefore should not be
// retried). Everything else — e.g. an unreachable indexer or a transient
// network error during factory.Init — is treated as retryable.
func isTerminalInitError(err error) bool {
	if err == nil {
		return false
	}

	// Network/IO failures (connection refused/reset, i/o timeout, no such
	// host, TLS/proxy errors, EOF) are transient and must be retried. Detecting
	// the net.Error / net.OpError wrapper via errors.As is robust even when the
	// underlying error is wrapped with %w.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return false
	}

	msg := strings.ToLower(err.Error())
	if msg == "backend init cancelled" {
		return true
	}

	// SQLite lock/busy contention and temporary resource unavailability are
	// transient.
	for _, s := range []string{
		"temporarily unavailable",
		"database is locked",
		"database is busy",
	} {
		if strings.Contains(msg, s) {
			return false
		}
	}

	// Explicit permanent config/precondition failures are terminal so the
	// onboarding wizard resets promptly instead of retrying forever. These
	// include the reachable permanent factory messages (factory.go): an
	// incompatible store type, or an SDK/backend that cannot be constructed
	// for a non-network reason (e.g. a missing/misconfigured app key).
	// Genuine network/IO wraps are already ruled out above via errors.As.
	for _, s := range []string{
		"no app key set",
		"no access keys",
		"no such file or directory",
		"permission denied",
		"no indexer url configured",
		"unexpected sqlite store type",
		"failed to create sia backend",
		"failed to create sdk client",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}

	// Default unknown errors to transient (retried, bounded by maxInitRetries).
	// A misclassified network/IO failure must never trigger the destructive
	// onboarding onFailure (ResetToAppKeySet) with zero backoff.
	return false
}

// attemptInit performs a single backend initialization attempt. It opens the
// store, runs preValidate + initCore, and on success swaps in the backend.
// On failure (or context cancellation) it closes the store and returns the
// error without recording failure or firing onFailure — the caller decides
// whether to retry or fail terminally.
func (m *Manager) attemptInit(ctx context.Context, openStore func() (S3DStore, error), s3Cfg config.S3Config, preValidate func(S3DStore) error) error {
	sqliteStore, err := openStore()
	if err != nil {
		return err
	}

	// Don't leak the store if initCore panics; re-panic for the async
	// goroutine's panic safety net to handle. The transferred flag is set
	// once swapBackend takes ownership, so a panic after that point must not
	// close a store that the backend now owns (which would leave the manager
	// reporting Running against a closed store).
	transferred := false
	defer func() {
		if r := recover(); r != nil {
			if !transferred {
				m.closeStore(sqliteStore, "after panic")
			}
			panic(r)
		}
	}()

	// Context may have been cancelled while we waited for the lock.
	if ctx.Err() != nil {
		m.closeStore(sqliteStore, "on context cancel")
		return ctx.Err()
	}

	// A concurrent Restart may have brought the backend to Running while we
	// waited for the lock / opened the store. Skip the expensive factory.Init
	// (SDK creation, network CheckAppAuth) and just release the store.
	if m.Backend() != nil {
		m.closeStore(sqliteStore, "superseded before init")
		return nil
	}

	result, initErr := m.initCore(ctx, sqliteStore, s3Cfg, preValidate)
	if initErr != nil {
		m.closeStore(sqliteStore, "after init error")
		return initErr
	}

	// A concurrent Restart() may have already brought the backend to Running
	// while this retry attempt was in flight; don't tear down and rebuild the
	// live backend with a possibly-stale result. Release the freshly-opened
	// store instead and treat the attempt as a no-op. Only supersede when an
	// actual backend is Running: if a concurrent Restart merely FAILED
	// (clearing starting but leaving no backend), this successful attempt must
	// still be swapped in so self-heal isn't silently broken.
	if m.Backend() != nil {
		// closeResources owns the store close via the factory cleanup closure,
		// so we must not also closeStore here — that would double-close the
		// SQLite handle on a live backend. Only closeStore as a fallback when
		// the init produced no cleanup closure.
		if result.cleanup == nil {
			m.closeStore(sqliteStore, "superseded by concurrent restart")
		}
		m.closeResources(result.cleanup, result.accountCli)
		return nil
	}
	m.swapBackend(result.backend, sqliteStore, result.cleanup, result.accountCli, result.s3Handler)
	transferred = true
	return nil
}

// runInitAttempt serializes a single init attempt under restartMu, releasing
// the lock via defer so it is never left held if doInit panics and re-panics.
// The backoff sleep between attempts happens outside this helper, so a
// concurrent Restart/Init is not starved for the outage duration.
func (m *Manager) runInitAttempt(ctx context.Context, doInit func(context.Context) error) (err error) {
	m.restartMu.Lock()
	defer m.restartMu.Unlock()
	return doInit(ctx)
}

// runAsyncInit launches doInit in a goroutine, retrying transient failures
// with exponential backoff. Sets up starting state, initWg tracking,
// restartMu serialization, context cancellation, and a panic safety net.
func (m *Manager) runAsyncInit(ctx context.Context, onFailure func(), doInit func(context.Context) error) {
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
				m.recordInitFailure(fmt.Sprintf("panic in async init: %v", r), onFailure)
			}
		}()

		var lastErr error
		for attempt := 0; ; attempt++ {
			// A concurrent Restart already brought the backend to Running —
			// stop the retry loop instead of re-running OpenDatabase+factory.Init
			// against the live store (which would churn heavy SDK init work and
			// contend on the in-use SQLite handle) for the remaining budget.
			if m.Backend() != nil {
				m.log.Info("async init superseded by concurrent restart; stopping retry loop")
				return
			}

			// Bounded: once the attempt budget is exhausted, surface the last
			// failure via recordInitFailure/onFailure instead of retrying
			// forever. The check runs before doInit so exactly maxInitRetries
			// attempts are made, and Cleanup's initWg.Wait() always returns on
			// shutdown. so a concurrent
			// Restart (in progress or about to succeed) or a live backend is
			// never torn down nor its onboarding keys wiped.
			if attempt >= maxInitRetries {
				m.recordInitFailure(userFriendlyInitError(lastErr), onFailure)
				m.log.Error("async init failed (retry budget exhausted)",
					zap.Error(lastErr), zap.Int("attempt", attempt))
				return
			}

			// Hold restartMu only around the actual init attempt (released via
			// defer in runInitAttempt so a panic can never leave it locked),
			// not across the backoff sleep between attempts. Otherwise a
			// concurrent Restart/Init would be starved for the entire outage.
			err := m.runInitAttempt(ctx, doInit)
			lastErr = err

			if err == nil {
				m.log.Info("async init succeeded")
				return
			}

			// Context cancellation is a shutdown, not a genuine init failure:
			// stop immediately without firing onFailure (which could reset
			// onboarding state on a clean shutdown).
			// so a live backend won by a concurrent Restart is never torn down
			// by a stale cancel.
			if ctx.Err() != nil {
				m.recordInitFailure("backend init cancelled", nil)
				m.log.Warn("async init stopped: context cancelled", zap.Error(err))
				return
			}

			// Config/precondition failures are not retried — retrying cannot
			// fix them.
			// Restart is never torn down by a stale terminal failure.
			if isTerminalInitError(err) {
				m.recordInitFailure(userFriendlyInitError(err), onFailure)
				m.log.Error("async init failed (not retrying)", zap.Error(err))
				return
			}

			// Transient failure (e.g. indexer unreachable): retry with backoff.
			delay := m.retryDelay(attempt)
			m.log.Warn("async init failed; retrying", zap.Error(err), zap.Duration("retryIn", delay), zap.Int("attempt", attempt+1))
			select {
			case <-ctx.Done():
				m.recordInitFailure("backend init cancelled", nil)
				return
			case <-time.After(delay):
			}
		}
	}()
}

// InitFromConfigAsync starts InitFromConfig in a background goroutine.
// The HTTP server can start immediately while the backend initializes.
func (m *Manager) InitFromConfigAsync(ctx context.Context, onFailure func()) {
	m.runAsyncInit(ctx, onFailure, func(ctx context.Context) error {
		s3Cfg := m.store.S3Config()
		return m.attemptInit(ctx, func() (S3DStore, error) {
			return m.factory.OpenDatabase(s3Cfg.Directory + "/s3d.db")
		}, s3Cfg, nil)
	})
}

// InitAfterOnboardingAsync launches InitAfterOnboarding in a background
// goroutine with the same lifecycle guarantees.
//
// The sqliteStore passed in was used by onboarding to persist the app key and
// access keys to disk. A failed init attempt closes the store, so the retry
// loop must reopen the database from its path on every attempt (mirroring
// InitFromConfigAsync) — a closed handle cannot be reused and would otherwise
// loop forever on "sql: database is closed". The passed handle is therefore
// redundant once ownership is transferred, so it is closed here to avoid
// leaking it.
func (m *Manager) InitAfterOnboardingAsync(ctx context.Context, sqliteStore S3DStore, onFailure func()) {
	m.closeStore(sqliteStore, "after transferring ownership to async init")
	m.runAsyncInit(ctx, onFailure, func(ctx context.Context) error {
		s3Cfg := m.store.S3Config()
		return m.attemptInit(ctx, func() (S3DStore, error) {
			return m.factory.OpenDatabase(s3Cfg.Directory + "/s3d.db")
		}, s3Cfg, validateAccessKeysExist)
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
