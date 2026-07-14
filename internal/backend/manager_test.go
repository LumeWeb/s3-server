package backend

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SiaFoundation/s3d/s3"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/status"
	"go.lumeweb.com/s3-server/internal/testutil"
	"go.sia.tech/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testUser = "test-user"

// testSecretKey is a non-sensitive placeholder for test fixtures.
const testSecretKey = "test-secret-key-do-not-use-in-prod"

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
	mu          sync.Mutex
	closeErr    error
	accessKeys  []AccessKeyInfo
	appKeySet   bool
	indexerURL  string
	privateKey  types.PrivateKey
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
func (s *stubS3DStore) CreateUser(name string) error   { return nil }
func (s *stubS3DStore) DeleteUser(name string) error   { return nil }
func (s *stubS3DStore) ListUsers() ([]string, error)   { return nil, nil }
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
func (s *stubS3DStore) Close() error { return s.closeErr }

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
func (b *stubBackend) FlushObjects(ctx context.Context) error { return nil }
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
func (b *stubBackend) DeleteBucketLifecycleConfiguration(ctx context.Context, accessKeyID, bucket string) error { return nil }
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
	cfg   config.PanelConfig
	s3Cfg config.S3Config
}

func (s *stubStore) Config() config.PanelConfig                   { return s.cfg }
func (s *stubStore) DataDir() string                              { return "" }
func (s *stubStore) ResetTokenPath() string                       { return "" }
func (s *stubStore) S3Config() config.S3Config                    { return s.s3Cfg }
func (s *stubStore) SetAdminPassword(string) error                { return nil }
func (s *stubStore) ClearAdminPassword() error                   { return nil }
func (s *stubStore) ValidateAdminPassword(string) bool            { return false }
func (s *stubStore) OnboardingState() string                      { return "" }
func (s *stubStore) SetOnboardingState(string) error              { return nil }
func (s *stubStore) SSLConfig() config.SSLConfig                  { return config.SSLConfig{} }
func (s *stubStore) SetSSLConfig(config.SSLConfig) error          { return nil }
func (s *stubStore) SetS3Config(config.S3Config) error            { return nil }
func (s *stubStore) LogConfig() config.LogConfig                  { return config.LogConfig{Level: "info", Format: "json"} }
func (s *stubStore) SetLogConfig(config.LogConfig) error          { return nil }
func (s *stubStore) CreateSession() (string, error)               { return "stub-session", nil }
func (s *stubStore) ValidateSession(string) bool                  { return true }
func (s *stubStore) DeleteSession(string)                         {}
func (s *stubStore) StartSessionCleanup(<-chan struct{})          {}
func (s *stubStore) AccessKeys() []config.KeyPair                 { return nil }
func (s *stubStore) SetAccessKeys(keys []config.KeyPair) error   { return nil }

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

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey, UserName: testUser}}}
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
	assert.Contains(t, err.Error(), "failed to open database")
}

func TestManager_InitFromConfig_InitError(t *testing.T) {
	m, st, factory, _ := newTestManager(t)

	st.s3Cfg = config.S3Config{Directory: "/tmp/s3d"}

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey, UserName: testUser}}}
	factory.openDBFn = func(dbPath string) (S3DStore, error) { return stubStore, nil }
	factory.initFn = func(ctx context.Context, cfg config.S3Config, store S3DStore) (Backend, http.Handler, func(), error) {
		return nil, nil, nil, assert.AnError
	}

	err := m.InitFromConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to init s3d backend")
}

func TestManager_InitAfterOnboarding(t *testing.T) {
	m, st, factory, swapper := newTestManager(t)

	s3Cfg := config.S3Config{Directory: "/tmp/s3d", IndexerURL: "https://sia.storage"}
	st.s3Cfg = s3Cfg

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey, UserName: testUser}}}
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

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey, UserName: testUser}}}
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

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey, UserName: testUser}}}
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
	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey, UserName: testUser}}}
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
	stubStore.accessKeys = append(stubStore.accessKeys, AccessKeyInfo{AccessKeyID: "AKIA456", SecretKey: testSecretKey, UserName: testUser})
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
		return &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey, UserName: testUser}}}, nil
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
	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey, UserName: testUser}}}
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
	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey, UserName: testUser}}}
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

	stubStore := &stubS3DStore{accessKeys: []AccessKeyInfo{{AccessKeyID: "AKIA123", SecretKey: testSecretKey, UserName: testUser}}}
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
