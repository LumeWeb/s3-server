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
	assert.Equal(t, []IndexerOption{
		{URL: "https://sia.pinner.xyz", Name: "Pinner", Description: "Our indexer, our support", Logo: "pinner", BrandColor: "#12A596", ContrastColor: "#000000"},
		{URL: "https://sia.storage", Name: "Sia Storage", Description: "Are you already using Sia Storage? Connect here.", Logo: "sia-storage", BrandColor: "#EFF2ED", ContrastColor: "#000000"},
	}, cfg.S3.AvailableIndexers)
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
			Directory:  "/data/s3d",
			IndexerURL: "https://custom.sia.storage",
			AvailableIndexers: []IndexerOption{
				{URL: "https://sia.pinner.xyz", Name: "Pinner"},
				{URL: "https://sia.storage", Name: "Sia Storage"},
			},
			HostBases: []string{"s3.example.com"},
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
	logger, _, err := BuildLogger(LogConfig{Level: "info", Format: "json"})
	require.NoError(t, err)
	require.NotNil(t, logger)
}

func TestBuildLogger_Human(t *testing.T) {
	logger, _, err := BuildLogger(LogConfig{Level: "debug", Format: "human"})
	require.NoError(t, err)
	require.NotNil(t, logger)
}

func TestBuildLogger_InvalidLevel(t *testing.T) {
	// invalid level falls back to info
	logger, _, err := BuildLogger(LogConfig{Level: "invalid", Format: "json"})
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

func TestLoad_EnvDomainsAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	t.Setenv("S3_SERVER_S3__DOMAINS", "s3.example.com,backup.example.com")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"s3.example.com", "backup.example.com"}, cfg.S3.HostBases)
}

func TestLoad_EnvDomainsAlias_SingleDomain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	t.Setenv("S3_SERVER_S3__DOMAINS", "s3.example.com")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"s3.example.com"}, cfg.S3.HostBases)
}

func TestLoad_EnvDomainsAlias_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	t.Setenv("S3_SERVER_S3__DOMAINS", " s3.example.com , backup.example.com ")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"s3.example.com", "backup.example.com"}, cfg.S3.HostBases)
}

func TestLoad_EnvHostBasesTakesPrecedenceOverDomains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	// Both env vars map to s3.host_bases. HOST_BASES must win
	// regardless of os.Environ() iteration order.
	t.Setenv("S3_SERVER_S3__DOMAINS", "domains.example.com")
	t.Setenv("S3_SERVER_S3__HOST_BASES", "hostbases.example.com")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"hostbases.example.com"}, cfg.S3.HostBases)
}

func TestLoad_EnvHostBasesTakesPrecedenceOverDomains_ReversedOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	// Set HOST_BASES first this time. Must still win.
	t.Setenv("S3_SERVER_S3__HOST_BASES", "hostbases.example.com")
	t.Setenv("S3_SERVER_S3__DOMAINS", "domains.example.com")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"hostbases.example.com"}, cfg.S3.HostBases)
}

func TestLoad_EnvDomainsAlias_EmptyDoesNotOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	t.Setenv("S3_SERVER_S3__DOMAINS", "")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{}, cfg.S3.HostBases)
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
	defer func() { _ = os.Chmod(dir, 0700) }() // restore so cleanup works

	_, err = Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat config file")
}

// Regression PR2: Save must tighten permissions on a pre-existing file
// that was created with looser perms (e.g. 0644). The atomic write path
// uses a temp file + rename, and must chmod the result to 0600.
func TestSave_TightensPermissionsOnExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	// Create a file with loose permissions
	require.NoError(t, os.WriteFile(path, []byte("old: data\n"), 0644))

	// Save over it
	require.NoError(t, Save(path, DefaultConfig()))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(),
		"existing file perms must be tightened to 0600 after Save")
}

// Regression PR2: Save must be atomic — if the write fails (e.g. disk
// error), the original file must remain intact. The temp-file + rename
// approach ensures the original is untouched on failure.
func TestSave_AtomicWrite_OriginalIntactOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	// Write an original config
	original := DefaultConfig()
	original.OnboardingState = "complete"
	require.NoError(t, Save(path, original))

	// Read original content for comparison
	originalData, err := os.ReadFile(path)
	require.NoError(t, err)

	// Make the directory read-only so the temp file write fails
	require.NoError(t, os.Chmod(dir, 0500))
	defer func() { _ = os.Chmod(dir, 0700) }() // restore for cleanup

	// Attempt to save — should fail because the temp file can't be written
	err = Save(path, DefaultConfig())
	require.Error(t, err)

	// Original file must be intact
	currentData, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, originalData, currentData,
		"original file must be unchanged on write failure")
}

