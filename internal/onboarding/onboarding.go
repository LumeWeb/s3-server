package onboarding

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/SiaFoundation/s3d/sia"
	"github.com/labstack/echo/v5"
	"go.lumeweb.com/s3-server/internal/api"
	"go.lumeweb.com/s3-server/internal/backend"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/store"
	"go.sia.tech/core/types"
	"go.uber.org/zap"
	"golang.org/x/crypto/nacl/box"
)

// AppID is the Sia application identifier — must match factory.go's types.HashBytes([]byte("s3d")).
func init() {
	h := types.HashBytes([]byte("s3d"))
	AppID = hex.EncodeToString(h[:])
}

var AppID string

var ErrNotOnboarding = errors.New("onboarding already complete")

type OnboardingState string

const (
	StatePending   OnboardingState = config.DefaultOnboardingState
	StateAdminSet  OnboardingState = "admin_password_set"
	StateAppKeySet OnboardingState = "app_key_set"
	StateComplete  OnboardingState = "complete"
)

// transitionTo delegates to the FSM and persists the new state to the store.
func (svc *Service) transitionTo(target OnboardingState) error {
	if err := svc.fsm.Transition(target); err != nil {
		return err
	}
	return svc.store.SetOnboardingState(string(target))
}

// BackendInitializer is the interface for s3d backend operations needed during onboarding.
// Defined here by the consumer; satisfied implicitly by *backend.Manager.
type BackendInitializer interface {
	OpenDatabase(dbPath string) (backend.S3DStore, error)
	InitAfterOnboarding(ctx context.Context, sqliteStore backend.S3DStore) error
}

type SetAppKeyRequest struct {
	EncryptedAppKey string `json:"encrypted_app_key"`
	IndexerURL      string `json:"indexer_url,omitempty"`
}

type SetAdminPasswordRequest struct {
	Password string `json:"password"`
}

type StatusResponse struct {
	State         string `json:"state"`
	HasAppKey     bool   `json:"has_app_key"`
	HasAccessKeys bool   `json:"has_access_keys"`
	HasAdmin      bool   `json:"has_admin"`
}

type ConfigResponse struct {
	IndexerURL        string   `json:"indexer_url"`
	AvailableIndexers []string `json:"available_indexers"`
	AppID             string   `json:"app_id"`
	AppName           string   `json:"app_name"`
	AppDesc           string   `json:"app_description"`
	LogoURL           string   `json:"logo_url"`
	ServiceURL        string   `json:"service_url"`
	CallbackURL       string   `json:"callback_url"`
}

