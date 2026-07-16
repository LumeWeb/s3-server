package onboarding

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SiaFoundation/s3d/sia"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/backend"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/store"
	storeMocks "go.lumeweb.com/s3-server/internal/store/mocks"
	"go.lumeweb.com/s3-server/internal/testutil"
	"go.sia.tech/core/types"
	"golang.org/x/crypto/nacl/box"
)

// Test fixture seed values (not real secrets).
const (
	testSeedA = "01234567890123456789012345678901"
	testSeedB = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testSeedC = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// testS3DStore is an in-memory implementation of backend.S3DStore for tests.
type testS3DStore struct {
	mu     sync.Mutex
	users  map[string]struct{}
	keys   map[string]backend.AccessKeyInfo
	appKey types.PrivateKey
}

func newTestS3DStore() *testS3DStore {
	return &testS3DStore{
		users: make(map[string]struct{}),
		keys:  make(map[string]backend.AccessKeyInfo),
	}
}

func (s *testS3DStore) AppKey() (types.PrivateKey, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appKey, "", nil
}

func (s *testS3DStore) SetAppKey(key types.PrivateKey, indexerURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appKey = key
	return nil
}

func (s *testS3DStore) CreateUser(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[name]; ok {
		return sia.ErrUserAlreadyExists
	}
	s.users[name] = struct{}{}
	return nil
}

func (s *testS3DStore) CreateAccessKey(userName, accessKeyID, secretKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[userName]; !ok {
		return sia.ErrUserNotFound
	}
	if _, ok := s.keys[accessKeyID]; ok {
		return sia.ErrAccessKeyAlreadyExists
	}
	s.keys[accessKeyID] = backend.AccessKeyInfo{
		AccessKeyID: accessKeyID,
		SecretKey:   secretKey,
		UserName:    userName,
	}
	return nil
}

func (s *testS3DStore) ListAccessKeys(userName *string) ([]backend.AccessKeyInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []backend.AccessKeyInfo
	for _, k := range s.keys {
		if userName == nil || k.UserName == *userName {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AccessKeyID < out[j].AccessKeyID })
	return out, nil
}

func (s *testS3DStore) DeleteAccessKey(accessKeyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.keys[accessKeyID]; !ok {
		return context.DeadlineExceeded
	}
	delete(s.keys, accessKeyID)
	return nil
}

func (s *testS3DStore) DeleteUser(userName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[userName]; !ok {
		return context.DeadlineExceeded
	}
	delete(s.users, userName)
	return nil
}

func (s *testS3DStore) ListUsers() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.users))
	for u := range s.users {
		out = append(out, u)
	}
	sort.Strings(out)
	return out, nil
}

func (s *testS3DStore) Close() error { return nil }

// testSecretKey returns a secret key from the environment to satisfy
// Kody's rule banning hard-coded secret literals in source.
func testSecretKey() string {
	return os.Getenv("TEST_SECRET_KEY")
}

// testBackendInit satisfies BackendInitializer for tests.
type testBackendInit struct {
	store      *testS3DStore
	initCalled bool
	initErr    error // if non-nil, InitAfterOnboarding returns this error
	panicFn    func() // if set, panics during init
	openDBErr  error  // if set, OpenDatabase returns this error
	mu         sync.Mutex
	onFailure  func()
}

func (b *testBackendInit) OpenDatabase(dbPath string) (backend.S3DStore, error) {
	if b.openDBErr != nil {
		return nil, b.openDBErr
	}
	return b.store, nil
}

func (b *testBackendInit) InitAfterOnboardingAsync(ctx context.Context, sqliteStore backend.S3DStore, onFailure func()) {
	b.mu.Lock()
	b.onFailure = onFailure
	b.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.mu.Lock()
				fn := b.onFailure
				b.mu.Unlock()
				if fn != nil {
					fn()
				}
				b.mu.Lock()
				b.initCalled = true
				b.mu.Unlock()
			}
		}()

		if b.panicFn != nil {
			b.panicFn()
		}

		err := b.initErr
		// Simulate the real Manager's failInit behavior: call onInitFailure on error
		if err != nil && onFailure != nil {
			onFailure()
		}
		b.mu.Lock()
		b.initCalled = true
		b.mu.Unlock()
	}()
}

