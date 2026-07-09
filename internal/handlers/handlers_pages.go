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
	"go.lumeweb.com/s3-server/internal/api"
	"go.lumeweb.com/s3-server/internal/backend"
	"go.lumeweb.com/s3-server/internal/build"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/status"
	"go.lumeweb.com/s3-server/internal/updater"
	"go.lumeweb.com/s3-server/internal/views"
)

func (s *Services) dashboardPage(c *echo.Context) error {
	groups := s.groupedKeys()
	return views.Dashboard(s.version, groups, s.csrfToken(c)).Render(c.Request().Context(), c.Response())
}

func (s *Services) settingsPage(c *echo.Context) error {
	s3Cfg := s.store.S3Config()
	sslCfg := s.store.SSLConfig()
	var updateStatus updater.UpdateStatus
	if s.updater != nil {
		updateStatus = s.updater.Status()
	}
	return views.Settings(s.csrfToken(c), s3Cfg, sslCfg, updateStatus, s.platformName, build.PanelVersion, build.S3dVersion).Render(c.Request().Context(), c.Response())
}

func (s *Services) usersPage(c *echo.Context) error {
	ks, err := s.requireKeyStore(c)
	if err != nil {
		return err
	}

	users, err := ks.ListUsers()
	if err != nil {
		return api.SendInternal(c, api.TypeUserListFailed, "failed to list users", err)
	}

	keys, err := ks.ListAccessKeys(nil)
	if err != nil {
		return api.SendInternal(c, api.TypeAccessKeyListFailed, "failed to list access keys", err)
	}
	keyCounts := make(map[string]int, len(keys))
	for _, k := range keys {
		keyCounts[k.UserName]++
	}

	viewUsers := make([]views.UserInfo, len(users))
	for i, name := range users {
		viewUsers[i] = views.UserInfo{
			Name:     name,
			KeyCount: keyCounts[name],
		}
	}

	return views.Users(viewUsers, s.csrfToken(c)).Render(c.Request().Context(), c.Response())
}

func (s *Services) keysPage(c *echo.Context) error {
	groups := s.groupedKeys()
	backendRunning := false
	if s.backendStatus != nil {
		backendRunning = s.backendStatus() == status.Running
	}
	return views.Keys(groups, s.csrfToken(c), backendRunning).Render(c.Request().Context(), c.Response())
}

func (s *Services) bucketsPage(c *echo.Context) error {
	b, accessKey, err := s.requireBackend(c)
	if err != nil {
		return err
	}

	buckets, err := b.ListBuckets(c.Request().Context(), accessKey)
	if err != nil {
		return api.SendInternal(c, api.TypeBucketListFailed, "failed to list buckets", err)
	}

	viewBuckets := make([]views.BucketInfo, len(buckets))
	for i, bucket := range buckets {
		viewBuckets[i] = views.BucketInfo{
			Name:       bucket.Name,
			CreatedAt:  formatPanelTime(bucket.CreationDate.Time),
			Versioning: "", // fetched lazily via HTMX to avoid N+1 backend calls
		}
	}

	return views.Buckets(viewBuckets, s.csrfToken(c)).Render(c.Request().Context(), c.Response())
}

func (s *Services) backupsPage(c *echo.Context) error {
	dir := s.store.S3Config().Directory
	if dir == "" {
		return api.SendValidation(c, api.TypeDataDirectoryNotConfigured, "data directory not configured")
	}

	backupsDir := filepath.Join(dir, "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return views.Backups(nil, s.csrfToken(c)).Render(c.Request().Context(), c.Response())
		}
		return api.SendInternal(c, api.TypeBackupListFailed, "failed to list backups", err)
	}

	viewBackups := make([]views.BackupInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		viewBackups = append(viewBackups, views.BackupInfo{
			Filename:  info.Name(),
			Size:      info.Size(),
			CreatedAt: formatPanelTime(info.ModTime()),
		})
	}

	sort.Slice(viewBackups, func(i, j int) bool {
		return viewBackups[i].CreatedAt > viewBackups[j].CreatedAt
	})

	return views.Backups(viewBackups, s.csrfToken(c)).Render(c.Request().Context(), c.Response())
}

func (s *Services) monitoringPage(c *echo.Context) error {
	stats, err := s.fetchUploadStats(c)
	if err != nil {
		return api.SendInternal(c, api.TypeMonitoringStatsFailed, "failed to load monitoring stats", err)
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
	order := []string{backend.DefaultUserName}
	seen := map[string]bool{backend.DefaultUserName: true}
	var groups []views.UserKeyGroup

	byUser := map[string][]config.KeyPair{}
	for _, k := range keys {
		u := k.UserName
		if u == "" {
			u = backend.DefaultUserName
		}
		if !seen[u] {
			order = append(order, u)
			seen[u] = true
		}
		byUser[u] = append(byUser[u], config.KeyPair{
			AccessKey: k.AccessKeyID,
			SecretKey: "", // never expose secret in rendered HTML; only returned once at creation
		})
	}

	for _, u := range order {
		if _, ok := byUser[u]; !ok {
			continue
		}
		groups = append(groups, views.UserKeyGroup{
			UserName:  u,
			IsDefault: u == backend.DefaultUserName,
			Keys:      byUser[u],
		})
	}
	return groups
}

func formatPanelTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}
