package backend

import (
	"errors"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/config"
	"go.uber.org/zap/zaptest"
)

// Regression PR56: auto-mode disk query failure falls back to conservative cap.
// When disk.Usage fails (container restriction, unsupported FS, etc.),
// resolveDiskLimit applies a safe 25 GB cap instead of unlimited, preventing
// unbounded writes that would exhaust the host disk.
func TestFactory_resolveDiskLimit_AutoFailure_FallsBackToConservativeCap(t *testing.T) {
	log := zaptest.NewLogger(t)
	f := &s3dFactory{log: log, diskQuery: func(string) (*disk.UsageStat, error) {
		return nil, errors.New("disk query failed")
	}}

	s3Cfg := config.S3Config{
		Directory:      "/this/path/does/not/exist",
		DiskUsageLimit: config.DiskUsageLimitAuto,
	}

	limitBytes, hasLimit, err := f.resolveDiskLimit(s3Cfg)
	require.NoError(t, err, "auto-mode disk failure should not be fatal")
	assert.True(t, hasLimit, "auto-mode failure should still enforce a cap")
	assert.Equal(t, uint64(25*1024*1024*1024), limitBytes)
}

// Regression PR56: numeric-mode limit resolution must remain strict.
// Invalid numeric values should still produce an error.
func TestFactory_resolveDiskLimit_NumericInvalid_Fails(t *testing.T) {
	log := zaptest.NewLogger(t)
	f := &s3dFactory{log: log}

	s3Cfg := config.S3Config{
		Directory:      "/tmp",
		DiskUsageLimit: config.DiskUsageLimit("not_a_number"),
	}

	_, _, err := f.resolveDiskLimit(s3Cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk usage limit")
}

// Regression PR56: auto-mode with successful disk query should compute
// 80% of total capacity.
func TestFactory_resolveDiskLimit_AutoSuccess(t *testing.T) {
	log := zaptest.NewLogger(t)
	f := &s3dFactory{log: log, diskQuery: func(string) (*disk.UsageStat, error) {
		return &disk.UsageStat{Total: 1000 * 1024 * 1024 * 1024}, nil // 1 TB
	}}

	s3Cfg := config.S3Config{
		Directory:      "/tmp",
		DiskUsageLimit: config.DiskUsageLimitAuto,
	}

	limitBytes, hasLimit, err := f.resolveDiskLimit(s3Cfg)
	require.NoError(t, err)
	assert.True(t, hasLimit)
	// 80% of mocked 1 TB = 800 GB — exact because the mock is actually used.
	assert.Equal(t, uint64(800*1024*1024*1024), limitBytes)
}

// Regression PR56: numeric value should be converted correctly.
func TestFactory_resolveDiskLimit_NumericValid(t *testing.T) {
	log := zaptest.NewLogger(t)
	f := &s3dFactory{log: log}

	s3Cfg := config.S3Config{
		Directory:      "/tmp",
		DiskUsageLimit: config.DiskUsageLimit("100"),
	}

	limitBytes, hasLimit, err := f.resolveDiskLimit(s3Cfg)
	require.NoError(t, err)
	assert.True(t, hasLimit)
	assert.Equal(t, uint64(100*1024*1024*1024), limitBytes)
}

// Regression PR56: unlimited (0) should return no limit.
func TestFactory_resolveDiskLimit_Zero_Unlimited(t *testing.T) {
	log := zaptest.NewLogger(t)
	f := &s3dFactory{log: log}

	s3Cfg := config.S3Config{
		Directory:      "/tmp",
		DiskUsageLimit: config.DiskUsageLimitUnlimited,
	}

	limitBytes, hasLimit, err := f.resolveDiskLimit(s3Cfg)
	require.NoError(t, err)
	assert.False(t, hasLimit)
	assert.Equal(t, uint64(0), limitBytes)
}
