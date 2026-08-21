package backend

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SiaFoundation/s3d/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/status"
	"go.lumeweb.com/s3-server/internal/testutil"
	"go.sia.tech/core/types"
)

const testUser = "test-user"

// testSecretKey is sourced from TEST_SECRET_KEY env (set in CI/Makefile).
func testSecretKey() string {
	return os.Getenv("TEST_SECRET_KEY")
}

// stubFactory implements Factory for testing.
type stubFactory struct {
	openDBFn func(dbPath string) (S3DStore, error)
	initFn   func(ctx context.Context, s3Cfg config.S3Config, sqliteStore S3DStore) (Backend, http.Handler, func(), error)
}

func (f *stubFactory) OpenDatabase(dbPath string) (S3DStore, error) {
	return f.openDBFn(dbPath)
}

func (f *stubFactory) Init(ctx context.Context, s3Cfg config.S3Config, sqliteStore S3DStore) (Backend, http.Handler, func(), error) {
	return f.initFn(ctx, s3Cfg, sqliteStore)
}

// stubS3DStore implements S3DStore for testing.
type stubS3DStore struct {
	mu         sync.Mutex
	closeErr   error
	closeCount atomic.Int32
	accessKeys []AccessKeyInfo
	appKeySet  bool
	indexerURL string
	privateKey types.PrivateKey
}

func (s *stubS3DStore) AppKey() (types.PrivateKey, string, error) {
	return s.privateKey, s.indexerURL, nil
}
func (s *stubS3DStore) SetAppKey(key types.PrivateKey, indexerURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.privateKey = key
	s.indexerURL = indexerURL
	s.appKeySet = true
	return nil
}
func (s *stubS3DStore) CreateUser(name string) error { return nil }
func (s *stubS3DStore) DeleteUser(name string) error { return nil }
func (s *stubS3DStore) ListUsers() ([]string, error) { return nil, nil }
func (s *stubS3DStore) CreateAccessKey(userName, accessKeyID, secretKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accessKeys = append(s.accessKeys, AccessKeyInfo{AccessKeyID: accessKeyID, SecretKey: secretKey, UserName: userName})
	return nil
}
func (s *stubS3DStore) DeleteAccessKey(accessKeyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.accessKeys[:0]
	for _, k := range s.accessKeys {
		if k.AccessKeyID != accessKeyID {
			filtered = append(filtered, k)
		}
	}
	s.accessKeys = filtered
	return nil
}
func (s *stubS3DStore) ListAccessKeys(userName *string) ([]AccessKeyInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accessKeys, nil
}
func (s *stubS3DStore) Close() error {
	s.closeCount.Add(1)
	return s.closeErr
}

// stubBackend implements Backend for testing.
type stubBackend struct {
	closed bool
}

func (b *stubBackend) S3Backend() s3.Backend { return nil }
func (b *stubBackend) ListBuckets(ctx context.Context, accessKeyID string) ([]s3.BucketInfo, error) {
	return nil, nil
}
func (b *stubBackend) ListAllBuckets(ctx context.Context) ([]BucketInfo, error) {
	return nil, nil
}
func (b *stubBackend) BucketStats(ctx context.Context, bucketName string) (int, int64, error) {
	return 0, 0, nil
}
func (b *stubBackend) BucketOwner(ctx context.Context, bucketName string) (string, error) {
	return "", nil
}
func (b *stubBackend) BucketVersioning(ctx context.Context, bucketName string) (string, error) {
	return "", nil
}
func (b *stubBackend) BucketCountForUser(ctx context.Context, userName string) (int, error) {
	return 0, nil
}
func (b *stubBackend) CreateBucket(ctx context.Context, accessKeyID, name string) error { return nil }
func (b *stubBackend) DeleteBucket(ctx context.Context, accessKeyID, name string) error { return nil }
func (b *stubBackend) FlushObjects(ctx context.Context) error                           { return nil }
func (b *stubBackend) PutBucketVersioning(ctx context.Context, accessKeyID, bucket, status string) error {
	return nil
}
func (b *stubBackend) GetBucketVersioning(ctx context.Context, accessKeyID, bucket string) (string, error) {
	return "", nil
}
func (b *stubBackend) PutBucketLifecycleConfiguration(ctx context.Context, accessKeyID, bucket string, config s3.LifecycleConfiguration) error {
	return nil
}
func (b *stubBackend) GetBucketLifecycleConfiguration(ctx context.Context, accessKeyID, bucket string) (s3.LifecycleConfiguration, error) {
	return s3.LifecycleConfiguration{}, nil
}
func (b *stubBackend) DeleteBucketLifecycleConfiguration(ctx context.Context, accessKeyID, bucket string) error {
	return nil
}
func (b *stubBackend) BackupSQLite3(ctx context.Context, destPath string) error { return nil }
func (b *stubBackend) Close() error {
	b.closed = true
	return nil
}

// stubAccountClient tracks whether Close was called so tests can assert that
// superseded init attempts release the SDK account client (no leak).
type stubAccountClient struct {
	mu          sync.Mutex
	closeCalled bool
}

func (c *stubAccountClient) Account(ctx context.Context) (AccountInfo, error) {
	return AccountInfo{}, nil
}
func (c *stubAccountClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeCalled = true
	return nil
}
func (c *stubAccountClient) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCalled
}

// stubS3Swapper implements S3Swapper for testing without importing handlers.
type stubS3Swapper struct {
	mu      sync.Mutex
	handler http.Handler
}

func (s *stubS3Swapper) Swap(handler http.Handler) {
	s.mu.Lock()
	s.handler = handler
	s.mu.Unlock()
}

// stubStore implements store.Store for testing without importing store/mocks.
type stubStore struct {
	mu    sync.Mutex
	cfg   config.PanelConfig
	s3Cfg config.S3Config
}

func (s *stubStore) Config() config.PanelConfig { return s.cfg }
func (s *stubStore) DataDir() string            { return "" }
func (s *stubStore) ResetTokenPath() string     { return "" }
func (s *stubStore) S3Config() config.S3Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.s3Cfg
}
func (s *stubStore) SetAdminPassword(string) error       { return nil }
func (s *stubStore) ClearAdminPassword() error           { return nil }
func (s *stubStore) ValidateAdminPassword(string) bool   { return false }
func (s *stubStore) OnboardingState() string             { return "" }
func (s *stubStore) SetOnboardingState(string) error     { return nil }
func (s *stubStore) SSLConfig() config.SSLConfig         { return config.SSLConfig{} }
func (s *stubStore) SetSSLConfig(config.SSLConfig) error { return nil }
func (s *stubStore) SetS3Config(cfg config.S3Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s3Cfg = cfg
	return nil
}
func (s *stubStore) LogConfig() config.LogConfig {
	return config.LogConfig{Level: "info", Format: "json"}
}
func (s *stubStore) SetLogConfig(config.LogConfig) error       { return nil }
func (s *stubStore) CreateSession() (string, error)            { return "stub-session", nil }
func (s *stubStore) ValidateSession(string) bool               { return true }
func (s *stubStore) DeleteSession(string)                      {}
func (s *stubStore) StartSessionCleanup(<-chan struct{})       {}
func (s *stubStore) AccessKeys() []config.KeyPair              { return nil }
func (s *stubStore) SetAccessKeys(keys []config.KeyPair) error { return nil }

func newTestManager(t *testing.T) (*Manager, *stubStore, *stubFactory, *stubS3Swapper) {
	st := &stubStore{}
	factory := &stubFactory{}
	swapper := &stubS3Swapper{}
	log := testutil.NewTestLogger()
	m := NewManager(st, factory, swapper, log)
	return m, st, factory, swapper
}

func TestNewManager(t *testing.T) {
	m, _, _, _ := newTestManager(t)
	assert.NotNil(t, m)
	assert.Nil(t, m.Backend())
	assert.Equal(t, status.Stopped, m.Status())
}

