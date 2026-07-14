package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.lumeweb.com/s3-server/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// Store is the interface for persisting and retrieving panel configuration and sessions.
type Store interface {
	Config() config.PanelConfig
	SetAdminPassword(password string) error
	ClearAdminPassword() error
	ValidateAdminPassword(password string) bool
	DataDir() string
	ResetTokenPath() string
	AccessKeys() []config.KeyPair
	SetAccessKeys(keys []config.KeyPair) error
	OnboardingState() string
	SetOnboardingState(state string) error
	SSLConfig() config.SSLConfig
	SetSSLConfig(cfg config.SSLConfig) error
	S3Config() config.S3Config
	SetS3Config(cfg config.S3Config) error
	LogConfig() config.LogConfig
	SetLogConfig(cfg config.LogConfig) error
	CreateSession() (string, error)
	ValidateSession(token string) bool
	DeleteSession(token string)
	StartSessionCleanup(stop <-chan struct{})
}

// SessionManager is the interface for managing in-memory sessions.
type SessionManager interface {
	Create() (string, error)
	Validate(token string) bool
	Delete(token string)
	StartCleanup(stop <-chan struct{})
}

// fileStore implements Store backed by a YAML config file.
type fileStore struct {
	cfgPath  string
	config   config.PanelConfig
	mu       sync.RWMutex
	sessions SessionManager
}

func New(cfgPath string) (Store, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return &fileStore{
		cfgPath:  cfgPath,
		config:   cfg,
		sessions: NewMemorySessionManager(24 * time.Hour),
	}, nil
}

func (s *fileStore) Config() config.PanelConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *fileStore) SetAdminPassword(password string) error {
	if strings.TrimSpace(password) == "" {
		return errors.New("password must not be empty")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.mu.Lock()
	old := s.config.AdminPasswordHash
	s.config.AdminPasswordHash = string(hash)
	if err := s.save(); err != nil {
		s.config.AdminPasswordHash = old
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return nil
}

// ClearAdminPassword removes the stored bcrypt hash entirely.
// This is used during password reset flows to ensure no password is valid
// until a new one is explicitly set. Using SetAdminPassword("") would
// generate a hash of the empty string, which bcrypt would match on empty
// input: a critical bypass.
func (s *fileStore) ClearAdminPassword() error {
	s.mu.Lock()
	old := s.config.AdminPasswordHash
	s.config.AdminPasswordHash = ""
	if err := s.save(); err != nil {
		s.config.AdminPasswordHash = old
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return nil
}

func (s *fileStore) ValidateAdminPassword(password string) bool {
	s.mu.RLock()
	hash := s.config.AdminPasswordHash
	s.mu.RUnlock()
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *fileStore) DataDir() string {
	return filepath.Dir(s.cfgPath)
}

func (s *fileStore) ResetTokenPath() string {
	return filepath.Join(s.DataDir(), ".reset-token")
}

func (s *fileStore) AccessKeys() []config.KeyPair {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.AccessKeys
}

func (s *fileStore) SetAccessKeys(keys []config.KeyPair) error {
	s.mu.Lock()
	old := s.config.AccessKeys
	s.config.AccessKeys = keys
	if err := s.save(); err != nil {
		s.config.AccessKeys = old
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return nil
}

func (s *fileStore) OnboardingState() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.OnboardingState
}

func (s *fileStore) SetOnboardingState(state string) error {
	s.mu.Lock()
	old := s.config.OnboardingState
	s.config.OnboardingState = state
	if err := s.save(); err != nil {
		s.config.OnboardingState = old
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return nil
}

func (s *fileStore) SSLConfig() config.SSLConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.SSL
}

func (s *fileStore) SetSSLConfig(cfg config.SSLConfig) error {
	s.mu.Lock()
	old := s.config.SSL
	s.config.SSL = cfg
	if err := s.save(); err != nil {
		s.config.SSL = old
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return nil
}

func (s *fileStore) S3Config() config.S3Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.S3
}

func (s *fileStore) SetS3Config(cfg config.S3Config) error {
	s.mu.Lock()
	old := s.config.S3
	s.config.S3 = cfg
	if err := s.save(); err != nil {
		s.config.S3 = old
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return nil
}

func (s *fileStore) LogConfig() config.LogConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Log
}

func (s *fileStore) SetLogConfig(cfg config.LogConfig) error {
	s.mu.Lock()
	old := s.config.Log
	s.config.Log = cfg
	if err := s.save(); err != nil {
		s.config.Log = old
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return nil
}

func (s *fileStore) CreateSession() (string, error) {
	return s.sessions.Create()
}

func (s *fileStore) ValidateSession(token string) bool {
	return s.sessions.Validate(token)
}

func (s *fileStore) DeleteSession(token string) {
	s.sessions.Delete(token)
}

func (s *fileStore) StartSessionCleanup(stop <-chan struct{}) {
	s.sessions.StartCleanup(stop)
}

// save persists the current config to disk. Caller must hold s.mu (read or write).
func (s *fileStore) save() error {
	return config.Save(s.cfgPath, s.config)
}

// memorySessionManager implements SessionManager with an in-memory map.
type memorySessionManager struct {
	sessions map[string]*session
	mu       sync.RWMutex
	ttl      time.Duration
}

type session struct {
	token     string
	expiresAt time.Time
}

func NewMemorySessionManager(ttl time.Duration) SessionManager {
	return &memorySessionManager{
		sessions: make(map[string]*session),
		ttl:      ttl,
	}
}

func (sm *memorySessionManager) Create() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	sm.mu.Lock()
	sm.sessions[token] = &session{token: token, expiresAt: time.Now().Add(sm.ttl)}
	sm.mu.Unlock()
	return token, nil
}

func (sm *memorySessionManager) Validate(token string) bool {
	sm.mu.RLock()
	s, ok := sm.sessions[token]
	sm.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(s.expiresAt) {
		sm.mu.Lock()
		delete(sm.sessions, token)
		sm.mu.Unlock()
		return false
	}
	return true
}

func (sm *memorySessionManager) Delete(token string) {
	sm.mu.Lock()
	delete(sm.sessions, token)
	sm.mu.Unlock()
}

func (sm *memorySessionManager) StartCleanup(stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sm.prune()
			case <-stop:
				return
			}
		}
	}()
}

func (sm *memorySessionManager) prune() {
	now := time.Now()
	sm.mu.Lock()
	for token, s := range sm.sessions {
		if now.After(s.expiresAt) {
			delete(sm.sessions, token)
		}
	}
	sm.mu.Unlock()
}
