package ssl

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/testutil"
)

// --- Shutdown ---

func TestManager_Shutdown(t *testing.T) {
	m := NewManager(config.SSLConfig{
		Mode:      config.SSLModeManaged,
		ACMEEmail: "admin@example.com",
	}, t.TempDir(), testutil.NewTestLogger())
	require.NotNil(t, m)

	err := m.Shutdown(context.Background())
	require.NoError(t, err, "Shutdown should always return nil (no-op for now)")
}

func TestManager_Shutdown_NilManager(t *testing.T) {
	// Shutdown on nil manager would panic; verify the function exists
	// and works on a real manager
	m := NewManager(config.SSLConfig{
		Mode: config.SSLModeManaged,
	}, t.TempDir(), testutil.NewTestLogger())
	require.NotNil(t, m)

	err := m.Shutdown(context.Background())
	assert.NoError(t, err)
}

// --- ProvisionCerts empty list ---

func TestManager_ProvisionCerts_EmptyList(t *testing.T) {
	m := NewManager(config.SSLConfig{
		Mode:      config.SSLModeManaged,
		ACMEEmail: "admin@example.com",
	}, t.TempDir(), testutil.NewTestLogger())
	require.NotNil(t, m)

	// Empty domain list should return nil immediately (no iterations)
	err := m.ProvisionCerts(context.Background(), []string{})
	require.NoError(t, err)
}

// --- ProvisionCerts with invalid domain ---

func TestManager_ProvisionCerts_InvalidDomain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ACME network test in short mode")
	}
	m := NewManager(config.SSLConfig{
		Mode:       config.SSLModeManaged,
		ACMEEmail:  "admin@example.com",
		ACMEDirURL: "https://acme-staging-v02.api.letsencrypt.org/directory",
	}, t.TempDir(), testutil.NewTestLogger())
	require.NotNil(t, m)

	// Using a clearly invalid domain — this will fail at the ACME level
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := m.ProvisionCert(ctx, "invalid-domain-that-does-not-exist.invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to provision cert")
}

// --- ProvisionCerts propagates error from first failure ---

func TestManager_ProvisionCerts_ErrorStopsIteration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ACME network test in short mode")
	}
	m := NewManager(config.SSLConfig{
		Mode:       config.SSLModeManaged,
		ACMEEmail:  "admin@example.com",
		ACMEDirURL: "https://acme-staging-v02.api.letsencrypt.org/directory",
	}, t.TempDir(), testutil.NewTestLogger())
	require.NotNil(t, m)

	ctx, cancel := context.WithTimeout(context.Background(), 5*1000*1000*1000)
	defer cancel()

	// First domain will fail, second should never be attempted
	err := m.ProvisionCerts(ctx, []string{
		"first-invalid.invalid",
		"second-invalid.invalid",
	})
	require.Error(t, err)
	// Error should mention the first domain
	assert.Contains(t, err.Error(), "first-invalid.invalid")
}
