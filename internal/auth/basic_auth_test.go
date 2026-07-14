package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	storemocks "go.lumeweb.com/s3-server/internal/store/mocks"
)

func TestBasicAuth_ValidCredentials(t *testing.T) {
	mockStore := storemocks.NewMockStore(t)
	mockStore.On("ValidateAdminPassword", "secret").Return(true)

	called := false
	h := BasicAuth(mockStore, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/prometheus", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	require.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBasicAuth_NoAuthHeader(t *testing.T) {
	mockStore := storemocks.NewMockStore(t)

	h := BasicAuth(mockStore, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/prometheus", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "Basic")
}

func TestBasicAuth_WrongUsername(t *testing.T) {
	mockStore := storemocks.NewMockStore(t)

	h := BasicAuth(mockStore, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/prometheus", nil)
	req.SetBasicAuth("user", "secret")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestBasicAuth_WrongPassword(t *testing.T) {
	mockStore := storemocks.NewMockStore(t)
	mockStore.On("ValidateAdminPassword", "wrong").Return(false)

	h := BasicAuth(mockStore, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/prometheus", nil)
	req.SetBasicAuth("admin", "wrong")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