func TestManager_InitFromConfig(t *testing.T) {
	m, st, factory, swapper := newTestManager(t)

	s3Cfg := config.S3Config{Directory: "/tmp/s3d", IndexerURL: "https://sia.storage"}
	st.s3Cfg = s3Cfg

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser}}}
	stubBack := &stubBackend{}
	cleanupCalled := false

	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		assert.Equal(t, "/tmp/s3d/s3d.db", dbPath)
		return stubStore, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		assert.Equal(t, s3Cfg, cfg)
		return stubBack, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}), func() { cleanupCalled = true }, nil
	}

	err := m.InitFromConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, stubBack, m.Backend())
	assert.Equal(t, status.Running, m.Status())
	assert.NotNil(t, swapper.handler) // S3 handler was swapped

	// Verify cleanup was wired correctly
	m.Cleanup()
	assert.True(t, cleanupCalled, "cleanup should have been called")
}

func TestManager_InitFromConfig_OpenDatabaseError(t *testing.T) {
	m, st, factory, _ := newTestManager(t)

	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		return nil, assert.AnError
	}

	err := m.InitFromConfig(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestManager_InitFromConfig_InitError(t *testing.T) {
	m, st, factory, _ := newTestManager(t)

	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser}}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return nil, nil, nil, assert.AnError
	}

	err := m.InitFromConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assert.AnError")
}

func TestManager_InitAfterOnboarding(t *testing.T) {
	m, st, factory, swapper := newTestManager(t)

	s3Cfg := config.S3Config{Directory: "/tmp/s3d", IndexerURL: "https://sia.storage"}
	st.s3Cfg = s3Cfg

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser}}}
	stubBack := &stubBackend{}

	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return stubBack, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	err := m.InitAfterOnboarding(context.Background(), stubStore)
	require.NoError(t, err)
	assert.Equal(t, stubBack, m.Backend())
	assert.NotNil(t, swapper.handler)
}

func TestManager_InitAfterOnboarding_NoAccessKeys(t *testing.T) {
	m, st, _, _ := newTestManager(t)

	st.s3Cfg = config.S3Config{}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{}}
	err := m.InitAfterOnboarding(context.Background(), stubStore)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no access keys")
}

func TestManager_OpenDatabase(t *testing.T) {
	m, _, factory, _ := newTestManager(t)

	stubStore := &stubS3DStore{}
	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		assert.Equal(t, "/tmp/s3d.db", dbPath)
		return stubStore, nil
	}

	store, err := m.OpenDatabase("/tmp/s3d.db")
	require.NoError(t, err)
	assert.Equal(t, stubStore, store)
}

func TestManager_Cleanup_NoBackend(t *testing.T) {
	m, _, _, _ := newTestManager(t)
	m.Cleanup() // should not panic
}

func TestManager_Cleanup_WithBackend(t *testing.T) {
	m, st, factory, _ := newTestManager(t)

	s3Cfg := config.S3Config{Directory: "/tmp/s3d"}
	st.s3Cfg = s3Cfg

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser}}}
	stubBack := &stubBackend{}
	cleanupCalled := false

	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return stubBack, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {
			cleanupCalled = true
		}, nil
	}

	err := m.InitFromConfig(context.Background())
	require.NoError(t, err)

	m.Cleanup()
	assert.True(t, cleanupCalled)
	assert.Equal(t, status.Stopped, m.Status())
}

func TestManager_Backend_NilBeforeInit(t *testing.T) {
	m, _, _, _ := newTestManager(t)
	assert.Nil(t, m.Backend())
	assert.Equal(t, status.Stopped, m.Status())
}

func TestManager_Status_Transitions(t *testing.T) {
	m, st, factory, _ := newTestManager(t)

	// stopped before init
	assert.Equal(t, status.Stopped, m.Status())

	s3Cfg := config.S3Config{Directory: "/tmp/s3d", IndexerURL: "https://sia.storage"}
	st.s3Cfg = s3Cfg

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser}}}
	stubBack := &stubBackend{}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return stubBack, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	// running after init
	err := m.InitFromConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, status.Running, m.Status())

	// stopped after cleanup
	m.Cleanup()
	assert.Equal(t, status.Stopped, m.Status())
}

func TestManager_Restart(t *testing.T) {
	m, st, factory, swapper := newTestManager(t)

	s3Cfg := config.S3Config{Directory: "/tmp/s3d", IndexerURL: "https://sia.storage"}
	st.s3Cfg = s3Cfg

	// first init
	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser}}}
	stubBack1 := &stubBackend{}
	cleanup1Called := false

	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		return stubStore, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return stubBack1, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {
			cleanup1Called = true
		}, nil
	}

	err := m.InitFromConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, stubBack1, m.Backend())

	// now restart with an additional key persisted directly in the store
	stubStore.accessKeys = append(stubStore.accessKeys, AccessKeyInfo{AccessKeyID: "AKIA456", SecretKey: testSecretKey(), UserName: testUser})
	stubBack2 := &stubBackend{}
	cleanup2Called := false

	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return stubBack2, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {
			cleanup2Called = true
		}, nil
	}

	err = m.Restart(context.Background())
	require.NoError(t, err)
	assert.True(t, cleanup1Called, "old cleanup should have been called")
	assert.Equal(t, stubBack2, m.Backend())
	assert.NotNil(t, swapper.handler)

	// Verify new cleanup was wired
	m.Cleanup()
	assert.True(t, cleanup2Called, "new cleanup should have been called")
}

func TestManager_Restart_Concurrent(t *testing.T) {
	m, st, factory, _ := newTestManager(t)

	s3Cfg := config.S3Config{Directory: "/tmp/s3d", IndexerURL: "https://sia.storage"}
	st.s3Cfg = s3Cfg

	var initCount atomic.Int32
	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		return &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser}}}, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		initCount.Add(1)
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	// initial setup
	err := m.InitFromConfig(context.Background())
	require.NoError(t, err)

	// concurrent restarts: only one should win the lock at a time
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			done <- m.Restart(context.Background())
		}()
	}
	for i := 0; i < 3; i++ {
		require.NoError(t, <-done)
	}

	// all 3 restarts + initial init = 4 total
	assert.Equal(t, int32(4), initCount.Load())
	assert.NotNil(t, m.Backend())
}

// Regression PR9: Restart failure must set initError so Status() reports Error.
func TestManager_Restart_SetsInitErrorOnFailure(t *testing.T) {
	m, st, factory, _ := newTestManager(t)

	s3Cfg := config.S3Config{Directory: "/tmp/s3d", IndexerURL: "https://sia.storage"}
	st.s3Cfg = s3Cfg

	// First init succeeds
	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser}}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}
	require.NoError(t, m.InitFromConfig(context.Background()))
	assert.Empty(t, m.InitError())

	// Restart: OpenDatabase succeeds, but Init fails
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return nil, nil, nil, assert.AnError
	}

	err := m.Restart(context.Background())
	require.Error(t, err)
	assert.NotEmpty(t, m.InitError(), "initError must be set after Restart failure")
	assert.Equal(t, status.Error, m.Status())
}

// Regression PR9: Successful restart must clear stale initError.
func TestManager_Restart_ClearsInitErrorOnSuccess(t *testing.T) {
	m, st, factory, _ := newTestManager(t)

	s3Cfg := config.S3Config{Directory: "/tmp/s3d", IndexerURL: "https://sia.storage"}
	st.s3Cfg = s3Cfg

	// First init fails: sets initError
	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser}}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return nil, nil, nil, assert.AnError
	}
	require.Error(t, m.InitFromConfig(context.Background()))
	assert.NotEmpty(t, m.InitError())

	// Restart: Init succeeds this time: initError should be cleared
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}
	require.NoError(t, m.Restart(context.Background()))
	assert.Empty(t, m.InitError(), "initError must be cleared after successful Restart")
	assert.Equal(t, status.Running, m.Status())
}

