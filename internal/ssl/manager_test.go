package ssl

import (
	"net/http"
	"testing"

	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager_NoneMode(t *testing.T) {
	m := NewManager(config.SSLConfig{Mode: config.SSLModeNone}, "/tmp", testutil.NewTestLogger())
	assert.Nil(t, m)
}

func TestNewManager_PlatformMode(t *testing.T) {
	m := NewManager(config.SSLConfig{Mode: config.SSLModePlatform}, "/tmp", testutil.NewTestLogger())
	assert.Nil(t, m)
}

func TestNewManager_ManagedMode(t *testing.T) {
	m := NewManager(config.SSLConfig{
		Mode:      config.SSLModeManaged,
		ACMEEmail: "admin@example.com",
	}, t.TempDir(), testutil.NewTestLogger())
	assert.NotNil(t, m)
	assert.True(t, m.IsManaged())
	assert.Equal(t, config.SSLModeManaged, m.Mode())
}

func TestManager_TLSConfig(t *testing.T) {
	m := NewManager(config.SSLConfig{
		Mode:      config.SSLModeManaged,
		ACMEEmail: "admin@example.com",
	}, t.TempDir(), testutil.NewTestLogger())
	require.NotNil(t, m)

	tlsCfg := m.TLSConfig()
	assert.NotNil(t, tlsCfg)
	assert.NotNil(t, tlsCfg.GetCertificate)

	// should have h2 and http/1.1 in NextProtos
	foundH2 := false
	foundHTTP11 := false
	for _, proto := range tlsCfg.NextProtos {
		if proto == "h2" {
			foundH2 = true
		}
		if proto == "http/1.1" {
			foundHTTP11 = true
		}
	}
	assert.True(t, foundH2, "expected h2 in NextProtos")
	assert.True(t, foundHTTP11, "expected http/1.1 in NextProtos")
}

func TestManager_HTTPHandler(t *testing.T) {
	m := NewManager(config.SSLConfig{
		Mode:      config.SSLModeManaged,
		ACMEEmail: "admin@example.com",
	}, t.TempDir(), testutil.NewTestLogger())
	require.NotNil(t, m)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	wrapped := m.HTTPHandler(inner)
	assert.NotNil(t, wrapped)
	// wrapped should be different from inner since it adds challenge handling
}

func TestNilManager_IsManaged(t *testing.T) {
	var m *Manager
	assert.False(t, m.IsManaged())
}

func TestNilManager_Mode(t *testing.T) {
	var m *Manager
	assert.Equal(t, config.SSLModeNone, m.Mode())
}
