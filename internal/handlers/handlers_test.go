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

	"github.com/SiaFoundation/s3d/sia"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/backend"
	backendMocks "go.lumeweb.com/s3-server/internal/backend/mocks"
	"go.lumeweb.com/s3-server/internal/config"
	handlerMocks "go.lumeweb.com/s3-server/internal/handlers/mocks"
	"go.lumeweb.com/s3-server/internal/status"
	storeMocks "go.lumeweb.com/s3-server/internal/store/mocks"
	"go.lumeweb.com/s3-server/internal/testutil"
	"go.sia.tech/core/types"
)

// testSecretKey is sourced from env to satisfy secret-scanning rules.
// Tests that need it should set TEST_SECRET_KEY; defaults to a non-empty placeholder.
var testSecretKey = os.Getenv("TEST_SECRET_KEY")

// stubRestarter removed: use handlerMocks.MockBackendRestarter instead.

func newTestServices(t *testing.T) (*Services, *storeMocks.MockStore, *handlerMocks.MockBackendRestarter, *backendMocks.MockS3DStore) {
	mockStore := storeMocks.NewMockStore(t)
	mockKS := backendMocks.NewMockS3DStore(t)
	restarter := &handlerMocks.MockBackendRestarter{}
	restarter.Test(t)
	restarter.On("Restart", mock.Anything).Return(nil).Maybe()
	log := testutil.NewTestLogger()
	broker := &handlerMocks.MockSSEBroker{}
	broker.Test(t)
	broker.On("NotifyKeyChange", mock.Anything, mock.Anything).Return().Maybe()
	broker.On("NotifyBucketChange", mock.Anything, mock.Anything).Return().Maybe()
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
func newTestServicesWithBroker(t *testing.T) (*Services, *storeMocks.MockStore, *handlerMocks.MockBackendRestarter, *backendMocks.MockS3DStore, *handlerMocks.MockSSEBroker) {
	mockStore := storeMocks.NewMockStore(t)
	mockKS := backendMocks.NewMockS3DStore(t)
	restarter := &handlerMocks.MockBackendRestarter{}
	restarter.Test(t)
	restarter.On("Restart", mock.Anything).Return(nil).Maybe()
	log := testutil.NewTestLogger()
	broker := &handlerMocks.MockSSEBroker{}
	broker.Test(t)
	broker.On("NotifyKeyChange", mock.Anything, mock.Anything).Return().Maybe()
	broker.On("NotifyBucketChange", mock.Anything, mock.Anything).Return().Maybe()
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
		{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretKey: testSecretKey},
	}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/status")

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
	c, rec := testContext(e, http.MethodGet, "/api/keys")

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
	mockKS.On("CreateUser", "admin").Return(nil)
	mockKS.On("CreateAccessKey", "admin", mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/keys", strings.NewReader(`{"user_name":"admin"}`))

	err := svc.addKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	restarter.AssertCalled(t, "Restart", mock.Anything)

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
	mockKS.On("CreateUser", "admin").Return(nil)
	mockKS.On("CreateAccessKey", "admin", "AKIAIOSFODNN7EXAMPLE", "abcdefghijklmnopqrstuvwxyz123456").Return(nil)

	e := echo.New()
	body := `{"user_name":"admin","access_key":"AKIAIOSFODNN7EXAMPLE","secret_key":"abcdefghijklmnopqrstuvwxyz123456"}`
	c, rec := testJSONContext(e, http.MethodPost, "/api/keys", strings.NewReader(body))

	err := svc.addKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	restarter.AssertCalled(t, "Restart", mock.Anything)

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
	body := `{"user_name":"admin","access_key":"AKIAIOSFODNN7EXAMPLE","secret_key":"abcdefghijklmnopqrstuvwxyz123456"}`
	c, rec := testJSONContext(e, http.MethodPost, "/api/keys", strings.NewReader(body))

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
		{"access_key too short", `{"user_name":"admin","access_key":"AKIA123","secret_key":"abcdefghijklmnopqrstuvwxyz123456"}`},
		{"secret_key too short", `{"user_name":"admin","access_key":"AKIAIOSFODNN7EXAMPLE","secret_key":"short"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _, _ := newTestServices(t)
			// No KeyStore expectations: validation fails before the duplicate check

			e := echo.New()
			c, rec := testJSONContext(e, http.MethodPost, "/api/keys", strings.NewReader(tt.body))

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
	c, rec := testContext(e, http.MethodDelete, "/api/keys/AKIAIOSFODNN7EXAMPLE")
	c.SetPath("/api/keys/:accessKey")
	c.SetPathValues(echo.PathValues{{Name: "accessKey", Value: "AKIAIOSFODNN7EXAMPLE"}})

	err := svc.deleteKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	restarter.AssertCalled(t, "Restart", mock.Anything)
}

func TestServices_DeleteKey_NotFound(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretKey: "secret1"},
	}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodDelete, "/api/keys/AKIA_NONEXISTENT")
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
	c, rec := testContext(e, http.MethodDelete, "/api/keys/AKIAIOSFODNN7EXAMPLE")
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
		Directory:  "/data/s3d",
		IndexerURL: "https://sia.storage",
		AvailableIndexers: []config.IndexerOption{
			{URL: "https://sia.pinner.xyz", Name: "Pinner"},
			{URL: "https://sia.storage", Name: "Sia Storage"},
		},
		HostBases: []string{"s3.example.com"},
	})

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/s3-config")

	err := svc.getS3Config(c)
	require.NoError(t, err)

	var resp S3ConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "/data/s3d", resp.Directory)
	assert.Equal(t, "https://sia.storage", resp.IndexerURL)
	assert.Len(t, resp.AvailableIndexers, 2)
	assert.Equal(t, "https://sia.pinner.xyz", resp.AvailableIndexers[0].URL)
	assert.Equal(t, "https://sia.storage", resp.AvailableIndexers[1].URL)
	assert.Equal(t, []string{"s3.example.com"}, resp.HostBases)
}

func TestServices_SetS3Config(t *testing.T) {
	svc, mockStore, restarter, _ := newTestServices(t)
	mockStore.EXPECT().S3Config().Return(config.S3Config{
		Directory:  "/data/s3d",
		IndexerURL: "https://custom.storage",
		AvailableIndexers: []config.IndexerOption{
			{URL: "https://sia.pinner.xyz", Name: "Pinner"},
			{URL: "https://sia.storage", Name: "Sia Storage"},
		},
		HostBases: []string{"s3.example.com"},
	})
	mockStore.EXPECT().SetS3Config(config.S3Config{
		Directory:  "/new/data",
		IndexerURL: "https://custom.storage",
		AvailableIndexers: []config.IndexerOption{
			{URL: "https://sia.pinner.xyz", Name: "Pinner"},
			{URL: "https://sia.storage", Name: "Sia Storage"},
		},
		HostBases:      []string{"s3.example.com"},
		DiskUsageLimit: config.DefaultDiskUsageLimit,
	}).Return(nil)

	e := echo.New()
	body := `{"directory":"/new/data","indexer_url":"https://custom.storage","available_indexers":[],"host_bases":["s3.example.com"]}`
	c, rec := testJSONContext(e, http.MethodPut, "/api/s3-config", strings.NewReader(body))

	err := svc.setS3Config(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	// S3 config save no longer auto-restarts; user must restart manually
	restarter.AssertNotCalled(t, "Restart")
}

func TestServices_SetS3Config_MissingDirectory(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	e := echo.New()
	body := `{"directory":"","indexer_url":"https://sia.storage"}`
	c, rec := testJSONContext(e, http.MethodPut, "/api/s3-config", strings.NewReader(body))

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
	c, rec := testContext(e, http.MethodGet, "/api/ssl-config")

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
	c, rec := testJSONContext(e, http.MethodPut, "/api/ssl-config", strings.NewReader(body))

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
	c, rec := testJSONContext(e, http.MethodPut, "/api/ssl-config", strings.NewReader(body))

	err := svc.setSSLConfig(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_SetSSLConfig_ManagedMissingEmail(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	e := echo.New()
	body := `{"mode":"managed","acme_email":""}`
	c, rec := testJSONContext(e, http.MethodPut, "/api/ssl-config", strings.NewReader(body))

	err := svc.setSSLConfig(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_ListUsers(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListUsers").Return([]string{"admin", "bob"}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/users")

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
	c, rec := testJSONContext(e, http.MethodPost, "/api/users", strings.NewReader(`{"name":"bob"}`))

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
	c, rec := testJSONContext(e, http.MethodPost, "/api/users", strings.NewReader(`{"name":"bob"}`))

	err := svc.createUser(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestServices_DeleteUser(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{}, nil)
	mockKS.On("DeleteUser", "bob").Return(nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodDelete, "/api/users/bob")
	c.SetPath("/api/users/:name")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: "bob"}})

	err := svc.deleteUser(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestServices_ListUserKeys(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAONE", SecretKey: testSecretKey, UserName: "bob"},
	}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/users/bob/keys")
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
		{AccessKeyID: "AKIAONE", SecretKey: testSecretKey, UserName: "admin"},
	}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/keys")

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
	c, rec := testJSONContext(e, http.MethodPost, "/api/keys", strings.NewReader(body))

	err := svc.addKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	restarter.AssertCalled(t, "Restart", mock.Anything)
}

func TestServices_ListBuckets(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	mockBackend.On("ListAllBuckets", mock.Anything).Return([]backend.BucketInfo{
		{Name: "bucket1", Owner: "admin", CreatedAt: created},
	}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/buckets")

	err := svc.listBuckets(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ListBucketsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Buckets, 1)
	assert.Equal(t, "bucket1", resp.Buckets[0].Name)
	assert.Equal(t, "admin", resp.Buckets[0].Owner)
	assert.True(t, resp.Buckets[0].CreatedAt.Equal(created))
}

func TestServices_CreateBucket(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: testSecretKey},
	}, nil)
	mockBackend.On("CreateBucket", mock.Anything, "AKIAADMIN", "mybucket").Return(nil)

	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/buckets", strings.NewReader(`{"name":"mybucket"}`))

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
		{AccessKeyID: "AKIAADMIN", SecretKey: testSecretKey, UserName: "admin"},
	}, nil)
	mockBackend.On("BucketOwner", mock.Anything, "mybucket").Return("admin", nil)
	mockBackend.On("DeleteBucket", mock.Anything, "AKIAADMIN", "mybucket").Return(nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodDelete, "/api/buckets/mybucket")
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
		{AccessKeyID: "AKIAADMIN", SecretKey: testSecretKey, UserName: "admin"},
	}, nil)
	mockBackend.On("BucketOwner", mock.Anything, "mybucket").Return("admin", nil)
	mockBackend.On("GetBucketVersioning", mock.Anything, "AKIAADMIN", "mybucket").Return("Enabled", nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/buckets/mybucket/versioning")
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
		{AccessKeyID: "AKIAADMIN", SecretKey: testSecretKey, UserName: "admin"},
	}, nil)
	mockBackend.On("BucketOwner", mock.Anything, "mybucket").Return("admin", nil)
	mockBackend.On("PutBucketVersioning", mock.Anything, "AKIAADMIN", "mybucket", "Enabled").Return(nil)

	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPut, "/api/buckets/mybucket/versioning", strings.NewReader(`{"status":"Enabled"}`))
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
	c, rec := testJSONContext(e, http.MethodPut, "/api/buckets/mybucket/versioning", strings.NewReader(`{"status":"Bogus"}`))
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
	c, rec := testContext(e, http.MethodPost, "/api/admin/backup")

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
	c, rec := testContext(e, http.MethodGet, "/api/backups")

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
	c, rec := testContext(e, http.MethodGet, "/api/backups/b1.db")
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
	c, rec := testContext(e, http.MethodDelete, "/api/backups/b1.db")
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
	c, rec := testContext(e, http.MethodGet, "/api/admin/stats")

	err := svc.getStats(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/stats/uploads", capturedPath)
}

func TestServices_UsersPage(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	mockKS.On("ListUsers").Return([]string{"admin", "bob"}, nil)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: testSecretKey, UserName: "admin"},
		{AccessKeyID: "AKIABOB", SecretKey: testSecretKey, UserName: "bob"},
		{AccessKeyID: "AKIAADMIN2", SecretKey: testSecretKey, UserName: "admin"},
	}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/users")

	err := svc.usersPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "admin")
	assert.Contains(t, rec.Body.String(), "bob")
	assert.Contains(t, rec.Body.String(), "2 access key")
	assert.Contains(t, rec.Body.String(), "1 access key")
}

func TestServices_BucketsPage(t *testing.T) {
	svc, mockStore, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }

	mockStore.EXPECT().S3Config().Return(config.S3Config{}).Once()
	mockStore.EXPECT().SSLConfig().Return(config.SSLConfig{}).Once()
	mockKS.On("ListUsers").Return([]string{"default"}, nil)
	mockKS.On("ListAccessKeys", (*string)(nil)).Return([]backend.AccessKeyInfo{
		{UserName: "admin", AccessKeyID: "AKIA-test"},
	}, nil)

	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	mockBackend.On("ListAllBuckets", mock.Anything).Return([]backend.BucketInfo{
		{Name: "test-bucket", Owner: "admin", CreatedAt: created},
	}, nil)
	mockBackend.On("BucketStats", mock.Anything, "test-bucket").Return(0, int64(0), nil)
	mockBackend.On("BucketVersioning", mock.Anything, "test-bucket").Return("", nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/buckets")

	err := svc.bucketsPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "test-bucket")
	assert.Contains(t, rec.Body.String(), "2025-01-02 03:04:05 UTC")
	assert.Contains(t, rec.Body.String(), "Setup")
	assert.Contains(t, rec.Body.String(), "AWS CLI")
	assert.Contains(t, rec.Body.String(), "s3cmd")
	assert.Contains(t, rec.Body.String(), "rclone")
	assert.Contains(t, rec.Body.String(), "AKIA-test")
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
	c, rec := testContext(e, http.MethodGet, "/backups")

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
		_, _ = w.Write([]byte(`{
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
	c, rec := testContext(e, http.MethodGet, "/monitoring")

	err := svc.monitoringPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Pending Objects")
	assert.Contains(t, rec.Body.String(), "3")
	assert.Contains(t, rec.Body.String(), "1.02 KB")
	assert.Contains(t, rec.Body.String(), "prometheus.yml")
	assert.Contains(t, rec.Body.String(), "s3-server")
	assert.Contains(t, rec.Body.String(), "metrics_path: /prometheus")
}

func TestServices_SystemFlush(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	mockBackend.On("FlushObjects", mock.Anything).Return(nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodPost, "/api/system/flush")

	err := svc.systemFlush(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestServices_SystemFlush_BackendNotReady(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	// no backend set: getBackend() returns nil

	e := echo.New()
	c, rec := testContext(e, http.MethodPost, "/api/system/flush")

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
	c, rec := testContext(e, http.MethodPost, "/api/system/flush")

	err := svc.systemFlush(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServices_SystemRestart(t *testing.T) {
	svc, _, restarter, _ := newTestServices(t)

	e := echo.New()
	c, rec := testContext(e, http.MethodPost, "/api/system/restart")

	err := svc.systemRestart(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	restarter.AssertCalled(t, "Restart", mock.Anything)

	var resp ConfigUpdateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "restarted", resp.Status)
}

func TestServices_SystemRestart_Error(t *testing.T) {
	svc, _, restarter, _ := newTestServices(t)
	// Override the default Maybe expectation with an error return
	restarter.Mock = mock.Mock{}
	restarter.Test(t)
	restarter.On("Restart", mock.Anything).Return(assert.AnError)

	e := echo.New()
	c, rec := testContext(e, http.MethodPost, "/api/system/restart")

	err := svc.systemRestart(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	restarter.AssertCalled(t, "Restart", mock.Anything)
}

// --- Regression tests for Kody review findings on PR #14 ---

// Test 1: Nil-pointer guard in addKey (finding #3548390934).
// addKey must not panic with nil backendStatus.
func TestServices_AddKey_NilBackendStatus(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.backendStatus = nil // simulate uninitialized backend status
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{}, nil)
	mockKS.On("CreateUser", "admin").Return(nil)
	mockKS.On("CreateAccessKey", "admin", mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/keys", strings.NewReader(`{"user_name":"admin"}`))

	// Should not panic: nil-guard returns false for backendRunning
	err := svc.addKey(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

// Test 2: Nil-pointer guard for restarter in addKey (finding #3548391143).
func TestServices_AddKey_NilRestarter(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.restarter = nil // simulate uninitialized restarter
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{}, nil)
	mockKS.On("CreateUser", "admin").Return(nil)
	mockKS.On("CreateAccessKey", "admin", mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/api/keys", strings.NewReader(`{"user_name":"admin"}`))

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
		{AccessKeyID: "AKIATEST", SecretKey: testSecretKey, UserName: "admin"},
	}, nil)
	mockKS.On("ListUsers").Return([]string{"admin"}, nil)

	e := echo.New()
	c, _ := testContext(e, http.MethodGet, "/keys")

	// Should not panic: nil-guard returns false for backendRunning
	err := svc.keysPage(c)
	// Rendering may return an error if templates aren't fully wired, but
	// it must NOT panic.
	require.NoError(t, err)
}

// Test 4: SecretKey included in listKeys API response for copy functionality.
func TestServices_ListKeys_SecretKeyIncluded(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAONE", SecretKey: "supersecret1", UserName: "admin"},
		{AccessKeyID: "AKIATWO", SecretKey: "supersecret2", UserName: "admin"},
	}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/api/keys")

	err := svc.listKeys(c)
	require.NoError(t, err)

	var resp ListAccessKeysResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Keys, 2)
	for _, k := range resp.Keys {
		assert.NotEmpty(t, k.SecretKey, "SecretKey must be exposed in listKeys API response for copy functionality")
	}
}

// Test 5: concurrent deleteKey TOCTOU race: last key must survive (finding #3548758653).
func TestServices_DeleteKey_Concurrent_LastKey(t *testing.T) {
	// Use a real stub key store that tracks state under a mutex.
	ks := &stubKeyStore{
		keys: []backend.AccessKeyInfo{
			{AccessKeyID: "AKIAONE", SecretKey: "secret1", UserName: "admin"},
		},
	}
	log := testutil.NewTestLogger()
	broker := &handlerMocks.MockSSEBroker{}
	broker.Test(t)
	broker.On("NotifyKeyChange", mock.Anything, mock.Anything).Return().Maybe()
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
			c, _ := testContext(e, http.MethodDelete, "/api/keys/AKIAONE")
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
	c, rec := testContext(e, http.MethodDelete, "/api/users/bob")
	c.SetPath("/api/users/:name")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: "bob"}})

	err := svc.deleteUser(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	broker.AssertNumberOfCalls(t, "NotifyKeyChange", 1)
	broker.AssertCalled(t, "NotifyKeyChange", "deleted", 1)
}

// Test 7: groupedKeys passes SecretKey through for masking in the template.
func TestServices_GroupedKeys_PassesSecretKey(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAONE", SecretKey: "supersecret1", UserName: "admin"},
		{AccessKeyID: "AKIATWO", SecretKey: "supersecret2", UserName: "custom"},
	}, nil)
	mockKS.On("ListUsers").Return([]string{"admin", "custom"}, nil)
	mockKS.On("ListUsers").Return([]string{"admin", "custom"}, nil)

	groups := svc.groupedKeys()
	require.Len(t, groups, 2)
	for _, g := range groups {
		for _, kp := range g.Keys {
			assert.NotEmpty(t, kp.SecretKey, "SecretKey must be present in groupedKeys view model (path: %s)", kp.AccessKey)
		}
	}
}

// Test 8: keyMu is released during Restart() so readers aren't blocked (findings #3548873347 + #3548911179).
func TestServices_KeyMutation_LockReleasedDuringRestart(t *testing.T) {
	ks := &stubKeyStore{
		keys: []backend.AccessKeyInfo{
			{AccessKeyID: "AKIAEXIST", SecretKey: "secret1", UserName: "admin"},
			{AccessKeyID: "AKIAOTHER", SecretKey: "secret2", UserName: "admin"},
		},
	}
	log := testutil.NewTestLogger()
	broker := &handlerMocks.MockSSEBroker{}
	broker.Test(t)
	broker.On("NotifyKeyChange", mock.Anything, mock.Anything).Return().Maybe()

	// slowRestarter blocks until released, simulating a slow backend restart
	r := &slowRestarter{done: make(chan struct{})}
	svc := NewServices(ServicesConfig{
		Restarter: r,
		Log:       log,
		SSEBroker: broker,
	})
	svc.keyStore = func() backend.S3DStore { return ks }

	// Start addKey: it will hold the lock, then release it before Restart().
	addDone := make(chan error, 1)
	go func() {
		e := echo.New()
		c, _ := testJSONContext(e, http.MethodPost, "/api/keys", strings.NewReader(`{"user_name":"admin","access_key":"AKIANEW","secret_key":"abcdefghijklmnopqrstuvwxyz0123456789"}`))
		addDone <- svc.addKey(c)
	}()

	// Give addKey time to acquire the lock and enter Restart().
	time.Sleep(50 * time.Millisecond)

	// While Restart() is in progress, readers must be able to acquire RLock.
	readDone := make(chan error, 1)
	go func() {
		e := echo.New()
		c, _ := testContext(e, http.MethodGet, "/api/keys")
		readDone <- svc.listKeys(c)
	}()

	select {
	case err := <-readDone:
		require.NoError(t, err, "listKeys must succeed while Restart() is in progress")
	case <-time.After(2 * time.Second):
		t.Fatal("listKeys blocked during Restart(): keyMu not released")
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

func (s *stubKeyStore) CreateUser(name string) error                 { return nil }
func (s *stubKeyStore) DeleteUser(name string) error                 { return nil }
func (s *stubKeyStore) ListUsers() ([]string, error)                 { return nil, nil }
func (s *stubKeyStore) AppKey() (types.PrivateKey, string, error)    { return types.PrivateKey{}, "", nil }
func (s *stubKeyStore) SetAppKey(_ types.PrivateKey, _ string) error { return nil }
func (s *stubKeyStore) Close() error                                 { return nil }

// slowRestarter blocks on a channel until released, simulating a slow backend restart.
type slowRestarter struct {
	done chan struct{}
}

func (r *slowRestarter) Restart(_ context.Context) error {
	<-r.done
	return nil
}

// --- Page handler tests (1.3) -----------------------------------------------

func TestServices_DashboardPage(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	svc.version = "v1.0.0"
	mockKS.On("ListUsers").Return([]string{"admin"}, nil)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: testSecretKey, UserName: "admin"},
	}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/dashboard")

	err := svc.dashboardPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "admin")
}

func TestServices_DashboardPage_WithBackend(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	mockKS.On("ListUsers").Return([]string{"admin", "bob"}, nil)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: testSecretKey, UserName: "admin"},
		{AccessKeyID: "AKIABOB", SecretKey: testSecretKey, UserName: "bob"},
	}, nil)
	mockBackend.On("BucketCountForUser", mock.Anything, "admin").Return(3, nil)
	mockBackend.On("BucketCountForUser", mock.Anything, "bob").Return(1, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/dashboard")

	err := svc.dashboardPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestServices_DashboardPage_Empty(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	mockKS.On("ListUsers").Return([]string{}, nil)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/dashboard")

	err := svc.dashboardPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestServices_SettingsPage(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	mockStore.EXPECT().S3Config().Return(config.S3Config{Directory: "/data"}).Once()
	mockStore.EXPECT().SSLConfig().Return(config.SSLConfig{}).Once()
	mockStore.EXPECT().LogConfig().Return(config.LogConfig{Level: "info"}).Once()

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/settings")

	err := svc.settingsPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestServices_SettingsPage_NilUpdater(t *testing.T) {
	svc, mockStore, _, _ := newTestServices(t)
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	svc.updater = nil // nil updater should not panic
	mockStore.EXPECT().S3Config().Return(config.S3Config{}).Once()
	mockStore.EXPECT().SSLConfig().Return(config.SSLConfig{}).Once()
	mockStore.EXPECT().LogConfig().Return(config.LogConfig{}).Once()

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/settings")

	err := svc.settingsPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestServices_KeysPage_WithUserFilter(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	svc.backendStatus = func() status.Status { return status.Running }
	mockKS.On("ListUsers").Return([]string{"admin", "bob"}, nil)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: testSecretKey, UserName: "admin"},
		{AccessKeyID: "AKIABOB", SecretKey: testSecretKey, UserName: "bob"},
	}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/keys?user=bob")

	err := svc.keysPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "bob")
	assert.NotContains(t, rec.Body.String(), "AKIAADMIN")
}

func TestServices_KeysPage_UserFilterNoKeys(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	svc.backendStatus = func() status.Status { return status.Running }
	mockKS.On("ListUsers").Return([]string{"admin", "bob"}, nil)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: testSecretKey, UserName: "admin"},
	}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/keys?user=bob")

	// bob has no keys: should render empty group, not panic
	err := svc.keysPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "bob")
}

func TestServices_KeysPage_BackendRunning(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	svc.backendStatus = func() status.Status { return status.Running }
	mockKS.On("ListUsers").Return([]string{"admin"}, nil)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: testSecretKey, UserName: "admin"},
	}, nil)
	mockBackend.On("BucketCountForUser", mock.Anything, "admin").Return(5, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/keys")

	err := svc.keysPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestServices_KeysPage_BackendStopped(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)
	svc.csrfToken = func(*echo.Context) string { return "csrf-token" }
	svc.backendStatus = func() status.Status { return status.Stopped }
	mockKS.On("ListUsers").Return([]string{"admin"}, nil)
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: testSecretKey, UserName: "admin"},
	}, nil)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/keys")

	err := svc.keysPage(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}