// Regression PR9: Cleanup after Restart must not double-close the sqlite store.
// The cleanup closure already closes the store, and Cleanup() must only call
// it once.
func TestManager_RestartThenCleanup_NoDoubleClose(t *testing.T) {
	m, st, factory, _ := newTestManager(t)

	s3Cfg := config.S3Config{Directory: "/tmp/s3d", IndexerURL: "https://sia.storage"}
	st.s3Cfg = s3Cfg

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser}}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }

	var cleanupCount atomic.Int32
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {
			cleanupCount.Add(1)
		}, nil
	}

	// Initial init
	require.NoError(t, m.InitFromConfig(context.Background()))

	// Restart: old cleanup runs, new cleanup set
	require.NoError(t, m.Restart(context.Background()))

	// Old cleanup should have been called during Restart
	assert.Equal(t, int32(1), cleanupCount.Load(), "old cleanup must be called during Restart")

	// Final Cleanup: only new cleanup should run
	m.Cleanup()
	assert.Equal(t, int32(2), cleanupCount.Load(), "final cleanup must call the new cleanup closure exactly once")
}

// --- Regression tests for Kody feedback on PR #53 ---

// TestManager_InitAfterOnboarding_Error_ClearsStarting verifies that
// InitAfterOnboarding clears the starting flag on all error paths.
// Regression for Kody finding: starting=true was never cleared on
// error branches, leaving dashboard stuck on "Starting...".
func TestManager_InitAfterOnboarding_Error_ClearsStarting(t *testing.T) {
	t.Run("no access keys", func(t *testing.T) {
		m, st, _, _ := newTestManager(t)
		st.s3Cfg = config.S3Config{}

		stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{}}
		err := m.InitAfterOnboarding(context.Background(), stubStore)
		require.Error(t, err)

		assert.Equal(t, status.Error, m.Status(), "status must not be Starting after error")
		assert.NotEmpty(t, m.InitError(), "initError must be set")
	})

	t.Run("factory init error", func(t *testing.T) {
		m, st, factory, _ := newTestManager(t)
		st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

		stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}
		factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
			return nil, nil, nil, errors.New("init failed")
		}

		err := m.InitAfterOnboarding(context.Background(), stubStore)
		require.Error(t, err)

		assert.Equal(t, status.Error, m.Status(), "status must not be Starting after factory init error")
		assert.NotEmpty(t, m.InitError(), "initError must be set")
	})

	t.Run("list access keys error", func(t *testing.T) {
		m, st, _, _ := newTestManager(t)
		st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

		err := m.InitAfterOnboarding(context.Background(), &failingListKeysStore{})
		require.Error(t, err)

		assert.Equal(t, status.Error, m.Status(), "status must not be Starting after list keys error")
	})
}

// TestManager_InitAfterOnboarding_Success_ClearsStarting verifies that
// the success path of InitAfterOnboarding clears the starting flag.
// Regression for Kody finding: success path was missing m.starting = false.
func TestManager_InitAfterOnboarding_Success_ClearsStarting(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	err := m.InitAfterOnboarding(context.Background(), stubStore)
	require.NoError(t, err)

	assert.Equal(t, status.Running, m.Status())
	assert.Empty(t, m.InitError())
}

// TestManager_InitAfterOnboarding_Error_CallsOnInitFailure verifies that
// InitAfterOnboarding invokes the onInitFailure callback on all error paths.
// Regression for Kody finding: onInitFailure was registered but never called.
func TestManager_InitAfterOnboarding_Error_CallsOnInitFailure(t *testing.T) {
	t.Run("no access keys", func(t *testing.T) {
		m, st, factory, _ := newTestManager(t)
		st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

		var failureCalled atomic.Bool
		factory.openDBFn = func(dbPath string) (S3DStore, error) {
			return &stubS3DStore{accessKeys: []AccessKeyInfo{}}, nil
		}
		m.InitAfterOnboardingAsync(context.Background(), nil, func() { failureCalled.Store(true) })
		m.initWg.Wait()
		require.NotEmpty(t, m.InitError())

		assert.True(t, failureCalled.Load(), "onFailure must be called on error")
	})

	t.Run("factory init error is retried, not terminal", func(t *testing.T) {
		m, st, factory, _ := newTestManager(t)
		st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

		factory.openDBFn = func(dbPath string) (S3DStore, error) {
			return &stubS3DStore{accessKeys: []AccessKeyInfo{
				{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
			}}, nil
		}
		var failureCalled atomic.Bool
		factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
			return nil, nil, nil, errors.New("failed to check app auth: connection refused")
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		m.InitAfterOnboardingAsync(ctx, nil, func() { failureCalled.Store(true) })

		// A transient factory error must not be treated as terminal: it is
		// retried (status stays starting) and onFailure is not called.
		time.Sleep(50 * time.Millisecond)
		assert.False(t, failureCalled.Load(), "onFailure must not be called for a retryable error")
		assert.Equal(t, status.Starting, m.Status(), "status must stay starting while retrying")

		// Cancelling the context stops the retry loop without onFailure.
		cancel()
		require.Eventually(t, func() bool {
			return m.Status() != status.Starting
		}, 2*time.Second, 10*time.Millisecond)
		assert.False(t, failureCalled.Load(), "onFailure must not be called on context cancel")
	})
}

// TestManager_RecordInitFailure verifies that recordInitFailure clears the
// starting flag, sets initError, and calls onFailure.
func TestManager_FailInit(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	var failureCalled atomic.Bool
	m.mu.Lock()
	m.starting = true
	m.mu.Unlock()

	m.recordInitFailure("test error: something went wrong", func() { failureCalled.Store(true) })

	assert.Equal(t, status.Error, m.Status(), "status must be Error after recordInitFailure")
	assert.Contains(t, m.InitError(), "test error")
	assert.True(t, failureCalled.Load(), "onFailure must be called after recordInitFailure")
}

// TestManager_InitFromConfigAsync_Panic_RecoversAndFails verifies that
// a panic inside InitFromConfig is recovered and doesn't crash the process.
// Regression for Kody finding: InitFromConfigAsync goroutine had no recover().
func TestManager_InitFromConfigAsync_Panic_RecoversAndFails(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		panic("s3dSia.New panic: nil pointer dereference")
	}

	var failureCalled atomic.Bool
	m.InitFromConfigAsync(context.Background(), func() {
		failureCalled.Store(true)
	})

	// Wait for the goroutine to finish
	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	assert.Equal(t, status.Error, m.Status(), "status must be Error after panic recovery")
	assert.Contains(t, m.InitError(), "panic")
	assert.True(t, failureCalled.Load(), "onInitFailure must be called after panic recovery")
}

// TestManager_InitFromConfigAsync_NoDuplicateNotifications verifies that
// InitFromConfigAsync does not send duplicate status notifications when
// InitFromConfig already cleared the starting flag.
// Regression for Kody finding: redundant starting=false + notifyStatusChange().
func TestManager_InitFromConfigAsync_NoDuplicateNotifications(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	var notifyCount atomic.Int32
	m.SetOnStatusChange(func(statusStr, initErr string) {
		notifyCount.Add(1)
	})

	m.InitFromConfigAsync(context.Background(), nil)

	// Wait for the goroutine to finish
	require.Eventually(t, func() bool {
		return m.Status() == status.Running
	}, 2*time.Second, 10*time.Millisecond)

	// starting→true notification (1) + running notification from InitFromConfig (1) = 2 total.
	// If the goroutine redundantly notified, count would be 3.
	assert.Equal(t, int32(2), notifyCount.Load(),
		"exactly 2 status notifications expected (starting + running), not 3")
}

