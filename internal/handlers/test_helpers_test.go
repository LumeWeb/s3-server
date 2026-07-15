package handlers

import (
	"io"
	"net/http/httptest"

	"github.com/labstack/echo/v5"
)

// testContext creates an Echo context + recorder for a request without a body.
// Eliminates the repeated httptest.NewRequest → NewRecorder → e.NewContext pattern.
func testContext(e *echo.Echo, method, path string) (*echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// testJSONContext creates an Echo context + recorder for a JSON request.
// Sets Content-Type: application/json automatically.
func testJSONContext(e *echo.Echo, method, path string, body io.Reader) (*echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}
