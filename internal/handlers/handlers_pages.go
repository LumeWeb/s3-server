package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/SiaFoundation/s3d/s3"
	"github.com/labstack/echo/v5"
	"github.com/samber/lo"
	"go.lumeweb.com/s3-server/internal/build"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/status"
	"go.lumeweb.com/s3-server/internal/updater"
	"go.lumeweb.com/s3-server/internal/views"
	"go.uber.org/zap"
)

func (s *Services) dashboardPage(c *echo.Context) error {
	groups := s.groupedKeys()
	userCount := len(groups)
	bucketCount := 0
	b := s.getBackend()
	if b != nil {
		for _, g := range groups {
			if count, err := b.BucketCountForUser(c.Request().Context(), g.UserName); err == nil {
				bucketCount += count
			}
		}
	}
	return views.Dashboard(s.version, groups, userCount, bucketCount, s.csrfToken(c)).Render(c.Request().Context(), c.Response())
}

func (s *Services) settingsPage(c *echo.Context) error {
	s3Cfg := s.store.S3Config()
	sslCfg := s.store.SSLConfig()
	logCfg := s.store.LogConfig()
	var updateStatus updater.UpdateStatus
	if s.updater != nil {
		updateStatus = s.updater.Status()
	}
	return views.Settings(s.csrfToken(c), s3Cfg, sslCfg, logCfg, updateStatus, s.platformName, build.PanelVersion, build.S3dVersion).Render(c.Request().Context(), c.Response())
}

func (s *Services) usersPage(c *echo.Context) error {
	ks, err := s.requireKeyStore(c)
	if err != nil {
		return s.renderPageError(c, "Backend not started", "The S3 backend hasn't started yet. This can happen after initial setup or a configuration change.")
	}

	users, err := ks.ListUsers()
	if err != nil {
		s.log.Error("failed to list users", zap.Error(err))
		return s.renderPageError(c, "Failed to load users", "An error occurred while fetching the user list.")
	}

	keys, err := ks.ListAccessKeys(nil)
	if err != nil {
		s.log.Error("failed to list access keys", zap.Error(err))
		return s.renderPageError(c, "Failed to load access keys", "An error occurred while fetching the access key list.")
	}
	keyCounts := make(map[string]int, len(keys))
	for _, k := range keys {
		keyCounts[k.UserName]++
	}

	viewUsers := make([]views.UserInfo, len(users))
	b := s.getBackend()
	for i, name := range users {
		bucketCount := 0
		if b != nil {
			if count, err := b.BucketCountForUser(c.Request().Context(), name); err == nil {
				bucketCount = count
			} else {
				s.log.Warn("failed to get bucket count for user", zap.String("user", name), zap.Error(err))
			}
		}
		viewUsers[i] = views.UserInfo{
			Name:        name,
			KeyCount:    keyCounts[name],
			BucketCount: bucketCount,
		}
	}

	return views.Users(viewUsers, s.csrfToken(c)).Render(c.Request().Context(), c.Response())
}

func (s *Services) keysPage(c *echo.Context) error {
	groups := s.groupedKeys()

	// Optional ?user=<name> filter: when navigating from the users page.
	if filterUser := c.QueryParam("user"); filterUser != "" {
		found := false
		filtered := make([]views.UserKeyGroup, 0, 1)
		for _, g := range groups {
			if g.UserName == filterUser {
				filtered = append(filtered, g)
				found = true
				break
			}
		}
		if !found {
			// User exists but has no keys: show an empty group so the
			// page renders "No access keys yet." instead of a blank list.
			filtered = append(filtered, views.UserKeyGroup{
				UserName: filterUser,
				Keys:     []config.KeyPair{},
			})
		}
		groups = filtered
	}

	// Fetch bucket counts per user via the read-only SQLite handle.
	b := s.getBackend()
	if b != nil {
		for i := range groups {
			if count, err := b.BucketCountForUser(c.Request().Context(), groups[i].UserName); err == nil {
				groups[i].BucketCount = count
			}
		}
	}

	backendRunning := false
	if s.backendStatus != nil {
		backendRunning = s.backendStatus() == status.Running
	}
	return views.Keys(groups, s.csrfToken(c), backendRunning).Render(c.Request().Context(), c.Response())
}

func (s *Services) bucketsPage(c *echo.Context) error {
	b := s.getBackend()
	if b == nil {
		return s.renderPageError(c, "Backend not started", "The S3 backend hasn't started yet. This can happen after initial setup or a configuration change.")
	}

	ks, err := s.requireKeyStore(c)
	if err != nil {
		return s.renderPageError(c, "Backend not started", "The S3 backend hasn't started yet. This can happen after initial setup or a configuration change.")
	}
	users, err := ks.ListUsers()
	if err != nil {
		s.log.Error("failed to list users", zap.Error(err))
		return s.renderPageError(c, "Failed to load users", "An error occurred while fetching the user list.")
	}

	buckets, err := b.ListAllBuckets(c.Request().Context())
	if err != nil {
		s.log.Error("failed to list buckets", zap.Error(err))
		return s.renderPageError(c, "Failed to load buckets", "An error occurred while fetching the bucket list.")
	}

	viewBuckets := make([]views.BucketInfo, len(buckets))
	for i, bucket := range buckets {
		count, size, err := b.BucketStats(c.Request().Context(), bucket.Name)
		if err != nil {
			s.log.Warn("failed to get bucket stats", zap.String("bucket", bucket.Name), zap.Error(err))
		}
		versioning, err := b.BucketVersioning(c.Request().Context(), bucket.Name)
		if err != nil {
			s.log.Warn("failed to get bucket versioning", zap.String("bucket", bucket.Name), zap.Error(err))
		}
		viewBuckets[i] = views.BucketInfo{
			Name:        bucket.Name,
			Owner:       bucket.Owner,
			CreatedAt:   formatPanelTime(bucket.CreatedAt),
			ObjectCount: count,
			TotalSize:   size,
			Versioning:  versioning,
		}
	}

	return views.Buckets(viewBuckets, users, s.csrfToken(c)).Render(c.Request().Context(), c.Response())
}