// failingListKeysStore returns an error from ListAccessKeys.
type failingListKeysStore struct{}

func (s *failingListKeysStore) AppKey() (types.PrivateKey, string, error) {
	return types.PrivateKey{}, "", nil
}
func (s *failingListKeysStore) SetAppKey(key types.PrivateKey, indexerURL string) error {
	return nil
}
func (s *failingListKeysStore) CreateUser(name string) error { return nil }
func (s *failingListKeysStore) DeleteUser(name string) error { return nil }
func (s *failingListKeysStore) ListUsers() ([]string, error) { return nil, nil }
func (s *failingListKeysStore) CreateAccessKey(userName, accessKeyID, secretKey string) error {
	return nil
}
func (s *failingListKeysStore) DeleteAccessKey(accessKeyID string) error { return nil }
func (s *failingListKeysStore) ListAccessKeys(userName *string) ([]AccessKeyInfo, error) {
	return nil, errors.New("database locked")
}
func (s *failingListKeysStore) Close() error { return nil }

// TestManager_InitFromConfigAsync_SerializedWithRestart verifies that
// InitFromConfigAsync acquires restartMu so a concurrent Restart cannot
// race on the same SQLite DB and factory.Init.
// Regression for Kody finding: async init and Restart race on backend state.
func TestManager_InitFromConfigAsync_SerializedWithRestart(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	// Gate: openDB blocks until released, so async init holds restartMu
	// for as long as the test needs.
	openDBGate := make(chan struct{})

	var initFinished atomic.Bool

	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		<-openDBGate // block until test releases
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		initFinished.Store(true)
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	m.InitFromConfigAsync(context.Background(), nil)

	// Give the goroutine time to acquire restartMu
	time.Sleep(50 * time.Millisecond)

	// Now try to call Restart — it should block until async init finishes
	restartDone := make(chan error, 1)
	go func() {
		restartDone <- m.Restart(context.Background())
	}()

	// Restart should not complete while init is still blocked
	select {
	case <-restartDone:
		t.Fatal("Restart completed before async init finished — restartMu not held")
	case <-time.After(100 * time.Millisecond):
		// Expected: Restart is blocked
	}

	// Release the gate so async init can proceed
	close(openDBGate)

	// Now both async init and Restart should complete
	select {
	case err := <-restartDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Restart did not complete after async init was released")
	}

	require.True(t, initFinished.Load(), "factory.Init must have been called")
}

// --- Regression tests for DRY consolidation (initCore + runAsyncInit) ---

// TestManager_InitAfterOnboardingAsync_TracksInitWg verifies that
// InitAfterOnboardingAsync is tracked by initWg so Cleanup waits for it.
func TestManager_InitAfterOnboardingAsync_TracksInitWg(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	gate := make(chan struct{})
	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		<-gate
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	m.InitAfterOnboardingAsync(context.Background(), nil, nil)

	// Cleanup should block while the goroutine is still running
	done := make(chan struct{})
	go func() {
		m.Cleanup()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Cleanup completed before async init finished — initWg not tracked")
	case <-time.After(100 * time.Millisecond):
		// Expected: Cleanup is blocked
	}

	close(gate)

	select {
	case <-done:
		// Expected: Cleanup completed after init finished
	case <-time.After(2 * time.Second):
		t.Fatal("Cleanup did not complete after async init finished")
	}
}

// TestManager_InitAfterOnboardingAsync_ContextCancellation verifies
// that InitAfterOnboardingAsync respects context cancellation.
func TestManager_InitAfterOnboardingAsync_ContextCancellation(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		t.Fatal("factory.Init should not be called when context is cancelled")
		return nil, nil, nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before launch

	m.InitAfterOnboardingAsync(ctx, nil, nil)

	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	assert.NotEqual(t, status.Starting, m.Status(), "status must not be Starting after context cancel")
}

// TestManager_InitAfterOnboardingAsync_ContextCancel_ClosesStore verifies
// that a pre-cancelled context does not leak the sqliteStore handle.
// The doInit closure (not runAsyncInit) is responsible for closing the store.
func TestManager_InitAfterOnboardingAsync_ContextCancel_ClosesStore(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	// The async init reopens the DB from its path each attempt, so the
	// reopened handle is the one that must be closed on context cancel.
	reopened := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return reopened, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		t.Fatal("factory.Init should not be called when context is cancelled")
		return nil, nil, nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m.InitAfterOnboardingAsync(ctx, nil, nil)

	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	assert.Equal(t, int32(1), reopened.closeCount.Load(),
		"store must be closed once when context is cancelled before init")
}

// TestManager_SwapBackend_TearsDownPreviousBackend verifies that
// swapBackend calls the previous backend's cleanup function before
// swapping in the new one, preventing resource leaks when a concurrent
// Restart races with an async init.
func TestManager_SwapBackend_TearsDownPreviousBackend(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	oldCleanupCalled := atomic.Int32{}
	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }

	// First init: sets up a backend with a cleanup fn
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {
			oldCleanupCalled.Add(1)
		}, nil
	}
	require.NoError(t, m.InitFromConfig(context.Background()))
	require.Equal(t, int32(0), oldCleanupCalled.Load(), "cleanup should not be called yet")

	// Second init: swapBackend should call the old cleanup
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}
	require.NoError(t, m.Restart(context.Background()))

	assert.Equal(t, int32(1), oldCleanupCalled.Load(),
		"previous backend cleanup must be called during swapBackend overwrite")
}

// TestManager_InitAfterOnboardingAsync_SerializedWithRestart verifies
// that InitAfterOnboardingAsync acquires restartMu to serialize with Restart.
func TestManager_InitAfterOnboardingAsync_SerializedWithRestart(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	gate := make(chan struct{})
	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		<-gate
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	m.InitAfterOnboardingAsync(context.Background(), nil, nil)

	time.Sleep(50 * time.Millisecond)

	// Restart should block while async init holds restartMu
	restartDone := make(chan error, 1)
	go func() {
		restartDone <- m.Restart(context.Background())
	}()
	select {
	case <-restartDone:
		t.Fatal("Restart completed before async init finished — restartMu not held")
	case <-time.After(100 * time.Millisecond):
		// Expected: Restart is blocked
	}

	close(gate)

	select {
	case err := <-restartDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Restart did not complete after async init was released")
	}
}

// TestManager_InitFromConfigAsync_OnFailureCalledOnce verifies that
// onFailure is called exactly once when OpenDatabase fails.
func TestManager_InitFromConfigAsync_OnFailureCalledOnce(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	// A genuinely permanent open failure (permission-denied) is terminal; a
	// transient SQLite lock/busy is retried (see TestManager_..._SelfHealsOnLockedDB).
	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		return nil, errors.New("failed to open database: permission denied")
	}

	var failureCount atomic.Int32
	m.InitFromConfigAsync(context.Background(), func() {
		failureCount.Add(1)
	})

	require.Eventually(t, func() bool {
		return m.Status() == status.Error
	}, 2*time.Second, 10*time.Millisecond)

	assert.Equal(t, int32(1), failureCount.Load(), "onFailure must be called exactly once")
}

// TestManager_InitAfterOnboarding_StoreClosedOnError verifies that
// InitAfterOnboarding closes the store on the no-access-keys error path.
func TestManager_InitAfterOnboarding_StoreClosedOnError(t *testing.T) {
	m, st, _, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{}}
	_ = m.InitAfterOnboarding(context.Background(), stubStore)

	assert.Equal(t, int32(1), stubStore.closeCount.Load(),
		"store must be closed once on no-access-keys error")
}

