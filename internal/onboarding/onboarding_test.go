package onboarding

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

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

// testBackendInit satisfies BackendInitializer for tests.
type testBackendInit struct {
	store      backend.S3DStore
	initCalled bool
}

func (b *testBackendInit) OpenDatabase(dbPath string) (backend.S3DStore, error) {
	return b.store, nil
}

func (b *testBackendInit) InitAfterOnboarding(ctx context.Context, sqliteStore backend.S3DStore) error {
	b.initCalled = true
	return nil
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
	copy(appKey[:], []byte("01234567890123456789012345678901"))
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
	svc, _, _, _ := newTestService(t, StateAppKeySet)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/app-key", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.SetAppKeyHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
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
	assert.True(t, init.initCalled)

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
	copy(appKey[:], []byte("01234567890123456789012345678901"))
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
	assert.True(t, init.initCalled)
}

// Regression: SetAppKeyHandler when SetOnboardingState fails: FSM transitions
// first, then persist fails and rolls back the FSM to StateAdminSet.
// sqliteStore is closed. Returns 500, not a misleading 200.
func TestSetAppKeyHandler_PersistFailure_ReturnsError(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StateAdminSet)

	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/tmp/test-s3d"})
	mockStore.EXPECT().SetOnboardingState("app_key_set").Return(errors.New("disk full"))

	var appKey [32]byte
	copy(appKey[:], []byte("01234567890123456789012345678901"))
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