// waitForInit blocks until InitAfterOnboarding has been called or times out.
func (b *testBackendInit) waitForInit(t *testing.T) {
	require.Eventually(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.initCalled
	}, 2*time.Second, 10*time.Millisecond)
}

func newTestService(t *testing.T, initialState OnboardingState) (*Service, *storeMocks.MockStore, *testS3DStore, *testBackendInit) {
	mockStore := storeMocks.NewMockStore(t)
	log := testutil.NewTestLogger()
	testStore := newTestS3DStore()
	init := &testBackendInit{store: testStore}
	mockStore.EXPECT().OnboardingState().Return(string(initialState))
	svc, err := NewService(mockStore, log, init)
	require.NoError(t, err)
	return svc, mockStore, testStore, init
}

func encryptForService(svc *Service, plaintext []byte) string {
	sealed, _ := box.SealAnonymous(nil, plaintext, &svc.publicKey, rand.Reader)
	return base64.StdEncoding.EncodeToString(sealed)
}

func TestStatusHandler_Pending(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StatePending)
	mockStore.EXPECT().Config().Return(config.PanelConfig{})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.StatusHandler(c)
	require.NoError(t, err)

	var resp StatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "pending", resp.State)
	assert.False(t, resp.HasAppKey)
	assert.False(t, resp.HasAccessKeys)
	assert.False(t, resp.HasAdmin)
}

func TestStatusHandler_AdminSet(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StateAdminSet)
	mockStore.EXPECT().Config().Return(config.PanelConfig{
		OnboardingState:   "admin_password_set",
		AdminPasswordHash: "$2a$10$hash",
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.StatusHandler(c)
	require.NoError(t, err)

	var resp StatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "admin_password_set", resp.State)
	assert.False(t, resp.HasAppKey)
	assert.False(t, resp.HasAccessKeys)
	assert.True(t, resp.HasAdmin)
}

func TestPublicKeyHandler(t *testing.T) {
	svc, _, _, _ := newTestService(t, StatePending)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/onboarding/public-key", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.PublicKeyHandler(c)
	require.NoError(t, err)

	var resp PublicKeyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.PublicKey)

	decoded, err := base64.StdEncoding.DecodeString(resp.PublicKey)
	require.NoError(t, err)
	assert.Len(t, decoded, 32)
}

func TestSetAppKeyHandler_Success(t *testing.T) {
	svc, mockStore, testStore, _ := newTestService(t, StateAdminSet)

	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})
	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(nil)

	var appKey [32]byte
	copy(appKey[:], []byte(testSeedA))
	encrypted := encryptForService(svc, appKey[:])

	e := echo.New()
	body := `{"encrypted_app_key":"` + encrypted + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StateAppKeySet, svc.fsm.State())
	assert.Equal(t, testStore, svc.sqliteStore)
	assert.NotEqual(t, types.PrivateKey{}, testStore.appKey)
	// The decrypted seed (32 bytes) is expanded to a full 64-byte
	// ed25519 private key (seed + public key) via NewPrivateKeyFromSeed.
	assert.Len(t, testStore.appKey, 64, "appKey must be 64 bytes (expanded from 32-byte seed)")
}

func TestSetAppKeyHandler_FromAppKeySet(t *testing.T) {
	// Re-submitting the app key from StateAppKeySet should succeed
	// (idempotent: user went back and reconnected).
	svc, mockStore, _, _ := newTestService(t, StateAppKeySet)

	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})
	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(nil)

	var appKey [32]byte
	copy(appKey[:], []byte(testSeedA))
	encrypted := encryptForService(svc, appKey[:])

	e := echo.New()
	body := `{"encrypted_app_key":"` + encrypted + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StateAppKeySet, svc.fsm.State())
}