// TestManager_InitAfterOnboarding_StoreClosedOnFactoryInitError verifies
// that initCore closes the store when factory.Init fails.
func TestManager_InitAfterOnboarding_StoreClosedOnFactoryInitError(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return nil, nil, nil, errors.New("init failed")
	}

	_ = m.InitAfterOnboarding(context.Background(), stubStore)

	assert.Equal(t, int32(1), stubStore.closeCount.Load(),
		"store must be closed once on factory.Init error")
}

// TestManager_InitFromConfig_Panic_OpenDatabase verifies that a panic
// in OpenDatabase is recovered and doesn't crash the process.
func TestManager_InitFromConfig_Panic_OpenDatabase(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		panic("OpenDatabase panic: nil pointer")
	}

	// InitFromConfig should not panic — defer recovers, calls failInit, re-panics.
	// The test calls InitFromConfigAsync so the outer recover catches the re-panic.
	var failureCalled atomic.Bool
	m.InitFromConfigAsync(context.Background(), func() {
		failureCalled.Store(true)
	})

	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	assert.Equal(t, status.Error, m.Status(), "status must be Error after OpenDatabase panic")
	assert.Contains(t, m.InitError(), "panic")
	assert.True(t, failureCalled.Load(), "onFailure must be called after panic recovery")
}

// TestManager_InitAfterOnboardingAsync_Panic_RecoversAndFails verifies
// that a panic inside factory.Init during async onboarding is recovered.
func TestManager_InitAfterOnboardingAsync_Panic_RecoversAndFails(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	reopened := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return reopened, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		panic("factory.Init panic: nil pointer")
	}

	var failureCalled atomic.Bool
	m.InitAfterOnboardingAsync(context.Background(), nil, func() {
		failureCalled.Store(true)
	})

	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	assert.Equal(t, status.Error, m.Status(), "status must be Error after panic recovery")
	assert.Contains(t, m.InitError(), "panic")
	assert.True(t, failureCalled.Load(), "onFailure must be called after panic recovery")
	assert.Equal(t, int32(1), reopened.closeCount.Load(),
		"reopened store must be closed once by panic recovery")
}

// TestManager_InitFromConfigAsync_Panic_InInitCore_FailInitCalledOnce
// verifies that a panic inside initCore (e.g. factory.Init) triggers
// failInit exactly once — not twice — despite both initCore's defer and
// initFromConfigLocked's defer catching the re-panic.
// Regression for Kody finding: double failInit on the panic path.
func TestManager_InitFromConfigAsync_Panic_InInitCore_FailInitCalledOnce(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		panic("factory.Init panic: nil pointer")
	}

	var failureCount atomic.Int32
	m.InitFromConfigAsync(context.Background(), func() {
		failureCount.Add(1)
	})

	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	assert.Equal(t, status.Error, m.Status(), "status must be Error after panic")
	assert.Contains(t, m.InitError(), "panic")
	assert.Equal(t, int32(1), failureCount.Load(),
		"onFailure must be called exactly once — not twice — on initCore panic")
	assert.Equal(t, int32(1), stubStore.closeCount.Load(),
		"store must be closed exactly once by initCore's panic recovery")
}

// TestManager_runInitAttempt_ReleasesLockOnPanic verifies that restartMu
// is released via defer even when the init attempt panics and re-panics, so
// a subsequent init/restart never deadlocks.
// Regression for Kody finding: runAsyncInit's explicit unlock was skipped on
// the panic path, leaving restartMu permanently locked.
func TestManager_runInitAttempt_ReleasesLockOnPanic(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	// First attempt panics; runInitAttempt must re-panic but release the lock.
	assert.Panics(t, func() {
		_ = m.runInitAttempt(context.Background(), func(ctx context.Context) error {
			panic("init panic")
		})
	})

	// A subsequent attempt must acquire the lock promptly (i.e. not deadlock).
	require.Eventually(t, func() bool {
		acquired := make(chan struct{})
		go func() {
			defer close(acquired)
			_ = m.runInitAttempt(context.Background(), func(ctx context.Context) error {
				return nil
			})
		}()
		select {
		case <-acquired:
			return true
		case <-time.After(time.Second):
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)
}

// TestManager_Cleanup_DuringAsyncInit verifies that Cleanup waits for
// an in-flight async init goroutine to finish.
func TestManager_Cleanup_DuringAsyncInit(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	gate := make(chan struct{})
	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		<-gate
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	m.InitFromConfigAsync(context.Background(), nil)

	// Give goroutine time to acquire restartMu
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		m.Cleanup()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Cleanup completed before async init finished")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}

	close(gate)

	select {
	case <-done:
		// Expected
	case <-time.After(2 * time.Second):
		t.Fatal("Cleanup did not complete after async init finished")
	}
}

// TestManager_NotifyStatusChange_NoDeadlock verifies that a status change
// callback that re-enters Manager (via Status()) does not deadlock.
func TestManager_NotifyStatusChange_NoDeadlock(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	var callbackStatus atomic.Value
	callbackStatus.Store("")
	m.SetOnStatusChange(func(statusStr, initErr string) {
		// Re-enter Manager — this would deadlock if mu was held
		callbackStatus.Store(m.Status().String())
	})

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	m.InitFromConfigAsync(context.Background(), nil)

	require.Eventually(t, func() bool {
		return m.Status() == status.Running
	}, 2*time.Second, 10*time.Millisecond)

	assert.Equal(t, "running", callbackStatus.Load(), "callback must have observed Running status")
}

// TestManager_RestartFailure_DoesNotReplayStaleOnFailure verifies that after a
// successful async init, a subsequent Restart failure does NOT re-invoke the
// stale onInitFailure callback (which would kick the user back to onboarding).
// Regression for Kody finding: onInitFailure was never cleared.
func TestManager_RestartFailure_DoesNotReplayStaleOnFailure(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	var failureCount atomic.Int32
	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	// Simulate successful async init with onFailure registered
	m.InitFromConfigAsync(context.Background(), func() {
		failureCount.Add(1)
	})

	require.Eventually(t, func() bool {
		return m.Status() == status.Running
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(0), failureCount.Load(), "onFailure must not fire on success")

	// Now make Restart fail — should NOT replay the stale onFailure
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return nil, nil, nil, errors.New("restart init failed")
	}

	err := m.Restart(context.Background())
	require.Error(t, err)
	assert.Equal(t, int32(0), failureCount.Load(),
		"stale onFailure must not replay on Restart failure after successful init")
}

// TestManager_InitFromConfigAsync_SelfHeals verifies that a transient
// indexer/connection failure is retried with backoff until it succeeds,
// without ever going terminal or firing onFailure — the fix for requiring a
// manual restart after the indexer connection drops at startup.
func TestManager_InitFromConfigAsync_SelfHeals(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }

	var attempts atomic.Int32
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		// Fail the first few attempts (indexer down), then recover.
		if attempts.Add(1) <= 2 {
			return nil, nil, nil, errors.New("failed to check app auth: connection refused")
		}
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	var onFailureCalled atomic.Bool
	// Use a millisecond-scale backoff so the test doesn't wait on the
	// production 5s base delay.
	m.retryDelay = func(attempt int) time.Duration { return 5 * time.Millisecond }
	m.InitFromConfigAsync(context.Background(), func() { onFailureCalled.Store(true) })

	require.Eventually(t, func() bool {
		return m.Status() == status.Running
	}, 2*time.Second, 10*time.Millisecond)

	assert.False(t, onFailureCalled.Load(), "onFailure must not fire when init self-heals")
	assert.GreaterOrEqual(t, attempts.Load(), int32(3), "init must have been retried")
	assert.Empty(t, m.InitError(), "initError must be cleared on success")
}

// TestManager_InitAfterOnboardingAsync_SelfHeals verifies that a transient
// factory failure during async onboarding is retried with backoff until it
// succeeds, and that each attempt REOPENS the database from its path rather
// than reusing a store closed by a previous failed attempt.
// Regression for Kody finding: the retry loop reused the same closed
// sqliteStore pointer, so "sql: database is closed" looped forever.
func TestManager_InitAfterOnboardingAsync_SelfHeals(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	// Each attempt must reopen the DB from path, so track the open count.
	var openCount atomic.Int32
	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		openCount.Add(1)
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}

	var attempts atomic.Int32
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		if attempts.Add(1) <= 2 {
			return nil, nil, nil, errors.New("failed to check app auth: connection refused")
		}
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	m.retryDelay = func(attempt int) time.Duration { return 5 * time.Millisecond }

	var onFailureCalled atomic.Bool
	m.InitAfterOnboardingAsync(context.Background(), nil, func() { onFailureCalled.Store(true) })

	require.Eventually(t, func() bool {
		return m.Status() == status.Running
	}, 2*time.Second, 10*time.Millisecond)

	assert.False(t, onFailureCalled.Load(), "onFailure must not fire when init self-heals")
	assert.GreaterOrEqual(t, attempts.Load(), int32(3), "init must have been retried")
	assert.GreaterOrEqual(t, openCount.Load(), int32(3),
		"database must be reopened on every attempt, not reusing a closed handle")
	assert.Empty(t, m.InitError(), "initError must be cleared on success")
}

