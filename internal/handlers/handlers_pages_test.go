package handlers

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
)

func TestDeriveEndpoint_DefaultHTTP(t *testing.T) {
	s := &Services{}
	req := httptest.NewRequest(http.MethodGet, "/_panel/buckets", nil)
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)

	got := s.deriveEndpoint(c)
	assert.Equal(t, "http://example.com", got)
}

func TestDeriveEndpoint_TLS(t *testing.T) {
	s := &Services{}
	req := httptest.NewRequest(http.MethodGet, "/_panel/buckets", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)

	got := s.deriveEndpoint(c)
	assert.Equal(t, "https://example.com", got)
}

func TestDeriveEndpoint_XForwardedProto(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"lowercase https", "https", "https://example.com"},
		{"uppercase HTTPS normalized", "HTTPS", "https://example.com"},
		{"mixed case Http", "Http", "http://example.com"},
		{"comma-separated first wins", "https, http", "https://example.com"},
		{"comma no space", "https,http", "https://example.com"},
		{"invalid scheme rejected", "ftp", "http://example.com"},
		{"empty header ignored", "", "http://example.com"},
		{"garbage rejected", "javascript", "http://example.com"},
		{"injection attempt rejected", "https://evil.com", "http://example.com"},
	}

	s := &Services{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/_panel/buckets", nil)
			if tc.header != "" {
				req.Header.Set("X-Forwarded-Proto", tc.header)
			}
			rec := httptest.NewRecorder()
			e := echo.New()
			c := e.NewContext(req, rec)

			got := s.deriveEndpoint(c)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDeriveEndpoint_CustomHost(t *testing.T) {
	s := &Services{}
	req := httptest.NewRequest(http.MethodGet, "/_panel/buckets", nil)
	req.Host = "s3.example.com:8080"
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)

	got := s.deriveEndpoint(c)
	assert.Equal(t, "http://s3.example.com:8080", got)
}