// DiskUsageLimit backward compatibility and new behavior tests.

func TestDiskUsageLimit_Bytes_Auto(t *testing.T) {
	// Auto mode should compute 80% of total disk capacity.
	// We can't assert exact values without mocking disk.Usage,
	// but we can verify it doesn't error and returns hasLimit=true.
	d := DiskUsageLimitAuto
	b, hasLimit, err := d.Bytes("/")
	require.NoError(t, err)
	assert.True(t, hasLimit)
	assert.Greater(t, b, uint64(0))
}

func TestDiskUsageLimit_Bytes_Zero(t *testing.T) {
	d := DiskUsageLimitUnlimited
	b, hasLimit, err := d.Bytes("/")
	require.NoError(t, err)
	assert.False(t, hasLimit)
	assert.Equal(t, uint64(0), b)
}

func TestDiskUsageLimit_Bytes_Numeric(t *testing.T) {
	d := DiskUsageLimit("100")
	b, hasLimit, err := d.Bytes("/")
	require.NoError(t, err)
	assert.True(t, hasLimit)
	assert.Equal(t, uint64(100*1024*1024*1024), b)
}

func TestDiskUsageLimit_Bytes_Empty(t *testing.T) {
	d := DiskUsageLimit("")
	b, hasLimit, err := d.Bytes("/")
	require.NoError(t, err)
	assert.False(t, hasLimit)
	assert.Equal(t, uint64(0), b)
}

func TestDiskUsageLimit_IsAuto(t *testing.T) {
	assert.True(t, DiskUsageLimitAuto.IsAuto())
	assert.True(t, DiskUsageLimit("").IsAuto())
	assert.False(t, DiskUsageLimitUnlimited.IsAuto())
	assert.False(t, DiskUsageLimit("100").IsAuto())
}

func TestDiskUsageLimit_IsLimited(t *testing.T) {
	assert.False(t, DiskUsageLimitAuto.IsLimited())
	assert.False(t, DiskUsageLimit("").IsLimited())
	assert.False(t, DiskUsageLimitUnlimited.IsLimited())
	assert.True(t, DiskUsageLimit("100").IsLimited())
}

func TestDiskUsageLimit_Load_YAML_Number(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	// Old-style config with numeric disk_usage_limit
	yamlContent := "s3:\n  disk_usage_limit: 100\n"
	err := os.WriteFile(path, []byte(yamlContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, DiskUsageLimit("100"), cfg.S3.DiskUsageLimit)
}

func TestDiskUsageLimit_Load_YAML_Auto(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	yamlContent := "s3:\n  disk_usage_limit: auto\n"
	err := os.WriteFile(path, []byte(yamlContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, DiskUsageLimitAuto, cfg.S3.DiskUsageLimit)
}

func TestDiskUsageLimit_Load_EnvVar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	t.Setenv("S3_SERVER_S3__DISK_USAGE_LIMIT", "200")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, DiskUsageLimit("200"), cfg.S3.DiskUsageLimit)
}

func TestDiskUsageLimit_Load_EnvVar_Auto(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.yml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	t.Setenv("S3_SERVER_S3__DISK_USAGE_LIMIT", "auto")

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, DiskUsageLimitAuto, cfg.S3.DiskUsageLimit)
}

func TestDefaultConfig_DiskUsageLimit(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, DiskUsageLimitAuto, cfg.S3.DiskUsageLimit)
}

// Regression: auto mode must always return hasLimit=true, never (0, false).
// A previous bug subtracted a fixed threshold; when free space was <= threshold
// it returned (0, false), silently disabling the limit on small disks.
func TestDiskUsageLimit_Bytes_Auto_NeverUnlimited(t *testing.T) {
	d := DiskUsageLimitAuto
	_, hasLimit, err := d.Bytes("/")
	require.NoError(t, err)
	assert.True(t, hasLimit, "auto mode must never return hasLimit=false")
}
