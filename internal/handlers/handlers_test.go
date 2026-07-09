package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SiaFoundation/s3d/s3"
	"github.com/SiaFoundation/s3d/sia"
	"go.lumeweb.com/s3-server/internal/backend"
	backendMocks "go.lumeweb.com/s3-server/internal/backend/mocks"
	"go.lumeweb.com/s3-server/internal/config"
	handlerMocks "go.lumeweb.com/s3-server/internal/handlers/mocks"
	storeMocks "go.lumeweb.com/s3-server/internal/store/mocks"
	"go.lumeweb.com/s3-server/internal/testutil"
	"go.sia.tech/core/types"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// stubRestarter implements BackendRestarter for testing.
type stubRestarter struct {
	restartErr error
	called     bool
}

func (r *stubRestarter) Restart(ctx context.Context) error {
	r.called = true
	return r.restartErr
}

func newTestServices(t *testing.T) (*Services, *storeMocks.MockStore, *stubRestarter, *backendMocks.MockS3DStore) {
	mockStore := storeMocks.NewMockStore(t)
	mockKS := backendMocks.NewMockS3DStore(t)
	restarter := &stubRestarter{}
	log := testutil.NewTestLogger()
	broker := &handlerMocks.MockSSEBroker{}
	broker.On("NotifyKeyChange", mock.Anything, mock.Anything).Return()
	broker.On("NotifyBucketChange", mock.Anything, mock.Anything).Return()
	svc := NewServices(ServicesConfig{
		Store:     mockStore,
		Restarter: restarter,
		Log:       log,
		SSEBroker: broker,
	})
	svc.keyStore = func() backend.S3DStore { return mockKS }
	return svc, mockStore, restarter, mockKS
}

// newTestServicesWithBroker is like newTestServices but returns the SSE broker
// so tests can assert notification calls.
func newTestServicesWithBroker(t *testing.T) (*Services, *storeMocks.MockStore, *stubRestarter, *backendMocks.MockS3DStore, *handlerMocks.MockSSEBroker) {
	mockStore := storeMocks.NewMockStore(t)
	mockKS := backendMocks.NewMockS3DStore(t)
	restarter := &stubRestarter{}
	log := testutil.NewTestLogger()
	broker := &handlerMocks.MockSSEBroker{}
	broker.On("NotifyKeyChange", mock.Anything, mock.Anything).Return()
	broker.On("NotifyBucketChange", mock.Anything, mock.Anything).Return()
	svc := NewServices(ServicesConfig{
		Store:     mockStore,
		Restarter: restarter,
		Log:       log,
		SSEBroker: broker,
	})
	svc.keyStore = func() backend.S3DStore { return mockKS }
	return svc, mockStore, restarter, mockKS, broker
}

func TestS3Handler_Swap(t *testing.T) {
	h := NewS3Handler()

	realHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("s3 response")) //nolint:errcheck
	})
	h.Swap(realHandler)

	req := httptest.NewRequest(http.MethodGet, "/bucket/key", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "s3 response", rec.Body.String())
}

func TestS3Handler_SwapConcurrent(t *testing.T) {
	h := NewS3Handler()

	realHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			h.Swap(realHandler)
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, rec.Code)
	}
	<-done
}

func TestServices_StatusAPI(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretKey: "secret"},
	}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.statusAPI(c)
	require.NoError(t, err)

	var resp StatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "stopped", resp.S3Status)
	assert.Equal(t, 1, resp.KeyCount)
}

func TestServices_ListKeys(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretKey: "secret1"},
		{AccessKeyID: "AKIAIOSFODNN8EXAMPLE", SecretKey: "secret2"},
	}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.listKeys(c)
	require.NoError(t, err)

	var resp ListAccessKeysResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Keys, 2)
	assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", resp.Keys[0].AccessKey)
}

func TestServices_AddKey_Generated(t *testing.T) {
	svc, _, restarter, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{}, nil)
	mockKS.On("CreateUser", backend.DefaultUserName).Return(nil)
	mockKS.On("CreateAccessKey", backend.DefaultUserName, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.addKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.True(t, restarter.called, "backend should restart after adding a key")

	var resp AddAccessKeyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Generated)
	assert.True(t, strings.HasPrefix(resp.AccessKey, "AKIA"))
	assert.Len(t, resp.AccessKey, 20)
	assert.Len(t, resp.SecretKey, 40)
}

