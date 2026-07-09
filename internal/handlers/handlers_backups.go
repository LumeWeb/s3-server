package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/samber/lo"
	"go.lumeweb.com/s3-server/internal/api"
)

type BackupInfo struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

type ListBackupsResponse struct {
	Files []BackupInfo `json:"files"`
}

type BackupResponse struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
}

func (s *Services) createBackup(c *echo.Context) error {
	if s.adminHandler == nil {
		return api.SendNotReady(c, api.TypeAdminHandlerNotConfigured, "admin handler not configured", nil)
	}

	dir := s.store.S3Config().Directory
	if dir == "" {
		return api.SendValidation(c, api.TypeDataDirectoryNotConfigured, "data directory not configured")
	}

	backupsDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backupsDir, 0750); err != nil {
		return api.SendInternal(c, api.TypeBackupDirCreateFailed, "failed to create backups directory", err)
	}

	filename := fmt.Sprintf("s3d-backup-%s.db.gz", time.Now().UTC().Format("20060102-150405"))
	destPath := filepath.Join(backupsDir, filename)
	for i := 1; ; i++ {
		_, err := os.Stat(destPath)
		if os.IsNotExist(err) {
			break
		} else if err != nil {
			return api.SendInternal(c, api.TypeBackupPathCheckFailed, "failed to check backup path", err)
		}
		filename = fmt.Sprintf("s3d-backup-%s-%d.db.gz", time.Now().UTC().Format("20060102-150405"), i)
		destPath = filepath.Join(backupsDir, filename)
	}

	body, _ := json.Marshal(map[string]string{"path": destPath})
	proxyReq := c.Request().Clone(c.Request().Context())
	proxyURL := *c.Request().URL
	proxyReq.URL = &proxyURL
	proxyReq.Method = http.MethodPost
	proxyReq.URL.Path = "/system/sqlite3/backup"
	proxyReq.Body = io.NopCloser(bytes.NewReader(body))
	proxyReq.ContentLength = int64(len(body))
	proxyReq.Header = proxyReq.Header.Clone()
	proxyReq.Header.Set("Content-Type", "application/json")

	rec := &captureRecorder{header: make(http.Header)}
	s.adminHandler.ServeHTTP(rec, proxyReq)
	if rec.code >= 300 {
		for k, v := range rec.header {
			for _, vv := range v {
				c.Response().Header().Add(k, vv)
			}
		}
		c.Response().WriteHeader(rec.code)
		c.Response().Write(rec.body.Bytes()) //nolint:errcheck
		return nil
	}

	return c.JSON(http.StatusOK, BackupResponse{Filename: filename, Path: destPath})
}

func (s *Services) listBackups(c *echo.Context) error {
	dir := s.store.S3Config().Directory
	if dir == "" {
		return api.SendValidation(c, api.TypeDataDirectoryNotConfigured, "data directory not configured")
	}
	backupsDir := filepath.Join(dir, "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return c.JSON(http.StatusOK, ListBackupsResponse{Files: []BackupInfo{}})
		}
		return api.SendInternal(c, api.TypeBackupListFailed, "failed to list backups", err)
	}

	files := lo.FilterMap(entries, func(entry os.DirEntry, _ int) (BackupInfo, bool) {
		if entry.IsDir() {
			return BackupInfo{}, false
		}
		info, err := entry.Info()
		if err != nil {
			return BackupInfo{}, false
		}
		return BackupInfo{
			Name:       info.Name(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime(),
		}, true
	})
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModifiedAt.After(files[j].ModifiedAt)
	})
	return c.JSON(http.StatusOK, ListBackupsResponse{Files: files})
}

func (s *Services) getBackup(c *echo.Context) error {
	return s.serveBackup(c, false)
}

func (s *Services) deleteBackup(c *echo.Context) error {
	return s.serveBackup(c, true)
}

func (s *Services) serveBackup(c *echo.Context, delete bool) error {
	filename, err := s.requireParam(c, "filename")
	if err != nil {
		return err
	}
	if filepath.Base(filename) != filename || filename == "." || filename == ".." {
		return api.SendBadRequest(c, api.TypeInvalidFilename, "invalid filename")
	}

	dir := s.store.S3Config().Directory
	if dir == "" {
		return api.SendValidation(c, api.TypeDataDirectoryNotConfigured, "data directory not configured")
	}
	backupsDir := filepath.Join(dir, "backups")
	path := filepath.Join(backupsDir, filename)
	cleanPath := filepath.Clean(path)
	cleanBackupsDir := filepath.Clean(backupsDir)
	if !stringsHasPrefix(cleanPath, cleanBackupsDir+string(filepath.Separator)) && cleanPath != cleanBackupsDir {
		return api.SendBadRequest(c, api.TypeInvalidFilename, "invalid filename")
	}

	if delete {
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				return api.SendNotFound(c, api.TypeBackupNotFound, "backup not found")
			}
			return api.SendInternal(c, api.TypeBackupDeleteFailed, "failed to delete backup", err)
		}
		return c.NoContent(http.StatusNoContent)
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return api.SendNotFound(c, api.TypeBackupNotFound, "backup not found")
		}
		return api.SendInternal(c, api.TypeBackupStatFailed, "failed to stat backup", err)
	}
	if info.IsDir() {
		return api.SendBadRequest(c, api.TypeInvalidFilename, "invalid filename")
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return api.SendNotFound(c, api.TypeBackupNotFound, "backup not found")
		}
		return api.SendInternal(c, api.TypeBackupOpenFailed, "failed to open backup", err)
	}
	defer f.Close()

	c.Response().Header().Set("Content-Type", "application/octet-stream")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	http.ServeContent(c.Response(), c.Request(), filename, info.ModTime(), f)
	return nil
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[0:len(prefix)] == prefix
}
