package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"go.lumeweb.com/s3-server/internal/api"
	"go.lumeweb.com/s3-server/internal/backend"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/status"
	"go.lumeweb.com/s3-server/internal/store"
	"go.lumeweb.com/s3-server/internal/updater"
	"go.uber.org/zap"
)

const (
	accessKeyCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	accessKeyPrefix  = "AKIA"
	accessKeyRandom  = 16 // AKIA + 16 = 20 chars total
	secretKeyBytes   = 30 // 30 bytes → 40 base64 chars
)

// S3Swapper is the interface for a thread-safe swappable http.Handler.
type S3Swapper interface {
	Swap(handler http.Handler)
}

// BackendRestarter is the interface for restarting the s3d backend after config changes.
type BackendRestarter interface {
	Restart(ctx context.Context) error
}

// UpdaterManager is the interface for communicating with the update sidecar
// via flag files on the /state volume.  When /state is not mounted, all
// methods return a zero-value status with SidecarMode=false.
type UpdaterManager interface {
	IsAvailable() bool
	IsAutoUpdateOn() bool
	EnableAutoUpdate() error
	DisableAutoUpdate() error
	TriggerUpdate() error
	Status() updater.UpdateStatus
}

type StatusResponse struct {
	S3Status  string `json:"s3_status"`
	KeyCount  int    `json:"key_count"`
	Version   string `json:"version"`
	InitError string `json:"init_error,omitempty"`
}

type AccessKeyResponse struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	UserName  string `json:"user_name"`
}

type ListAccessKeysResponse struct {
	Keys []AccessKeyResponse `json:"keys"`
}

type AddAccessKeyRequest struct {
	AccessKey string `json:"access_key" form:"access_key"`
	SecretKey string `json:"secret_key" form:"secret_key"`
	UserName  string `json:"user_name,omitempty" form:"user_name"`
}

type AddAccessKeyResponse struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Generated bool   `json:"generated"`
}

type ConfigUpdateResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type S3ConfigResponse struct {
	Directory         string                 `json:"directory"`
	IndexerURL        string                 `json:"indexer_url"`
	AvailableIndexers []config.IndexerOption `json:"available_indexers"`
	HostBases         []string               `json:"host_bases"`
	DiskUsageLimit    config.DiskUsageLimit  `json:"disk_usage_limit"`
	UploadWastePct    float64                `json:"upload_waste_pct"`
}

type SetS3ConfigRequest struct {
	Directory         string                 `json:"directory"`
	IndexerURL        string                 `json:"indexer_url"`
	AvailableIndexers []config.IndexerOption `json:"available_indexers"`
	HostBases         []string               `json:"host_bases"`
	DiskUsageLimit    config.DiskUsageLimit  `json:"disk_usage_limit"`
	UploadWastePct    float64                `json:"upload_waste_pct"`
}

type SSLConfigResponse struct {
	Mode       string `json:"mode"`
	ACMEEmail  string `json:"acme_email,omitempty"`
	ACMEDirURL string `json:"acme_dir_url,omitempty"`
}

type SetSSLConfigRequest struct {
	Mode       string `json:"mode"`
	ACMEEmail  string `json:"acme_email,omitempty"`
	ACMEDirURL string `json:"acme_dir_url,omitempty"`
}

type LogConfigResponse struct {
	Level  string `json:"level"`
	Format string `json:"format"`
}

type SetLogConfigRequest struct {
	Level  string `json:"level"`
	Format string `json:"format"`
}

type UserListResponse struct {
	Users []string `json:"users"`
}

type UserResponse struct {
	Name string `json:"name"`
}

type BucketResponse struct {
	Name      string    `json:"name"`
	Owner     string    `json:"owner,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ListBucketsResponse struct {
	Buckets []BucketResponse `json:"buckets"`
}

type CreateBucketRequest struct {
	Name  string `json:"name"`
	Owner string `json:"owner,omitempty"`
}

type BucketVersioningResponse struct {
	Status string `json:"status"`
}

type SetBucketVersioningRequest struct {
	Status string `json:"status"`
}

// S3Handler is a thread-safe swappable http.Handler for S3 requests.
type S3Handler struct {
	mu      sync.RWMutex
	handler http.Handler
}

const notConfiguredHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Not Configured - S3</title>
<link rel="icon" type="image/svg+xml" href="/favicon.svg"/>
<link rel="apple-touch-icon" href="/favicon.svg"/>
<meta name="theme-color" content="#0d1d1c"/>
<meta name="application-name" content="S3"/>
<link href="https://fonts.googleapis.com/css2?family=Poppins:wght@400;500;600;700&display=swap" rel="stylesheet"/>
<style>
body{font-family:"Poppins",sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#0d1d1c;color:#bdc2c1}
a{color:#abeedb;text-decoration:none;font-weight:600;font-size:.875rem;padding:.5rem 1.5rem;border:1px solid #abeedb;border-radius:.375rem;transition:background .15s}a:hover{background:#0d2d2a}
h1{font-size:1.25rem;font-weight:700;color:#abeedb;margin-bottom:.5rem}
p{color:#bdc2c1;margin-bottom:1.5rem;font-size:.875rem}
div{text-align:center}
</style>
<meta http-equiv="refresh" content="5;url=/_panel/">
</head>
<body>
<div>
<h1>S3 not configured</h1>
<p>Complete onboarding to get started.</p>
<a href="/_panel/">Go to setup</a>
</div>
</body>
</html>`

func NewS3Handler() *S3Handler {
	return &S3Handler{
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(notConfiguredHTML)) //nolint:errcheck
		}),
	}
}