// TestManager_InitFromConfigAsync_SelfHealsOnLockedDB verifies that a
// transient SQLite "database is locked" open failure is treated as retryable
// (not terminal) and self-heals once the lock clears.
// Regression for Kody finding: isTerminalInitError matched "failed to open
// database" and classified lock/busy (which factory.OpenDatabase prefixes with
// that string) as terminal, forcing a manual restart for a transient condition.
func TestManager_InitFromConfigAsync_SelfHealsOnLockedDB(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	var openAttempts atomic.Int32
	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		if openAttempts.Add(1) <= 2 {
			return nil, errors.New("failed to open database: database is locked")
		}
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	var onFailureCalled atomic.Bool
	m.retryDelay = func(attempt int) time.Duration { return 5 * time.Millisecond }
	m.InitFromConfigAsync(context.Background(), func() { onFailureCalled.Store(true) })

	require.Eventually(t, func() bool {
		return m.Status() == status.Running
	}, 2*time.Second, 10*time.Millisecond)

	assert.False(t, onFailureCalled.Load(), "onFailure must not fire; locked DB is transient")
	assert.GreaterOrEqual(t, openAttempts.Load(), int32(3), "open must have been retried")
	assert.Empty(t, m.InitError(), "initError must be cleared on success")
}

// TestManager_InitFromConfigAsync_StaleTerminal_DoesNotFireOnFailure verifies
// that a terminal failure surfaced by the async retry loop after a concurrent
// successful Restart already brought the backend to Running does NOT fire
// onFailure (which for onboarding wipes access keys and reverts onboarding).
// Regression for Kody finding: the terminal/budget branches lacked the
// stillStarting guard the panic branch has.
func TestManager_InitFromConfigAsync_StaleTerminal_DoesNotFireOnFailure(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }

	// First attempt fails transiently (indexer down); later attempts would
	// hit a terminal failure.
	var attempt atomic.Int32
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		if attempt.Add(1) == 1 {
			return nil, nil, nil, errors.New("failed to check app auth: connection refused")
		}
		return nil, nil, nil, errors.New("no access keys")
	}

	// Gate the first backoff sleep so we can interleave a concurrent success.
	enteredBackoff := make(chan struct{})
	releaseBackoff := make(chan struct{})
	var backoffUsed atomic.Bool
	m.retryDelay = func(attempt int) time.Duration {
		if backoffUsed.CompareAndSwap(false, true) {
			close(enteredBackoff)
			<-releaseBackoff
		}
		return time.Millisecond
	}

	var onFailureCalled atomic.Bool
	m.InitFromConfigAsync(context.Background(), func() { onFailureCalled.Store(true) })

	// Wait until the async loop has failed once and is parked in backoff.
	<-enteredBackoff

	// Simulate a concurrent successful Restart bringing the backend to Running
	// (swapBackend clears starting and sets the backend).
	m.swapBackend(&stubBackend{}, stubStore, func() {}, nil,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	require.Equal(t, status.Running, m.Status())

	// Release the retry loop; its next attempt hits a terminal error, but the
	// backend is already Running so onFailure must not fire.
	close(releaseBackoff)

	// Give the loop time to run its terminal attempt.
	time.Sleep(300 * time.Millisecond)
	assert.False(t, onFailureCalled.Load(),
		"onFailure must not fire after a concurrent successful restart")
}

// TestManager_InitFromConfigAsync_SupersededSuccess_DoesNotSwapBackend
// verifies that once a concurrent successful Restart brings the backend to
// Running while the retry loop is parked in backoff, the loop short-circuits
// at the top of its next iteration and stops without running a further
// initCore against the live store, leaving the concurrently-started backend
// untouched. Regression for Kody findings: the success path used to swap in a
// stale backend, and the loop used to keep churning full init attempts.
func TestManager_InitFromConfigAsync_SupersededSuccess_DoesNotSwapBackend(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }

	// Track full initCore attempts to confirm the loop short-circuits before
	// running another against the live store once the backend is Running.
	var attempt atomic.Int32
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		if attempt.Add(1) == 1 {
			return nil, nil, nil, errors.New("failed to check app auth: connection refused")
		}
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	// Gate the first backoff sleep so we can interleave a concurrent success.
	enteredBackoff := make(chan struct{})
	releaseBackoff := make(chan struct{})
	var backoffUsed atomic.Bool
	m.retryDelay = func(attempt int) time.Duration {
		if backoffUsed.CompareAndSwap(false, true) {
			close(enteredBackoff)
			<-releaseBackoff
		}
		return time.Millisecond
	}

	var onFailureCalled atomic.Bool
	m.InitFromConfigAsync(context.Background(), func() { onFailureCalled.Store(true) })

	// Wait until the async loop has failed once and is parked in backoff.
	<-enteredBackoff
	require.Equal(t, int32(1), attempt.Load(), "exactly one failed init attempt so far")

	// Concurrently bring the backend to Running.
	concurrentBackend := &stubBackend{}
	m.swapBackend(concurrentBackend, stubStore, func() {}, nil,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	require.Equal(t, status.Running, m.Status())

	// Release the retry loop. Its next iteration must short-circuit at the top
	// (backend already Running) and stop without running a full initCore.
	close(releaseBackoff)

	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	// Give the loop time to run (should be a no-op).
	time.Sleep(300 * time.Millisecond)
	assert.Same(t, concurrentBackend, m.Backend(),
		"retry must not tear down and replace the concurrently-started backend")
	assert.False(t, onFailureCalled.Load(), "onFailure must not fire")
	// The loop must stop without running another full init against the live store.
	assert.Equal(t, int32(1), attempt.Load(),
		"retry loop must short-circuit instead of running further initCore attempts")
}

// TestManager_closeResources_RunsCleanupAndClosesAccountClient verifies that
// closeResources releases both the init cleanup func and the account client,
// which the superseded branch and teardown rely on to avoid leaking the SDK
// connection and backend resources.
func TestManager_closeResources_RunsCleanupAndClosesAccountClient(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	var cleanupCalled atomic.Bool
	cleanup := func() { cleanupCalled.Store(true) }
	accountCli := &stubAccountClient{}

	m.closeResources(cleanup, accountCli)

	assert.True(t, cleanupCalled.Load(), "cleanup must be called")
	assert.True(t, accountCli.Closed(), "account client must be closed")

	// nil cleanup and nil account client must not panic.
	m.closeResources(nil, nil)
}

