package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/config"
)

func newTestStore(t *testing.T) (Store, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, config.ConfigFile)
	s, err := New(cfgPath)
	require.NoError(t, err)
	return s, cfgPath
}

func TestNew_CreatesStoreWithDefaults(t *testing.T) {
	s, _ := newTestStore(t)
	require.NotNil(t, s)

	cfg := s.Config()
	assert.Equal(t, config.DefaultOnboardingState, cfg.OnboardingState)
	assert.Equal(t, config.SSLModeNone, cfg.SSL.Mode)
	assert.Empty(t, cfg.AdminPasswordHash)
}

func TestNew_MissingConfigFile_UsesDefaults(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "nonexistent", "panel.yml"))
	require.NoError(t, err)

	cfg := s.Config()
	assert.Equal(t, config.DefaultOnboardingState, cfg.OnboardingState)
}

func TestStore_SetAdminPassword(t *testing.T) {
	s, _ := newTestStore(t)

	err := s.SetAdminPassword("testpass123")
	require.NoError(t, err)

	assert.True(t, s.ValidateAdminPassword("testpass123"))
	assert.False(t, s.ValidateAdminPassword("wrong"))
}

func TestStore_SetAdminPassword_RejectsEmpty(t *testing.T) {
	s, _ := newTestStore(t)

	err := s.SetAdminPassword("")
	require.Error(t, err)

	err = s.SetAdminPassword("   ")
	require.Error(t, err)

	// No password should validate after failed set
	assert.False(t, s.ValidateAdminPassword(""))
	assert.False(t, s.ValidateAdminPassword("   "))
	assert.False(t, s.ValidateAdminPassword("anything"))
}

func TestStore_ClearAdminPassword(t *testing.T) {
	s, _ := newTestStore(t)

	// Set a password first
	err := s.SetAdminPassword("testpass123")
	require.NoError(t, err)
	require.True(t, s.ValidateAdminPassword("testpass123"))

	// Clear it
	err = s.ClearAdminPassword()
	require.NoError(t, err)

	// Hash should be empty — no password should validate
	assert.False(t, s.ValidateAdminPassword("testpass123"))
	assert.False(t, s.ValidateAdminPassword(""))
	assert.False(t, s.ValidateAdminPassword("anything"))

	// Config should reflect cleared state
	cfg := s.Config()
	assert.Empty(t, cfg.AdminPasswordHash)
}

func TestStore_ValidateAdminPassword_EmptyHash(t *testing.T) {
	s, _ := newTestStore(t)

	// No password set — nothing should validate
	assert.False(t, s.ValidateAdminPassword(""))
	assert.False(t, s.ValidateAdminPassword("somepass"))
}

func TestStore_DataDir(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, config.ConfigFile)
	s, err := New(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, dir, s.DataDir())
}

func TestStore_ResetTokenPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, config.ConfigFile)
	s, err := New(cfgPath)
	require.NoError(t, err)

	expected := filepath.Join(dir, ".reset-token")
	assert.Equal(t, expected, s.ResetTokenPath())
}

func TestStore_AccessKeys_Empty(t *testing.T) {
	s, _ := newTestStore(t)
	keys := s.AccessKeys()
	assert.Empty(t, keys)
}

func TestStore_SetAccessKeys(t *testing.T) {
	s, _ := newTestStore(t)

	keys := []config.KeyPair{
		{AccessKey: "AKIA123", SecretKey: "secret1"},
		{AccessKey: "AKIA456", SecretKey: "secret2"},
	}
	err := s.SetAccessKeys(keys)
	require.NoError(t, err)

	got := s.AccessKeys()
	assert.Len(t, got, 2)
	assert.Equal(t, "AKIA123", got[0].AccessKey)
	assert.Equal(t, "AKIA456", got[1].AccessKey)
}

func TestStore_OnboardingState(t *testing.T) {
	s, _ := newTestStore(t)
	assert.Equal(t, config.DefaultOnboardingState, s.OnboardingState())

	err := s.SetOnboardingState("complete")
	require.NoError(t, err)
	assert.Equal(t, "complete", s.OnboardingState())
}

func TestStore_SSLConfig(t *testing.T) {
	s, _ := newTestStore(t)

	orig := s.SSLConfig()
	assert.Equal(t, config.SSLModeNone, orig.Mode)

	newCfg := config.SSLConfig{
		Mode:      config.SSLModeManaged,
		ACMEEmail: "admin@example.com",
	}
	err := s.SetSSLConfig(newCfg)
	require.NoError(t, err)

	got := s.SSLConfig()
	assert.Equal(t, config.SSLModeManaged, got.Mode)
	assert.Equal(t, "admin@example.com", got.ACMEEmail)
}

func TestStore_S3Config(t *testing.T) {
	s, _ := newTestStore(t)

	orig := s.S3Config()
	assert.Equal(t, "/var/lib/s3-server", orig.Directory)

	newCfg := config.S3Config{
		Directory:  "/custom/data",
		IndexerURL: "https://custom.indexer",
	}
	err := s.SetS3Config(newCfg)
	require.NoError(t, err)

	got := s.S3Config()
	assert.Equal(t, "/custom/data", got.Directory)
	assert.Equal(t, "https://custom.indexer", got.IndexerURL)
}

func TestStore_CreateSession(t *testing.T) {
	s, _ := newTestStore(t)

	token, err := s.CreateSession()
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	assert.True(t, s.ValidateSession(token))
	assert.False(t, s.ValidateSession("invalid-token"))
}

func TestStore_DeleteSession(t *testing.T) {
	s, _ := newTestStore(t)

	token, err := s.CreateSession()
	require.NoError(t, err)
	require.True(t, s.ValidateSession(token))

	s.DeleteSession(token)
	assert.False(t, s.ValidateSession(token))
}

func TestStore_PersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, config.ConfigFile)

	// Create store, set password
	s1, err := New(cfgPath)
	require.NoError(t, err)
	err = s1.SetAdminPassword("persisted-pass")
	require.NoError(t, err)
	err = s1.SetOnboardingState("complete")
	require.NoError(t, err)

	// Create new store from same path — data should persist
	s2, err := New(cfgPath)
	require.NoError(t, err)

	assert.True(t, s2.ValidateAdminPassword("persisted-pass"))
	assert.Equal(t, "complete", s2.OnboardingState())
}

func TestMemorySessionManager_Create(t *testing.T) {
	sm := NewMemorySessionManager(0) // zero TTL — immediately expired
	token, err := sm.Create()
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	// With zero TTL, session is immediately expired
	assert.False(t, sm.Validate(token))
}

func TestMemorySessionManager_Validate(t *testing.T) {
	sm := NewMemorySessionManager(60 * time.Second)

	token, err := sm.Create()
	require.NoError(t, err)
	assert.True(t, sm.Validate(token))
	assert.False(t, sm.Validate("nonexistent"))
}

func TestMemorySessionManager_Delete(t *testing.T) {
	sm := NewMemorySessionManager(60 * time.Second)

	token, err := sm.Create()
	require.NoError(t, err)
	require.True(t, sm.Validate(token))

	sm.Delete(token)
	assert.False(t, sm.Validate(token))
}