func TestServices_AddKey_Custom(t *testing.T) {
	svc, _, restarter, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{}, nil)
	mockKS.On("CreateUser", backend.DefaultUserName).Return(nil)
	mockKS.On("CreateAccessKey", backend.DefaultUserName, "AKIAIOSFODNN7EXAMPLE", "abcdefghijklmnopqrstuvwxyz123456").Return(nil)

	e := echo.New()
	body := `{"access_key":"AKIAIOSFODNN7EXAMPLE","secret_key":"abcdefghijklmnopqrstuvwxyz123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.addKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.True(t, restarter.called)

	var resp AddAccessKeyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.False(t, resp.Generated)
	assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", resp.AccessKey)
}

func TestServices_AddKey_Duplicate(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretKey: "existing"},
	}, nil)

	e := echo.New()
	body := `{"access_key":"AKIAIOSFODNN7EXAMPLE","secret_key":"abcdefghijklmnopqrstuvwxyz123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.addKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestServices_AddKey_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"missing access_key", `{"access_key":"","secret_key":"abcdefghijklmnopqrstuvwxyz123456"}`},
		{"missing secret_key", `{"access_key":"AKIAIOSFODNN7EXAMPLE","secret_key":""}`},
		{"access_key too short", `{"access_key":"AKIA123","secret_key":"abcdefghijklmnopqrstuvwxyz123456"}`},
		{"secret_key too short", `{"access_key":"AKIAIOSFODNN7EXAMPLE","secret_key":"short"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _, _ := newTestServices(t)
			// No KeyStore expectations — validation fails before the duplicate check

			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := svc.addKey(c)
			require.NoError(t, err)
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		})
	}
}

func TestServices_DeleteKey(t *testing.T) {
	svc, _, restarter, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretKey: "secret1"},
		{AccessKeyID: "AKIAIOSFODNN8EXAMPLE", SecretKey: "secret2"},
	}, nil)
	mockKS.On("DeleteAccessKey", "AKIAIOSFODNN7EXAMPLE").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/keys/AKIAIOSFODNN7EXAMPLE", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/keys/:accessKey")
	c.SetPathValues(echo.PathValues{{Name: "accessKey", Value: "AKIAIOSFODNN7EXAMPLE"}})

	err := svc.deleteKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.True(t, restarter.called, "backend should restart after deleting a key")
}

func TestServices_DeleteKey_NotFound(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretKey: "secret1"},
	}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/keys/AKIA_NONEXISTENT", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/keys/:accessKey")
	c.SetPathValues(echo.PathValues{{Name: "accessKey", Value: "AKIA_NONEXISTENT"}})

	err := svc.deleteKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestServices_DeleteKey_LastKey(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretKey: "secret1"},
	}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/keys/AKIAIOSFODNN7EXAMPLE", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/keys/:accessKey")
	c.SetPathValues(echo.PathValues{{Name: "accessKey", Value: "AKIAIOSFODNN7EXAMPLE"}})

	err := svc.deleteKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestGenerateAccessKey(t *testing.T) {
	ak, sk := generateAccessKey()

	assert.True(t, strings.HasPrefix(ak, "AKIA"))
	assert.Len(t, ak, 20)
	assert.Len(t, sk, 40)

	for _, c := range ak[4:] {
		assert.Contains(t, accessKeyCharset, string(c))
	}
}

func TestGenerateAccessKey_Uniqueness(t *testing.T) {
	keys := make(map[string]bool)
	for i := 0; i < 100; i++ {
		ak, _ := generateAccessKey()
		assert.False(t, keys[ak], "duplicate key generated: %s", ak)
		keys[ak] = true
	}
}

func TestServices_GetS3Config(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	mockStore.EXPECT().S3Config().Return(config.S3Config{
		Directory:         "/data/s3d",
		IndexerURL:        "https://sia.storage",
		AvailableIndexers: []string{"https://sia.pinner.xyz", "https://sia.storage"},
		HostBases:         []string{"s3.example.com"},
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/s3-config", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.getS3Config(c)
	require.NoError(t, err)

	var resp S3ConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "/data/s3d", resp.Directory)
	assert.Equal(t, "https://sia.storage", resp.IndexerURL)
	assert.Equal(t, []string{"https://sia.pinner.xyz", "https://sia.storage"}, resp.AvailableIndexers)
	assert.Equal(t, []string{"s3.example.com"}, resp.HostBases)
}

func TestServices_SetS3Config(t *testing.T) {
	svc, mockStore, restarter, _ := newTestServices(t)
	mockStore.EXPECT().S3Config().Return(config.S3Config{
		Directory:         "/data/s3d",
		IndexerURL:        "https://custom.storage",
		AvailableIndexers: []string{"https://sia.pinner.xyz", "https://sia.storage"},
		HostBases:         []string{"s3.example.com"},
	})
	mockStore.EXPECT().SetS3Config(config.S3Config{
		Directory:         "/new/data",
		IndexerURL:        "https://custom.storage",
		AvailableIndexers: []string{},
		HostBases:         []string{"s3.example.com"},
	}).Return(nil)

	e := echo.New()
	body := `{"directory":"/new/data","indexer_url":"https://custom.storage","available_indexers":[],"host_bases":["s3.example.com"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/s3-config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.setS3Config(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, restarter.called, "backend should be restarted after S3 config change")
}

func TestServices_SetS3Config_MissingDirectory(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	e := echo.New()
	body := `{"directory":"","indexer_url":"https://sia.storage"}`
	req := httptest.NewRequest(http.MethodPut, "/api/s3-config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.setS3Config(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_GetSSLConfig(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	mockStore.EXPECT().SSLConfig().Return(config.SSLConfig{
		Mode:      config.SSLModeManaged,
		ACMEEmail: "admin@example.com",
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/ssl-config", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.getSSLConfig(c)
	require.NoError(t, err)

	var resp SSLConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "managed", resp.Mode)
	assert.Equal(t, "admin@example.com", resp.ACMEEmail)
}

func TestServices_SetSSLConfig(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	mockStore.EXPECT().SetSSLConfig(config.SSLConfig{
		Mode:      config.SSLModeManaged,
		ACMEEmail: "admin@example.com",
	}).Return(nil)

	e := echo.New()
	body := `{"mode":"managed","acme_email":"admin@example.com"}`
	req := httptest.NewRequest(http.MethodPut, "/api/ssl-config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.setSSLConfig(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ConfigUpdateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "updated", resp.Status)
}

func TestServices_SetSSLConfig_InvalidMode(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	e := echo.New()
	body := `{"mode":"invalid"}`
	req := httptest.NewRequest(http.MethodPut, "/api/ssl-config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.setSSLConfig(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_SetSSLConfig_ManagedMissingEmail(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	e := echo.New()
	body := `{"mode":"managed","acme_email":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/ssl-config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.setSSLConfig(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_ListUsers(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListUsers").Return([]string{"admin", "bob"}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.listUsers(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp UserListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, []string{"admin", "bob"}, resp.Users)
}

func TestServices_CreateUser(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("CreateUser", "bob").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"name":"bob"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.createUser(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp UserResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "bob", resp.Name)
}

func TestServices_CreateUser_Duplicate(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("CreateUser", "bob").Return(sia.ErrUserAlreadyExists)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"name":"bob"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.createUser(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestServices_DeleteUser(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{}, nil)
	mockKS.On("DeleteUser", "bob").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/users/bob", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/users/:name")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: "bob"}})

	err := svc.deleteUser(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestServices_DeleteUser_DefaultUser(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/users/"+backend.DefaultUserName, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/users/:name")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: backend.DefaultUserName}})

	err := svc.deleteUser(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_ListUserKeys(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAONE", SecretKey: "secret", UserName: "bob"},
	}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/users/bob/keys", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/users/:name/keys")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: "bob"}})

	err := svc.listUserKeys(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ListAccessKeysResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Keys, 1)
	assert.Equal(t, "AKIAONE", resp.Keys[0].AccessKey)
	assert.Equal(t, "bob", resp.Keys[0].UserName)
}

func TestServices_ListKeys_IncludesUserName(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAONE", SecretKey: "secret", UserName: "admin"},
	}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.listKeys(c)
	require.NoError(t, err)

	var resp ListAccessKeysResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Keys, 1)
	assert.Equal(t, "admin", resp.Keys[0].UserName)
}

func TestServices_AddKey_WithUserName(t *testing.T) {
	svc, _, restarter, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{}, nil)
	mockKS.On("CreateUser", "custom").Return(nil)
	mockKS.On("CreateAccessKey", "custom", mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

	e := echo.New()
	body := `{"user_name":"custom"}`
	req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.addKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.True(t, restarter.called)
}

func TestServices_ListBuckets(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: "secret"},
	}, nil)

	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	mockBackend.On("ListBuckets", mock.Anything, "AKIAADMIN").Return([]s3.BucketInfo{
		{Name: "bucket1", CreationDate: s3.NewContentTime(created)},
	}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/buckets", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.listBuckets(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ListBucketsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Buckets, 1)
	assert.Equal(t, "bucket1", resp.Buckets[0].Name)
	assert.True(t, resp.Buckets[0].CreatedAt.Equal(created))
}

func TestServices_CreateBucket(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: "secret"},
	}, nil)
	mockBackend.On("CreateBucket", mock.Anything, "AKIAADMIN", "mybucket").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/buckets", strings.NewReader(`{"name":"mybucket"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.createBucket(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp BucketResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "mybucket", resp.Name)
}

func TestServices_DeleteBucket(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: "secret"},
	}, nil)
	mockBackend.On("DeleteBucket", mock.Anything, "AKIAADMIN", "mybucket").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/buckets/mybucket", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/buckets/:name")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: "mybucket"}})

	err := svc.deleteBucket(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestServices_GetBucketVersioning(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: "secret"},
	}, nil)
	mockBackend.On("GetBucketVersioning", mock.Anything, "AKIAADMIN", "mybucket").Return("Enabled", nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/buckets/mybucket/versioning", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/buckets/:name/versioning")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: "mybucket"}})

	err := svc.getBucketVersioning(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp BucketVersioningResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Enabled", resp.Status)
}

