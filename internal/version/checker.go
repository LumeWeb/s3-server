package version

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"go.uber.org/zap"
)

// Checker is the interface for checking for new versions.
type Checker interface {
	Result() CheckResult
	Start(ctx context.Context)
}

type CheckResult struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	UpdateType     string `json:"update_type,omitempty"`
	ImageName      string `json:"image_name"`
	CheckedAt      string `json:"checked_at"`
}

// GHCRChecker implements Checker by polling GHCR tags.
type GHCRChecker struct {
	currentVersion string
	imageName      string
	interval       time.Duration
	log            *zap.Logger

	result CheckResult
}

func NewChecker(currentVersion, imageName string, log *zap.Logger) *GHCRChecker {
	return &GHCRChecker{
		currentVersion: currentVersion,
		imageName:      imageName,
		interval:       24 * time.Hour,
		log:            log,
		result: CheckResult{
			CurrentVersion: currentVersion,
			ImageName:      imageName,
		},
	}
}

func (c *GHCRChecker) SetInterval(d time.Duration) {
	c.interval = d
}

func (c *GHCRChecker) Result() CheckResult {
	return c.result
}

func (c *GHCRChecker) Start(ctx context.Context) {
	c.check(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.check(ctx)
		}
	}
}

func (c *GHCRChecker) check(ctx context.Context) {
	result, err := c.checkOnce(ctx)
	if err != nil {
		c.log.Debug("version check failed", zap.Error(err))
		return
	}

	c.result = *result

	if result.UpdateAvailable {
		c.log.Info("update available",
			zap.String("current", result.CurrentVersion),
			zap.String("latest", result.LatestVersion),
			zap.String("type", result.UpdateType),
		)
	}
}

func (c *GHCRChecker) checkOnce(ctx context.Context) (*CheckResult, error) {
	latest, err := c.fetchLatestTag(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest version: %w", err)
	}

	current, err := semver.NewVersion(strings.TrimPrefix(c.currentVersion, "v"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse current version: %w", err)
	}

	latestVer, err := semver.NewVersion(strings.TrimPrefix(latest, "v"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse latest version: %w", err)
	}

	result := &CheckResult{
		CurrentVersion:  c.currentVersion,
		LatestVersion:   latest,
		UpdateAvailable: latestVer.GreaterThan(current),
		ImageName:       c.imageName,
		CheckedAt:       time.Now().Format(time.RFC3339),
	}

	if result.UpdateAvailable {
		switch {
		case latestVer.Major() > current.Major():
			result.UpdateType = "major"
		case latestVer.Minor() > current.Minor():
			result.UpdateType = "minor"
		default:
			result.UpdateType = "patch"
		}
	}

	return result, nil
}

func (c *GHCRChecker) fetchLatestTag(ctx context.Context) (string, error) {
	repo, err := name.NewRepository(c.imageName, name.WithDefaultRegistry("ghcr.io"))
	if err != nil {
		return "", fmt.Errorf("invalid image name: %w", err)
	}

	tags, err := remote.List(repo, remote.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("failed to list tags: %w", err)
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("no tags found")
	}

	var latest *semver.Version
	var latestStr string
	for _, tag := range tags {
		v, err := semver.NewVersion(tag)
		if err != nil {
			continue
		}
		if latest == nil || v.GreaterThan(latest) {
			latest = v
			latestStr = tag
		}
	}

	if latest == nil {
		return "", fmt.Errorf("no valid semver tags found")
	}

	return latestStr, nil
}
