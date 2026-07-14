package ssl

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"go.lumeweb.com/s3-server/internal/config"
	"github.com/caddyserver/certmagic"
	"go.uber.org/zap"
)

// Manager handles TLS certificate provisioning via certmagic.
type Manager struct {
	magic *certmagic.Config
	cfg   config.SSLConfig
	log   *zap.Logger
}

// NewManager creates an SSL manager. Returns nil if SSL mode is not "managed".
func NewManager(sslCfg config.SSLConfig, dataDir string, log *zap.Logger) *Manager {
	if sslCfg.Mode != config.SSLModeManaged {
		return nil
	}

	storage := &certmagic.FileStorage{
		Path: dataDir + "/certmagic",
	}

	magic := certmagic.New(certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(cert certmagic.Certificate) (*certmagic.Config, error) {
			return certmagic.NewDefault(), nil
		},
		Logger: log.Named("certmagic-cache"),
	}), certmagic.Config{
		Storage: storage,
		Logger:  log.Named("certmagic"),
	})

	acme := certmagic.NewACMEIssuer(magic, certmagic.ACMEIssuer{
		Email:  sslCfg.ACMEEmail,
		Agreed: true,
		Logger: log.Named("acme"),
	})

	if sslCfg.ACMEDirURL != "" {
		acme.CA = sslCfg.ACMEDirURL
	}

	magic.Issuers = []certmagic.Issuer{acme}

	return &Manager{
		magic: magic,
		cfg:   sslCfg,
		log:   log,
	}
}

// TLSConfig returns a *tls.Config for the HTTPS server.
// Adds h2 and http/1.1 to NextProtos for proper protocol negotiation.
func (m *Manager) TLSConfig() *tls.Config {
	tlsCfg := m.magic.TLSConfig()
	tlsCfg.NextProtos = append([]string{"h2", "http/1.1"}, tlsCfg.NextProtos...)
	return tlsCfg
}

// HTTPHandler wraps the given handler to solve HTTP-01 ACME challenges.
// If no ACME issuer with HTTP-01 enabled is found, returns the handler unchanged.
func (m *Manager) HTTPHandler(h http.Handler) http.Handler {
	for _, iss := range m.magic.Issuers {
		if acme, ok := iss.(*certmagic.ACMEIssuer); ok && !acme.DisableHTTPChallenge {
			return acme.HTTPChallengeHandler(h)
		}
	}
	return h
}

// ProvisionCert eagerly obtains a certificate for the given domain.
// Blocks until the cert is obtained or an error occurs.
func (m *Manager) ProvisionCert(ctx context.Context, domain string) error {
	m.log.Info("provisioning certificate", zap.String("domain", domain))
	if err := m.magic.ManageSync(ctx, []string{domain}); err != nil {
		return fmt.Errorf("failed to provision cert for %s: %w", domain, err)
	}
	m.log.Info("certificate provisioned", zap.String("domain", domain))
	return nil
}

// ProvisionCerts eagerly obtains certificates for multiple domains.
func (m *Manager) ProvisionCerts(ctx context.Context, domains []string) error {
	for _, d := range domains {
		if err := m.ProvisionCert(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

// Shutdown cleans up certmagic resources.
func (m *Manager) Shutdown(ctx context.Context) error {
	// certmagic doesn't require explicit shutdown: certs are persisted to storage.
	// This hook exists for future cleanup if needed.
	return nil
}

// IsManaged returns true if SSL mode is "managed".
func (m *Manager) IsManaged() bool {
	return m != nil
}

// Mode returns the configured SSL mode string.
func (m *Manager) Mode() config.SSLMode {
	if m == nil {
		return config.SSLModeNone
	}
	return m.cfg.Mode
}