func TestServices_PutBucketVersioning(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: "secret"},
	}, nil)
	mockBackend.On("PutBucketVersioning", mock.Anything, "AKIAADMIN", "mybucket", "Enabled").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/buckets/mybucket/versioning", strings.NewReader(`{"status":"Enabled"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/buckets/:name/versioning")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: "mybucket"}})

	err := svc.putBucketVersioning(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp BucketVersioningResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "Enabled", resp.Status)
}

func TestServices_PutBucketVersioning_InvalidStatus(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/buckets/mybucket/versioning", strings.NewReader(`{"status":"Bogus"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/buckets/:name/versioning")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: "mybucket"}})

	err := svc.putBucketVersioning(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_CreateBackup(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	tmpDir := t.TempDir()
	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: tmpDir}).Once()

	var capturedPath string
	svc.adminHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/system/sqlite3/backup", r.URL.Path)
		var req struct {
			Path string `json:"path"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.True(t, strings.HasPrefix(req.Path, tmpDir))
		capturedPath = req.Path
		w.WriteHeader(http.StatusOK)
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/backup", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.createBackup(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp BackupResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, capturedPath, resp.Path)
	assert.True(t, strings.HasPrefix(resp.Filename, "s3d-backup-"))
}

func TestServices_ListBackups(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	tmpDir := t.TempDir()
	backupsDir := filepath.Join(tmpDir, "backups")
	require.NoError(t, os.MkdirAll(backupsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(backupsDir, "b1.db"), []byte("hello"), 0644))
	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: tmpDir}).Once()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/backups", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.listBackups(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ListBackupsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Files, 1)
	assert.Equal(t, "b1.db", resp.Files[0].Name)
	assert.Equal(t, int64(5), resp.Files[0].Size)
}

func TestServices_GetBackup(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	tmpDir := t.TempDir()
	backupsDir := filepath.Join(tmpDir, "backups")
	require.NoError(t, os.MkdirAll(backupsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(backupsDir, "b1.db"), []byte("hello"), 0644))
	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: tmpDir}).Once()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/backups/b1.db", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/backups/:filename")
	c.SetPathValues(echo.PathValues{{Name: "filename", Value: "b1.db"}})

	err := svc.getBackup(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello", rec.Body.String())
}

func TestServices_DeleteBackup(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	tmpDir := t.TempDir()
	backupsDir := filepath.Join(tmpDir, "backups")
	require.NoError(t, os.MkdirAll(backupsDir, 0755))
	path := filepath.Join(backupsDir, "b1.db")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0644))
	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: tmpDir}).Once()

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/backups/b1.db", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/backups/:filename")
	c.SetPathValues(echo.PathValues{{Name: "filename", Value: "b1.db"}})

	err := svc.deleteBackup(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestServices_GetStats_Proxy(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	var capturedPath string
	svc.adminHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"pendingObjects":1}`)) //nolint:errcheck
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/stats", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.getStats(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/stats/uploads", capturedPath)
}