type OnboardingStepResponse struct {
	Status    string `json:"status"`
	UserName  string `json:"user_name,omitempty"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
}

// generateAccessKey generates a random S3-compatible access key.
// Format: "AKIA" prefix + 16 random uppercase alphanumeric characters (20 total).
// AWS access key IDs use this format and s3d accepts 16-128 characters.
func generateAccessKey() (string, error) {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const prefix = "AKIA"
	const randomLen = 16
	buf := make([]byte, randomLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, 0, len(prefix)+randomLen)
	out = append(out, prefix...)
	for _, b := range buf {
		out = append(out, chars[int(b)%len(chars)])
	}
	return string(out), nil
}

// generateSecretKey generates a random 40-character secret key (30 random bytes base64-encoded).
// s3d requires 32-128 characters.
func generateSecretKey() (string, error) {
	buf := make([]byte, 30)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// Service handles the onboarding state machine: collect app key, access keys,
// admin password, then mark complete. It delegates s3d backend initialization
// to a BackendInitializer.
type Service struct {
	store   store.Store
	log     *zap.Logger
	backend BackendInitializer
	fsm     StateMachine

	publicKey  [32]byte
	privateKey [32]byte

	mu          sync.Mutex
	sqliteStore backend.S3DStore
}

func NewService(s store.Store, log *zap.Logger, be BackendInitializer) (*Service, error) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate NaCl keypair: %w", err)
	}
	return &Service{
		store:      s,
		log:        log,
		backend:    be,
		fsm:        NewFSM(OnboardingState(s.OnboardingState())),
		publicKey:  *pub,
		privateKey: *priv,
	}, nil
}

func (svc *Service) PublicKey() [32]byte {
	return svc.publicKey
}

// PublicKeyBase64 returns the server's NaCl public key as a base64 string.
func (svc *Service) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(svc.publicKey[:])
}

func (svc *Service) StatusHandler(c *echo.Context) error {
	svc.mu.Lock()
	sqliteStore := svc.sqliteStore
	svc.mu.Unlock()

	hasAccessKeys := false
	if sqliteStore != nil {
		if keys, err := sqliteStore.ListAccessKeys(nil); err == nil {
			hasAccessKeys = len(keys) > 0
		}
	}

	cfg := svc.store.Config()
	state := svc.fsm.State()
	resp := StatusResponse{
		State:         string(state),
		HasAppKey:     state == StateAppKeySet || state == StateComplete,
		HasAccessKeys: hasAccessKeys,
		HasAdmin:      cfg.AdminPasswordHash != "",
	}
	return c.JSON(http.StatusOK, resp)
}

type PublicKeyResponse struct {
	PublicKey string `json:"public_key"`
}

func (svc *Service) PublicKeyHandler(c *echo.Context) error {
	return c.JSON(http.StatusOK, PublicKeyResponse{
		PublicKey: base64.StdEncoding.EncodeToString(svc.publicKey[:]),
	})
}

// ConfigHandler returns the Sia app metadata the browser needs for onboarding.
// The app ID, name, and description must match what factory.go uses on the Go side.
func (svc *Service) ConfigHandler(c *echo.Context) error {
	s3Cfg := svc.store.S3Config()
	return c.JSON(http.StatusOK, ConfigResponse{
		IndexerURL:        s3Cfg.IndexerURL,
		AvailableIndexers: s3Cfg.AvailableIndexers,
		AppID:             AppID,
		AppName:           "S3d",
		AppDesc:           "A S3-compatible storage service backed by Sia",
		LogoURL:           "https://example.com/logo.png",
		ServiceURL:        "https://github.com/Siafoundation/s3d",
		CallbackURL:       "",
	})
}

func (svc *Service) SetAppKeyHandler(c *echo.Context) error {
	state := svc.fsm.State()
	if state == StateComplete {
		return api.SendError(c, api.ErrOnboardingComplete, api.TypeOnboardingComplete, "onboarding already complete", nil)
	}
	if state != StateAdminSet {
		return api.SendError(c, api.ErrOnboardingRequired, api.TypeOnboardingRequired, "admin password must be set first", nil)
	}

	var req SetAppKeyRequest
	if err := c.Bind(&req); err != nil {
		return api.SendBadRequest(c, api.TypeInvalidRequestBody, "invalid request body")
	}

	ciphertext, err := base64.StdEncoding.DecodeString(req.EncryptedAppKey)
	if err != nil {
		return api.SendError(c, api.ErrInvalidAppKey, api.TypeInvalidBase64, "invalid base64 encoding", err)
	}

	plaintext, ok := box.OpenAnonymous(nil, ciphertext, &svc.publicKey, &svc.privateKey)
	if !ok {
		return api.SendError(c, api.ErrDecryptionFailed, api.TypeDecryptionFailed, "failed to decrypt app key", nil)
	}

	if len(plaintext) != 32 {
		return api.SendError(c, api.ErrInvalidAppKey, api.TypeAppKeySizeInvalid, "app key must be 32 bytes", nil)
	}

	// The decrypted app key is a 32-byte seed. Expand it to a full 64-byte
	// ed25519 private key (seed + public key) as expected by types.PrivateKey.
	appKey := types.NewPrivateKeyFromSeed(plaintext)

	// persist selected indexer URL if provided (e.g. custom from onboarding)
	if req.IndexerURL != "" {
		resolved := ResolveIndexerURL(req.IndexerURL)
		s3Cfg := svc.store.S3Config()
		s3Cfg.IndexerURL = resolved
		if err := svc.store.SetS3Config(s3Cfg); err != nil {
			svc.log.Error("failed to save indexer URL", zap.Error(err))
		}
	}

	// store in s3d's SQLite database
	s3Cfg := svc.store.S3Config()
	dbPath := s3Cfg.Directory + "/s3d.db"
	svc.log.Info("opening s3d database", zap.String("dbPath", dbPath), zap.String("directory", s3Cfg.Directory))
	sqliteStore, err := svc.backend.OpenDatabase(dbPath)
	if err != nil {
		svc.log.Error("failed to open s3d database",
			zap.String("dbPath", dbPath),
			zap.String("directory", s3Cfg.Directory),
			zap.Error(err))
		return api.SendInternal(c, api.TypeDatabaseOpenFailed, "failed to open database", err)
	}

	if err := sqliteStore.SetAppKey(appKey, s3Cfg.IndexerURL); err != nil {
		svc.log.Error("failed to store app key in sqlite",
			zap.String("indexerURL", s3Cfg.IndexerURL),
			zap.String("dbPath", dbPath),
			zap.Error(err))
		if closeErr := sqliteStore.Close(); closeErr != nil {
			svc.log.Error("failed to close database after set app key error", zap.Error(closeErr))
		}
		return api.SendInternal(c, api.TypeDatabaseStoreFailed, "failed to store app key", err)
	}

	// Advance the FSM first. If this fails, the store is untouched.
	if err := svc.fsm.Transition(StateAppKeySet); err != nil {
		svc.log.Error("failed to transition onboarding state", zap.Error(err))
		if closeErr := sqliteStore.Close(); closeErr != nil {
			svc.log.Error("failed to close database after state transition error", zap.Error(closeErr))
		}
		return api.SendInternal(c, api.TypeOnboardingStateFailed, "failed to update onboarding state", err)
	}
	// FSM advanced — persist the new state. If persist fails, roll back
	// the FSM so the store and FSM stay consistent for a clean retry.
	if err := svc.store.SetOnboardingState(string(StateAppKeySet)); err != nil {
		svc.log.Error("failed to persist onboarding state", zap.Error(err))
		svc.fsm.SetState(StateAdminSet) // best-effort rollback
		if closeErr := sqliteStore.Close(); closeErr != nil {
			svc.log.Error("failed to close database after persist error", zap.Error(closeErr))
		}
		return api.SendInternal(c, api.TypeOnboardingStateFailed, "failed to update onboarding state", err)
	}

	svc.mu.Lock()
	svc.sqliteStore = sqliteStore
	svc.mu.Unlock()
	svc.log.Info("app key stored")

	return c.JSON(http.StatusOK, OnboardingStepResponse{Status: "app_key_set"})
}

func (svc *Service) SetAccessKeysHandler(c *echo.Context) error {
	state := svc.fsm.State()
	if state == StateComplete {
		return api.SendError(c, api.ErrOnboardingComplete, api.TypeOnboardingComplete, "onboarding already complete", nil)
	}
	if state != StateAppKeySet {
		return api.SendError(c, api.ErrOnboardingRequired, api.TypeOnboardingRequired, "set app key first", nil)
	}

	svc.mu.Lock()
	sqliteStore := svc.sqliteStore
	svc.mu.Unlock()

	if sqliteStore == nil {
		return api.SendInternal(c, api.TypeSQLiteNotInitialized, "sqlite store not initialized", nil)
	}

	// Auto-generate credentials — no user input required.
	const userName = "admin"
	accessKey, err := generateAccessKey()
	if err != nil {
		svc.log.Error("failed to generate access key", zap.Error(err))
		return api.SendInternal(c, api.TypeAccessKeyCreateFailed, "failed to generate access key", err)
	}
	secretKey, err := generateSecretKey()
	if err != nil {
		svc.log.Error("failed to generate secret key", zap.Error(err))
		return api.SendInternal(c, api.TypeAccessKeyCreateFailed, "failed to generate secret key", err)
	}

	// Create the first user during onboarding.
	if err := sqliteStore.CreateUser(userName); err != nil && !errors.Is(err, sia.ErrUserAlreadyExists) {
		svc.log.Error("failed to create user", zap.String("user", userName), zap.Error(err))
		return api.SendInternal(c, api.TypeUserCreateFailed, "failed to create user", err)
	}

	if err := sqliteStore.CreateAccessKey(userName, accessKey, secretKey); err != nil {
		if errors.Is(err, sia.ErrAccessKeyAlreadyExists) {
			return api.SendConflict(c, api.TypeConflict, "access key already exists")
		}
		svc.log.Error("failed to create access key",
			zap.String("user", userName),
			zap.String("accessKey", accessKey),
			zap.Error(err))
		return api.SendInternal(c, api.TypeAccessKeyCreateFailed, "failed to create access key", err)
	}

	// init s3d backend now that we have app key, access keys, and a user
	svc.log.Info("initializing s3d backend after onboarding")

	// s3dSia.New() may panic on certain config issues (nil pointer, missing
	// fields). Wrap in a deferred recover so we get a proper error log
	// instead of middleware.Recover() silently returning 500.
	var initErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				initErr = fmt.Errorf("panic in s3d backend init: %v", r)
			}
		}()
		initErr = svc.backend.InitAfterOnboarding(c.Request().Context(), sqliteStore)
	}()

	if initErr != nil {
		svc.log.Error("failed to init s3d backend", zap.Error(initErr))
		return api.SendInternal(c, api.TypeBackendInitFailed, "failed to init s3d backend", initErr)
	}
	svc.log.Info("s3d backend init succeeded")

	if err := svc.transitionTo(StateComplete); err != nil {
		svc.log.Error("failed to update onboarding state", zap.Error(err))
		return api.SendInternal(c, api.TypeOnboardingStateFailed, "failed to update onboarding state", err)
	}

	svc.log.Info("onboarding complete", zap.String("user", userName))
	return c.JSON(http.StatusOK, OnboardingStepResponse{
		Status:    "access_keys_set",
		UserName:  userName,
		AccessKey: accessKey,
		SecretKey: secretKey,
	})
}

func (svc *Service) SetAdminPasswordHandler(c *echo.Context) error {
	if svc.fsm.State() == StateComplete {
		return api.SendError(c, api.ErrOnboardingComplete, api.TypeOnboardingComplete, "onboarding already complete", nil)
	}

	var req SetAdminPasswordRequest
	if err := c.Bind(&req); err != nil {
		return api.SendBadRequest(c, api.TypeInvalidRequestBody, "invalid request body")
	}

	if len(req.Password) < 8 {
		return api.SendValidation(c, api.TypePasswordTooShort, "password must be at least 8 characters")
	}

	if err := svc.store.SetAdminPassword(req.Password); err != nil {
		svc.log.Error("failed to set admin password", zap.Error(err))
		return api.SendInternal(c, api.TypePasswordSetFailed, "failed to set password", err)
	}

	if err := svc.transitionTo(StateAdminSet); err != nil {
		svc.log.Error("failed to update onboarding state", zap.Error(err))
		return api.SendInternal(c, api.TypeOnboardingStateFailed, "failed to update onboarding state", err)
	}

	return c.JSON(http.StatusOK, OnboardingStepResponse{Status: "admin_password_set"})
}

// ResetHandler resets onboarding back to the initial state so the user can
// start over. It clears the admin password, closes the SQLite store if open,
// and transitions the FSM back to StatePending.
func (svc *Service) ResetHandler(c *echo.Context) error {
	// Close the sqlite store if it's open — the next onboarding run will
	// re-open it with a fresh app key.
	svc.mu.Lock()
	sqliteStore := svc.sqliteStore
	svc.sqliteStore = nil
	svc.mu.Unlock()

	if sqliteStore != nil {
		if err := sqliteStore.Close(); err != nil {
			svc.log.Error("failed to close sqlite store during reset", zap.Error(err))
		}
	}

	// Clear the admin password hash — SetAdminPassword("") would generate a
	// bcrypt hash of the empty string, allowing login with an empty password.
	if err := svc.store.ClearAdminPassword(); err != nil {
		svc.log.Error("failed to clear admin password during reset", zap.Error(err))
	}

	// Reset the FSM and persist.
	if err := svc.transitionTo(StatePending); err != nil {
		svc.log.Error("failed to reset onboarding state", zap.Error(err))
		return api.SendInternal(c, api.TypeOnboardingResetFailed, "failed to reset onboarding", err)
	}

	svc.log.Info("onboarding reset to pending")
	return c.JSON(http.StatusOK, OnboardingStepResponse{Status: "reset"})
}

const indexerProbeTimeout = 5 * time.Second

// isPrivateHost reports whether the given host resolves to (or is) a
// loopback, private, or link-local address. This prevents SSRF attacks
// where a user supplies an indexer URL pointing to internal services.
func isPrivateHost(host string) bool {
	// Strip port if present.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	// Check literal IP first (avoids DNS lookup for IP literals).
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}

	// Resolve hostname and check all returned IPs.
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS resolution failed — block to be safe.
		return true
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}

// ssrfSafeClient returns an http.Client whose dialer validates the resolved
// IP at connection time, eliminating the TOCTOU between a pre-check DNS
// lookup and the actual HTTP request (DNS rebinding attacks).
func ssrfSafeClient() *http.Client {
	dialer := &net.Dialer{Timeout: indexerProbeTimeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, _ := net.SplitHostPort(addr)
			addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil || len(addrs) == 0 {
				return nil, errors.New("blocked: DNS resolution failed")
			}
			for _, a := range addrs {
				if a.IP.IsLoopback() || a.IP.IsPrivate() || a.IP.IsLinkLocalUnicast() || a.IP.IsUnspecified() {
					return nil, errors.New("blocked: private address")
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addrs[0].IP.String(), port))
		},
	}
	return &http.Client{
		Timeout:   indexerProbeTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// ResolveIndexerURL determines the full URL for an indexer when no scheme is provided.
// It tries HTTPS first, then falls back to HTTP. Hosts that resolve to
// private/loopback/link-local addresses are rejected at connection time to
// prevent SSRF, including DNS rebinding attacks.
func ResolveIndexerURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	// If the URL already has a scheme, validate the host and return as-is.
	if parsed.Scheme != "" {
		if isPrivateHost(parsed.Host) {
			return ""
		}
		return rawURL
	}

	// No scheme — check the bare host before probing.
	if isPrivateHost(rawURL) {
		return ""
	}

	client := ssrfSafeClient()

	httpsURL := "https://" + rawURL
	resp, err := client.Head(httpsURL)
	if err == nil {
		_ = resp.Body.Close()
		return httpsURL
	}

	httpURL := "http://" + rawURL
	resp, err = client.Head(httpURL)
	if err == nil {
		_ = resp.Body.Close()
		return httpURL
	}

	return httpsURL
}
