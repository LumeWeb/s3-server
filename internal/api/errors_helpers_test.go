package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSendConflict(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SendConflict(c, TypeConflict, "resource already exists")
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"CONFLICT"`)
	assert.Contains(t, rec.Body.String(), `"type":"CONFLICT"`)
	assert.Contains(t, rec.Body.String(), `"message":"resource already exists"`)
}

func TestSendValidation(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SendValidation(c, TypePasswordTooShort, "password must be at least 8 characters")
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"VALIDATION_ERROR"`)
	assert.Contains(t, rec.Body.String(), `"type":"PASSWORD_TOO_SHORT"`)
}

func TestSendNotReady(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SendNotReady(c, TypeBackendNotInitialized, "backend not ready", errors.New("init failed"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"NOT_READY"`)
	assert.Contains(t, rec.Body.String(), `"type":"BACKEND_NOT_INITIALIZED"`)
}

func TestSendNotReady_NilErr(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SendNotReady(c, TypeBackendNotInitialized, "backend not ready", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestSetLogger(t *testing.T) {
	log := zap.NewNop()
	SetLogger(log)
	// SetLogger just assigns; verify it doesn't panic.
	// The logger is used by SendError for logging; calling SendError after
	// SetLogger verifies the logger path works.
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	_ = SendBadRequest(c, TypeInvalidRequestBody, "bad input")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