func TestServices_GetPrometheus_Proxy(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	var capturedPath string
	svc.adminHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# metrics")) //nolint:errcheck
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/prometheus", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.getPrometheus(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/prometheus", capturedPath)
}

func TestServices_UsersPage(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	mockKS.On("ListUsers").Return([]string{"admin", "bob"}, nil)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: "secret", UserName: "admin"},
		{AccessKeyID: "AKIABOB", SecretKey: "secret", UserName: "bob"},
		{AccessKeyID: "AKIAADMIN2", SecretKey: "secret", UserName: "admin"},
	}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.usersPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "admin")
	assert.Contains(t, rec.Body.String(), "bob")
	assert.Contains(t, rec.Body.String(), "2 access key")
	assert.Contains(t, rec.Body.String(), "1 access key")
}

func TestServices_BucketsPage(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }

	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: "secret"},
	}, nil)

	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	mockBackend.On("ListBuckets", mock.Anything, "AKIAADMIN").Return([]s3.BucketInfo{
		{Name: "test-bucket", CreationDate: s3.NewContentTime(created)},
	}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/buckets", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.bucketsPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "test-bucket")
	assert.Contains(t, rec.Body.String(), "2025-01-02 03:04:05 UTC")
}

func TestServices_BackupsPage(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	tmpDir := t.TempDir()
	backupsDir := filepath.Join(tmpDir, "backups")
	require.NoError(t, os.MkdirAll(backupsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(backupsDir, "b1.db"), []byte("hello"), 0644))
	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: tmpDir}).Once()
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/backups", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.backupsPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "b1.db")
	assert.Contains(t, rec.Body.String(), "5 B")
}

