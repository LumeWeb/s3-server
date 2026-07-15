package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/config"
)

func TestStore_LogConfig(t *testing.T) {
	s, _ := newTestStore(t)
	// Default should be the config defaults
	cfg := s.LogConfig()
	assert.Equal(t, "info", cfg.Level)
	assert.Equal(t, "json", cfg.Format)
}

func TestStore_SetLogConfig(t *testing.T) {
	s, _ := newTestStore(t)

	logCfg := config.LogConfig{Level: "debug", Format: "json"}
	require.NoError(t, s.SetLogConfig(logCfg))

	got := s.LogConfig()
	assert.Equal(t, "debug", got.Level)
	assert.Equal(t, "json", got.Format)
}

func TestStore_SetLogConfig_Persists(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, config.ConfigFile)
	s1, err := New(cfgPath)
	require.NoError(t, err)

	require.NoError(t, s1.SetLogConfig(config.LogConfig{Level: "warn", Format: "human"}))

	// Re-open the store and verify persistence
	s2, err := New(cfgPath)
	require.NoError(t, err)
	got := s2.LogConfig()
	assert.Equal(t, "warn", got.Level)
	assert.Equal(t, "human", got.Format)
}

func TestStore_SetLogConfig_SaveError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, config.ConfigFile)
	s, err := New(cfgPath)
	require.NoError(t, err)

	// Trigger a save so the config file exists, then make the parent dir unwritable
	require.NoError(t, s.SetLogConfig(config.LogConfig{Level: "info"}))
	require.NoError(t, os.Chmod(dir, 0555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	err = s.SetLogConfig(config.LogConfig{Level: "warn"})
	assert.Error(t, err)
}
