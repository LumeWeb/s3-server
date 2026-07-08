package onboarding

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/routes"
	storeMocks "go.lumeweb.com/s3-server/internal/store/mocks"
)

// setupCSRFServer creates an Echo instance with CSRF middleware and onboarding routes,
// mirroring main.go's setup. Returns the server, service, and mock store for expectations.
func setupCSRFServer(t *testing.T, state OnboardingState) (*echo.Echo, *Service, *storeMocks.MockStore) {
	t.Helper()
	svc, mockStore, _, _ := newTestService(t, state)
	mockStore.EXPECT().Config().Return(config.PanelConfig{}).Maybe()

	e := echo.New()
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup: "header:X-CSRF-Token,form:_csrf",
		CookiePath:  routes.PanelRoot,
	}))

	e.GET(routes.OnboardingStatus, svc.StatusHandler)
	e.POST(routes.OnboardingAdminPass, svc.SetAdminPasswordHandler)

	return e, svc, mockStore
}

// seedCSRF makes a GET request to seed the CSRF cookie and returns the token + cookies.
func seedCSRF(t *testing.T, e *echo.Echo) (string, []*http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, routes.OnboardingStatus, nil)
	req.Header.Del("Sec-Fetch-Site") // ensure cookie-based CSRF, not SecFetchSite bypass
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "GET to seed CSRF should succeed")

	cookies := rec.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "_csrf" {
			return c.Value, cookies
		}
	}
	require.Fail(t, "CSRF cookie not found in response")
	return "", nil
}

func TestCSRF_RejectsPostWithoutToken(t *testing.T) {
	e, _, _ := setupCSRFServer(t, StatePending)

	body := `{"password":"testpass123"}`
	req := httptest.NewRequest(http.MethodPost, routes.OnboardingAdminPass, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Del("Sec-Fetch-Site")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// Echo v5 CSRF returns 400 (bad request: no token extracted) when no
	// token is present at all, and 403 when token mismatch. Either way,
	// the request must NOT reach the handler.
	assert.NotEqual(t, http.StatusOK, rec.Code, "POST without CSRF token should be rejected")
	assert.Less(t, rec.Code, 500, "should be a 4xx error, not server error")
}

func TestCSRF_AcceptsPostWithValidToken(t *testing.T) {
	e, _, mockStore := setupCSRFServer(t, StatePending)
	mockStore.EXPECT().SetAdminPassword("testpass123").Return(nil)
	mockStore.EXPECT().SetOnboardingState("admin_password_set").Return(nil)

	token, cookies := seedCSRF(t, e)

	body := `{"password":"testpass123"}`
	req := httptest.NewRequest(http.MethodPost, routes.OnboardingAdminPass, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	req.Header.Del("Sec-Fetch-Site")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "POST with valid CSRF token should succeed")
}

func TestCSRF_RejectsPostWithWrongToken(t *testing.T) {
	e, _, _ := setupCSRFServer(t, StatePending)

	_, cookies := seedCSRF(t, e)

	body := `{"password":"testpass123"}`
	req := httptest.NewRequest(http.MethodPost, routes.OnboardingAdminPass, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", "bogus-token")
	req.Header.Del("Sec-Fetch-Site")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code, "POST with wrong CSRF token should be rejected")
}
