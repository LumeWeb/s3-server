package version

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/s3-server/internal/testutil"
)

// --- fetchLatestTag error paths (standalone function) ---

func TestFetchLatestTag_InvalidImageName(t *testing.T) {
	_, err := fetchLatestTag(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid image name")
}

func TestFetchLatestTag_UnreachableRepo(t *testing.T) {
	_, err := fetchLatestTag(context.Background(), "nonexistent-fake-repo-12345/image")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list tags")
}

// --- pickLatestSemverTag (pure function) ---

func TestPickLatestSemverTag(t *testing.T) {
	tests := []struct {
		name    string
		tags    []string
		want    string
		wantErr bool
	}{
		{
			name: "simple ascending",
			tags: []string{"1.0.0", "1.1.0", "1.2.0"},
			want: "1.2.0",
		},
		{
			name: "unsorted",
			tags: []string{"2.0.0", "1.0.0", "1.5.0", "1.10.0"},
			want: "2.0.0",
		},
		{
			name: "with v prefix",
			tags: []string{"v1.0.0", "v2.0.0", "v1.5.0"},
			want: "v2.0.0",
		},
		{
			name: "mixed semver and non-semver",
			tags: []string{"latest", "1.0.0", "main", "2.0.0", "nightly"},
			want: "2.0.0",
		},
		{
			name: "prerelease tags",
			tags: []string{"1.0.0", "1.1.0-alpha", "1.0.1"},
			want: "1.1.0-alpha", // semver: prerelease of 1.1.0 > 1.0.1 but < 1.1.0
		},
		{
			name: "empty list",
			tags: []string{},
			wantErr: true,
		},
		{
			name: "no valid semver",
			tags: []string{"latest", "main", "nightly"},
			wantErr: true,
		},
		{
			name: "single tag",
			tags: []string{"1.5.0"},
			want: "1.5.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pickLatestSemverTag(tt.tags)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no valid semver tags")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- compareVersions (pure function) ---

func TestCompareVersions(t *testing.T) {
	mk := func(s string) *semver.Version {
		v, err := semver.NewVersion(s)
		require.NoError(t, err)
		return v
	}

	tests := []struct {
		name           string
		currentStr     string
		latestStr      string
		current        *semver.Version
		latest         *semver.Version
		wantUpdate     bool
		wantUpdateType string
	}{
		{
			name:       "patch update",
			currentStr: "1.0.0",
			latestStr:  "1.0.1",
			current:    mk("1.0.0"),
			latest:     mk("1.0.1"),
			wantUpdate: true,
			wantUpdateType: "patch",
		},
		{
			name:       "minor update",
			currentStr: "1.0.0",
			latestStr:  "1.1.0",
			current:    mk("1.0.0"),
			latest:     mk("1.1.0"),
			wantUpdate: true,
			wantUpdateType: "minor",
		},
		{
			name:       "major update",
			currentStr: "1.5.3",
			latestStr:  "2.0.0",
			current:    mk("1.5.3"),
			latest:     mk("2.0.0"),
			wantUpdate: true,
			wantUpdateType: "major",
		},
		{
			name:       "no update (same version)",
			currentStr: "1.0.0",
			latestStr:  "1.0.0",
			current:    mk("1.0.0"),
			latest:    mk("1.0.0"),
			wantUpdate: false,
		},
		{
			name:       "no update (older)",
			currentStr: "2.0.0",
			latestStr:  "1.0.0",
			current:    mk("2.0.0"),
			latest:    mk("1.0.0"),
			wantUpdate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareVersions(tt.currentStr, tt.latestStr, "test/image", tt.current, tt.latest)
			assert.Equal(t, tt.currentStr, result.CurrentVersion)
			assert.Equal(t, tt.latestStr, result.LatestVersion)
			assert.Equal(t, "test/image", result.ImageName)
			assert.NotEmpty(t, result.CheckedAt)
			assert.Equal(t, tt.wantUpdate, result.UpdateAvailable)
			if tt.wantUpdate {
				assert.Equal(t, tt.wantUpdateType, result.UpdateType)
			} else {
				assert.Empty(t, result.UpdateType)
			}
		})
	}
}

// --- checkOnce with injected tagFetcher ---

func TestCheckOnce_UpdateAvailable(t *testing.T) {
	c := NewChecker("1.0.0", "test/image", testutil.NewTestLogger())
	c.WithTagFetcher(func(ctx context.Context, imageName string) (string, error) {
		assert.Equal(t, "test/image", imageName)
		return "1.1.0", nil
	})

	result, err := c.checkOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", result.CurrentVersion)
	assert.Equal(t, "1.1.0", result.LatestVersion)
	assert.True(t, result.UpdateAvailable)
	assert.Equal(t, "minor", result.UpdateType)
}

func TestCheckOnce_NoUpdate(t *testing.T) {
	c := NewChecker("2.0.0", "test/image", testutil.NewTestLogger())
	c.WithTagFetcher(func(ctx context.Context, _ string) (string, error) {
		return "2.0.0", nil
	})

	result, err := c.checkOnce(context.Background())
	require.NoError(t, err)
	assert.False(t, result.UpdateAvailable)
	assert.Empty(t, result.UpdateType)
}

func TestCheckOnce_MajorUpdate(t *testing.T) {
	c := NewChecker("1.5.3", "test/image", testutil.NewTestLogger())
	c.WithTagFetcher(func(ctx context.Context, _ string) (string, error) {
		return "2.0.0", nil
	})

	result, err := c.checkOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, result.UpdateAvailable)
	assert.Equal(t, "major", result.UpdateType)
}

func TestCheckOnce_PatchUpdate(t *testing.T) {
	c := NewChecker("1.0.0", "test/image", testutil.NewTestLogger())
	c.WithTagFetcher(func(ctx context.Context, _ string) (string, error) {
		return "1.0.1", nil
	})

	result, err := c.checkOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, result.UpdateAvailable)
	assert.Equal(t, "patch", result.UpdateType)
}