func TestServices_MonitoringPage(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	svc.adminHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/stats/uploads", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"pendingObjects": 3,
			"pendingSize": 1024,
			"uploadedObjects": 10,
			"uploadedSize": 2048,
			"unpinnedObjects": 1,
			"failedUploads": 0,
			"orphanedObjects": 0,
			"multipartUploads": 2
		}`)) //nolint:errcheck
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/monitoring", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.monitoringPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Pending Objects")
	assert.Contains(t, rec.Body.String(), "3")
	assert.Contains(t, rec.Body.String(), "1.0 KB")
}

func TestServices_SystemFlush(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	mockBackend.On("FlushObjects", mock.Anything).Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/system/flush", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.systemFlush(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestServices_SystemFlush_BackendNotReady(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	// no backend set — getBackend() returns nil

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/system/flush", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.systemFlush(c)
	require.ErrorIs(t, err, errResponseSent)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestServices_SystemFlush_Error(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	mockBackend.On("FlushObjects", mock.Anything).Return(assert.AnError)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/system/flush", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.systemFlush(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServices_SystemRestart(t *testing.T) {
	svc, _, restarter, _ := newTestServices(t)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/system/restart", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.systemRestart(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, restarter.called, "backend should be restarted")

	var resp ConfigUpdateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "restarted", resp.Status)
}

func TestServices_SystemRestart_Error(t *testing.T) {
	svc, _, restarter, _ := newTestServices(t)
	restarter.restartErr = assert.AnError

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/system/restart", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.systemRestart(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.True(t, restarter.called)
}

// --- Regression tests for Kody review findings on PR #14 ---

// Test 1: Nil-pointer guard in addKey HTMX path (finding #3548390934).
// addKey/deleteKey call backendStatus() in the HTMX branch; with nil
// backendStatus it must not panic.
func TestServices_AddKey_NilBackendStatus(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.backendStatus = nil // simulate uninitialized backend status
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{}, nil)
	mockKS.On("CreateUser", backend.DefaultUserName).Return(nil)
	mockKS.On("CreateAccessKey", backend.DefaultUserName, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Should not panic — nil-guard returns false for backendRunning
	err := svc.addKey(c)
	// HTMX path renders HTML; non-nil error means rendering succeeded or
	// returned a view error, but NOT a panic
	_ = err
}

// Test 2: Nil-pointer guard for restarter in addKey (finding #3548391143).
func TestServices_AddKey_NilRestarter(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.restarter = nil // simulate uninitialized restarter
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{}, nil)
	mockKS.On("CreateUser", backend.DefaultUserName).Return(nil)
	mockKS.On("CreateAccessKey", backend.DefaultUserName, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.addKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp AddAccessKeyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Generated)
}

// Test 3: Nil-pointer guard in keysPage (finding #3548391278).
func TestServices_KeysPage_NilBackendStatus(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.backendStatus = nil
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIATEST", SecretKey: "secret", UserName: backend.DefaultUserName},
	}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/keys", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Should not panic — nil-guard returns false for backendRunning
	err := svc.keysPage(c)
	// Rendering may return an error if templates aren't fully wired, but
	// it must NOT panic.
	_ = err
}

// Test 4: SecretKey omitted from listKeys API response (finding #3548601488).
func TestServices_ListKeys_SecretKeyOmitted(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAONE", SecretKey: "supersecret1", UserName: "admin"},
		{AccessKeyID: "AKIATWO", SecretKey: "supersecret2", UserName: "admin"},
	}, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.listKeys(c)
	require.NoError(t, err)

	var resp ListAccessKeysResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Keys, 2)
	for _, k := range resp.Keys {
		assert.Empty(t, k.SecretKey, "SecretKey must not be exposed in listKeys API response")
	}
}

// Test 5: concurrent deleteKey TOCTOU race — last key must survive (finding #3548758653).
func TestServices_DeleteKey_Concurrent_LastKey(t *testing.T) {
	// Use a real stub key store that tracks state under a mutex.
	ks := &stubKeyStore{
		keys: []backend.AccessKeyInfo{
			{AccessKeyID: "AKIAONE", SecretKey: "secret1", UserName: backend.DefaultUserName},
		},
	}
	log := testutil.NewTestLogger()
	broker := &handlerMocks.MockSSEBroker{}
	broker.On("NotifyKeyChange", mock.Anything, mock.Anything).Return()
	svc := NewServices(ServicesConfig{
		Log:       log,
		SSEBroker: broker,
	})
	svc.keyStore = func() backend.S3DStore { return ks }

	// Two goroutines try to delete the (only) key simultaneously.
	var wg sync.WaitGroup
	var errs [2]error
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			e := echo.New()
			req := httptest.NewRequest(http.MethodDelete, "/api/keys/AKIAONE", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetPath("/api/keys/:accessKey")
			c.SetPathValues(echo.PathValues{{Name: "accessKey", Value: "AKIAONE"}})
			errs[idx] = svc.deleteKey(c)
		}(i)
	}
	wg.Wait()

	// Exactly one goroutine should succeed (204), the other gets validation
	// error (cannot delete last key). The key must still exist.
	assert.Len(t, ks.keys, 1, "last key must not be deleted")
}

// Test 6: deleteUser notifies SSE broker (finding #3548873232).
func TestServices_DeleteUser_NotifiesSSE(t *testing.T) {
	svc, _, _, mockKS, broker := newTestServicesWithBroker(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAONE", UserName: "bob"},
	}, nil)
	mockKS.On("DeleteUser", "bob").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/users/bob", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/users/:name")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: "bob"}})

	err := svc.deleteUser(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	calls := broker.KeyChangeCalls()
	require.Len(t, calls, 1, "NotifyKeyChange must be called once")
	assert.Equal(t, "deleted", calls[0].Args[0])
	assert.Equal(t, 1, calls[0].Args[1]) // 1 remaining key
}

// Test 7: groupedKeys never includes SecretKey in view model (finding #3548873470).
func TestServices_GroupedKeys_NoSecretKey(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAONE", SecretKey: "supersecret1", UserName: backend.DefaultUserName},
		{AccessKeyID: "AKIATWO", SecretKey: "supersecret2", UserName: "custom"},
	}, nil)

	groups := svc.groupedKeys()
	require.Len(t, groups, 2)
	for _, g := range groups {
		for _, kp := range g.Keys {
			assert.Empty(t, kp.SecretKey, "SecretKey must be empty in groupedKeys view model (path: %s)", kp.AccessKey)
		}
	}
}

// Test 8: keyMu is released during Restart() so readers aren't blocked (findings #3548873347 + #3548911179).
func TestServices_KeyMutation_LockReleasedDuringRestart(t *testing.T) {
	ks := &stubKeyStore{
		keys: []backend.AccessKeyInfo{
			{AccessKeyID: "AKIAEXIST", SecretKey: "secret1", UserName: backend.DefaultUserName},
			{AccessKeyID: "AKIAOTHER", SecretKey: "secret2", UserName: backend.DefaultUserName},
		},
	}
	log := testutil.NewTestLogger()
	broker := &handlerMocks.MockSSEBroker{}
	broker.On("NotifyKeyChange", mock.Anything, mock.Anything).Return()

	// slowRestarter blocks until released, simulating a slow backend restart
	r := &slowRestarter{done: make(chan struct{})}
	svc := NewServices(ServicesConfig{
		Restarter: r,
		Log:       log,
		SSEBroker: broker,
	})
	svc.keyStore = func() backend.S3DStore { return ks }

	// Start addKey — it will hold the lock, then release it before Restart().
	addDone := make(chan error, 1)
	go func() {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"access_key":"AKIANEW","secret_key":"abcdefghijklmnopqrstuvwxyz0123456789"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		addDone <- svc.addKey(c)
	}()

	// Give addKey time to acquire the lock and enter Restart().
	time.Sleep(50 * time.Millisecond)

	// While Restart() is in progress, readers must be able to acquire RLock.
	readDone := make(chan error, 1)
	go func() {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		readDone <- svc.listKeys(c)
	}()

	select {
	case err := <-readDone:
		require.NoError(t, err, "listKeys must succeed while Restart() is in progress")
	case <-time.After(2 * time.Second):
		t.Fatal("listKeys blocked during Restart() — keyMu not released")
	}

	// Release the slow restarter to let addKey finish.
	close(r.done)
	require.NoError(t, <-addDone)
}

// --- Test helpers ---

// stubKeyStore is a minimal in-memory S3DStore for concurrency tests.
type stubKeyStore struct {
	mu   sync.Mutex
	keys []backend.AccessKeyInfo
}

func (s *stubKeyStore) ListAccessKeys(_ *string) ([]backend.AccessKeyInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]backend.AccessKeyInfo, len(s.keys))
	copy(out, s.keys)
	return out, nil
}

func (s *stubKeyStore) DeleteAccessKey(accessKeyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, k := range s.keys {
		if k.AccessKeyID == accessKeyID {
			s.keys = append(s.keys[:i], s.keys[i+1:]...)
			return nil
		}
	}
	return sia.ErrUserNotFound
}

func (s *stubKeyStore) CreateAccessKey(userName, accessKeyID, secretKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = append(s.keys, backend.AccessKeyInfo{
		AccessKeyID: accessKeyID,
		SecretKey:   secretKey,
		UserName:    userName,
	})
	return nil
}

func (s *stubKeyStore) CreateUser(name string) error         { return nil }
func (s *stubKeyStore) DeleteUser(name string) error         { return nil }
func (s *stubKeyStore) ListUsers() ([]string, error)        { return nil, nil }
func (s *stubKeyStore) AppKey() (types.PrivateKey, string, error) { return types.PrivateKey{}, "", nil }
func (s *stubKeyStore) SetAppKey(_ types.PrivateKey, _ string) error { return nil }
func (s *stubKeyStore) Close() error                          { return nil }

// slowRestarter blocks on a channel until released, simulating a slow backend restart.
type slowRestarter struct {
	done chan struct{}
}

func (r *slowRestarter) Restart(_ context.Context) error {
	<-r.done
	return nil
}