// TestManager_recordInitFailure verifies the guard used by both sync and async
// init: it fires onFailure only when no live backend exists and no concurrent
// Restart is in progress. Once a concurrent Restart has brought the backend to
// Running, it is a no-op. When a Restart is in flight, it is suppressed.
// Regression for Kody finding: the non-atomic check-then-act guard could fire
// onFailure (wiping onboarding access keys) after a live backend was started.
func TestManager_recordInitFailure(t *testing.T) {
	t.Run("no backend fires onFailure", func(t *testing.T) {
		m, _, _, _ := newTestManager(t)
		// Simulate the retry loop having set starting.
		m.mu.Lock()
		m.starting = true
		m.mu.Unlock()

		var onFailureCalled atomic.Bool
		m.recordInitFailure("boom", func() { onFailureCalled.Store(true) })

		assert.Equal(t, status.Error, m.Status())
		assert.True(t, onFailureCalled.Load(), "onFailure must fire when no backend exists")
	})

	t.Run("already started is no-op", func(t *testing.T) {
		m, _, _, _ := newTestManager(t)
		// A concurrent Restart already brought the backend to Running.
		live := &stubBackend{}
		m.mu.Lock()
		m.starting = false
		m.backend = live
		m.mu.Unlock()

		var onFailureCalled atomic.Bool
		m.recordInitFailure("boom", func() { onFailureCalled.Store(true) })

		assert.Equal(t, status.Running, m.Status(),
			"init error must not be recorded once the backend is Running")
		assert.False(t, onFailureCalled.Load(), "onFailure must not fire after a successful restart")
		assert.Same(t, live, m.Backend(), "live backend must be preserved")
	})

	t.Run("restart in progress is no-op", func(t *testing.T) {
		m, _, _, _ := newTestManager(t)
		m.mu.Lock()
		m.starting = true
		m.restarting = true
		m.mu.Unlock()

		var onFailureCalled atomic.Bool
		m.recordInitFailure("boom", func() { onFailureCalled.Store(true) })

		assert.False(t, onFailureCalled.Load(),
			"onFailure must never fire while a concurrent Restart is in progress")
		assert.True(t, m.starting, "init state must be left untouched for the in-flight restart")
	})

	t.Run("failed restart does not suppress onFailure", func(t *testing.T) {
		m, _, _, _ := newTestManager(t)
		// Simulate a failed concurrent Restart: starting was cleared,
		// no backend exists, status is Error.
		m.mu.Lock()
		m.starting = false
		m.initError = "restart failed"
		m.mu.Unlock()

		var onFailureCalled atomic.Bool
		m.recordInitFailure("terminal init failure", func() { onFailureCalled.Store(true) })

		assert.True(t, onFailureCalled.Load(),
			"onFailure must fire even if starting was cleared by a failed Restart")
		assert.Contains(t, m.InitError(), "terminal init failure")
	})
}

// TestManager_recordInitFailure_HoldsLockAcrossOnFailure verifies that the
// RLock is held across the onFailure callback, so a concurrent Restart cannot
// bring the backend to Running in the window between the check and the
// destructive callback. Uses TryLock (write lock) inside onFailure: if the
// RLock is held (correct), TryLock returns false; if the RLock was released
// early (bug), TryLock returns true and the test fails.
// Regression for Kody findings 3827502641 / 3827502801: TOCTOU race.
func TestManager_recordInitFailure_HoldsLockAcrossOnFailure(t *testing.T) {
	m, _, _, _ := newTestManager(t)
	m.mu.Lock()
	m.starting = true
	m.mu.Unlock()

	var tryLockSucceeded atomic.Bool
	var onFailureCalled atomic.Bool

	m.recordInitFailure("boom", func() {
		onFailureCalled.Store(true)
		// Try to acquire a write lock. If the RLock is held (correct),
		// TryLock returns false. If the RLock was NOT held (bug),
		// TryLock returns true.
		tryLockSucceeded.Store(m.mu.TryLock())
		if tryLockSucceeded.Load() {
			m.mu.Unlock()
		}
	})

	assert.True(t, onFailureCalled.Load(), "onFailure must be called")
	assert.False(t, tryLockSucceeded.Load(),
		"RLock must be held during onFailure — TryLock must fail")
}

// TestManager_InitFromConfigAsync_AfterFailedRestart_SwapsInBackend verifies
// that a successful retry is still swapped in after a concurrent Restart
// FAILED (clearing starting but leaving no backend). The supersede guard must
// only trigger when an actual backend is Running, otherwise self-heal is
// silently broken.
// Regression for Kody finding: the guard on !stillStarting alone discarded the
// successful init after a failed restart, leaving status=Error with no backend.
func TestManager_InitFromConfigAsync_AfterFailedRestart_SwapsInBackend(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }

	var attempt atomic.Int32
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		if attempt.Add(1) == 1 {
			return nil, nil, nil, errors.New("failed to check app auth: connection refused")
		}
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	enteredBackoff := make(chan struct{})
	releaseBackoff := make(chan struct{})
	var backoffUsed atomic.Bool
	m.retryDelay = func(attempt int) time.Duration {
		if backoffUsed.CompareAndSwap(false, true) {
			close(enteredBackoff)
			<-releaseBackoff
		}
		return time.Millisecond
	}

	var onFailureCalled atomic.Bool
	m.InitFromConfigAsync(context.Background(), func() { onFailureCalled.Store(true) })

	<-enteredBackoff

	// Simulate a concurrent Restart that FAILED: clears starting, no backend,
	// status=Error.
	m.recordInitFailure("restart failed", nil)
	require.Equal(t, status.Error, m.Status())
	require.Nil(t, m.Backend())

	// Subsequent retry attempts succeed; they must be swapped in.
	retryBackend := &stubBackend{}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return retryBackend, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}
	close(releaseBackoff)

	require.Eventually(t, func() bool {
		return m.Status() == status.Running
	}, 2*time.Second, 10*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	assert.Same(t, retryBackend, m.Backend(),
		"successful retry must be swapped in after a failed concurrent restart")
	assert.False(t, onFailureCalled.Load(), "onFailure must not fire")
}

// TestManager_InitFromConfigAsync_CancelAfterConcurrentRestart_KeepsRunning
// verifies that cancelling initCtx (shutdown) after a concurrent successful
// Restart already brought the backend to Running does not tear it down or mark
// it Error. Regression for Kody finding: the context-cancellation branches in
// the retry loop called failInit unconditionally, setting status=Error on a
// live Running backend.
func TestManager_InitFromConfigAsync_CancelAfterConcurrentRestart_KeepsRunning(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }

	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return nil, nil, nil, errors.New("failed to check app auth: connection refused")
	}

	enteredBackoff := make(chan struct{})
	releaseBackoff := make(chan struct{})
	var backoffUsed atomic.Bool
	m.retryDelay = func(attempt int) time.Duration {
		if backoffUsed.CompareAndSwap(false, true) {
			close(enteredBackoff)
			<-releaseBackoff
		}
		return time.Millisecond
	}

	var onFailureCalled atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.InitFromConfigAsync(ctx, func() { onFailureCalled.Store(true) })

	<-enteredBackoff

	// Concurrent Restart brings the backend to Running.
	concurrentBackend := &stubBackend{}
	m.swapBackend(concurrentBackend, stubStore, func() {}, nil,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	require.Equal(t, status.Running, m.Status())

	// Shutdown cancels initCtx while the loop is parked in backoff.
	cancel()
	close(releaseBackoff)

	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)
	time.Sleep(300 * time.Millisecond)

	assert.Equal(t, status.Running, m.Status(),
		"shutdown must not tear down a concurrently-started live backend")
	assert.Same(t, concurrentBackend, m.Backend(), "live backend must be preserved")
	assert.False(t, onFailureCalled.Load(), "onFailure must not fire on shutdown")
}