func (s *Services) backupsPage(c *echo.Context) error {
	dir := s.store.S3Config().Directory
	if dir == "" {
		return s.renderPageError(c, "No data directory", "The data directory is not configured. Set it in Settings before managing backups.")
	}

	backupsDir := filepath.Join(dir, "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return views.Backups(nil, s.csrfToken(c)).Render(c.Request().Context(), c.Response())
		}
		s.log.Error("failed to list backups", zap.Error(err))
		return s.renderPageError(c, "Failed to load backups", "An error occurred while reading the backups directory.")
	}

	viewBackups := lo.FilterMap(entries, func(entry os.DirEntry, _ int) (views.BackupInfo, bool) {
		if entry.IsDir() {
			return views.BackupInfo{}, false
		}
		info, err := entry.Info()
		if err != nil {
			return views.BackupInfo{}, false
		}
		return views.BackupInfo{
			Filename:  info.Name(),
			Size:      info.Size(),
			CreatedAt: formatPanelTime(info.ModTime()),
		}, true
	})

	sort.Slice(viewBackups, func(i, j int) bool {
		return viewBackups[i].CreatedAt > viewBackups[j].CreatedAt
	})

	return views.Backups(viewBackups, s.csrfToken(c)).Render(c.Request().Context(), c.Response())
}

func (s *Services) monitoringPage(c *echo.Context) error {
	stats, err := s.fetchUploadStats(c)
	if err != nil {
		s.log.Error("failed to load monitoring stats", zap.Error(err))
		return s.renderPageError(c, "Failed to load monitoring", "An error occurred while fetching monitoring stats.")
	}
	return views.Monitoring(stats, s.csrfToken(c)).Render(c.Request().Context(), c.Response())
}

func (s *Services) fetchUploadStats(c *echo.Context) (views.UploadStats, error) {
	var stats views.UploadStats
	if s.adminHandler == nil {
		return stats, errors.New("admin handler not configured")
	}

	req := c.Request().Clone(c.Request().Context())
	req.URL.Path = "/stats/uploads"

	rec := &captureRecorder{header: make(http.Header)}
	s.adminHandler.ServeHTTP(rec, req)
	if rec.code == 0 {
		rec.code = http.StatusOK
	}
	if rec.code >= 300 {
		return stats, fmt.Errorf("admin stats returned status %d", rec.code)
	}

	var s3stats s3.UploadStats
	if err := json.Unmarshal(rec.body.Bytes(), &s3stats); err != nil {
		return stats, err
	}

	stats.PendingObjects = s3stats.PendingObjects
	stats.PendingSize = s3stats.PendingSize
	stats.UploadedObjects = s3stats.UploadedObjects
	stats.UploadedSize = s3stats.UploadedSize
	stats.UnpinnedObjects = s3stats.UnpinnedObjects
	stats.FailedUploads = s3stats.FailedUploads
	stats.OrphanedObjects = s3stats.OrphanedObjects
	stats.MultipartUploads = s3stats.MultipartUploads

	// Fetch account info: non-fatal. Zero values are fine if unavailable.
	if s.accountClient != nil {
		if ac := s.accountClient(); ac != nil {
			if info, err := ac.Account(c.Request().Context()); err == nil {
				stats.AccountMaxPinnedData = info.MaxPinnedData
				stats.AccountRemainingStorage = info.RemainingStorage
				stats.AccountPinnedData = info.PinnedData
				stats.AccountPinnedSize = info.PinnedSize
				stats.AccountReady = info.Ready
			}
		}
	}

	return stats, nil
}

// groupedKeys returns all access keys grouped by owning user, with the
// default user (s3d) always first.
func (s *Services) groupedKeys() []views.UserKeyGroup {
	s.keyMu.RLock()
	defer s.keyMu.RUnlock()
	return s.groupedKeysLocked()
}

// groupedKeysLocked builds the key groups without acquiring keyMu.
// Caller must hold keyMu (read or write).
func (s *Services) groupedKeysLocked() []views.UserKeyGroup {
	keys := s.listAccessKeysLocked()

	// Build the user order from ListUsers so users with zero keys appear.
	ks := s.getKeyStore()
	var allUsers []string
	if ks != nil {
		if users, err := ks.ListUsers(); err == nil {
			allUsers = users
		}
	}

	var order []string
	seen := map[string]bool{}
	for _, u := range allUsers {
		if u == "" || seen[u] {
			continue
		}
		order = append(order, u)
		seen[u] = true
	}
	var groups []views.UserKeyGroup

	byUser := map[string][]config.KeyPair{}
	for _, k := range keys {
		u := k.UserName
		if !seen[u] {
			order = append(order, u)
			seen[u] = true
		}
		byUser[u] = append(byUser[u], config.KeyPair{
			AccessKey: k.AccessKeyID,
			SecretKey: k.SecretKey,
		})
	}

	for _, u := range order {
		groups = append(groups, views.UserKeyGroup{
			UserName: u,
			Keys:     byUser[u],
		})
	}
	return groups
}

func formatPanelTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}