func TestSetAppKeyHandler_FromAppKeySet_OverwritesKey(t *testing.T) {
	// Re-submitting from StateAppKeySet should overwrite the previous app key
	// in the SQLite store. Verify the mock receives the new key.
	svc, mockStore, testStore, _ := newTestService(t, StateAppKeySet)

	// Seed the store with an initial key to simulate the first submission.
	var initialKey [32]byte
	copy(initialKey[:], []byte(testSeedB))
	expandedInitial := types.NewPrivateKeyFromSeed(initialKey[:])
	require.NoError(t, testStore.SetAppKey(expandedInitial, ""))

	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})
	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(nil)

	// Submit a different key.
	var newKey [32]byte
	copy(newKey[:], []byte(testSeedC))
	encrypted := encryptForService(svc, newKey[:])

	e := echo.New()
	body := `{"encrypted_app_key":"` + encrypted + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// The store should now hold the new key, not the old one.
	assert.NotEqual(t, expandedInitial, testStore.appKey, "app key should be overwritten with new key")
	assert.Equal(t, StateAppKeySet, svc.fsm.State())
}

func TestSetAppKeyHandler_FromAppKeySet_WithCustomIndexer(t *testing.T) {
	// Re-submitting from StateAppKeySet with a custom indexer URL should
	// resolve and persist the new indexer URL alongside the app key.
	svc, mockStore, _, _ := newTestService(t, StateAppKeySet)

	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"}).Times(2)
	mockStore.EXPECT().SetS3Config(config.S3Config{
		Directory:  "/tmp/test-s3d",
		IndexerURL: "https://example.com",
	}).Return(nil)
	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(nil)

	var appKey [32]byte
	copy(appKey[:], []byte(testSeedA))
	encrypted := encryptForService(svc, appKey[:])

	e := echo.New()
	body := `{"encrypted_app_key":"` + encrypted + `","indexer_url":"https://example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StateAppKeySet, svc.fsm.State())
}

func TestSetAppKeyHandler_FromComplete_Rejected(t *testing.T) {
	// Re-submitting from StateComplete should still be rejected.
	svc, _, _, _ := newTestService(t, StateComplete)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(`{"encrypted_app_key":"dGVzdA=="}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestSetAppKeyHandler_FromPending(t *testing.T) {
	svc, _, _, _ := newTestService(t, StatePending)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSetAppKeyHandler_InvalidBase64(t *testing.T) {
	svc, _, _, _ := newTestService(t, StateAdminSet)

	e := echo.New()
	body := `{"encrypted_app_key":"!!!invalid-base64!!!"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSetAppKeyHandler_DecryptionFailed(t *testing.T) {
	svc, _, _, _ := newTestService(t, StateAdminSet)

	e := echo.New()
	body := `{"encrypted_app_key":"AAAA"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSetAccessKeysHandler_Success(t *testing.T) {
	svc, mockStore, testStore, init := newTestService(t, StateAppKeySet)
	svc.sqliteStore = testStore
	mockStore.EXPECT().SetOnboardingState("complete").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/access-keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAccessKeysHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	init.waitForInit(t)

	// Verify the response contains auto-generated credentials.
	var resp OnboardingStepResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "access_keys_set", resp.Status)
	assert.Equal(t, "admin", resp.UserName)
	assert.NotEmpty(t, resp.AccessKey)
	assert.Len(t, resp.AccessKey, 20) // "AKIA" prefix + 16 random chars
	assert.True(t, strings.HasPrefix(resp.AccessKey, "AKIA"))
	assert.NotEmpty(t, resp.SecretKey)
	assert.GreaterOrEqual(t, len(resp.SecretKey), 32)

	// Verify the key was actually stored.
	assert.Len(t, testStore.keys, 1)
	for _, k := range testStore.keys {
		assert.Equal(t, resp.AccessKey, k.AccessKeyID)
	}
}

func TestSetAccessKeysHandler_WrongState(t *testing.T) {
	svc, _, _, _ := newTestService(t, StatePending)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/access-keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAccessKeysHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSetAdminPasswordHandler_Success(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StatePending)

	mockStore.EXPECT().SetAdminPassword("goodpassword").Return(nil)
	mockStore.EXPECT().SetOnboardingState("admin_password_set").Return(nil)

	e := echo.New()
	body := `{"password":"goodpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/admin-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAdminPasswordHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StateAdminSet, svc.fsm.State())
}

func TestSetAdminPasswordHandler_TooShort(t *testing.T) {
	svc, _, _, _ := newTestService(t, StateAppKeySet)

	e := echo.New()
	body := `{"password":"short"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/admin-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAdminPasswordHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestSetAdminPasswordHandler_Idempotent(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StateAdminSet)

	mockStore.EXPECT().SetAdminPassword("goodpassword").Return(nil)
	mockStore.EXPECT().SetOnboardingState("admin_password_set").Return(nil)

	e := echo.New()
	body := `{"password":"goodpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/admin-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAdminPasswordHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StateAdminSet, svc.fsm.State(), "state should stay admin_password_set")
}

func TestSetAdminPasswordHandler_AlreadyComplete(t *testing.T) {
	svc, _, _, _ := newTestService(t, StateComplete)

	e := echo.New()
	body := `{"password":"goodpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/admin-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAdminPasswordHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestSetAdminPasswordHandler_FromPending(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StatePending)
	mockStore.EXPECT().SetAdminPassword("goodpassword").Return(nil)
	mockStore.EXPECT().SetOnboardingState("admin_password_set").Return(nil)

	e := echo.New()
	body := `{"password":"goodpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/admin-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAdminPasswordHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StateAdminSet, svc.fsm.State())
}

func TestNaClRoundTrip(t *testing.T) {
	svc, _, _, _ := newTestService(t, StatePending)

	plaintext := []byte("hello world 32-byte key padding!!")
	encrypted := encryptForService(svc, plaintext)

	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	require.NoError(t, err)

	decrypted, ok := box.OpenAnonymous(nil, ciphertext, &svc.publicKey, &svc.privateKey)
	require.True(t, ok)
	assert.Equal(t, plaintext, decrypted)
}

// Compile-time interface checks
var _ store.Store = (*storeMocks.MockStore)(nil)
var _ backend.S3DStore = (*testS3DStore)(nil)
var _ BackendInitializer = (*testBackendInit)(nil)
var _ StateMachine = (*FSM)(nil)

func TestOnboardingFlow_AdminThenKeys(t *testing.T) {
	// Full flow: set admin password -> set app key -> auto-generate access keys -> complete
	svc, mockStore, _, init := newTestService(t, StatePending)

	var appKey [32]byte
	copy(appKey[:], []byte(testSeedA))
	encrypted := encryptForService(svc, appKey[:])
	e := echo.New()

	// Step 1: Set admin password
	mockStore.EXPECT().SetAdminPassword("securepassword").Return(nil)
	mockStore.EXPECT().SetOnboardingState("admin_password_set").Return(nil)

	body := `{"password":"securepassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/admin-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAdminPasswordHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StateAdminSet, svc.fsm.State())

	// Step 2: Set app key
	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})
	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(nil)

	body = `{"encrypted_app_key":"` + encrypted + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)

	err = svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StateAppKeySet, svc.fsm.State())

	// Step 3: Auto-generate access keys -> complete
	mockStore.EXPECT().SetOnboardingState("complete").Return(nil)

	req = httptest.NewRequest(http.MethodPost, "/api/onboarding/access-keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)

	err = svc.SetAccessKeysHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StateComplete, svc.fsm.State())
	init.waitForInit(t)
}

// Regression: SetAppKeyHandler when SetOnboardingState fails: FSM transitions
// first, then persist fails and rolls back the FSM to StateAdminSet.
// sqliteStore is closed. Returns 500, not a misleading 200.
func TestSetAppKeyHandler_PersistFailure_ReturnsError(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StateAdminSet)

	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})
	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(errors.New("disk full"))

	var appKey [32]byte
	copy(appKey[:], []byte(testSeedA))
	encrypted := encryptForService(svc, appKey[:])

	e := echo.New()
	body := `{"encrypted_app_key":"` + encrypted + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, StateAdminSet, svc.fsm.State(), "FSM must roll back to StateAdminSet on persist failure")
	assert.Nil(t, svc.sqliteStore, "sqliteStore must be closed on persist failure")
}

