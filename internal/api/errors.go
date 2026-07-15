package api

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"
	"go.uber.org/zap"
)

// ErrorCode is the broad HTTP-level error category.
type ErrorCode string

const (
	ErrUnauthorized       ErrorCode = "UNAUTHORIZED"
	ErrForbidden          ErrorCode = "FORBIDDEN"
	ErrBadRequest         ErrorCode = "BAD_REQUEST"
	ErrNotFound           ErrorCode = "NOT_FOUND"
	ErrConflict           ErrorCode = "CONFLICT"
	ErrInternal           ErrorCode = "INTERNAL_ERROR"
	ErrNotReady           ErrorCode = "NOT_READY"
	ErrValidation         ErrorCode = "VALIDATION_ERROR"
	ErrOnboardingRequired ErrorCode = "ONBOARDING_REQUIRED"
	ErrOnboardingComplete ErrorCode = "ONBOARDING_COMPLETE"
	ErrInvalidAppKey      ErrorCode = "INVALID_APP_KEY"
	ErrDecryptionFailed   ErrorCode = "DECRYPTION_FAILED"
)

// ErrorType is a stable error subtype identifier. The frontend uses these
// to look up human-readable messages via a JS map. Add new types here as
// needed: they are intentionally separate from the broad ErrorCode so a
// single code (e.g. INTERNAL_ERROR) can carry multiple specific types.
type ErrorType string

