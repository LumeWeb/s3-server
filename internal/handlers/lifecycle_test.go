package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SiaFoundation/s3d/s3"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/s3-server/internal/backend"
	backendMocks "go.lumeweb.com/s3-server/internal/backend/mocks"
)

// setupLifecycleServices creates a test Services with a MockBackend wired in.
// If withKey is true, the key store is configured to return a single access key
// (needed for tests that reach adminAccessKey()).
func setupLifecycleServices(t *testing.T, withKey bool) (*Services, *backendMocks.MockBackend, *backendMocks.MockS3DStore) {
	svc, _, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }
	if withKey {
		mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
			{AccessKeyID: "AKIATEST", SecretKey: "secret"},
		}, nil)
	}
	return svc, mockBackend, mockKS
}

func lifecycleContext(method, bucket, body string) (*echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	var req *http.Request
	path := "/api/buckets/" + bucket + "/lifecycle"
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/buckets/:name/lifecycle")
	c.SetPathValues(echo.PathValues{{Name: "name", Value: bucket}})
	return c, rec
}

func TestServices_GetBucketLifecycle_OK(t *testing.T) {
	svc, mockBackend, _ := setupLifecycleServices(t, true)

	allObj := "*"
	mockBackend.On("GetBucketLifecycleConfiguration", mock.Anything, "AKIATEST", "my-bucket").
		Return(s3.LifecycleConfiguration{
			Rules: []s3.LifecycleRule{
				{
					ID:     "rule-1",
					Status: s3.LifecycleStatusEnabled,
					Filter: &s3.LifecycleFilter{Prefix: &allObj},
					Expiration: &s3.LifecycleExpiration{Days: 30},
				},
			},
		}, nil)

	c, rec := lifecycleContext(http.MethodGet, "my-bucket", "")

	err := svc.getBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp LifecycleConfigJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Rules, 1)
	assert.Equal(t, "rule-1", resp.Rules[0].ID)
	assert.Equal(t, "Enabled", resp.Rules[0].Status)
	assert.Equal(t, "*", resp.Rules[0].Prefix)
	assert.Equal(t, 30, resp.Rules[0].ExpirationDays)
}

func TestServices_GetBucketLifecycle_Empty(t *testing.T) {
	svc, mockBackend, _ := setupLifecycleServices(t, true)

	mockBackend.On("GetBucketLifecycleConfiguration", mock.Anything, "AKIATEST", "empty-bucket").
		Return(s3.LifecycleConfiguration{}, nil)

	c, rec := lifecycleContext(http.MethodGet, "empty-bucket", "")

	err := svc.getBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp LifecycleConfigJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Rules)
}

func TestServices_PutBucketLifecycle_OK(t *testing.T) {
	svc, mockBackend, _ := setupLifecycleServices(t, true)

	mockBackend.On("PutBucketLifecycleConfiguration", mock.Anything, "AKIATEST", "my-bucket", mock.AnythingOfType("s3.LifecycleConfiguration")).
		Return(nil).Run(func(args mock.Arguments) {
		cfg := args.Get(3).(s3.LifecycleConfiguration)
		require.Len(t, cfg.Rules, 1)
		assert.Equal(t, s3.LifecycleStatusEnabled, cfg.Rules[0].Status)
		assert.NotNil(t, cfg.Rules[0].Expiration)
		assert.Equal(t, 30, cfg.Rules[0].Expiration.Days)
	})

	body := `{"rules":[{"id":"rule-1","status":"Enabled","prefix":"logs/","expiration_days":30}]}`
	c, rec := lifecycleContext(http.MethodPut, "my-bucket", body)

	err := svc.putBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp LifecycleConfigJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Rules, 1)
	assert.Equal(t, "rule-1", resp.Rules[0].ID)
	assert.Equal(t, "logs/", resp.Rules[0].Prefix)
	assert.Equal(t, 30, resp.Rules[0].ExpirationDays)
}

func TestServices_PutBucketLifecycle_InvalidStatus(t *testing.T) {
	svc, _, _ := setupLifecycleServices(t, false)

	body := `{"rules":[{"status":"Maybe","prefix":"","expiration_days":30}]}`
	c, rec := lifecycleContext(http.MethodPut, "my-bucket", body)

	err := svc.putBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_PutBucketLifecycle_InvalidExpiration(t *testing.T) {
	svc, _, _ := setupLifecycleServices(t, false)

	body := `{"rules":[{"status":"Enabled","prefix":"","expiration_days":0}]}`
	c, rec := lifecycleContext(http.MethodPut, "my-bucket", body)

	err := svc.putBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestServices_PutBucketLifecycle_MissingName(t *testing.T) {
	svc, _, _ := setupLifecycleServices(t, false)

	body := `{"rules":[]}`
	c, rec := lifecycleContext(http.MethodPut, "", body)

	err := svc.putBucketLifecycle(c)
	require.ErrorIs(t, err, errResponseSent)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServices_DeleteBucketLifecycle_OK(t *testing.T) {
	svc, mockBackend, _ := setupLifecycleServices(t, true)

	mockBackend.On("DeleteBucketLifecycleConfiguration", mock.Anything, "AKIATEST", "my-bucket").
		Return(nil)

	c, rec := lifecycleContext(http.MethodDelete, "my-bucket", "")

	err := svc.deleteBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestServices_GetBucketLifecycle_NoBackend(t *testing.T) {
	svc, _, _ := setupLifecycleServices(t, false)
	svc.backend = func() backend.Backend { return nil }

	c, rec := lifecycleContext(http.MethodGet, "my-bucket", "")

	err := svc.getBucketLifecycle(c)
	require.ErrorIs(t, err, errResponseSent)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestServices_DeleteBucketLifecycle_MissingName(t *testing.T) {
	svc, _, _ := setupLifecycleServices(t, false)

	c, rec := lifecycleContext(http.MethodDelete, "", "")

	err := svc.deleteBucketLifecycle(c)
	require.ErrorIs(t, err, errResponseSent)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServices_GetBucketLifecycle_BackendError(t *testing.T) {
	svc, mockBackend, _ := setupLifecycleServices(t, true)

	mockBackend.On("GetBucketLifecycleConfiguration", mock.Anything, "AKIATEST", "my-bucket").
		Return(s3.LifecycleConfiguration{}, assert.AnError)

	c, rec := lifecycleContext(http.MethodGet, "my-bucket", "")

	err := svc.getBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
