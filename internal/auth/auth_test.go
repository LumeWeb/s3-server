package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	storeMocks "go.lumeweb.com/s3-server/internal/store/mocks"
	"go.lumeweb.com/s3-server/internal/routes"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestAuth(t *testing.T) (*Auth, *storeMocks.MockStore) {
	mockStore := storeMocks.NewMockStore(t)
	return New(mockStore, 24*time.Hour, false), mockStore
}

func TestLoginHandler_EmptyPassword(t *testing.T) {
	a, _ := newTestAuth(t)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, routes.PanelLogin, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := a.LoginHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "password required")
}

func TestLoginHandler_InvalidPassword(t *testing.T) {
	a, mockStore := newTestAuth(t)
	mockStore.EXPECT().ValidateAdminPassword("wrongpass").Return(false)

	e := echo.New()
	form := strings.NewReader("password=wrongpass")
	req := httptest.NewRequest(http.MethodPost, routes.PanelLogin, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := a.LoginHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLoginHandler_Success(t *testing.T) {
	a, mockStore := newTestAuth(t)
	mockStore.EXPECT().ValidateAdminPassword("correctpass").Return(true)
	mockStore.EXPECT().CreateSession().Return("test-token", nil)

	e := echo.New()
	form := strings.NewReader("password=correctpass")
	req := httptest.NewRequest(http.MethodPost, routes.PanelLogin, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := a.LoginHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, routes.PanelDashboard, rec.Header().Get("Location"))

	cookie := findCookie(rec, SessionCookieName)
	require.NotNil(t, cookie)
	assert.Equal(t, "test-token", cookie.Value)
	assert.Equal(t, routes.PanelRoot, cookie.Path)
	assert.True(t, cookie.HttpOnly)
}

func TestLogoutHandler(t *testing.T) {
	a, mockStore := newTestAuth(t)
	mockStore.EXPECT().DeleteSession("test-token").Return()

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, routes.PanelLogout, nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "test-token"})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := a.LogoutHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, routes.PanelLogin, rec.Header().Get("Location"))

	cookie := findCookie(rec, SessionCookieName)
	require.NotNil(t, cookie)
	assert.Equal(t, "", cookie.Value)
	assert.Equal(t, -1, cookie.MaxAge)
}

func TestAuthMiddleware_ValidCookie(t *testing.T) {
	a, mockStore := newTestAuth(t)
	mockStore.EXPECT().ValidateSession("valid-token").Return(true)

	called := false
	handler := a.AuthMiddleware(func(c *echo.Context) error {
		called = true
		return c.String(http.StatusOK, "ok")
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, routes.PanelRoot, nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "valid-token"})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler(c)
	require.NoError(t, err)
	assert.True(t, called)
}

func TestAuthMiddleware_ValidBearerToken(t *testing.T) {
	a, mockStore := newTestAuth(t)
	mockStore.EXPECT().ValidateSession("bearer-token").Return(true)

	called := false
	handler := a.AuthMiddleware(func(c *echo.Context) error {
		called = true
		return c.String(http.StatusOK, "ok")
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, routes.PanelRoot, nil)
	req.Header.Set("Authorization", "Bearer bearer-token")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler(c)
	require.NoError(t, err)
	assert.True(t, called)
}

func TestAuthMiddleware_InvalidSession_APIRequest(t *testing.T) {
	a, mockStore := newTestAuth(t)
	mockStore.EXPECT().ValidateSession("bad-token").Return(false)

	handler := a.AuthMiddleware(func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, routes.PanelRoot, nil)
	req.Header.Set("Accept", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "bad-token"})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddleware_InvalidSession_BrowserRequest(t *testing.T) {
	a, mockStore := newTestAuth(t)
	mockStore.EXPECT().ValidateSession("bad-token").Return(false)

	handler := a.AuthMiddleware(func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, routes.PanelRoot, nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "bad-token"})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, routes.PanelLogin, rec.Header().Get("Location"))
}

func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestLoginHandler_SecureCookie(t *testing.T) {
	mockStore := storeMocks.NewMockStore(t)
	mockStore.EXPECT().ValidateAdminPassword("password").Return(true)
	mockStore.EXPECT().CreateSession().Return("test-token", nil)

	a := New(mockStore, 24*time.Hour, true)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, routes.PanelLogin, strings.NewReader("password=password"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := a.LoginHandler(c)
	require.NoError(t, err)

	cookie := findCookie(rec, SessionCookieName)
	require.NotNil(t, cookie)
	assert.True(t, cookie.Secure, "cookie should have Secure flag when auth is configured as secure")
}

func TestLoginHandler_InsecureCookie(t *testing.T) {
	mockStore := storeMocks.NewMockStore(t)
	mockStore.EXPECT().ValidateAdminPassword("password").Return(true)
	mockStore.EXPECT().CreateSession().Return("test-token", nil)

	a := New(mockStore, 24*time.Hour, false)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, routes.PanelLogin, strings.NewReader("password=password"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := a.LoginHandler(c)
	require.NoError(t, err)

	cookie := findCookie(rec, SessionCookieName)
	require.NotNil(t, cookie)
	assert.False(t, cookie.Secure, "cookie should not have Secure flag when auth is configured as insecure")
}