const (
	// Internal errors
	TypeDatabaseOpenFailed     ErrorType = "DATABASE_OPEN_FAILED"
	TypeDatabaseStoreFailed    ErrorType = "DATABASE_STORE_FAILED"
	TypeSQLiteNotInitialized   ErrorType = "SQLITE_NOT_INITIALIZED"
	TypeBackendNotInitialized  ErrorType = "BACKEND_NOT_INITIALIZED"
	TypeBackendInitFailed      ErrorType = "BACKEND_INIT_FAILED"
	TypeS3ConfigSaveFailed     ErrorType = "S3_CONFIG_SAVE_FAILED"
	TypeSSLConfigSaveFailed    ErrorType = "SSL_CONFIG_SAVE_FAILED"
	TypeLogConfigSaveFailed    ErrorType = "LOG_CONFIG_SAVE_FAILED"
	TypePasswordSetFailed      ErrorType = "PASSWORD_SET_FAILED"
	TypeConflict               ErrorType = "CONFLICT"
	TypeUserCreateFailed       ErrorType = "USER_CREATE_FAILED"
	TypeUserDeleteFailed       ErrorType = "USER_DELETE_FAILED"
	TypeAccessKeyCreateFailed  ErrorType = "ACCESS_KEY_CREATE_FAILED"
	TypeAccessKeyDeleteFailed  ErrorType = "ACCESS_KEY_DELETE_FAILED"
	TypeBackupCreateFailed     ErrorType = "BACKUP_CREATE_FAILED"
	TypeBackupDeleteFailed     ErrorType = "BACKUP_DELETE_FAILED"
	TypeBackupStatFailed       ErrorType = "BACKUP_STAT_FAILED"
	TypeBackupOpenFailed       ErrorType = "BACKUP_OPEN_FAILED"
	TypeLifecyclePutFailed     ErrorType = "LIFECYCLE_PUT_FAILED"
	TypeLifecycleDeleteFailed  ErrorType = "LIFECYCLE_DELETE_FAILED"
	TypeBackupListFailed       ErrorType = "BACKUP_LIST_FAILED"
	TypeBucketListFailed       ErrorType = "BUCKET_LIST_FAILED"
	TypeBucketCreateFailed     ErrorType = "BUCKET_CREATE_FAILED"
	TypeBucketDeleteFailed     ErrorType = "BUCKET_DELETE_FAILED"
	TypeFlushFailed            ErrorType = "FLUSH_FAILED"
	TypeUserListFailed         ErrorType = "USER_LIST_FAILED"
	TypeAccessKeyListFailed    ErrorType = "ACCESS_KEY_LIST_FAILED"
	TypeMonitoringStatsFailed  ErrorType = "MONITORING_STATS_FAILED"
	TypeSSEBrokerNotConfigured ErrorType = "SSE_BROKER_NOT_CONFIGURED"
	TypeInvalidRequestURL      ErrorType = "INVALID_REQUEST_URL"
	TypeBackupDirCreateFailed  ErrorType = "BACKUP_DIR_CREATE_FAILED"
	TypeBackupPathCheckFailed  ErrorType = "BACKUP_PATH_CHECK_FAILED"

	// Validation / bad request
	TypeInvalidRequestBody    ErrorType = "INVALID_REQUEST_BODY"
	TypePasswordTooShort      ErrorType = "PASSWORD_TOO_SHORT"
	TypeAccessKeyMissing      ErrorType = "ACCESS_KEY_MISSING"
	TypeSecretKeyMissing      ErrorType = "SECRET_KEY_MISSING"
	TypeAccessKeyLength       ErrorType = "ACCESS_KEY_LENGTH"
	TypeSecretKeyLength       ErrorType = "SECRET_KEY_LENGTH"
	TypeNameRequired          ErrorType = "NAME_REQUIRED"
	TypeFilenameRequired      ErrorType = "FILENAME_REQUIRED"
	TypeInvalidFilename       ErrorType = "INVALID_FILENAME"
	TypeDirectoryRequired     ErrorType = "DIRECTORY_REQUIRED"
	TypeIndexerURLRequired    ErrorType = "INDEXER_URL_REQUIRED"
	TypeIndexerLocked         ErrorType = "INDEXER_LOCKED"
	TypeSSLModeInvalid        ErrorType = "SSL_MODE_INVALID"
	TypeACMEEmailRequired     ErrorType = "ACME_EMAIL_REQUIRED"
	TypeBackupNotFound        ErrorType = "BACKUP_NOT_FOUND"
	TypeAccessKeyNotFound     ErrorType = "ACCESS_KEY_NOT_FOUND"
	TypeCannotDeleteLastKey   ErrorType = "CANNOT_DELETE_LAST_KEY"
	TypeRuleStatusInvalid     ErrorType = "RULE_STATUS_INVALID"
	TypeExpirationDaysInvalid ErrorType = "EXPIRATION_DAYS_INVALID"
	TypeAtLeastOneKeyRequired ErrorType = "AT_LEAST_ONE_KEY_REQUIRED"

	// Onboarding flow
	TypeOnboardingComplete         ErrorType = "ONBOARDING_COMPLETE"
	TypeOnboardingRequired         ErrorType = "ONBOARDING_REQUIRED"
	TypeOnboardingResetFailed      ErrorType = "ONBOARDING_RESET_FAILED"
	TypeOnboardingStateFailed      ErrorType = "ONBOARDING_STATE_FAILED"
	TypeAdminPasswordRequired      ErrorType = "ADMIN_PASSWORD_REQUIRED"
	TypeAppKeyRequired             ErrorType = "APP_KEY_REQUIRED"
	TypeInvalidBase64              ErrorType = "INVALID_BASE64"
	TypeDecryptionFailed           ErrorType = "DECRYPTION_FAILED_ERROR"
	TypeAppKeySizeInvalid          ErrorType = "APP_KEY_SIZE_INVALID"
	TypeDataDirectoryNotConfigured ErrorType = "DATA_DIRECTORY_NOT_CONFIGURED"
	TypeAdminHandlerNotConfigured  ErrorType = "ADMIN_HANDLER_NOT_CONFIGURED"

	// Auth
	TypeAuthRequired       ErrorType = "AUTH_REQUIRED"
	TypePasswordRequired   ErrorType = "PASSWORD_REQUIRED"
	TypePasswordIncorrect  ErrorType = "PASSWORD_INCORRECT"
	TypeInvalidRequest     ErrorType = "INVALID_REQUEST"
	TypeResetTokenNotFound ErrorType = "RESET_TOKEN_NOT_FOUND"
	TypeResetTokenExpired  ErrorType = "RESET_TOKEN_EXPIRED"

	// Update / sidecar
	TypeUpdateNotAvailable  ErrorType = "UPDATE_NOT_AVAILABLE"
	TypeUpdateToggleFailed  ErrorType = "UPDATE_TOGGLE_FAILED"
	TypeUpdateTriggerFailed ErrorType = "UPDATE_TRIGGER_FAILED"
)

