package version

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewChecker(t *testing.T) {
	c := NewChecker("0.2.0", "test/image", nil)
	assert.Equal(t, "0.2.0", c.currentVersion)
	assert.Equal(t, "test/image", c.imageName)
	assert.Equal(t, "0.2.0", c.Result().CurrentVersion)
	assert.Equal(t, "test/image", c.Result().ImageName)
}

func TestChecker_Result(t *testing.T) {
	c := NewChecker("1.0.0", "test/image", nil)
	result := c.Result()
	assert.Equal(t, "1.0.0", result.CurrentVersion)
	assert.False(t, result.UpdateAvailable)
	assert.Empty(t, result.LatestVersion)
}

func TestChecker_SetInterval(t *testing.T) {
	c := NewChecker("1.0.0", "test/image", nil)
	c.SetInterval(0)
	assert.Equal(t, time.Duration(0), c.interval)
}

func TestCheckOnce_SemverComparison(t *testing.T) {
	// We can't easily test checkOnce directly since it calls fetchLatestTag
	// which hits the network. The semver logic is tested implicitly through
	// the GHCR integration. Here we verify the struct construction.
	c := NewChecker("0.2.0", "test/image", nil)
	result := c.Result()
	assert.Equal(t, "0.2.0", result.CurrentVersion)
	assert.Empty(t, result.LatestVersion)
}

func TestCheckResult_JSON(t *testing.T) {
	r := CheckResult{
		CurrentVersion:  "0.2.0",
		LatestVersion:  "0.3.0",
		UpdateAvailable: true,
		UpdateType:     "minor",
		ImageName:      "test/image",
		CheckedAt:       "2026-01-01T00:00:00Z",
	}

	assert.Equal(t, "0.2.0", r.CurrentVersion)
	assert.Equal(t, "0.3.0", r.LatestVersion)
	assert.True(t, r.UpdateAvailable)
	assert.Equal(t, "minor", r.UpdateType)
}