// TestManager_isTerminalInitError verifies terminal vs transient classification.
func TestManager_isTerminalInitError(t *testing.T) {
	terminal := []string{
		"no app key set",
		"no access keys",
		"no such file or directory",
		"permission denied",
		"no indexer URL configured",
		"backend init cancelled",
		"unexpected sqlite store type",
		"failed to create sia backend: some permanent failure",
		"failed to create SDK client: missing app key",
	}
	transient := []string{
		"failed to open database: database is locked",
		"failed to open database: database is busy",
		"failed to open database: file is temporarily unavailable",
		"failed to check app auth: connection refused",
		"dial tcp 1.2.3.4:5984: connect: connection refused",
		"indexer request timed out",
		"connection reset by peer",
		"lookup indexer.lumeweb.com: no such host",
		"read tcp 1.2.3.4:5984: i/o timeout",
		"unexpected EOF",
	}
	for _, msg := range terminal {
		assert.True(t, isTerminalInitError(errors.New(msg)), "expected terminal: %q", msg)
	}
	for _, msg := range transient {
		assert.False(t, isTerminalInitError(errors.New(msg)), "expected transient: %q", msg)
	}
	// A network failure during SDK client creation is wrapped with %w
	// preserving the underlying net.Error, so it must be treated as transient
	// even though "failed to create SDK client" is a terminal substring.
	netErrDuringSDK := fmt.Errorf("failed to create SDK client: %w",
		&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")})
	assert.False(t, isTerminalInitError(netErrDuringSDK), "wrapped net.Error during SDK client creation must be transient")
	assert.False(t, isTerminalInitError(nil), "nil must not be terminal")
}

// TestManager_InitFromConfigAsync_MaxRetriesReached_FailsTerminally verifies
// that a persistent non-terminal init failure is bounded: after
// maxInitRetries attempts the async init stops retrying, surfaces the error
// via failInit (setting status to error) and fires onFailure — it must not
// retry forever.
// Regression for Kody finding: permanent failures not matched by
// isTerminalInitError left the backend stuck on 'starting' forever and never
// fired onFailure (e.g. the onboarding wizard was never re-triggered).
func TestManager_InitFromConfigAsync_MaxRetriesReached_FailsTerminally(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}
	// Persistent, but NOT terminal by isTerminalInitError's substring rules —
	// a non-terminal error forces the loop to retry to the maxInitRetries
	// budget and exercise the budget-exhaustion branch (a terminal error like
	// "permission denied" would exit on the first attempt instead).
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return nil, nil, nil, errors.New("failed to check app auth: connection refused")
	}

	var attempts atomic.Int32
	origInit := factory.initFn
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		attempts.Add(1)
		return origInit(ctx, cfg, store)
	}

	m.retryDelay = func(attempt int) time.Duration { return time.Millisecond }

	var onFailureCalled atomic.Bool
	m.InitFromConfigAsync(context.Background(), func() { onFailureCalled.Store(true) })

	// The loop must eventually stop retrying and go terminal.
	require.Eventually(t, func() bool {
		return m.Status() == status.Error
	}, 5*time.Second, 10*time.Millisecond)

	assert.True(t, onFailureCalled.Load(), "onFailure must fire after the retry budget is exhausted")
	assert.LessOrEqual(t, attempts.Load(), int32(maxInitRetries),
		"init must not exceed the retry budget")
	assert.NotEmpty(t, m.InitError())
}

// TestManager_InitFromConfigAsync_RetryBackoffBounds verifies the backoff
// schedule grows from the base and never exceeds the cap.
func TestManager_InitFromConfigAsync_RetryBackoffBounds(t *testing.T) {
	assert.Equal(t, initRetryBaseDelay, retryBackoff(0))
	assert.Equal(t, initRetryBaseDelay*2, retryBackoff(1))
	assert.LessOrEqual(t, retryBackoff(100), initRetryMaxDelay, "backoff must be capped")
	assert.Equal(t, initRetryMaxDelay, retryBackoff(100), "long-running retries should sit at the cap")
}

// TestManager_InitFromConfigAsync_ReadsConfigInsideLock verifies that
// s3Cfg is read inside the doInit closure (after restartMu is acquired),
// not captured synchronously before the goroutine starts.
// Regression for Kody finding #3602140134: stale config if config
// changes between the sync call and goroutine execution.
func TestManager_InitFromConfigAsync_ReadsConfigInsideLock(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	var cfgSeenByInit atomic.Value
	cfgSeenByInit.Store(config.S3Config{})

	// blockOpen blocks until released, so we can change the config
	// before the goroutine reads it.
	blockOpen := make(chan struct{})
	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		<-blockOpen
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		cfgSeenByInit.Store(cfg)
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	m.InitFromConfigAsync(context.Background(), nil)

	// Change config while the goroutine is blocked. Use the store's
	// SetS3Config method to avoid a data race on the struct field.
	require.NoError(t, st.SetS3Config(config.S3Config{Directory: "/tmp/s3d-v2"}))
	close(blockOpen)

	require.Eventually(t, func() bool {
		return m.Status() == status.Running
	}, 2*time.Second, 10*time.Millisecond)

	seen := cfgSeenByInit.Load().(config.S3Config)
	assert.Equal(t, "/tmp/s3d-v2", seen.Directory,
		"s3Cfg must be read inside the closure after restartMu, not captured synchronously")
}

// TestManager_WithStore_ContextCancel_DoesNotFireOnFailure verifies that
// when the context is cancelled before init starts, failInit is called
// with nil (not onFailure). This prevents a shutdown from resetting
// onboarding state via ResetToAppKeySet.
// Regression for Kody finding #3602140424: ctx-cancel path fired onFailure,
// resetting onboarding from complete to app_key_set on server shutdown.
func TestManager_WithStore_ContextCancel_DoesNotFireOnFailure(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	var onFailureCalled atomic.Bool
	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		return &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}, nil
	}
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		t.Fatal("factory.Init should not be called when context is cancelled")
		return nil, nil, nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m.InitAfterOnboardingAsync(ctx, nil, func() {
		onFailureCalled.Store(true)
	})

	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	assert.False(t, onFailureCalled.Load(),
		"onFailure must NOT fire on context cancellation — shutdown is not a genuine init failure")
	assert.Equal(t, status.Error, m.Status(),
		"status must still be Error (failInit was called)")
}

// TestManager_InitFromConfigAsync_ColdStartFailure_PreservesOnboardingState
// verifies that the cold-start onFailure callback does NOT call
// ResetToAppKeySet — it only logs. This prevents wiping valid production
// credentials on a transient backend init failure.
// Regression for Kody finding #3602355191: cold-start failure wiped all
// access keys and users, leaving S3 clients permanently unable to authenticate.
func TestManager_InitFromConfigAsync_ColdStartFailure_PreservesOnboardingState(t *testing.T) {
	m, st, factory, _ := newTestManager(t)
	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return nil, nil, nil, errors.New("failed to validate indexer: connection refused")
	}

	var onFailureCalled atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.InitFromConfigAsync(ctx, func() {
		// In main.go, the cold-start onFailure only logs — it does NOT
		// call ResetToAppKeySet.
		onFailureCalled.Store(true)
	})

	// A transient indexer failure is retried (not terminal): status stays
	// starting and onFailure is not fired, so nothing can reset onboarding.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, status.Starting, m.Status(),
		"status must stay Starting while a transient failure is retried")
	assert.False(t, onFailureCalled.Load(),
		"onFailure must not fire for a retryable cold-start failure")

	// Stop the retry loop via context cancellation.
	cancel()
	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	// The key assertion: access keys are still in the store (not wiped),
	// and onFailure still never fired.
	keys, err := stubStore.ListAccessKeys(nil)
	require.NoError(t, err)
	assert.Len(t, keys, 1, "production access keys must not be wiped on cold-start failure")
	assert.False(t, onFailureCalled.Load(),
		"onFailure must not fire on context cancellation")
}