type Error struct {
	Code    ErrorCode `json:"code"`
	Type    ErrorType `json:"type,omitempty"`
	Message string    `json:"message"`
	Err     error     `json:"-"`
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func (e *Error) HttpStatus() int {
	switch e.Code {
	case ErrUnauthorized, ErrOnboardingRequired:
		return http.StatusUnauthorized
	case ErrForbidden:
		return http.StatusForbidden
	case ErrBadRequest, ErrInvalidAppKey, ErrDecryptionFailed:
		return http.StatusBadRequest
	case ErrNotFound:
		return http.StatusNotFound
	case ErrConflict, ErrOnboardingComplete:
		return http.StatusConflict
	case ErrNotReady:
		return http.StatusServiceUnavailable
	case ErrValidation:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

// NewError creates an Error with a code, type, message, and optional wrapped error.
func NewError(code ErrorCode, typeStr ErrorType, message string, err error) *Error {
	return &Error{Code: code, Type: typeStr, Message: message, Err: err}
}

// logger is the package-level zap logger used by Send* functions.
// Set via SetLogger at startup. Defaults to a no-op logger so the package
// works in tests without configuration.
var logger = zap.NewNop()

// SetLogger sets the package-level logger used by all Send* functions.
// Call once at startup after building the zap logger.
func SetLogger(l *zap.Logger) {
	if l != nil {
		logger = l
	}
}

// SendError sends a typed JSON error response and logs it.
func SendError(c *echo.Context, code ErrorCode, typeStr ErrorType, message string, err error) error {
	apiErr := NewError(code, typeStr, message, err)

	// Log every error response centrally. Use Warn for client errors, Error
	// for server errors. Include path, code, type, and wrapped error.
	fields := []zap.Field{
		zap.String("path", c.Path()),
		zap.String("code", string(code)),
		zap.String("type", string(typeStr)),
		zap.String("message", message),
	}
	if err != nil {
		fields = append(fields, zap.Error(err))
	}

	switch code {
	case ErrInternal, ErrNotReady:
		logger.Error("api error", fields...)
	default:
		logger.Warn("api error", fields...)
	}

	return c.JSON(apiErr.HttpStatus(), apiErr)
}

// SendInternal sends an INTERNAL_ERROR response with a specific type.
func SendInternal(c *echo.Context, typeStr ErrorType, message string, err error) error {
	return SendError(c, ErrInternal, typeStr, message, err)
}

// SendBadRequest sends a BAD_REQUEST response with a specific type.
func SendBadRequest(c *echo.Context, typeStr ErrorType, message string) error {
	return SendError(c, ErrBadRequest, typeStr, message, nil)
}

// SendUnauthorized sends an UNAUTHORIZED response with a specific type.
func SendUnauthorized(c *echo.Context, typeStr ErrorType, message string) error {
	return SendError(c, ErrUnauthorized, typeStr, message, nil)
}

// SendNotFound sends a NOT_FOUND response with a specific type.
func SendNotFound(c *echo.Context, typeStr ErrorType, message string) error {
	return SendError(c, ErrNotFound, typeStr, message, nil)
}

// SendConflict sends a CONFLICT response with a specific type.
func SendConflict(c *echo.Context, typeStr ErrorType, message string) error {
	return SendError(c, ErrConflict, typeStr, message, nil)
}

// SendValidation sends a VALIDATION_ERROR response with a specific type.
func SendValidation(c *echo.Context, typeStr ErrorType, message string) error {
	return SendError(c, ErrValidation, typeStr, message, nil)
}

// SendNotReady sends a NOT_READY response with a specific type.
func SendNotReady(c *echo.Context, typeStr ErrorType, message string, err error) error {
	return SendError(c, ErrNotReady, typeStr, message, err)
}
