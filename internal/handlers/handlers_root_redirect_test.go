package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootToPanelRedirect(t *testing.T) {
	hostBases := []string{"s3.example.com"}

	// next records whether it was reached (i.e. request passed through to S3).
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("s3"))
	})
	h := RootToPanelRedirect(next, hostBases)

	run := func(method, target, host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	t.Run("anonymous root GET redirects to panel", func(t *testing.T) {
		rec := run(http.MethodGet, "/", "example.com")
		require.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, "/_panel/", rec.Header().Get("Location"))
	})

	t.Run("authenticated root GET passes through", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = "example.com"
		req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AKIAEXAMPLE/20260101/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=abc")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "s3", rec.Body.String())
	})

	t.Run("presigned root GET passes through", func(t *testing.T) {
		rec := run(http.MethodGet,
			"/?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAEXAMPLE/20260101/us-east-1/s3/aws4_request&X-Amz-Signature=abc",
			"example.com")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "s3", rec.Body.String())
	})

	t.Run("host-style bucket root GET passes through", func(t *testing.T) {
		rec := run(http.MethodGet, "/", "mybucket.s3.example.com")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "s3", rec.Body.String())
	})

	t.Run("root GET on host base apex redirects to panel", func(t *testing.T) {
		rec := run(http.MethodGet, "/", "s3.example.com")
		assert.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, "/_panel/", rec.Header().Get("Location"))
	})

	t.Run("non-root path passes through", func(t *testing.T) {
		rec := run(http.MethodGet, "/mybucket", "example.com")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "s3", rec.Body.String())
	})

	t.Run("non-GET root passes through", func(t *testing.T) {
		rec := run(http.MethodHead, "/", "example.com")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "s3", rec.Body.String())
	})
}
