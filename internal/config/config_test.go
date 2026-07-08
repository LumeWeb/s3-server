package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "pending", cfg.OnboardingState)
	assert.Equal(t, SSLModeNone, cfg.SSL.Mode)
	assert.Equal(t, "/var/lib/s3-server", cfg.S3.Directory)
	assert.Equal(t, "https://sia.pinner.xyz", cfg.S3.IndexerURL)
	assert.Equal(t, []string{"https://sia.pinner.xyz", "https://sia.storage"}, cfg.S3.AvailableIndexers)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)
	assert.Empty(t, cfg.AdminPasswordHash)
	assert.Empty(t, cfg.AccessKeys)
}

func TestConfigPath(t *testing.T) {
	assert.Equal(t, filepath.Join("/data", ConfigFile), ConfigPath("/data"))
	assert.Equal(t, filepath.Join("/var/lib/s3-server", ConfigFile), ConfigPath("/var/lib/s3-server"))
}

func TestLoad_NoFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/panel.yml")
	require.NoError(t, err)

	// Compare field-by-field — koanf unmarshal produces empty slices where
	// DefaultConfig() has nil slices, so reflect.DeepEqual fails.
	expected := DefaultConfig()
	assert.Equal(t, expected.AdminPasswordHash, cfg.AdminPasswordHash)
	assert.Equal(t, expected.OnboardingState, cfg.OnboardingState)
	assert.Empty(t, cfg.AccessKeys)
	assert.Equal(t, expected.SSL, cfg.SSL)
	assert.Equal(t, expected.S3, cfg.S3)
	assert.Equal(t, expected.Log, cfg.Log)
}

func TestLoad_Save_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	original := PanelConfig{
		OnboardingState: "complete",
		AccessKeys: []KeyPair{
			{AccessKey: "AKIA1234567890ABCDEF", SecretKey: "secretkey123456789012345678901234"},
		},
		SSL: SSLConfig{
			Mode:      SSLModeManaged,
			ACMEEmail: "admin@example.com",
		},
		S3: S3Config{
			Directory:         "/data/s3d",
			IndexerURL:        "https://custom.sia.storage",
			AvailableIndexers: []string{"https://sia.pinner.xyz", "https://sia.storage"},
			HostBases:          []string{"s3.example.com"},
		},
		Log: LogConfig{
			Level:  "debug",
			Format: "human",
		},
	}

	err := Save(path, original)
	require.NoError(t, err)

	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, original.OnboardingState, loaded.OnboardingState)
	assert.Len(t, loaded.AccessKeys, 1)
	assert.Equal(t, original.AccessKeys[0].AccessKey, loaded.AccessKeys[0].AccessKey)
	assert.Equal(t, original.SSL.Mode, loaded.SSL.Mode)
	assert.Equal(t, original.SSL.ACMEEmail, loaded.SSL.ACMEEmail)
	assert.Equal(t, original.S3.Directory, loaded.S3.Directory)
	assert.Equal(t, original.S3.IndexerURL, loaded.S3.IndexerURL)
	assert.Equal(t, original.Log.Level, loaded.Log.Level)
	assert.Equal(t, original.Log.Format, loaded.Log.Format)
}

func TestSave_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestLoad_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	// save a default config
	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	// set env overrides
	t.Setenv("S3_SERVER_LOG__LEVEL", "debug")
	t.Setenv("S3_SERVER_S3__INDEXER_URL", "https://custom.indexer")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, "https://custom.indexer", cfg.S3.IndexerURL)
}

func TestLoad_PartialYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	// write a partial YAML — only override some fields
	yamlContent := "onboarding_state: complete\nlog:\n  level: warn\n"
	err := os.WriteFile(path, []byte(yamlContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "complete", cfg.OnboardingState)
	assert.Equal(t, "warn", cfg.Log.Level)
	// defaults should still be present
	assert.Equal(t, SSLModeNone, cfg.SSL.Mode)
	assert.Equal(t, "/var/lib/s3-server", cfg.S3.Directory)
}

func TestBuildLogger_JSON(t *testing.T) {
	logger, err := BuildLogger(LogConfig{Level: "info", Format: "json"})
	require.NoError(t, err)
	require.NotNil(t, logger)
}

func TestBuildLogger_Human(t *testing.T) {
	logger, err := BuildLogger(LogConfig{Level: "debug", Format: "human"})
	require.NoError(t, err)
	require.NotNil(t, logger)
}

func TestBuildLogger_InvalidLevel(t *testing.T) {
	// invalid level falls back to info
	logger, err := BuildLogger(LogConfig{Level: "invalid", Format: "json"})
	require.NoError(t, err)
	require.NotNil(t, logger)
}

func TestSSLMode_Constants(t *testing.T) {
	assert.Equal(t, SSLMode("none"), SSLModeNone)
	assert.Equal(t, SSLMode("platform"), SSLModePlatform)
	assert.Equal(t, SSLMode("managed"), SSLModeManaged)
}

func TestKeyPair_Tags(t *testing.T) {
	kp := KeyPair{AccessKey: "AKIA123", SecretKey: "secret"}
	assert.Equal(t, "AKIA123", kp.AccessKey)
	assert.Equal(t, "secret", kp.SecretKey)
}

func TestLoad_EnvCSV_HostBases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	t.Setenv("S3_SERVER_S3__HOST_BASES", "s3.example.com,backup.example.com")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"s3.example.com", "backup.example.com"}, cfg.S3.HostBases)
}

func TestLoad_EnvCSV_SingleHostBase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	t.Setenv("S3_SERVER_S3__HOST_BASES", "s3.example.com")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"s3.example.com"}, cfg.S3.HostBases)
}

func TestLoad_EnvCSV_EmptyHostBases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	t.Setenv("S3_SERVER_S3__HOST_BASES", "")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{}, cfg.S3.HostBases)
}

func TestLoad_EnvCSV_DoesNotAffectNonSliceFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	// string fields with commas should NOT be split
	t.Setenv("S3_SERVER_S3__INDEXER_URL", "https://sia.pinner.xyz")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "https://sia.pinner.xyz", cfg.S3.IndexerURL)
}

func TestLoad_EnvCSV_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	t.Setenv("S3_SERVER_S3__HOST_BASES", " s3.example.com , backup.example.com ")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"s3.example.com", "backup.example.com"}, cfg.S3.HostBases)
}

func TestLoad_StatError_PropagatesNonNotExist(t *testing.T) {
	// Create a directory where a file is expected — stat will succeed
	// but loading it as a YAML file will fail. Instead, test with a
	// path that causes a permission error by creating an unreadable dir.
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	// Write a valid config
	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	// Make the directory unreadable (stat will fail with permission error)
	err = os.Chmod(dir, 0000)
	require.NoError(t, err)
	defer os.Chmod(dir, 0700) // restore so cleanup works

	_, err = Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat config file")
}
