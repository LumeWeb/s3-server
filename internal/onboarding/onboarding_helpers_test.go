package onboarding

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/config"
)

// --- PublicKey / PublicKeyBase64 ---

func TestPublicKey(t *testing.T) {
	svc, _, _, _ := newTestService(t, StatePending)

	pk := svc.PublicKey()
	assert.Len(t, pk, 32, "PublicKey must be 32 bytes")
	assert.Equal(t, svc.publicKey, pk)
}

func TestPublicKeyBase64(t *testing.T) {
	svc, _, _, _ := newTestService(t, StatePending)

	b64 := svc.PublicKeyBase64()
	assert.NotEmpty(t, b64)

	decoded, err := base64.StdEncoding.DecodeString(b64)
	require.NoError(t, err)
	assert.Len(t, decoded, 32)
}

// --- ConfigHandler ---

func TestConfigHandler(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StatePending)

	indexers := []config.IndexerOption{{Name: "Test", URL: "https://indexer.example.com"}}
	mockStore.EXPECT().S3Config().Return(config.S3Config{
		Directory:         "/data",
		IndexerURL:        "https://indexer.example.com",
		AvailableIndexers: indexers,
	})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/onboarding/config", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.ConfigHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "https://indexer.example.com", resp.IndexerURL)
	assert.Len(t, resp.AvailableIndexers, 1)
	assert.NotEmpty(t, resp.AppID, "AppID must be set (from init)")
	assert.Equal(t, "S3d", resp.AppName)
	assert.Equal(t, "A S3-compatible storage service backed by Sia", resp.AppDesc)
	assert.Equal(t, "https://github.com/Siafoundation/s3d", resp.ServiceURL)
}

func TestConfigHandler_EmptyIndexers(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StatePending)

	mockStore.EXPECT().S3Config().Return(config.S3Config{})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/onboarding/config", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.ConfigHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp ConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.IndexerURL)
	assert.Empty(t, resp.AvailableIndexers)
	assert.NotEmpty(t, resp.AppID)
}

// --- ResetHandler ---

func TestResetHandler_Success(t *testing.T) {
	svc, mockStore, testStore, _ := newTestService(t, StateAppKeySet)
	svc.sqliteStore = testStore

	mockStore.EXPECT().ClearAdminPassword().Return(nil)
	mockStore.EXPECT().SetOnboardingState("pending").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/reset", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.ResetHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StatePending, svc.fsm.State(), "FSM must be reset to pending")
	assert.Nil(t, svc.sqliteStore, "sqliteStore must be nil after reset")

	var resp OnboardingStepResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "reset", resp.Status)
}

func TestResetHandler_NoSQLiteStore(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StateAdminSet)
	// sqliteStore is nil — ResetHandler should still work

	mockStore.EXPECT().ClearAdminPassword().Return(nil)
	mockStore.EXPECT().SetOnboardingState("pending").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/reset", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.ResetHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StatePending, svc.fsm.State())
	assert.Nil(t, svc.sqliteStore)
}

func TestResetHandler_StatePersistFailure(t *testing.T) {
	svc, mockStore, testStore, _ := newTestService(t, StateComplete)
	svc.sqliteStore = testStore

	mockStore.EXPECT().ClearAdminPassword().Return(nil)
	mockStore.EXPECT().SetOnboardingState("pending").Return(assert.AnError)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/reset", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.ResetHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestResetHandler_ClearAdminPasswordError(t *testing.T) {
	svc, mockStore, testStore, _ := newTestService(t, StateComplete)
	svc.sqliteStore = testStore

	// ClearAdminPassword fails — handler should continue (logs error) and
	// still attempt the FSM reset. This is best-effort cleanup.
	mockStore.EXPECT().ClearAdminPassword().Return(assert.AnError)
	mockStore.EXPECT().SetOnboardingState("pending").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/reset", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.ResetHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code, "Reset should succeed even if ClearAdminPassword fails (best-effort)")
	assert.Equal(t, StatePending, svc.fsm.State())
}

func TestResetHandler_FromPending(t *testing.T) {
	svc, mockStore, _, _ := newTestService(t, StatePending)

	mockStore.EXPECT().ClearAdminPassword().Return(nil)
	mockStore.EXPECT().SetOnboardingState("pending").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/reset", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := svc.ResetHandler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, StatePending, svc.fsm.State())
}

// --- isPrivateHost ---

func TestIsPrivateHost(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		expect bool
	}{
		{"loopback IPv4", "127.0.0.1", true},
		{"loopback IPv6", "::1", true},
		{"private 10.x", "10.0.0.1", true},
		{"private 172.16.x", "172.16.0.1", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"link-local", "169.254.1.1", true},
		{"unspecified", "0.0.0.0", true},
		{"public IP", "8.8.8.8", false},
		{"loopback with port", "127.0.0.1:8080", true},
		{"private with port", "10.0.0.1:443", true},
		{"public with port", "8.8.8.8:443", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, isPrivateHost(tt.host))
		})
	}
}

func TestIsPrivateHost_DNSFailure(t *testing.T) {
	// DNS resolution failure should block (return true)
	assert.True(t, isPrivateHost("this-host-definitely-does-not-exist.invalid"))
}

// --- ssrfSafeClient ---

func TestSsrfSafeClient_BlocksWithPrivateAddress(t *testing.T) {
	// Start an HTTP server on localhost
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := ssrfSafeClient()
	// httptest.Server listens on 127.0.0.1 — ssrfSafeClient should block it
	resp, err := client.Get(srv.URL)
	if err == nil {
		_ = resp.Body.Close()
	}
	// Should fail to connect: private address blocked
	assert.Error(t, err, "ssrfSafeClient must block connections to private addresses")
}

func TestSsrfSafeClient_NoRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "/target")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := ssrfSafeClient()
	// Even if the server redirects, the client won't follow it.
	// (Though the connection itself will be blocked since httptest uses 127.0.0.1)
	_, err := client.Get(srv.URL + "/redirect")
	assert.Error(t, err, "expected connection error (127.0.0.1 blocked)")
}

// --- ResolveIndexerURL ---

func TestResolveIndexerURL_EmptyString(t *testing.T) {
	assert.Equal(t, "", ResolveIndexerURL(""))
}

func TestResolveIndexerURL_PrivateHostRejected(t *testing.T) {
	assert.Equal(t, "", ResolveIndexerURL("https://127.0.0.1"))
	assert.Equal(t, "", ResolveIndexerURL("https://10.0.0.1"))
	assert.Equal(t, "", ResolveIndexerURL("https://192.168.1.1:8080"))
}

func TestResolveIndexerURL_WithSchemePublicHost(t *testing.T) {
	// A URL with a scheme and public host: should return as-is.
	// We don't actually connect because the scheme is present.
	result := ResolveIndexerURL("https://example.com")
	assert.Equal(t, "https://example.com", result)
}

func TestResolveIndexerURL_InvalidURL(t *testing.T) {
	// Invalid URL should pass through (url.Parse is lenient)
	result := ResolveIndexerURL("not-a-url-at-all")
	// No scheme → probes with HTTPS → fails → returns httpsURL as fallback
	// But the host "not-a-url-at-all" may resolve to a public address
	// depending on DNS. Just verify it doesn't panic.
	_ = result
}
