package auth

import (
	"net/http"
	"strings"
	"time"

	"go.lumeweb.com/s3-server/internal/api"
	"go.lumeweb.com/s3-server/internal/routes"
	"go.lumeweb.com/s3-server/internal/store"
	"github.com/labstack/echo/v5"
)

const (
	SessionCookieName = "s3-server-session"
	DefaultSessionTTL = 24 * time.Hour
)

type Auth struct {
	store      store.Store
	sessionTTL time.Duration
	secure     bool
}

func New(s store.Store, ttl time.Duration, secure bool) *Auth {
	return &Auth{store: s, sessionTTL: ttl, secure: secure}
}

// isSecure returns true if the cookie should have the Secure flag set.
// True when the request arrived over TLS or via X-Forwarded-Proto: https.
func (a *Auth) isSecure(c *echo.Context) bool {
	if a.secure {
		return true
	}
	if c.Request().TLS != nil {
		return true
	}
	return c.Request().Header.Get("X-Forwarded-Proto") == "https"
}

func (a *Auth) LoginHandler(c *echo.Context) error {
	password := c.FormValue(routes.FormFieldPassword)
	if password == "" {
		return c.String(http.StatusUnauthorized, "password required")
	}

	if !a.store.ValidateAdminPassword(password) {
		return c.String(http.StatusUnauthorized, "invalid password")
	}

	token, err := a.store.CreateSession()
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to create session")
	}

	c.SetCookie(&http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     routes.PanelRoot,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   a.isSecure(c),
		Expires:  time.Now().Add(a.sessionTTL),
	})

	return c.Redirect(http.StatusFound, routes.PanelDashboard)
}

func (a *Auth) LogoutHandler(c *echo.Context) error {
	if cookie, err := c.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		a.store.DeleteSession(cookie.Value)
	}

	c.SetCookie(&http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     routes.PanelRoot,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   a.isSecure(c),
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})

	return c.Redirect(http.StatusFound, routes.PanelLogin)
}

func (a *Auth) AuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		// check cookie
		if cookie, err := c.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
			if a.store.ValidateSession(cookie.Value) {
				return next(c)
			}
		}

		// check bearer token
		if token := extractBearerToken(c); token != "" {
			if a.store.ValidateSession(token) {
				return next(c)
			}
		}

		// API requests get 401, browser requests redirect to login
		if isAPIRequest(c) {
			return api.SendUnauthorized(c, api.TypeAuthRequired, "authentication required")
		}
		return c.Redirect(http.StatusFound, routes.PanelLogin)
	}
}

func isAPIRequest(c *echo.Context) bool {
	accept := c.Request().Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}

func extractBearerToken(c *echo.Context) string {
	auth := c.Request().Header.Get("Authorization")
	const prefix = "Bearer "
	if strings.HasPrefix(auth, prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}