func TestCheckOnce_VPrefixStripped(t *testing.T) {
	c := NewChecker("v1.0.0", "test/image", testutil.NewTestLogger())
	c.WithTagFetcher(func(ctx context.Context, _ string) (string, error) {
		return "v1.0.1", nil
	})

	result, err := c.checkOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, result.UpdateAvailable)
	assert.Equal(t, "patch", result.UpdateType)
}

func TestCheckOnce_InvalidCurrentVersion(t *testing.T) {
	c := NewChecker("not-a-version", "test/image", testutil.NewTestLogger())
	c.WithTagFetcher(func(ctx context.Context, _ string) (string, error) {
		return "1.0.0", nil
	})

	_, err := c.checkOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse current version")
}

func TestCheckOnce_InvalidLatestVersion(t *testing.T) {
	c := NewChecker("1.0.0", "test/image", testutil.NewTestLogger())
	c.WithTagFetcher(func(ctx context.Context, _ string) (string, error) {
		return "not-a-version", nil
	})

	_, err := c.checkOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse latest version")
}

func TestCheckOnce_FetchError(t *testing.T) {
	c := NewChecker("1.0.0", "test/image", testutil.NewTestLogger())
	c.WithTagFetcher(func(ctx context.Context, _ string) (string, error) {
		return "", errors.New("network unreachable")
	})

	_, err := c.checkOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch latest version")
}

// --- check (logs error, updates result on success) ---

func TestCheck_FetchError_DoesNotUpdateResult(t *testing.T) {
	c := NewChecker("1.0.0", "test/image", testutil.NewTestLogger())
	c.WithTagFetcher(func(ctx context.Context, _ string) (string, error) {
		return "", errors.New("network unreachable")
	})

	c.check(context.Background())

	result := c.Result()
	assert.Equal(t, "1.0.0", result.CurrentVersion)
	assert.Empty(t, result.LatestVersion)
	assert.False(t, result.UpdateAvailable)
}

func TestCheck_Success_UpdatesResult(t *testing.T) {
	c := NewChecker("1.0.0", "test/image", testutil.NewTestLogger())
	c.WithTagFetcher(func(ctx context.Context, _ string) (string, error) {
		return "1.2.0", nil
	})

	c.check(context.Background())

	result := c.Result()
	assert.Equal(t, "1.0.0", result.CurrentVersion)
	assert.Equal(t, "1.2.0", result.LatestVersion)
	assert.True(t, result.UpdateAvailable)
	assert.Equal(t, "minor", result.UpdateType)
	assert.NotEmpty(t, result.CheckedAt)
}

// --- Result immutability ---

func TestResult_ReturnsCopy(t *testing.T) {
	c := NewChecker("1.0.0", "test/image", nil)
	r1 := c.Result()
	r1.CurrentVersion = "modified"

	r2 := c.Result()
	assert.Equal(t, "1.0.0", r2.CurrentVersion, "Result() must return a copy, not a reference")
}

// --- Start with short context ---

func TestStart_ContextCancellation(t *testing.T) {
	c := NewChecker("1.0.0", "test/image", testutil.NewTestLogger())
	c.SetInterval(10 * time.Millisecond)
	c.WithTagFetcher(func(ctx context.Context, _ string) (string, error) {
		return "", errors.New("should not be called more than once")
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		c.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2s after context cancellation")
	}
}
