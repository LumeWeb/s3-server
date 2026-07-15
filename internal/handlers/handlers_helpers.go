package handlers

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"
	"go.lumeweb.com/s3-server/internal/api"
	"go.lumeweb.com/s3-server/internal/backend"
	"go.lumeweb.com/s3-server/internal/views"
	"go.lumeweb.com/s3-server/internal/views/components"
	"go.uber.org/zap"
)

// errResponseSent is a sentinel error returned by helper functions after they
// have already written an HTTP error response. Callers check `if err != nil`
// and return it, preventing double-writes or continued processing.
var errResponseSent = errors.New("response already sent")

// sendErr writes an error response and logs if the write itself fails.
func (s *Services) sendErr(c *echo.Context, sendFn func() error) {
	if err := sendFn(); err != nil {
		s.log.Error("failed to write error response", zap.Error(err))
	}
}

// renderPageError renders an HTML error page instead of returning raw JSON.
// Used by page handlers (HTML routes) when a non-recoverable error occurs.
func (s *Services) renderPageError(c *echo.Context, title, message string) error {
	return views.PageLayout(title, "", "", s.csrfToken(c), components.PageError(title, message)).
		Render(c.Request().Context(), c.Response())
}

// requireBackend returns the backend and admin access key, or sends a NOT_READY
// error and returns errResponseSent.
func (s *Services) requireBackend(c *echo.Context) (backend.Backend, string, error) {
	b, err := s.requireBackendOnly(c)
	if err != nil {
		return nil, "", err
	}
	accessKey, err := s.adminAccessKey()
	if err != nil {
		s.sendErr(c, func() error { return api.SendNotReady(c, api.TypeBackendNotInitialized, err.Error(), nil) })
		return nil, "", errResponseSent
	}
	return b, accessKey, nil
}

// requireBackendOnly returns the backend without fetching an access key.
// Use for handlers that don't need to make S3 API calls (e.g. flush, restart).
func (s *Services) requireBackendOnly(c *echo.Context) (backend.Backend, error) {
	b := s.getBackend()
	if b == nil {
		s.sendErr(c, func() error {
			return api.SendNotReady(c, api.TypeBackendNotInitialized, "backend hasn't started yet", nil)
		})
		return nil, errResponseSent
	}
	return b, nil
}

// requireKeyStore returns the key store or sends an INTERNAL_ERROR and returns
// errResponseSent.
func (s *Services) requireKeyStore(c *echo.Context) (backend.S3DStore, error) {
	ks := s.getKeyStore()
	if ks == nil {
		s.sendErr(c, func() error {
			return api.SendInternal(c, api.TypeBackendNotInitialized, "backend hasn't started yet", nil)
		})
		return nil, errResponseSent
	}
	return ks, nil
}

// requireParam returns a URL path parameter or sends a BAD_REQUEST error and
// returns errResponseSent.
func (s *Services) requireParam(c *echo.Context, name string) (string, error) {
	val := c.Param(name)
	if val == "" {
		s.sendErr(c, func() error { return api.SendBadRequest(c, api.TypeNameRequired, name+" required") })
		return "", errResponseSent
	}
	return val, nil
}

// bindJSON decodes the request body into dst or sends a BAD_REQUEST error and
// returns errResponseSent.
func bindJSON[T any](c *echo.Context, log *zap.Logger, dst *T) error {
	if err := c.Bind(dst); err != nil {
		if werr := api.SendBadRequest(c, api.TypeInvalidRequestBody, "invalid request body"); werr != nil {
			log.Error("failed to write error response", zap.Error(werr))
		}
		return errResponseSent
	}
	return nil
}

// captureRecorder is an http.ResponseWriter that captures the response for
// later inspection (used by backup download handlers).
type captureRecorder struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (r *captureRecorder) Header() http.Header  { return r.header }
func (r *captureRecorder) WriteHeader(code int) { r.code = code }
func (r *captureRecorder) Write(p []byte) (int, error) {
	return r.body.Write(p)
}
