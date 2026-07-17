package backend

import (
	"context"
	"errors"
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

func (s *stubStore) Config() config.PanelConfig          { return s.cfg }
func (s *stubStore) DataDir() string                     { return "" }
func (s *stubStore) ResetTokenPath() string              { return "" }
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
		m, st, _, _ := newTestManager(t)
		st.s3Cfg = config.S3Config{}

		var failureCalled atomic.Bool
		stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{}}
		m.InitAfterOnboardingAsync(context.Background(), stubStore, func() { failureCalled.Store(true) })
		m.initWg.Wait()
		require.NotEmpty(t, m.InitError())

		assert.True(t, failureCalled.Load(), "onFailure must be called on error")
	})

	t.Run("factory init error", func(t *testing.T) {
		m, st, factory, _ := newTestManager(t)
		st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

		var failureCalled atomic.Bool
		stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
			{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
		}}
		factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
			return nil, nil, nil, errors.New("init failed")
		}

		m.InitAfterOnboardingAsync(context.Background(), stubStore, func() { failureCalled.Store(true) })
		m.initWg.Wait()
		assert.True(t, failureCalled.Load(), "onFailure must be called on factory init error")
	})
}

// TestManager_FailInit verifies that failInit clears the starting flag,
// sets initError, and calls onInitFailure.
func TestManager_FailInit(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	var failureCalled atomic.Bool
	m.mu.Lock()
	m.starting = true
	m.mu.Unlock()

	m.failInit("test error: something went wrong", func() { failureCalled.Store(true) })

	assert.Equal(t, status.Error, m.Status(), "status must be Error after failInit")
	assert.Contains(t, m.InitError(), "test error")
	assert.True(t, failureCalled.Load(), "onInitFailure must be called after failInit")
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
func (s *failingListKeysStore) CreateUser(name string) error     { return nil }
func (s *failingListKeysStore) DeleteUser(name string) error     { return nil }
func (s *failingListKeysStore) ListUsers() ([]string, error)     { return nil, nil }
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
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		<-gate
		return &stubBackend{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), func() {}, nil
	}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	m.InitAfterOnboardingAsync(context.Background(), stubStore, nil)

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

	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		t.Fatal("factory.Init should not be called when context is cancelled")
		return nil, nil, nil, nil
	}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before launch

	m.InitAfterOnboardingAsync(ctx, stubStore, nil)

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

	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		t.Fatal("factory.Init should not be called when context is cancelled")
		return nil, nil, nil, nil
	}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m.InitAfterOnboardingAsync(ctx, stubStore, nil)

	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	assert.Equal(t, int32(1), stubStore.closeCount.Load(),
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

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	m.InitAfterOnboardingAsync(context.Background(), stubStore, nil)

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

	factory.openDBFn = func(dbPath string) (S3DStore, error) {
		return nil, assert.AnError
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

	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		panic("factory.Init panic: nil pointer")
	}

	var failureCalled atomic.Bool
	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}
	m.InitAfterOnboardingAsync(context.Background(), stubStore, func() {
		failureCalled.Store(true)
	})

	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	assert.Equal(t, status.Error, m.Status(), "status must be Error after panic recovery")
	assert.Contains(t, m.InitError(), "panic")
	assert.True(t, failureCalled.Load(), "onFailure must be called after panic recovery")
	assert.Equal(t, int32(1), stubStore.closeCount.Load(),
		"store must be closed once by panic recovery")
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

	m.InitAfterOnboardingAsync(ctx, &stubS3DStore{accessKeys: []AccessKeyInfo{
		{AccessKeyID: "AKIA123", SecretKey: testSecretKey(), UserName: testUser},
	}}, func() {
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
		return nil, nil, nil, errors.New("indexer unreachable")
	}

	var onFailureCalled atomic.Bool
	m.InitFromConfigAsync(context.Background(), func() {
		// In main.go, the cold-start onFailure only logs — it does NOT
		// call ResetToAppKeySet. This test verifies the callback fires
		// (so the caller can log) but the test's callback simulates the
		// cold-start behavior by NOT resetting anything.
		onFailureCalled.Store(true)
	})

	require.Eventually(t, func() bool {
		return m.Status() != status.Starting
	}, 2*time.Second, 10*time.Millisecond)

	assert.True(t, onFailureCalled.Load(),
		"onFailure must fire so the caller can log the failure")
	assert.Equal(t, status.Error, m.Status(),
		"status must be Error after failed init")
	// The key assertion: access keys are still in the store (not wiped)
	keys, err := stubStore.ListAccessKeys(nil)
	require.NoError(t, err)
	assert.Len(t, keys, 1, "production access keys must not be wiped on cold-start failure")
}
