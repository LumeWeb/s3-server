package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
)

func TestError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		expected string
	}{
		{
			name:     "without wrapped error",
			err:      NewError(ErrBadRequest, TypeInvalidRequestBody, "bad input", nil),
			expected: "BAD_REQUEST: bad input",
		},
		{
			name:     "with wrapped error",
			err:      NewError(ErrInternal, TypeDatabaseOpenFailed, "db failed", errors.New("connection refused")),
			expected: "INTERNAL_ERROR: db failed: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	err := NewError(ErrInternal, TypeDatabaseOpenFailed, "msg", inner)
	assert.Equal(t, inner, err.Unwrap())

	errNoWrap := NewError(ErrBadRequest, TypeInvalidRequestBody, "msg", nil)
	assert.Nil(t, errNoWrap.Unwrap())
}

func TestError_HttpStatus(t *testing.T) {
	tests := []struct {
		code     ErrorCode
		expected int
	}{
		{ErrUnauthorized, http.StatusUnauthorized},
		{ErrOnboardingRequired, http.StatusUnauthorized},
		{ErrForbidden, http.StatusForbidden},
		{ErrBadRequest, http.StatusBadRequest},
		{ErrInvalidAppKey, http.StatusBadRequest},
		{ErrDecryptionFailed, http.StatusBadRequest},
		{ErrNotFound, http.StatusNotFound},
		{ErrConflict, http.StatusConflict},
		{ErrOnboardingComplete, http.StatusConflict},
		{ErrNotReady, http.StatusServiceUnavailable},
		{ErrValidation, http.StatusUnprocessableEntity},
		{ErrInternal, http.StatusInternalServerError},
		{ErrorCode("UNKNOWN"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			err := NewError(tt.code, "", "msg", nil)
			assert.Equal(t, tt.expected, err.HttpStatus())
		})
	}
}

func TestSendError_WritesJSON(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SendError(c, ErrInternal, TypeDatabaseOpenFailed, "something broke", errors.New("detail"))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"INTERNAL_ERROR"`)
	assert.Contains(t, rec.Body.String(), `"type":"DATABASE_OPEN_FAILED"`)
	assert.Contains(t, rec.Body.String(), `"message":"something broke"`)
}

func TestSendBadRequest(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SendBadRequest(c, TypeInvalidRequestBody, "invalid input")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSendNotFound(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SendNotFound(c, TypeBackupNotFound, "not here")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSendUnauthorized(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SendUnauthorized(c, TypeAuthRequired, "nope")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSendInternal(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := SendInternal(c, TypeDatabaseOpenFailed, "boom", errors.New("detail"))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
