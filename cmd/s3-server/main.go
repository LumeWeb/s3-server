package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/urfave/cli/v2"

	"go.lumeweb.com/s3-server/internal/admin"
	"go.lumeweb.com/s3-server/internal/api"
	"go.lumeweb.com/s3-server/internal/auth"
	"go.lumeweb.com/s3-server/internal/backend"
	"go.lumeweb.com/s3-server/internal/config"
	"go.lumeweb.com/s3-server/internal/handlers"
	"go.lumeweb.com/s3-server/internal/onboarding"
	"go.lumeweb.com/s3-server/internal/routes"
	"go.lumeweb.com/s3-server/internal/sse"
	"go.lumeweb.com/s3-server/internal/ssl"
	"go.lumeweb.com/s3-server/internal/store"
	"go.lumeweb.com/s3-server/internal/updater"
	"go.lumeweb.com/s3-server/internal/version"
	"go.lumeweb.com/s3-server/internal/views"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const appVersion = "0.1.0"

// csrfToken extracts the CSRF token from the Echo context.
func csrfToken(c *echo.Context) string {
	if v := c.Get("csrf"); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func envVar(suffix string) string {
	return config.EnvPrefix + suffix
}

func main() {
	app := &cli.App{
		Name:    "s3-server",
		Usage:   "Private S3-compatible storage server backed by Sia Network",
		Version: appVersion,
		Commands: []*cli.Command{
			serveCommand(),
		},
		DefaultCommand: "serve",
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func serveCommand() *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "Start the S3 server and admin panel",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "data-dir",
				Usage:   "Data directory for panel config and S3 metadata storage",
				EnvVars: []string{envVar("DATA_DIR")},
				Value:   "/var/lib/s3-server",
			},
			&cli.StringFlag{
				Name:    "listen-addr",
				Usage:   "Address to listen on (HTTPS in managed mode, HTTP otherwise)",
				EnvVars: []string{envVar("LISTEN_ADDR")},
				Value:   ":8080",
			},
			&cli.StringFlag{
				Name:    "http-addr",
				Usage:   "HTTP address for ACME challenges + redirect to HTTPS (managed mode only)",
				EnvVars: []string{envVar("HTTP_ADDR")},
				Value:   ":80",
			},
		},
		Action: runServe,
	}
}

// onboardingStepForState maps a server onboarding state to the corresponding
// URL step slug. Used by the route handler to redirect users to the correct
// step based on their server-side progress.
func onboardingStepForState(state string) string {
	switch onboarding.OnboardingState(state) {
	case onboarding.StateAdminSet:
		return routes.OnboardingStepConnect
	case onboarding.StateAppKeySet:
		return routes.OnboardingStepFinish
	case onboarding.StateComplete:
		return routes.OnboardingStepFinish
	default:
		return routes.OnboardingStepPassword
	}
}

// isStepReachable reports whether the requested step is behind or equal to
// the valid step in the onboarding sequence. This allows users to navigate
// back to previous steps (e.g. password → back to connect) but not forward
// to steps they haven't reached.
func isStepReachable(requested, valid string) bool {
	order := []string{routes.OnboardingStepPassword, routes.OnboardingStepConnect, routes.OnboardingStepFinish}
	reqIdx, validIdx := -1, -1
	for i, s := range order {
		if s == requested {
			reqIdx = i
		}
		if s == valid {
			validIdx = i
		}
	}
	if reqIdx == -1 || validIdx == -1 {
		return false
	}
	return reqIdx <= validIdx
}

