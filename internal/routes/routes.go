package routes

// Panel prefix: all panel routes live under this path.
const PanelPrefix = "/_panel"

// Panel route paths.
const (
	PanelRoot          = PanelPrefix + "/"
	PanelAssets        = PanelPrefix + "/assets"
	PanelLogin         = PanelPrefix + "/login"
	PanelLogout        = PanelPrefix + "/logout"
	PanelHealthz       = PanelPrefix + "/healthz"
	PanelResetPassword = PanelPrefix + "/reset-password"
	PanelDashboard     = PanelPrefix + "/dashboard"
	PanelOnboarding    = PanelPrefix + "/onboarding"
	PanelSettings      = PanelPrefix + "/settings"
	PanelUsers         = PanelPrefix + "/users"
	PanelKeys          = PanelPrefix + "/keys"
	PanelBuckets       = PanelPrefix + "/buckets"
	PanelBackups       = PanelPrefix + "/backups"
	PanelMonitoring    = PanelPrefix + "/monitoring"
)

// Onboarding page sub-routes (state machine steps). No leading slash;
// appended to PanelOnboarding by callers.
const (
	OnboardingStepPassword = "password"
	OnboardingStepConnect  = "connect"
	OnboardingStepFinish   = "finish"
)

// Onboarding API routes.
const (
	OnboardingStatus     = PanelPrefix + "/api/onboarding/status"
	OnboardingPublicKey  = PanelPrefix + "/api/onboarding/public-key"
	OnboardingAppKey     = PanelPrefix + "/api/onboarding/app-key"
	OnboardingAccessKeys = PanelPrefix + "/api/onboarding/access-keys"
	OnboardingAdminPass  = PanelPrefix + "/api/onboarding/admin-password"
	OnboardingConfig     = PanelPrefix + "/api/onboarding/config"
	OnboardingReset      = PanelPrefix + "/api/onboarding/reset"
)

// Panel API routes (authenticated).
const (
	PanelStatus    = PanelPrefix + "/api/status"
	PanelKeysAPI   = PanelPrefix + "/api/keys"
	PanelVersion   = PanelPrefix + "/api/version"
	PanelEvents    = PanelPrefix + "/api/events"
	PanelS3Config  = PanelPrefix + "/api/s3-config"
	PanelSSLConfig = PanelPrefix + "/api/ssl-config"
	PanelLogConfig = PanelPrefix + "/api/log-config"

	PanelAdminStats  = PanelPrefix + "/api/admin/stats"
	PanelAdminBackup = PanelPrefix + "/api/admin/backup"

	PanelUsersAPI    = PanelPrefix + "/api/users"
	PanelUserAPI     = PanelPrefix + "/api/users/:name"
	PanelUserKeysAPI = PanelPrefix + "/api/users/:name/keys"

	PanelBucketsAPI       = PanelPrefix + "/api/buckets"
	PanelBucketLifecycle  = PanelPrefix + "/api/buckets/:name/lifecycle"
	PanelBucketVersioning = PanelPrefix + "/api/buckets/:name/versioning"

	PanelBackupsAPI    = PanelPrefix + "/api/backups"
	PanelBackupFileAPI = PanelPrefix + "/api/backups/:filename"

	PanelSystemRestart = PanelPrefix + "/api/system/restart"
	PanelSystemFlush   = PanelPrefix + "/api/system/flush"

	// Password management (change is authenticated JSON API).
	// Reset is a pre-auth form POST to PanelResetPassword, not an API route.
	PanelPasswordChange = PanelPrefix + "/api/password/change"

	// Update control routes (sidecar mode only: adaptive based on /state volume).
	PanelUpdateStatus  = PanelPrefix + "/api/update/status"
	PanelUpdateToggle  = PanelPrefix + "/api/update/toggle"
	PanelUpdateTrigger = PanelPrefix + "/api/update/trigger"
)

// Form input field names.
const (
	FormFieldPassword = "password"
)
