package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SiaFoundation/s3d/s3"
	"github.com/SiaFoundation/s3d/s3/s3errs"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/s3-server/internal/api"
	"go.lumeweb.com/s3-server/internal/backend"
	backendMocks "go.lumeweb.com/s3-server/internal/backend/mocks"
)

// setupLifecycleServices creates a test Services with a MockBackend wired in.
// If withKey is true, the key store is configured to return a single access key
// (needed for tests that reach adminAccessKey()). BucketOwner is mocked to
// return an empty owner so accessKeyForBucket falls back to the admin key.
func setupLifecycleServices(t *testing.T, withKey bool) (*Services, *backendMocks.MockBackend, *backendMocks.MockS3DStore) {
	svc, _, _, mockKS := newTestServices(t)
	mockBackend := backendMocks.NewMockBackend(t)
	svc.backend = func() backend.Backend { return mockBackend }
	if withKey {
		mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
			{AccessKeyID: "AKIATEST", SecretKey: "secret", UserName: "admin"},
		}, nil)
		mockBackend.On("BucketOwner", mock.Anything, mock.Anything).Return("admin", nil)
	}
	return svc, mockBackend, mockKS
}

func lifecycleContext(method, bucket, body string) (*echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	path := "/api/buckets/" + bucket + "/lifecycle"
	var c *echo.Context
	var rec *httptest.ResponseRecorder
	if body != "" {
		c, rec = testJSONContext(e, method, path, strings.NewReader(body))
	} else {
		c, rec = testContext(e, method, path)
	}
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
					ID:         "rule-1",
					Status:     s3.LifecycleStatusEnabled,
					Filter:     &s3.LifecycleFilter{Prefix: &allObj},
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

// --- Regression tests ---

// TestRepro_GetLifecycle_UsesGetErrorType verifies that the GET lifecycle
// handler returns LIFECYCLE_GET_FAILED (not LIFECYCLE_PUT_FAILED) when the
// backend returns a non-ErrNoSuchLifecycleConfiguration error. Before the
// fix, the GET handler reused the PUT error type, causing the frontend to
// show "Failed to save the lifecycle configuration." even though the user
// only clicked the tab (a GET, not a save).
func TestRepro_GetLifecycle_UsesGetErrorType(t *testing.T) {
	svc, mockBackend, _ := setupLifecycleServices(t, true)

	mockBackend.On("GetBucketLifecycleConfiguration", mock.Anything, "AKIATEST", "my-bucket").
		Return(s3.LifecycleConfiguration{}, assert.AnError)

	c, rec := lifecycleContext(http.MethodGet, "my-bucket", "")

	err := svc.getBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var resp api.Error
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, api.ErrInternal, resp.Code)
	assert.Equal(t, api.TypeLifecycleGetFailed, resp.Type,
		"GET lifecycle must use LIFECYCLE_GET_FAILED, not LIFECYCLE_PUT_FAILED")
	assert.NotEqual(t, api.TypeLifecyclePutFailed, resp.Type,
		"GET lifecycle must not reuse the PUT error type")
}

// TestRepro_GetLifecycle_NoSuchConfigurationReturnsEmpty verifies that when
// the backend returns ErrNoSuchLifecycleConfiguration (bucket has no lifecycle
// rules), the handler returns 200 OK with an empty config, not a 500 error.
func TestRepro_GetLifecycle_NoSuchConfigurationReturnsEmpty(t *testing.T) {
	svc, mockBackend, _ := setupLifecycleServices(t, true)

	mockBackend.On("GetBucketLifecycleConfiguration", mock.Anything, "AKIATEST", "my-bucket").
		Return(s3.LifecycleConfiguration{}, s3errs.ErrNoSuchLifecycleConfiguration)

	c, rec := lifecycleContext(http.MethodGet, "my-bucket", "")

	err := svc.getBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp LifecycleConfigJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Rules)
}