func runServe(c *cli.Context) error {
	dataDir := c.String("data-dir")
	listenAddr := c.String("listen-addr")
	httpAddr := c.String("http-addr")

	// ensure backup storage exists before config load
	if err := os.MkdirAll(filepath.Join(dataDir, "backups"), 0750); err != nil {
		return fmt.Errorf("failed to create backups directory: %w", err)
	}

	// load panel config
	cfgPath := config.ConfigPath(dataDir)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// If the S3 directory is the default and doesn't exist, fall back to
	// --data-dir so local/dev launches work without /var/lib/s3-server.
	cfg.S3.Directory = config.ResolveS3Directory(cfg.S3.Directory, dataDir)

	// build logger
	log, logLevel, err := config.BuildLogger(cfg.Log)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer func() { _ = log.Sync() }()

	// Wire the zap logger into the api package so all Send* functions log
	// structured error responses automatically.
	api.SetLogger(log)

	// logLevelUpdater hot-reloads the zap log level when the user changes
	// it via the settings UI, without requiring a process restart.
	logLevelUpdater := &zapLogLevelUpdater{atomic: logLevel}

	// init store
	stor, err := store.New(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to init store: %w", err)
	}

	// Sync the resolved S3 directory back to the store. store.New() loads
	// config from disk independently, so the ResolveS3Directory fallback
	// isn't reflected in the store's in-memory copy until we write it back.
	origDir := stor.S3Config().Directory
	if origDir != cfg.S3.Directory {
		s3Cfg := stor.S3Config()
		s3Cfg.Directory = cfg.S3.Directory
		if err := stor.SetS3Config(s3Cfg); err != nil {
			return fmt.Errorf("failed to sync resolved S3 directory: %w", err)
		}
		log.Info("resolved S3 directory", zap.String("from", origDir), zap.String("to", cfg.S3.Directory))
	}

	// start session cleanup
	sessionStop := make(chan struct{})
	defer close(sessionStop)
	stor.StartSessionCleanup(sessionStop)

	// SSL setup — needed early for auth cookie Secure flag
	sslMgr := ssl.NewManager(cfg.SSL, dataDir, log)

	// swappable S3 handler — starts as 503 placeholder, swapped to real S3 after onboarding
	s3Handler := handlers.NewS3Handler()

	// backend manager owns the s3d lifecycle
	be := backend.NewManager(stor, backend.NewS3DFactory(log), s3Handler, log)

	// onboarding service handles the state machine
	onboardingSvc, err := onboarding.NewService(stor, log, be)
	if err != nil {
		return fmt.Errorf("failed to init onboarding service: %w", err)
	}
	// initCtx is cancelled on shutdown so the async backend init retry loop
	// (both cold-start and onboarding paths) can never block Cleanup's
	// initWg.Wait() from returning.
	initCtx, stopInit := context.WithCancel(c.Context)
	onboardingSvc.SetContext(initCtx)

	// sse broker for real-time dashboard updates
	sseBroker := sse.NewBroker(stor, be.Status, be.KeyStore, log)
	sseBroker.SetInitError(be.InitError)
	sseBroker.StartStatusLoop(c.Context, appVersion)

	// When backend status transitions (starting → running, starting → error),
	// publish an immediate SSE dashboard event instead of waiting for the
	// next status tick.
	be.SetOnStatusChange(func(statusStr, initErr string) {
		_ = sseBroker.PublishDashboard(sse.DashboardEvent{
			S3Status:  statusStr,
			InitError: initErr,
		})
	})

	// if onboarding already complete, init s3d asynchronously so the panel
	// is available immediately. The UI shows "backend starting" until
	// initialization completes, then transitions to "running" via SSE.
	// On failure, only log — do NOT reset onboarding state or delete
	// credentials. The user can retry from the dashboard. Resetting
	// would destroy valid production access keys on a transient failure.
	if stor.OnboardingState() == string(onboarding.StateComplete) {
		be.InitFromConfigAsync(initCtx, func() {
			log.Error("async backend init failed on cold start; onboarding state preserved")
		})
	}

	// start version checker (check + notify only, never self-apply)
	versionChecker := version.NewChecker(appVersion, "siafoundation/s3-server", log)
	go versionChecker.Start(c.Context)

	// init echo for panel routes
	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(middleware.Secure())

	// request logging middleware — logs method, path, status, duration
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:   true,
		LogMethod:   true,
		LogURI:      true,
		HandleError: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			duration := time.Since(v.StartTime)
			fields := []zap.Field{
				zap.String("method", v.Method),
				zap.String("path", v.URI),
				zap.Int("status", v.Status),
				zap.Duration("duration", duration),
			}
			if v.Error != nil {
				fields = append(fields, zap.Error(v.Error))
			}
			if duration > 1*time.Second {
				log.Warn("slow request", fields...)
			} else {
				log.Info("request", fields...)
			}
			return nil
		},
	}))
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup: "header:X-CSRF-Token,form:_csrf",
		CookiePath:  routes.PanelRoot,
		Skipper: func(c *echo.Context) bool {
			return strings.HasPrefix(c.Path(), routes.PanelAssets)
		},
	}))

	// auth — secure cookies when SSL is managed
	authSvc := auth.New(stor, 24*time.Hour, sslMgr.IsManaged())

	// public routes (no auth required)
	e.GET(routes.PanelLogin, func(c *echo.Context) error {
		if stor.OnboardingState() != string(onboarding.StateComplete) {
			return c.Redirect(http.StatusFound, routes.PanelOnboarding+"/"+onboardingStepForState(stor.OnboardingState()))
		}
		return views.Login(csrfToken(c)).Render(c.Request().Context(), c.Response())
	})
	e.POST(routes.PanelLogin, authSvc.LoginHandler)

	// password reset page (pre-auth — checks .reset-token file existence)
	e.GET(routes.PanelResetPassword, func(c *echo.Context) error {
		resetPath := stor.ResetTokenPath()
		_, err := os.Stat(resetPath)
		enabled := err == nil
		dataDir := stor.DataDir()
		return views.ResetPassword(csrfToken(c), enabled, dataDir).Render(c.Request().Context(), c.Response())
	})

	// password reset handler (pre-auth — form POST, deletes .reset-token on success)
	e.POST(routes.PanelResetPassword, func(c *echo.Context) error {
		newPassword := c.FormValue("new_password")
		if newPassword == "" {
			return c.String(http.StatusBadRequest, "new password is required")
		}
		if len(newPassword) < 8 {
			return c.String(http.StatusBadRequest, "password must be at least 8 characters")
		}
		resetPath := stor.ResetTokenPath()
		if _, err := os.Stat(resetPath); err != nil {
			return c.String(http.StatusUnauthorized, "password reset is not enabled")
		}
		if err := stor.SetAdminPassword(newPassword); err != nil {
			return c.String(http.StatusInternalServerError, "failed to set admin password")
		}
		if err := os.Remove(resetPath); err != nil {
			log.Warn("failed to remove reset token file", zap.Error(err))
		}
		return c.Redirect(http.StatusFound, routes.PanelLogin)
	})

	// onboarding page (no auth required — admin password not set yet)
	// Base route redirects to the step matching the server's onboarding state.
	e.GET(routes.PanelOnboarding, func(c *echo.Context) error {
		step := onboardingStepForState(stor.OnboardingState())
		return c.Redirect(http.StatusFound, routes.PanelOnboarding+"/"+step)
	})
	// Sub-route: validates the requested step against the server's state machine.
	// If the user accesses a step they haven't reached, redirect to the correct one.
	e.GET(routes.PanelOnboarding+"/:step", func(c *echo.Context) error {
		requestedStep := c.Param("step")
		serverState := stor.OnboardingState()
		validStep := onboardingStepForState(serverState)
		if requestedStep != validStep && !isStepReachable(requestedStep, validStep) {
			return c.Redirect(http.StatusFound, routes.PanelOnboarding+"/"+validStep)
		}
		pubKeyB64 := onboardingSvc.PublicKeyBase64()
		return views.Onboarding(csrfToken(c), pubKeyB64).Render(c.Request().Context(), c.Response())
	})

	// root panel redirect — onboarding or dashboard based on state
	e.GET(routes.PanelRoot, func(c *echo.Context) error {
		if stor.OnboardingState() != string(onboarding.StateComplete) {
			return c.Redirect(http.StatusFound, routes.PanelOnboarding+"/"+onboardingStepForState(stor.OnboardingState()))
		}
		return c.Redirect(http.StatusFound, routes.PanelDashboard)
	})

	// onboarding routes (no auth required — admin password not set yet)
	e.GET(routes.OnboardingStatus, onboardingSvc.StatusHandler)
	e.GET(routes.OnboardingPublicKey, onboardingSvc.PublicKeyHandler)
	e.GET(routes.OnboardingConfig, onboardingSvc.ConfigHandler)
	e.POST(routes.OnboardingAppKey, onboardingSvc.SetAppKeyHandler)
	e.POST(routes.OnboardingAccessKeys, onboardingSvc.SetAccessKeysHandler)
	e.POST(routes.OnboardingAdminPass, onboardingSvc.SetAdminPasswordHandler)
	e.POST(routes.OnboardingReset, onboardingSvc.ResetHandler)

	// authenticated routes
	panel := e.Group(routes.PanelPrefix)
	panel.Use(authSvc.AuthMiddleware)

	// if onboarding was reset (backend init failed), force users back to onboarding
	panel.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if stor.OnboardingState() != string(onboarding.StateComplete) {
				return c.Redirect(http.StatusFound, routes.PanelOnboarding+"/"+onboardingStepForState(stor.OnboardingState()))
			}
			return next(c)
		}
	})

	panel.POST("/logout", authSvc.LogoutHandler)

	// admin handler — used by panel services and mounted at /prometheus, /stats/, /system/
	adminHdlr := admin.Handler(be, log)

	// wire the stats fetcher on the broker so SSE clients receive upload stats
	sseBroker.SetStatsFetcher(&sse.AdminStatsFetcher{AdminHandler: adminHdlr, AccountClient: be.AccountClient})

	svc := handlers.NewServices(handlers.ServicesConfig{
		Store:           stor,
		KeyStore:        be.KeyStore,
		Backend:         be.Backend,
		AccountClient:   be.AccountClient,
		AdminHandler:    adminHdlr,
		Restarter:       be,
		BackendStatus:   be.Status,
		InitError:       be.InitError,
		Version:         appVersion,
		PlatformName:    config.ResolvePlatformName(cfg.PlatformID),
		Log:             log,
		LogLevelUpdater: logLevelUpdater,
		CSRFToken:       csrfToken,
		SSEBroker:       sseBroker,
		UpdateManager:   updater.New(),
	})
	handlers.RegisterRoutes(panel, svc)

	// version check endpoint (authenticated)
	panel.GET("/api/version", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, versionChecker.Result())
	})

	// favicon
	e.GET("/_panel/assets/favicon.svg", func(c *echo.Context) error {
		c.Response().Header().Set("Content-Type", "image/svg+xml")
		return c.String(http.StatusOK, views.FaviconSVG)
	})

	// pre-built tailwind CSS (embedded)
	e.GET("/_panel/assets/tailwind.css", func(c *echo.Context) error {
		c.Response().Header().Set("Content-Type", "text/css; charset=utf-8")
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return c.String(http.StatusOK, views.TailwindCSS)
	})

	// Vite-bundled assets (sodium, sia SDK, ky, WASM)
	e.GET("/_panel/assets/*", func(c *echo.Context) error {
		name := c.Param("*")
		data, err := views.WebFS.ReadFile("web/dist/" + name)
		if err != nil {
			return c.String(http.StatusNotFound, "not found")
		}
		switch {
		case strings.HasSuffix(name, ".js"):
			c.Response().Header().Set("Content-Type", "application/javascript; charset=utf-8")
		case strings.HasSuffix(name, ".wasm"):
			c.Response().Header().Set("Content-Type", "application/wasm")
		case strings.HasSuffix(name, ".css"):
			c.Response().Header().Set("Content-Type", "text/css; charset=utf-8")
		}
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return c.String(http.StatusOK, string(data))
	})

	// health check
	e.GET(routes.PanelHealthz, func(c *echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})

	// compose: S3 at root, panel under /_panel/
	// ServeMux matches longest prefix, so /_panel/ always wins over /
	mux := http.NewServeMux()
	mux.Handle("/_panel/", e) // Echo handles everything under /_panel/
	mux.HandleFunc("/favicon.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(views.FaviconSVG)) //nolint:errcheck
	})

	// s3d admin/monitoring endpoints — protected with HTTP Basic auth
	// (admin password). Compatible with s3d's route paths for tools like
	// Prometheus that expect /prometheus, /stats/, /system/.
	adminAuthed := auth.BasicAuth(stor, adminHdlr)
	mux.Handle("/prometheus", adminAuthed)
	mux.Handle("/stats/", adminAuthed)
	mux.Handle("/system/", adminAuthed)

	// Wrap S3 handler with per-bucket SSL cert provisioning middleware.
	// When SSL is managed and host bases are configured, bucket creation
	// triggers eager cert issuance for {bucket}.{hostbase}.
	s3Root := http.Handler(s3Handler)
	if sslMgr.IsManaged() && len(cfg.S3.HostBases) > 0 {
		var cancel context.CancelFunc
		s3Root, cancel = ssl.BucketCreateMiddleware(s3Handler, sslMgr, cfg.S3.HostBases, log)
		defer cancel()
	}
	mux.Handle("/", s3Root) // Everything else goes to S3

	var httpServer, httpsServer *http.Server

	// shutdown signal channel — used by server goroutines to trigger graceful shutdown on fatal errors
	quit := make(chan os.Signal, 1)

	if sslMgr.IsManaged() {
		// Managed SSL: provision certs for host bases, then start HTTP + HTTPS
		if len(cfg.S3.HostBases) > 0 {
			if err := sslMgr.ProvisionCerts(c.Context, cfg.S3.HostBases); err != nil {
				log.Warn("failed to provision some host base certs — will retry on TLS handshake", zap.Error(err))
			}
		}

		// HTTP server (ACME challenges + redirect) — default :80, override with --http-addr
		redirectAndChallenge := sslMgr.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := "https://" + r.Host + r.URL.RequestURI()
			// If HTTPS is on a non-standard port, append it to the redirect
			if _, port, err := net.SplitHostPort(listenAddr); err == nil && port != "443" {
				host := r.Host
				if h, _, err := net.SplitHostPort(r.Host); err == nil {
					host = h
				} else {
					// r.Host may be a bare IP literal (e.g. IPv6 "[::1]");
					// strip brackets to avoid double-wrapping in JoinHostPort.
					host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
				}
				target = "https://" + net.JoinHostPort(host, port) + r.URL.RequestURI()
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}))
		httpServer = &http.Server{
			Addr:    httpAddr,
			Handler: redirectAndChallenge,
		}

		// HTTPS server — uses listen-addr (default :8080, set to :443 for bare-metal)
		httpsServer = &http.Server{
			Addr:      listenAddr,
			Handler:   mux,
			TLSConfig: sslMgr.TLSConfig(),
		}

		go func() {
			log.Info("starting HTTP server (ACME challenges + redirect)", zap.String("addr", httpServer.Addr))
			if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("HTTP server error", zap.Error(err))
				quit <- syscall.SIGTERM
			}
		}()
		go func() {
			log.Info("starting HTTPS server", zap.String("addr", httpsServer.Addr))
			if err := httpsServer.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("HTTPS server error", zap.Error(err))
				quit <- syscall.SIGTERM
			}
		}()
	} else {
		// None or Platform: HTTP only
		httpServer = &http.Server{
			Addr:    listenAddr,
			Handler: mux,
		}
		go func() {
			log.Info("starting server", zap.String("addr", listenAddr))
			if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("server error", zap.Error(err))
				quit <- syscall.SIGTERM
			}
		}()
	}

	// wait for shutdown signal
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down...")

	// shut down SSE broker first — waits for subscriber goroutines, closes connections
	sseShutdownCtx, sseCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer sseCancel()
	if err := sseBroker.Shutdown(sseShutdownCtx); err != nil {
		log.Error("sse shutdown error", zap.Error(err))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if httpServer != nil {
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTP shutdown error", zap.Error(err))
		}
	}
	if httpsServer != nil {
		if err := httpsServer.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTPS shutdown error", zap.Error(err))
		}
	}

	// stop the async backend init retry loop so Cleanup's initWg.Wait()
	// returns promptly even if the backend never initialized.
	stopInit()

	// cleanup s3d backend
	be.Cleanup()

	log.Info("shutdown complete")
	return nil
}

// zapLogLevelUpdater implements handlers.LogLevelUpdater by wrapping
// zap.AtomicLevel so the log level can be changed at runtime without
// restarting the process.
type zapLogLevelUpdater struct {
	atomic zap.AtomicLevel
}

func (u *zapLogLevelUpdater) SetLevel(cfg config.LogConfig) {
	if parsed, err := zapcore.ParseLevel(cfg.Level); err == nil {
		u.atomic.SetLevel(parsed)
	}
}
