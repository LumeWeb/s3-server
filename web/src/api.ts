import ky from 'ky'

// --- CSRF ---

export function csrfToken(): string {
  const c = document.cookie.split('; ').find(r => r.startsWith('_csrf='))
  return c ? c.split('=')[1] : ''
}

export function withCsrf(opts: Record<string, any> = {}) {
  const t = csrfToken()
  if (!t) return opts
  return { ...opts, headers: { ...(opts.headers || {}), 'X-CSRF-Token': t } }
}

// --- Error messages ---

// Error type → human-readable message map.
// Populated from Go-side ErrorType constants (internal/api/errors.go).
export const ERROR_MESSAGES: Record<string, string> = Object.fromEntries([
  // — Internal errors —
  ['DATABASE_OPEN_FAILED', 'The server could not access its database. Check that the data directory exists and has correct permissions.'],
  ['DATABASE_STORE_FAILED', 'The server failed to save data to the database. Check disk space and permissions.'],
  ['SQLITE_NOT_INITIALIZED', 'The database has not been initialized. Complete the onboarding flow first.'],
  ['BACKEND_NOT_INITIALIZED', 'The S3 backend is not running. Try restarting the server or check the logs.'],
  ['BACKEND_INIT_FAILED', 'The S3 backend failed to start. Verify your configuration in Settings.'],
  ['S3_CONFIG_SAVE_FAILED', 'Failed to save the S3 configuration. Check that the config file is writable.'],
  ['SSL_CONFIG_SAVE_FAILED', 'Failed to save the SSL configuration. Check that the config file is writable.'],
  ['PASSWORD_SET_FAILED', 'Failed to set the admin password. Check the server logs.'],
  ['USER_CREATE_FAILED', 'Failed to create the user. Check the server logs.'],
  ['USER_DELETE_FAILED', 'Failed to delete the user.'],
  ['ACCESS_KEY_CREATE_FAILED', 'Failed to create the access key.'],
  ['ACCESS_KEY_DELETE_FAILED', 'Failed to delete the access key.'],
  ['BACKUP_CREATE_FAILED', 'Failed to create the backup. Check disk space and permissions.'],
  ['BACKUP_DELETE_FAILED', 'Failed to delete the backup.'],
  ['BACKUP_STAT_FAILED', 'Failed to check the backup file.'],
  ['BACKUP_OPEN_FAILED', 'Failed to open the backup file.'],
  ['LIFECYCLE_PUT_FAILED', 'Failed to save the lifecycle configuration.'],
  ['LIFECYCLE_DELETE_FAILED', 'Failed to delete the lifecycle configuration.'],
  ['BACKUP_LIST_FAILED', 'Failed to list backups.'],
  ['BUCKET_LIST_FAILED', 'Failed to list buckets.'],
  ['BUCKET_CREATE_FAILED', 'Failed to create the bucket.'],
  ['BUCKET_DELETE_FAILED', 'Failed to delete the bucket.'],
  ['FLUSH_FAILED', 'Failed to flush objects from the bucket.'],
  ['USER_LIST_FAILED', 'Failed to list users.'],
  ['ACCESS_KEY_LIST_FAILED', 'Failed to list access keys.'],
  ['MONITORING_STATS_FAILED', 'Failed to load monitoring statistics.'],
  ['SSE_BROKER_NOT_CONFIGURED', 'Real-time events are not available. The SSE broker is not configured.'],
  ['INVALID_REQUEST_URL', 'The request URL was invalid.'],
  ['BACKUP_DIR_CREATE_FAILED', 'Failed to create the backups directory. Check permissions.'],
  ['BACKUP_PATH_CHECK_FAILED', 'Failed to verify the backup path.'],
  // — Validation / bad request —
  ['INVALID_REQUEST_BODY', 'The request was malformed. Please check your input and try again.'],
  ['PASSWORD_TOO_SHORT', 'Password must be at least 8 characters.'],
  ['ACCESS_KEY_MISSING', 'Access key is required.'],
  ['SECRET_KEY_MISSING', 'Secret key is required.'],
  ['ACCESS_KEY_LENGTH', 'Access key must be 16-128 characters.'],
  ['SECRET_KEY_LENGTH', 'Secret key must be 32-128 characters.'],
  ['NAME_REQUIRED', 'A name is required.'],
  ['FILENAME_REQUIRED', 'A filename is required.'],
  ['INVALID_FILENAME', 'The filename is invalid.'],
  ['DIRECTORY_REQUIRED', 'A data directory is required.'],
  ['INDEXER_URL_REQUIRED', 'An indexer URL is required.'],
  ['SSL_MODE_INVALID', 'SSL mode must be none, platform, or managed.'],
  ['ACME_EMAIL_REQUIRED', 'An ACME email is required for managed SSL.'],
  ['BACKUP_NOT_FOUND', 'Backup not found.'],
  ['ACCESS_KEY_NOT_FOUND', 'Access key not found.'],
  ['CANNOT_DELETE_LAST_KEY', 'Cannot delete the last access key. At least one must remain.'],
  ['CANNOT_DELETE_DEFAULT_USER', 'Cannot delete the default user.'],
  ['RULE_STATUS_INVALID', 'Rule status must be Enabled or Disabled.'],
  ['EXPIRATION_DAYS_INVALID', 'Expiration days must be a positive integer.'],
  ['AT_LEAST_ONE_KEY_REQUIRED', 'At least one access key is required.'],
  // — Onboarding —
  ['ONBOARDING_COMPLETE', 'Onboarding has already been completed.'],
  ['ONBOARDING_REQUIRED', 'Onboarding must be completed first.'],
  ['ADMIN_PASSWORD_REQUIRED', 'The admin password must be set first.'],
  ['APP_KEY_REQUIRED', 'The app key must be set first.'],
  ['INVALID_BASE64', 'The app key has invalid base64 encoding.'],
  ['DECRYPTION_FAILED_ERROR', 'Failed to decrypt the app key.'],
  ['APP_KEY_SIZE_INVALID', 'The app key must be exactly 32 bytes.'],
  ['DATA_DIRECTORY_NOT_CONFIGURED', 'The data directory is not configured. Set it in Settings.'],
  ['ADMIN_HANDLER_NOT_CONFIGURED', 'The admin handler is not configured.'],
  // — Auth —
  ['AUTH_REQUIRED', 'Authentication is required. Please log in.'],
])