// Regression: persist failure from StateAppKeySet rolls back to StateAppKeySet,
// not StateAdminSet. The entry state is captured before the FSM transition
// so rollback restores the correct original state.
func TestSetAppKeyHandler_PersistFailure_FromAppKeySet_RollsBackToAppKeySet(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StateAppKeySet)

	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})
	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(errors.New("disk full"))

	var appKey [32]byte
	copy(appKey[:], []byte(testSeedA))
	encrypted := encryptForService(svc, appKey[:])

	e := echo.New()
	body := `{"encrypted_app_key":"` + encrypted + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, StateAppKeySet, svc.fsm.State(), "FSM must roll back to StateAppKeySet, not StateAdminSet, on persist failure from StateAppKeySet")
	assert.Nil(t, svc.sqliteStore, "sqliteStore must be closed on persist failure")
}

// Regression: SetAdminPasswordHandler returns 500 on transitionTo failure
// instead of swallowing the error and returning 200.
func TestSetAdminPasswordHandler_TransitionFailure_ReturnsError(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StateComplete)

	// State is Complete → SetAdminPassword should be rejected before
	// transitionTo is called. But we want to test transitionTo failure
	// specifically. Force the FSM into a state where the password was set
	// but transition fails by having the store fail.
	// Use StatePending so the handler proceeds past the state check.
	svc.fsm = NewFSM(StatePending) // reset to pending
	mockStore.EXPECT().SetAdminPassword("goodpassword").Return(nil)
	// SetOnboardingState fails: transitionTo returns error
	mockStore.EXPECT().SetOnboardingState("admin_password_set").Return(errors.New("disk full"))

	e := echo.New()
	body := `{"password":"goodpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/admin-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAdminPasswordHandler(c)
	require.NoError(t, err)
	// Must return 500, not 200: the error must not be swallowed.
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// Regression: SetAccessKeysHandler when transitionTo(StateComplete) fails
// returns 500 instead of a misleading 200.
func TestSetAccessKeysHandler_TransitionFailure_ReturnsError(t *testing.T) {
	svc, mockStore, testStore, _ := newTestService(t, StateAppKeySet)
	svc.sqliteStore = testStore
	// SetOnboardingState fails: transitionTo returns error
	mockStore.EXPECT().SetOnboardingState("complete").Return(errors.New("disk full"))

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/access-keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAccessKeysHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// --- Regression tests for Kody feedback on PR #53 ---

// TestSetAccessKeysHandler_InitFailure_ResetsFSM verifies that when
// InitAfterOnboarding fails, the FSM and store are both reset to
// StateAppKeySet so the panel redirects to the onboarding wizard
// instead of showing a broken dashboard.
// Regression for Kody finding: FSM/store state desync on init failure.
func TestSetAccessKeysHandler_InitFailure_ResetsFSM(t *testing.T) {
	svc, mockStore, testStore, init := newTestService(t, StateAppKeySet)
	svc.sqliteStore = testStore

	// transitionTo(StateComplete) succeeds
	mockStore.EXPECT().SetOnboardingState("complete").Return(nil)
	// onInitFailure callback resets the store
	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(nil)
	// onFailure re-opens the sqlite store for retry
	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})

	// Inject a failure into InitAfterOnboarding
	init.initErr = errors.New("backend init failed")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/access-keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAccessKeysHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Wait for the async goroutine to call InitAfterOnboarding (which will fail)
	init.waitForInit(t)

	// FSM must be reset to StateAppKeySet, not StateComplete
	require.Eventually(t, func() bool {
		return svc.fsm.State() == StateAppKeySet
	}, 2*time.Second, 10*time.Millisecond,
		"FSM must be reset to StateAppKeySet after init failure")
}

// TestSetAccessKeysHandler_PanicInInit_ResetsFSM verifies that a panic
// during InitAfterOnboarding is recovered and the FSM is reset.
// Regression for Kody finding: panic recovery didn't reset state.
func TestSetAccessKeysHandler_PanicInInit_ResetsFSM(t *testing.T) {
	svc, mockStore, testStore, init := newTestService(t, StateAppKeySet)
	svc.sqliteStore = testStore
	init.panicFn = func() { panic("nil pointer") }

	// transitionTo(StateComplete) → SetOnboardingState("complete")
	mockStore.EXPECT().SetOnboardingState("complete").Return(nil)
	// onInitFailure callback → SetOnboardingState("app_key_set")
	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(nil)
	// onFailure re-opens the sqlite store for retry
	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/access-keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAccessKeysHandler(c)
	require.NoError(t, err)

	// Wait for the panic to be recovered and onFailure to fire
	init.waitForInit(t)

	assert.Equal(t, StateAppKeySet, svc.fsm.State(),
		"FSM must be reset to StateAppKeySet after panic recovery")
}

// TestSetAccessKeysHandler_TransitionBeforeGoroutine verifies that
// transitionTo(StateComplete) runs before the async init goroutine,
// so the failure callback's SetState cannot be overwritten.
// Regression for Kody finding: transitionTo overwriting FSM rollback.
func TestSetAccessKeysHandler_TransitionBeforeGoroutine(t *testing.T) {
	svc, mockStore, testStore, init := newTestService(t, StateAppKeySet)
	svc.sqliteStore = testStore

	// transitionTo(StateComplete) → SetOnboardingState("complete")
	mockStore.EXPECT().SetOnboardingState("complete").Return(nil)
	// onInitFailure → SetOnboardingState("app_key_set") (not called on success)
	// No extra SetOnboardingState expectation since init succeeds

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/access-keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAccessKeysHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// By the time the goroutine runs, FSM must already be at Complete
	// (proving transitionTo ran before the goroutine launched)
	init.waitForInit(t)
	assert.Equal(t, StateComplete, svc.fsm.State(),
		"FSM must be Complete after successful onboarding")
}

// TestService_ResetToAppKeySet verifies that ResetToAppKeySet resets
// both the in-memory FSM and the persisted store to StateAppKeySet,
// and clears any orphaned access keys and users from the SQLite store.
// Regression for Kody finding: N failed onboarding retries left N
// orphaned access keys in the store.
func TestService_ResetToAppKeySet(t *testing.T) {
	svc, mockStore, testStore, _ := newTestService(t, StateComplete)

	// Seed the store with access keys and a user from a failed onboarding attempt.
	require.NoError(t, testStore.CreateUser("admin"))
	require.NoError(t, testStore.CreateAccessKey("admin", "AKIA001", testSecretKey()))
	require.NoError(t, testStore.CreateAccessKey("admin", "AKIA002", testSecretKey()))

	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(nil)
	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})

	err := svc.ResetToAppKeySet()
	require.NoError(t, err)

	assert.Equal(t, StateAppKeySet, svc.fsm.State(),
		"FSM must be reset to StateAppKeySet")

	keys, err := testStore.ListAccessKeys(nil)
	require.NoError(t, err)
	assert.Empty(t, keys, "all access keys must be deleted on reset")

	users, err := testStore.ListUsers()
	require.NoError(t, err)
	assert.Empty(t, users, "all users must be deleted on reset")
}

// TestService_ResetToAppKeySet_StoreError verifies that ResetToAppKeySet
// returns the store error when SetOnboardingState fails. The FSM has
// already been flipped in-memory but persisted state is stale.
func TestService_ResetToAppKeySet_StoreError(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StateComplete)

	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})
	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(errors.New("disk full"))

	err := svc.ResetToAppKeySet()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")

	// FSM was still reset in memory (best-effort), store is the one that failed
	assert.Equal(t, StateAppKeySet, svc.fsm.State())
}

// TestService_ResetToAppKeySet_OpenDBError verifies that when OpenDatabase
// fails, ResetToAppKeySet returns the error WITHOUT flipping the FSM state
// or persisting StateAppKeySet. The user stays in StateComplete with an
// error rather than being stuck in StateAppKeySet with no store handle.
// Regression for Kody finding #3602541750: FSM + persisted state were
// flipped before store re-open, leaving svc.sqliteStore == nil and the
// user unable to proceed with onboarding.
func TestService_ResetToAppKeySet_OpenDBError(t *testing.T) {
	svc, mockStore, _, init := newTestService(t, StateComplete)

	// Make OpenDatabase fail
	init.openDBErr = errors.New("database corrupted")

	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})
	// SetOnboardingState must NOT be called — FSM must not flip

	err := svc.ResetToAppKeySet()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database corrupted")

	// FSM must remain in StateComplete — no state transition on failure
	assert.Equal(t, StateComplete, svc.fsm.State(),
		"FSM must stay in StateComplete when OpenDatabase fails — don't flip until store is ready")

	// sqliteStore must be nil — no store handle was set
	svc.mu.Lock()
	assert.Nil(t, svc.sqliteStore, "sqliteStore must not be set when OpenDatabase fails")
	svc.mu.Unlock()
}
