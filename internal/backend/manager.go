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

	restartMu sync.Mutex // serializes concurrent Restart calls
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

// InitFromConfig initializes the s3d backend from an already-completed onboarding config.
// Called at startup when onboarding is already complete.
func (m *Manager) InitFromConfig(ctx context.Context) error {
	s3Cfg := m.store.S3Config()

	// I/O outside the lock
	sqliteStore, err := m.factory.OpenDatabase(s3Cfg.Directory + "/s3d.db")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	// I/O outside the lock
	backend, s3Handler, cleanup, err := m.factory.Init(ctx, s3Cfg, sqliteStore)
	if err != nil {
		if closeErr := sqliteStore.Close(); closeErr != nil {
			m.log.Error("failed to close database after init error", zap.Error(closeErr))
		}
		m.mu.Lock()
		m.initError = userFriendlyInitError(err)
		m.mu.Unlock()
		return fmt.Errorf("failed to init s3d backend: %w", err)
	}

	// Build the account client alongside the backend. Non-fatal: if it
	// fails, we log and continue with a noop client.
	accountCli, err := newAccountClient(sqliteStore, s3Cfg, m.log)
	if err != nil {
		m.log.Warn("failed to create account client, using noop", zap.Error(err))
		accountCli = NewNoopAccountClient(m.log)
	}

	// Lock only to swap pointers
	m.mu.Lock()
	m.backend = backend
	m.sqliteStore = sqliteStore
	m.cleanup = cleanup
	m.accountCli = accountCli
	m.initError = "" // clear on successful init
	m.mu.Unlock()

	m.s3.Swap(s3Handler)
	m.log.Info("s3d backend initialized from existing config")
	return nil
}

// InitAfterOnboarding initializes the s3d backend after onboarding completes.
// Called by the onboarding flow once app key + access keys are both set.
func (m *Manager) InitAfterOnboarding(ctx context.Context, sqliteStore S3DStore) error {
	s3Cfg := m.store.S3Config()

	keys, err := sqliteStore.ListAccessKeys(nil)
	if err != nil {
		return fmt.Errorf("failed to list access keys: %w", err)
	}
	if len(keys) == 0 {
		return fmt.Errorf("no access keys")
	}

	// I/O outside the lock
	backend, s3Handler, cleanup, err := m.factory.Init(ctx, s3Cfg, sqliteStore)
	if err != nil {
		return err
	}

	// Build the account client alongside the backend. Non-fatal.
	accountCli, err := newAccountClient(sqliteStore, s3Cfg, m.log)
	if err != nil {
		m.log.Warn("failed to create account client, using noop", zap.Error(err))
		accountCli = NewNoopAccountClient(m.log)
	}

	// Lock only to swap pointers
	m.mu.Lock()
	m.backend = backend
	m.sqliteStore = sqliteStore
	m.cleanup = cleanup
	m.accountCli = accountCli
	m.initError = "" // clear any stale error from a previous failed init
	m.mu.Unlock()

	m.s3.Swap(s3Handler)
	m.log.Info("s3d backend initialized")
	return nil
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
func (m *Manager) Cleanup() {
	m.mu.Lock()
	cleanup := m.cleanup
	accountCli := m.accountCli
	m.cleanup = nil
	m.sqliteStore = nil
	m.backend = nil
	m.accountCli = nil
	m.mu.Unlock()

	// I/O outside the lock: cleanup closure already closes the sqlite store
	if cleanup != nil {
		cleanup()
	}
	if accountCli != nil {
		if err := accountCli.Close(); err != nil {
			m.log.Error("failed to close account client", zap.Error(err))
		}
	}
}

// Restart tears down the running backend and re-initializes it from the current config.
// Called after config changes (e.g. access key changes) that require a backend restart.
func (m *Manager) Restart(ctx context.Context) error {
	m.restartMu.Lock()
	defer m.restartMu.Unlock()

	m.log.Info("restarting s3d backend")

	// Grab current resources under lock, nil out the fields
	m.mu.Lock()
	cleanup := m.cleanup
	oldAccountCli := m.accountCli
	m.cleanup = nil
	m.sqliteStore = nil
	m.backend = nil
	m.accountCli = nil
	m.mu.Unlock()

	// I/O outside the lock: cleanup closure already closes the sqlite store
	if cleanup != nil {
		cleanup()
	}
	if oldAccountCli != nil {
		if err := oldAccountCli.Close(); err != nil {
			m.log.Error("failed to close account client during restart", zap.Error(err))
		}
	}

	// Swap S3 handler to 503 placeholder during restart
	m.s3.Swap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("s3-server restarting...")) //nolint:errcheck
	}))

	// I/O outside the lock: re-initialize
	s3Cfg := m.store.S3Config()

	newSqliteStore, err := m.factory.OpenDatabase(s3Cfg.Directory + "/s3d.db")
	if err != nil {
		m.mu.Lock()
		m.initError = userFriendlyInitError(err)
		m.mu.Unlock()
		return fmt.Errorf("failed to restart backend: failed to open database: %w", err)
	}

	backend, s3Handler, newCleanup, err := m.factory.Init(ctx, s3Cfg, newSqliteStore)
	if err != nil {
		if closeErr := newSqliteStore.Close(); closeErr != nil {
			m.log.Error("failed to close database after restart init error", zap.Error(closeErr))
		}
		m.mu.Lock()
		m.initError = userFriendlyInitError(err)
		m.mu.Unlock()
		return fmt.Errorf("failed to restart backend: %w", err)
	}

	// Rebuild the account client with the new store.
	accountCli, err := newAccountClient(newSqliteStore, s3Cfg, m.log)
	if err != nil {
		m.log.Warn("failed to create account client during restart, using noop", zap.Error(err))
		accountCli = NewNoopAccountClient(m.log)
	}

	// Lock only to swap pointers
	m.mu.Lock()
	m.backend = backend
	m.sqliteStore = newSqliteStore
	m.cleanup = newCleanup
	m.accountCli = accountCli
	m.initError = "" // clear any stale error from a previous failed init
	m.mu.Unlock()

	m.s3.Swap(s3Handler)
	m.log.Info("s3d backend restarted")
	return nil
}

// Backend returns the current s3d backend, or nil if not initialized.
func (m *Manager) Backend() Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.backend
}

// Status returns the current backend status.
func (m *Manager) Status() status.Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.backend != nil {
		return status.Running
	}
	if m.initError != "" {
		return status.Error
	}
	return status.Stopped
}

// InitError returns the error message from a failed InitFromConfig, or empty.
func (m *Manager) InitError() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initError
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