// Codes that should show a modal dialog (critical, not inline)
export const DIALOG_CODES = new Set(['INTERNAL_ERROR', 'NOT_READY'])

// Dialog title by error code
export const DIALOG_TITLES: Record<string, string> = {
  INTERNAL_ERROR: 'Server Error',
  NOT_READY: 'Service Unavailable',
}

// --- Ky client ---

const api = ky.create({
  headers: { 'Content-Type': 'application/json' },
  timeout: 30000,
})

export async function handleReq<T = any>(promise: Promise<T>): Promise<T | undefined> {
  try {
    return await promise
  } catch (e: any) {
    // Ky v2 HTTPError pre-parses the response body into e.data
    const body = e.data || {}
    const code = body.code || ''
    const type = body.type || ''
    const rawMsg = body.message || body.error || e.message || 'Request failed'
    const humanMsg = ERROR_MESSAGES[type] || rawMsg
    if (DIALOG_CODES.has(code)) {
      window.dispatchEvent(new CustomEvent('__api:error', {
        detail: { code, type, message: humanMsg, raw: rawMsg },
      }))
      return undefined
    }
    if (window.__sseToast) {
      window.__sseToast(humanMsg, 'error')
    }
    return undefined
  }
}

// Expose __api on window for inline Alpine components
window.__api = {
  get: (url: string) => handleReq(api(url, withCsrf()).json()),
  post: (url: string, json?: any) => handleReq(api.post(url, { json, ...withCsrf() }).json()),
  put: (url: string, json?: any) => handleReq(api.put(url, { json, ...withCsrf() }).json()),
  del: (url: string) => handleReq(api.delete(url, withCsrf()).json()),
}
