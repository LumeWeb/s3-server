package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
	"net/http"

	"github.com/labstack/echo/v5"
	"go.lumeweb.com/s3-server/internal/api"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/status"
)

func (s *Services) getS3Config(c *echo.Context) error {
	cfg := s.store.S3Config()
	return c.JSON(http.StatusOK, S3ConfigResponse{
		Directory:          cfg.Directory,
		IndexerURL:         cfg.IndexerURL,
		AvailableIndexers:  cfg.AvailableIndexers,
		HostBases:          cfg.HostBases,
		DiskUsageLimit:     cfg.DiskUsageLimit,
		UploadWastePct:     cfg.UploadWastePct,
	})
}

func (s *Services) setS3Config(c *echo.Context) error {
	var req SetS3ConfigRequest
	if err := bindJSON(c, s.log, &req); err != nil {
		return err
	}

	if req.Directory == "" {
		return api.SendValidation(c, api.TypeDirectoryRequired, "directory is required")
	}
	if req.IndexerURL == "" {
		return api.SendValidation(c, api.TypeIndexerURLRequired, "indexer_url is required")
	}

	// Indexer cannot be changed after onboarding: requires data migration.
	current := s.store.S3Config()
	if current.IndexerURL != "" && current.IndexerURL != req.IndexerURL {
		return api.SendValidation(c, api.TypeIndexerLocked, "indexer cannot be changed after initial setup. Changing the indexer requires migrating existing data.")
	}

	if err := s.store.SetS3Config(config.S3Config{
		Directory:          req.Directory,
		IndexerURL:         req.IndexerURL,
		AvailableIndexers:  current.AvailableIndexers,
		HostBases:          req.HostBases,
		DiskUsageLimit:     req.DiskUsageLimit,
		UploadWastePct:     req.UploadWastePct,
	}); err != nil {
		return api.SendInternal(c, api.TypeS3ConfigSaveFailed, "failed to save S3 config", err)
	}

	return c.JSON(http.StatusOK, ConfigUpdateResponse{
		Status:  "updated",
		Message: "S3 configuration saved. Backend restart required for changes to take effect.",
	})
}

func (s *Services) getSSLConfig(c *echo.Context) error {
	cfg := s.store.SSLConfig()
	return c.JSON(http.StatusOK, SSLConfigResponse{
		Mode:       string(cfg.Mode),
		ACMEEmail:  cfg.ACMEEmail,
		ACMEDirURL: cfg.ACMEDirURL,
	})
}

func (s *Services) setSSLConfig(c *echo.Context) error {
	var req SetSSLConfigRequest
	if err := bindJSON(c, s.log, &req); err != nil {
		return err
	}

	mode := config.SSLMode(req.Mode)
	switch mode {
	case config.SSLModeNone, config.SSLModePlatform, config.SSLModeManaged:
		// valid
	default:
		return api.SendValidation(c, api.TypeSSLModeInvalid, "mode must be none, platform, or managed")
	}

	if mode == config.SSLModeManaged && req.ACMEEmail == "" {
		return api.SendValidation(c, api.TypeACMEEmailRequired, "acme_email is required for managed SSL")
	}

	if err := s.store.SetSSLConfig(config.SSLConfig{
		Mode:       mode,
		ACMEEmail:  req.ACMEEmail,
		ACMEDirURL: req.ACMEDirURL,
	}); err != nil {
		return api.SendInternal(c, api.TypeSSLConfigSaveFailed, "failed to save SSL config", err)
	}

	// SSL config change requires server restart: signal the process
	// The caller should display a message that the server will restart.
	return c.JSON(http.StatusOK, ConfigUpdateResponse{
		Status:  "updated",
		Message: "SSL configuration saved. Server restart required for changes to take effect.",
	})
}

func (s *Services) getLogConfig(c *echo.Context) error {
	cfg := s.store.LogConfig()
	return c.JSON(http.StatusOK, LogConfigResponse{
		Level:  cfg.Level,
		Format: cfg.Format,
	})
}

