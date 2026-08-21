package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/api"
	"go.lumeweb.com/s3-server/internal/backend"
	backendMocks "go.lumeweb.com/s3-server/internal/backend/mocks"
	handlerMocks "go.lumeweb.com/s3-server/internal/handlers/mocks"
	storeMocks "go.lumeweb.com/s3-server/internal/store/mocks"
	"go.lumeweb.com/s3-server/internal/testutil"
)

// --- requireKeyStore ---

func TestServices_RequireKeyStore_Success(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)

	e := echo.New()
	c, _ := testContext(e, http.MethodGet, "/")

	ks, err := svc.requireKeyStore(c)
	require.NoError(t, err)
	assert.Equal(t, mockKS, ks)
}

func TestServices_RequireKeyStore_NotInitialized(t *testing.T) {
	mockStore := storeMocks.NewMockStore(t)
	restarter := &handlerMocks.MockBackendRestarter{}
	restarter.Test(t)
	restarter.On("Restart", mock.Anything).Return(nil).Maybe()
	broker := &handlerMocks.MockSSEBroker{}
	broker.Test(t)
	broker.On("NotifyKeyChange", mock.Anything, mock.Anything).Return().Maybe()
	broker.On("NotifyBucketChange", mock.Anything, mock.Anything).Return().Maybe()
	broker.On("PublishFlush", mock.Anything).Return(nil).Maybe() // mock stub for SSE flush event, not a secret

	svc := NewServices(ServicesConfig{
		Store:     mockStore,
		Restarter: restarter,
		Log:       testutil.NewTestLogger(),
		SSEBroker: broker,
	})
	// keyStore not set → getKeyStore returns nil

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/")

	ks, err := svc.requireKeyStore(c)
	assert.Nil(t, ks)
	assert.ErrorIs(t, err, errResponseSent)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, string(api.TypeBackendNotInitialized), body["type"])
}

// --- requireBackendOnly ---

func TestServices_RequireBackendOnly_NotInitialized(t *testing.T) {
	svc, _, _, _ := newTestServices(t)
	// backend not set → getBackend returns nil

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/")

	b, err := svc.requireBackendOnly(c)
	assert.Nil(t, b)
	assert.ErrorIs(t, err, errResponseSent)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// --- requireParam ---

func TestServices_RequireParam_Success(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	e := echo.New()
	c, _ := testContext(e, http.MethodGet, "/")
	c.SetPath("/buckets/:name")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: "my-bucket"}})

	val, err := svc.requireParam(c, "name")
	require.NoError(t, err)
	assert.Equal(t, "my-bucket", val)
}

func TestServices_RequireParam_Missing(t *testing.T) {
	svc, _, _, _ := newTestServices(t)

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/")
	// No param set

	val, err := svc.requireParam(c, "name")
	assert.Empty(t, val)
	assert.ErrorIs(t, err, errResponseSent)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, string(api.TypeNameRequired), body["type"])
}

// --- bindJSON ---

func TestBindJSON_Success(t *testing.T) {
	e := echo.New()
	c, _ := testJSONContext(e, http.MethodPost, "/", strings.NewReader(`{"name":"test","value":42}`))

	type payload struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	var dst payload
	err := bindJSON(c, testutil.NewTestLogger(), &dst)
	require.NoError(t, err)
	assert.Equal(t, "test", dst.Name)
	assert.Equal(t, 42, dst.Value)
}

func TestBindJSON_InvalidJSON(t *testing.T) {
	e := echo.New()
	c, rec := testJSONContext(e, http.MethodPost, "/", strings.NewReader(`{invalid json}`))

	var dst map[string]any
	err := bindJSON(c, testutil.NewTestLogger(), &dst)
	assert.ErrorIs(t, err, errResponseSent)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, string(api.TypeInvalidRequestBody), body["type"])
}

func TestBindJSON_EmptyBody(t *testing.T) {
	e := echo.New()
	c, _ := testJSONContext(e, http.MethodPost, "/", strings.NewReader(``))

	var dst map[string]any
	// Echo v5 Bind succeeds on empty body (zero value) — no error
	err := bindJSON(c, testutil.NewTestLogger(), &dst)
	assert.NoError(t, err)
	assert.Nil(t, dst)
}

// --- captureRecorder ---

func TestCaptureRecorder(t *testing.T) {
	r := &captureRecorder{
		header: http.Header{},
	}

	r.WriteHeader(http.StatusTeapot)
	assert.Equal(t, http.StatusTeapot, r.code)

	n, err := r.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", r.body.String())

	r.Header().Set("X-Custom", "value")
	assert.Equal(t, "value", r.Header().Get("X-Custom"))
}

// --- requireBackend (with backend set) ---

func TestServices_RequireBackend_NoAccessKeys(t *testing.T) {
	svc, _, _, mockKS := newTestServices(t)

	// Set a backend that returns non-nil
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }

	// listAccessKeys calls keyStore.ListAccessKeys(nil) → empty → adminAccessKey error
	mockKS.On("ListAccessKeys", (*string)(nil)).Return([]backend.AccessKeyInfo{}, nil).Maybe()

	e := echo.New()
	c, rec := testContext(e, http.MethodGet, "/")

	b, key, err := svc.requireBackend(c)
	assert.Nil(t, b)
	assert.Empty(t, key)
	assert.ErrorIs(t, err, errResponseSent)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