func (h *S3Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	handler := h.handler
	h.mu.RUnlock()
	handler.ServeHTTP(w, r)
}

func (h *S3Handler) Swap(handler http.Handler) {
	h.mu.Lock()
	h.handler = handler
	h.mu.Unlock()
}

// LogLevelUpdater hot-reloads the zap log level at runtime.
// Implemented by the logger wrapper in main.go.
type LogLevelUpdater interface {
	SetLevel(level config.LogConfig)
}

// ServicesConfig holds the dependencies for panel services.
type ServicesConfig struct {
	Store           store.Store
	KeyStore        func() backend.S3DStore
	Backend         func() backend.Backend
	AccountClient   func() backend.AccountClient
	AdminHandler    http.Handler
	Restarter       BackendRestarter
	BackendStatus   func() status.Status
	InitError       func() string
	Version         string
	PlatformName    string
	Log             *zap.Logger
	LogLevelUpdater LogLevelUpdater // optional: hot-reloads log level without process restart
	CSRFToken       func(*echo.Context) string
	SSEBroker       SSEBroker
	UpdateManager   UpdaterManager
}

// Services handles panel API and page requests.
// SSEBroker is the interface for publishing SSE events to connected clients.
type SSEBroker interface {
	NotifyKeyChange(action string, count int)
	NotifyBucketChange(action, name string)
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type Services struct {
	store           store.Store
	keyStore        func() backend.S3DStore
	backend         func() backend.Backend
	accountClient   func() backend.AccountClient
	adminHandler    http.Handler
	restarter       BackendRestarter
	backendStatus   func() status.Status
	initError       func() string
	version         string
	platformName    string
	log             *zap.Logger
	logLevelUpdater LogLevelUpdater
	csrfToken       func(*echo.Context) string
	sseBroker       SSEBroker
	updater         UpdaterManager
	keyMu           sync.RWMutex
}

// NewServices creates a Services instance from the given config.
func NewServices(cfg ServicesConfig) *Services {
	return &Services{
		store:           cfg.Store,
		keyStore:        cfg.KeyStore,
		backend:         cfg.Backend,
		accountClient:   cfg.AccountClient,
		adminHandler:    cfg.AdminHandler,
		restarter:       cfg.Restarter,
		backendStatus:   cfg.BackendStatus,
		initError:       cfg.InitError,
		version:         cfg.Version,
		platformName:    cfg.PlatformName,
		log:             cfg.Log,
		logLevelUpdater: cfg.LogLevelUpdater,
		csrfToken:       cfg.CSRFToken,
		sseBroker:       cfg.SSEBroker,
		updater:         cfg.UpdateManager,
	}
}

func RegisterRoutes(g *echo.Group, svc *Services) {
	g.GET("/dashboard", svc.dashboardPage)
	g.GET("/settings", svc.settingsPage)
	g.GET("/users", svc.usersPage)
	g.GET("/keys", svc.keysPage)
	g.GET("/buckets", svc.bucketsPage)
	g.GET("/backups", svc.backupsPage)
	g.GET("/monitoring", svc.monitoringPage)
	g.GET("/api/status", svc.statusAPI)

	g.GET("/api/users", svc.listUsers)
	g.POST("/api/users", svc.createUser)
	g.DELETE("/api/users/:name", svc.deleteUser)
	g.GET("/api/users/:name/keys", svc.listUserKeys)

	g.GET("/api/keys", svc.listKeys)
	g.POST("/api/keys", svc.addKey)
	g.DELETE("/api/keys/:accessKey", svc.deleteKey)

	g.GET("/api/buckets", svc.listBuckets)
	g.POST("/api/buckets", svc.createBucket)
	g.DELETE("/api/buckets/:name", svc.deleteBucket)
	g.GET("/api/buckets/:name/versioning", svc.getBucketVersioning)
	g.PUT("/api/buckets/:name/versioning", svc.putBucketVersioning)

	g.GET("/api/buckets/:name/lifecycle", svc.getBucketLifecycle)
	g.PUT("/api/buckets/:name/lifecycle", svc.putBucketLifecycle)
	g.DELETE("/api/buckets/:name/lifecycle", svc.deleteBucketLifecycle)

	g.POST("/api/backups", svc.createBackup)
	g.GET("/api/admin/stats", svc.getStats)

	g.GET("/api/backups", svc.listBackups)
	g.GET("/api/backups/:filename", svc.getBackup)
	g.DELETE("/api/backups/:filename", svc.deleteBackup)

	g.GET("/api/s3-config", svc.getS3Config)
	g.PUT("/api/s3-config", svc.setS3Config)
	g.GET("/api/ssl-config", svc.getSSLConfig)
	g.PUT("/api/ssl-config", svc.setSSLConfig)
	g.GET("/api/log-config", svc.getLogConfig)
	g.PUT("/api/log-config", svc.setLogConfig)

	g.POST("/api/system/flush", svc.systemFlush)
	g.POST("/api/system/restart", svc.systemRestart)
	g.POST("/api/password/change", svc.changePassword)

	// SSE event stream: blocks until client disconnects
	g.GET("/api/events", svc.sseEvents)

	// Update control (adaptive: only works when /state volume is mounted)
	g.GET("/api/update/status", svc.updateStatusAPI)
	g.POST("/api/update/toggle", svc.updateToggleAPI)
	g.POST("/api/update/trigger", svc.updateTriggerAPI)
}

func (s *Services) getKeyStore() backend.S3DStore {
	if s.keyStore == nil {
		return nil
	}
	return s.keyStore()
}

func (s *Services) getBackend() backend.Backend {
	if s.backend == nil {
		return nil
	}
	return s.backend()
}

func (s *Services) adminAccessKey() (string, error) {
	keys := s.listAccessKeys()
	if len(keys) == 0 {
		return "", errors.New("no access keys available")
	}
	return keys[0].AccessKeyID, nil
}

// accessKeyForUser returns the first access key ID belonging to the given
// user. Returns an error if the user has no keys.
func (s *Services) accessKeyForUser(userName string) (string, error) {
	keys := s.listAccessKeys()
	for _, k := range keys {
		if k.UserName == userName {
			return k.AccessKeyID, nil
		}
	}
	return "", fmt.Errorf("user %q has no access keys", userName)
}

// accessKeyForBucket resolves an access key that owns the given bucket.
// It looks up the bucket owner's user name via the backend, then finds
// that user's access key. This ensures s3d ownership checks pass when
// the panel admin key differs from the bucket owner.
func (s *Services) accessKeyForBucket(b backend.Backend, ctx context.Context, bucket string) (string, error) {
	owner, err := b.BucketOwner(ctx, bucket)
	if err != nil {
		return "", fmt.Errorf("failed to resolve bucket owner: %w", err)
	}
	if owner == "" {
		// bucket doesn't exist or no owner recorded — fall back to admin key
		return s.adminAccessKey()
	}
	key, err := s.accessKeyForUser(owner)
	if err != nil {
		// owner recorded but has no keys (e.g. key deleted) — fall back to admin key
		return s.adminAccessKey()
	}
	return key, nil
}

func (s *Services) proxyToAdmin(c *echo.Context, adminPath string) error {
	if s.adminHandler == nil {
		return api.SendNotReady(c, api.TypeAdminHandlerNotConfigured, "admin handler not configured", nil)
	}

	req := c.Request()
	if req.URL == nil {
		return api.SendInternal(c, api.TypeInvalidRequestURL, "invalid request URL", nil)
	}

	proxyReq := req.Clone(req.Context())
	proxyURL := *req.URL
	proxyReq.URL = &proxyURL
	proxyReq.URL.Path = adminPath

	s.adminHandler.ServeHTTP(c.Response(), proxyReq)
	return nil
}

func (s *Services) listAccessKeys() []backend.AccessKeyInfo {
	s.keyMu.RLock()
	defer s.keyMu.RUnlock()
	return s.listAccessKeysLocked()
}

// listAccessKeysLocked returns access keys without acquiring keyMu.
// Caller must hold keyMu (read or write).
func (s *Services) listAccessKeysLocked() []backend.AccessKeyInfo {
	ks := s.getKeyStore()
	if ks == nil {
		return nil
	}
	keys, err := ks.ListAccessKeys(nil)
	if err != nil {
		s.log.Error("failed to list access keys", zap.Error(err))
		return nil
	}
	return keys
}