func (s *Services) setLogConfig(c *echo.Context) error {
	var req SetLogConfigRequest
	if err := bindJSON(c, s.log, &req); err != nil {
		return err
	}

	if req.Level == "" {
		return api.SendValidation(c, "LOG_LEVEL_REQUIRED", "log level is required")
	}
	// Validate level is a valid zap level
	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLevels[req.Level] {
		return api.SendValidation(c, "LOG_LEVEL_INVALID", "log level must be debug, info, warn, or error")
	}

	if req.Format != "" && req.Format != "json" && req.Format != "human" {
		return api.SendValidation(c, "LOG_FORMAT_INVALID", "log format must be json or human")
	}

	logCfg := config.LogConfig{
		Level:  req.Level,
		Format: req.Format,
	}
	if err := s.store.SetLogConfig(logCfg); err != nil {
		return api.SendInternal(c, api.TypeLogConfigSaveFailed, "failed to save log config", err)
	}

	// Hot-reload the log level if the updater is wired.
	if s.logLevelUpdater != nil {
		s.logLevelUpdater.SetLevel(logCfg)
	}

	return c.JSON(http.StatusOK, ConfigUpdateResponse{
		Status:  "updated",
		Message: "Log configuration saved and applied.",
	})
}

// systemFlush forces immediate upload of all pending (buffered locally) objects
// to the Sia network, bypassing the normal batching/padding efficiency threshold,
// then pins the uploaded objects so their local backup files can be removed.
// This is a system-wide operation: it affects all objects across all buckets,
// not a specific bucket. It does NOT delete any data.
func (s *Services) systemFlush(c *echo.Context) error {
	b, err := s.requireBackendOnly(c)
	if err != nil {
		return err
	}
	if err := b.FlushObjects(c.Request().Context()); err != nil {
		return api.SendInternal(c, api.TypeFlushFailed, "failed to flush objects", err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Services) systemRestart(c *echo.Context) error {
	if s.restarter == nil {
		return api.SendNotReady(c, api.TypeBackendNotInitialized, "restarter not configured", nil)
	}
	if err := s.restarter.Restart(c.Request().Context()); err != nil {
		return api.SendInternal(c, api.TypeBackendInitFailed, "failed to restart backend", err)
	}
	return c.JSON(http.StatusOK, ConfigUpdateResponse{
		Status:  "restarted",
		Message: "Backend restarted successfully.",
	})
}

func (s *Services) changePassword(c *echo.Context) error {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := bindJSON(c, s.log, &req); err != nil {
		return err
	}
	if req.CurrentPassword == "" {
		return api.SendValidation(c, api.TypePasswordRequired, "current password is required")
	}
	if req.NewPassword == "" {
		return api.SendValidation(c, api.TypePasswordRequired, "new password is required")
	}
	if len(req.NewPassword) < 8 {
		return api.SendValidation(c, api.TypePasswordTooShort, "new password must be at least 8 characters")
	}
	if !s.store.ValidateAdminPassword(req.CurrentPassword) {
		return api.SendError(c, api.ErrUnauthorized, api.TypePasswordIncorrect, "current password is incorrect", nil)
	}
	if err := s.store.SetAdminPassword(req.NewPassword); err != nil {
		return api.SendInternal(c, api.TypeS3ConfigSaveFailed, "failed to set admin password", err)
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Services) statusAPI(c *echo.Context) error {
	st := status.Stopped
	if s.backendStatus != nil {
		st = s.backendStatus()
	}

	keys := s.listAccessKeys()
	resp := StatusResponse{
		S3Status:  st.String(),
		KeyCount:  len(keys),
		Version:   s.version,
	}
	if s.initError != nil {
		resp.InitError = s.initError()
	}

	return c.JSON(http.StatusOK, resp)
}

func (s *Services) sseEvents(c *echo.Context) error {
	if s.sseBroker == nil {
		return api.SendInternal(c, api.TypeSSEBrokerNotConfigured, "SSE broker not configured", nil)
	}
	s.sseBroker.ServeHTTP(c.Response(), c.Request())
	return nil
}

// generateAccessKey creates an AWS-compatible access key pair.
// Access Key ID: AKIA + 16 random alphanumeric = 20 chars
// Secret Access Key: 30 random bytes base64-encoded = 40 chars
func generateAccessKey() (accessKey, secretKey string) {
	// access key ID
	akBytes := make([]byte, accessKeyRandom)
	charsetLen := big.NewInt(int64(len(accessKeyCharset)))
	for i := range akBytes {
		n, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			panic(err) // crypto/rand should never fail
		}
		akBytes[i] = accessKeyCharset[n.Int64()]
	}
	accessKey = accessKeyPrefix + string(akBytes)

	// secret access key
	skBytes := make([]byte, secretKeyBytes)
	if _, err := rand.Read(skBytes); err != nil {
		panic(err)
	}
	secretKey = base64.StdEncoding.EncodeToString(skBytes)

	return
}