// TestRepro_GetLifecycle_ResolvesBucketOwnerAccessKey verifies that the
// lifecycle GET handler resolves the bucket owner's access key (not the
// admin key) so s3d ownership checks pass. Before the fix, the handler
// used the first admin key, which caused ErrAccessDenied for buckets
// owned by non-admin users.
func TestRepro_GetLifecycle_ResolvesBucketOwnerAccessKey(t *testing.T) {
	svc, mockBackend, mockKS := setupLifecycleServices(t, false)

	// Two users: "admin" and "alice"
	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: "secret", UserName: "admin"},
		{AccessKeyID: "AKIAALICE", SecretKey: "secret", UserName: "alice"},
	}, nil)

	// Bucket "alice-bucket" is owned by "alice"
	mockBackend.On("BucketOwner", mock.Anything, "alice-bucket").Return("alice", nil)

	// The lifecycle call must use ALICE's key, not ADMIN's
	mockBackend.On("GetBucketLifecycleConfiguration", mock.Anything, "AKIAALICE", "alice-bucket").
		Return(s3.LifecycleConfiguration{}, nil)

	c, rec := lifecycleContext(http.MethodGet, "alice-bucket", "")

	err := svc.getBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	mockBackend.AssertCalled(t, "GetBucketLifecycleConfiguration",
		mock.Anything, "AKIAALICE", "alice-bucket")
}

// TestRepro_PutLifecycle_ResolvesBucketOwnerAccessKey verifies that PUT
// lifecycle also resolves the bucket owner's access key.
func TestRepro_PutLifecycle_ResolvesBucketOwnerAccessKey(t *testing.T) {
	svc, mockBackend, mockKS := setupLifecycleServices(t, false)

	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: "secret", UserName: "admin"},
		{AccessKeyID: "AKIAALICE", SecretKey: "secret", UserName: "alice"},
	}, nil)
	mockBackend.On("BucketOwner", mock.Anything, "alice-bucket").Return("alice", nil)
	mockBackend.On("PutBucketLifecycleConfiguration", mock.Anything, "AKIAALICE", "alice-bucket", mock.AnythingOfType("s3.LifecycleConfiguration")).
		Return(nil)

	body := `{"rules":[{"status":"Enabled","prefix":"","expiration_days":30}]}`
	c, rec := lifecycleContext(http.MethodPut, "alice-bucket", body)

	err := svc.putBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	mockBackend.AssertCalled(t, "PutBucketLifecycleConfiguration",
		mock.Anything, "AKIAALICE", "alice-bucket", mock.AnythingOfType("s3.LifecycleConfiguration"))
}

// TestRepro_DeleteLifecycle_ResolvesBucketOwnerAccessKey verifies that DELETE
// lifecycle also resolves the bucket owner's access key.
func TestRepro_DeleteLifecycle_ResolvesBucketOwnerAccessKey(t *testing.T) {
	svc, mockBackend, mockKS := setupLifecycleServices(t, false)

	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: "secret", UserName: "admin"},
		{AccessKeyID: "AKIAALICE", SecretKey: "secret", UserName: "alice"},
	}, nil)
	mockBackend.On("BucketOwner", mock.Anything, "alice-bucket").Return("alice", nil)
	mockBackend.On("DeleteBucketLifecycleConfiguration", mock.Anything, "AKIAALICE", "alice-bucket").
		Return(nil)

	c, rec := lifecycleContext(http.MethodDelete, "alice-bucket", "")

	err := svc.deleteBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	mockBackend.AssertCalled(t, "DeleteBucketLifecycleConfiguration",
		mock.Anything, "AKIAALICE", "alice-bucket")
}

// TestRepro_GetLifecycle_BucketOwnerNoKeysFallsBackToAdmin verifies that when
// the bucket owner has no access keys (e.g. key was deleted during
// offboarding), accessKeyForBucket falls back to the admin key instead of
// returning a 503 error.
func TestRepro_GetLifecycle_BucketOwnerNoKeysFallsBackToAdmin(t *testing.T) {
	svc, mockBackend, mockKS := setupLifecycleServices(t, false)

	mockKS.On("ListAccessKeys", mock.Anything).Return([]backend.AccessKeyInfo{
		{AccessKeyID: "AKIAADMIN", SecretKey: "secret", UserName: "admin"},
	}, nil)
	// "bob" owns the bucket but has no access keys
	mockBackend.On("BucketOwner", mock.Anything, "bob-bucket").Return("bob", nil)
	// Should fall back to admin key
	mockBackend.On("GetBucketLifecycleConfiguration", mock.Anything, "AKIAADMIN", "bob-bucket").
		Return(s3.LifecycleConfiguration{}, nil)

	c, rec := lifecycleContext(http.MethodGet, "bob-bucket", "")

	err := svc.getBucketLifecycle(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	mockBackend.AssertCalled(t, "GetBucketLifecycleConfiguration",
		mock.Anything, "AKIAADMIN", "bob-bucket")
}
