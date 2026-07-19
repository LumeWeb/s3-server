// Pure utility functions extracted from Alpine components for testability.
// These functions have no DOM or window dependencies, making them trivially
// testable in isolation. Alpine components call these for their logic.

// --- Password validation ---

export const MIN_PASSWORD_LENGTH = 8

export function validatePasswordMatch(newPw: string, confirmPw: string): string | null {
  if (newPw !== confirmPw) {
    return 'Passwords do not match'
  }
  if (newPw.length < MIN_PASSWORD_LENGTH) {
    return `Password must be at least ${MIN_PASSWORD_LENGTH} characters`
  }
  return null
}

// --- Settings: S3 config payload ---

export interface S3ConfigPayload {
  directory: string
  indexer_url: string
  host_bases: string[]
  disk_usage_limit: string
  upload_waste_pct: number
}

export function buildS3ConfigPayload(s3: {
  directory: string
  indexer_selection: string
  custom_indexer: string
  host_bases_input: string
  disk_usage_limit: string
  upload_waste_pct: number
}): S3ConfigPayload {
  const hostBases = s3.host_bases_input
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)

  const indexerURL = s3.indexer_selection === '__custom__' ? s3.custom_indexer : s3.indexer_selection

  return {
    directory: s3.directory,
    indexer_url: indexerURL,
    host_bases: hostBases,
    disk_usage_limit: s3.disk_usage_limit,
    upload_waste_pct: s3.upload_waste_pct,
  }
}

// --- Settings: read JSON from script tag ---

export function readJSONScript<T>(id: string, fallback: T): T {
  const el = document.getElementById(id)
  if (!el || !el.textContent) {
    return fallback
  }
  return JSON.parse(el.textContent) as T
}

// --- Monitoring: byte formatting ---

export function fmtBytes(bytes: number): string {
  if (!bytes || bytes === 0) return '0 B'
  const k = 1000
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`
}

// --- Monitoring: account percentage ---

export function accountPct(pinned: number, max: number): number {
  if (!max) return 0
  return (pinned / max) * 100
}

// --- Buckets: versioning toggle ---

export function nextVersioningStatus(currentStatus: string): string {
  return currentStatus === 'Enabled' ? 'Suspended' : 'Enabled'
}

// --- Buckets: lifecycle URL builder ---

export function bucketLifecycleURL(name: string, action: 'get' | 'put' | 'delete'): string {
  const base = `/_panel/api/buckets/${encodeURIComponent(name)}/lifecycle`
  return base
}

export function bucketVersioningURL(name: string): string {
  return `/_panel/api/buckets/${encodeURIComponent(name)}/versioning`
}

// --- Buckets: lifecycle rule normalization ---

export function normalizeLifecycleRules(rules: any[]): { prefix: string; expiration_days: number; status: string }[] {
  return (rules || []).map((r: any) => ({
    prefix: r.prefix || '',
    expiration_days: r.expiration_days || 30,
    status: r.status || 'Enabled',
  }))
}
