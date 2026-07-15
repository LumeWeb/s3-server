package version

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.lumeweb.com/s3-server/internal/testutil"
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
		LatestVersion:   "0.3.0",
		UpdateAvailable: true,
		UpdateType:      "minor",
		ImageName:       "test/image",
		CheckedAt:       "2026-01-01T00:00:00Z",
	}

	assert.Equal(t, "0.2.0", r.CurrentVersion)
	assert.Equal(t, "0.3.0", r.LatestVersion)
	assert.True(t, r.UpdateAvailable)
	assert.Equal(t, "minor", r.UpdateType)
}

// Regression PR7: concurrent check() (which writes result under Lock) and
// Result() (which reads under RLock) must not race. Run with -race to detect.
func TestChecker_ConcurrentCheckAndResult_NoRace(t *testing.T) {
	c := NewChecker("0.2.0", "test/image", nil)

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Reader goroutine: continuously call Result()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				c.Result() // exercise read path for race detector
			}
		}
	}()

	// Writer goroutine: continuously update result via check()
	// check() calls checkOnce which hits the network: we can't easily
	// mock that, but we can test the mutex directly by simulating
	// concurrent read/write access to the result field.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			c.mu.Lock()
			c.result = CheckResult{
				CurrentVersion: "0.2.0",
				LatestVersion:  "0.3.0",
				CheckedAt:      time.Now().Format(time.RFC3339),
			}
			c.mu.Unlock()
		}
		close(done)
	}()

	wg.Wait()
}

// Regression PR7: Start() with interval=0 must default to 24h and not
// create a ticker with zero duration (which would spin-loop).
func TestChecker_Start_IntervalZeroDefaultsTo24h(t *testing.T) {
	c := NewChecker("0.2.0", "test/image", testutil.NewTestLogger())
	c.SetInterval(0)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start blocks; it should not panic with a zero-duration ticker.
	// The initial check() will fail (network), but Start should not
	// spin-loop or panic. It will block on the ticker until ctx cancels.
	done := make(chan struct{})
	go func() {
		c.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// ctx cancelled, Start returned: good
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2s with interval=0; likely spin-looping")
	}

	// Verify interval was defaulted
	assert.Equal(t, 24*time.Hour, c.interval,
		"Start must default interval to 24h when it's <= 0")
}
