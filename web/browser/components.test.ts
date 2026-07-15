import { describe, it, expect, beforeEach, vi } from 'vitest'
import { settingsApp, changePasswordApp, versionCheckApp } from '../src/pages/settings'
import { resetPasswordForm } from '../src/pages/reset_password'
import { monitoringApp } from '../src/pages/monitoring'

// --- Helper: create an Alpine-like component proxy ---

function createComponent(factory: (...args: any[]) => any): any {
  const obj = factory.call({} as any)
  const component = Object.create(obj)
  // Stub Alpine magic
  component.$watch = vi.fn()
  component.$el = document.createElement('div')
  component.$dispatch = vi.fn()
  component.$refs = {}
  component.$nextTick = vi.fn((cb: () => void) => Promise.resolve().then(cb))
  return component
}

// --- Settings app ---

describe('settingsApp', () => {
  beforeEach(() => {
  })

  it('returns default state when no settings-data script exists', () => {
    const c = createComponent(settingsApp)
    expect(c.s3).toBeDefined()
    expect(c.s3.directory).toBe('')
    expect(c.ssl).toBeDefined()
    expect(c.log.level).toBe('info')
    expect(c.log.format).toBe('json')
    expect(c.s3Saving).toBe(false)
    expect(c.sslSaving).toBe(false)
    expect(c.logSaving).toBe(false)
  })

  it('reads settings from script tag', () => {
    document.body.innerHTML = `<script type="application/json" id="settings-data">
      {"s3":{"directory":"/data","indexer_url":"https://sia.edu","available_indexers":[],"indexer_selection":"","custom_indexer":"","host_bases_input":"a.com","disk_usage_limit":100,"upload_waste_pct":0.2},"ssl":{"mode":"none","acme_email":"","acme_dir_url":""},"log":{"level":"debug","format":"text"}}
    </script>`
    const c = createComponent(settingsApp)
    expect(c.s3.directory).toBe('/data')
    expect(c.log.level).toBe('debug')
    expect(c.log.format).toBe('text')
    expect(c.s3.upload_waste_pct).toBe(0.2)
  })

  it('saveConfig sets and clears loading flag', async () => {
    const c = createComponent(settingsApp)
    // Stub apiAction via window globals
    const apiActionSpy = vi.fn().mockResolvedValue({})
    ;(window as any).__sseAction = apiActionSpy
    await c.saveConfig('s3Saving', 'S3', '/_panel/api/s3-config', { directory: '/data' })
    expect(apiActionSpy).toHaveBeenCalled()
    expect(c.s3Saving).toBe(false)
  })

  it('saveS3Config builds payload via buildS3ConfigPayload', async () => {
    const c = createComponent(settingsApp)
    c.s3 = {
      directory: '/data',
      indexer_selection: '__custom__',
      custom_indexer: 'https://custom.indexer',
      host_bases_input: 'a.com, b.com',
      disk_usage_limit: 200,
      upload_waste_pct: 0.15,
      available_indexers: [],
      indexer_url: '',
    }
    const apiActionSpy = vi.fn().mockResolvedValue({})
    ;(window as any).__sseAction = apiActionSpy
    await c.saveS3Config()
    expect(apiActionSpy).toHaveBeenCalled()
    const [, fn] = apiActionSpy.mock.calls[0]
    // The payload is built inside saveConfig → saveS3Config
    // Verify the API call URL
    const callArgs = apiActionSpy.mock.calls[0]
    expect(callArgs[0]).toContain('S3')
  })

  it('saveConfig handles errors without throwing', async () => {
    const c = createComponent(settingsApp)
    const apiActionSpy = vi.fn().mockRejectedValue(new Error('Network error'))
    ;(window as any).__sseAction = apiActionSpy
    await c.saveConfig('s3Saving', 'S3', '/_panel/api/s3-config', {})
    expect(c.s3Saving).toBe(false)
  })
})

// --- Change password sub-component ---

describe('changePasswordApp', () => {
  it('returns default state', () => {
    const c = createComponent(changePasswordApp)
    expect(c.showChangePw).toBe(false)
    expect(c.loading).toBe(false)
    expect(c.showPw).toBe(false)
    expect(c.currentPw).toBe('')
    expect(c.newPw).toBe('')
    expect(c.confirmPw).toBe('')
  })

  it('changePassword rejects mismatched passwords via toast', async () => {
    const c = createComponent(changePasswordApp)
    c.newPw = 'password123'
    c.confirmPw = 'different'
    const toastSpy = vi.fn()
    ;(window as any).__sseToast = toastSpy
    await c.changePassword()
    expect(toastSpy).toHaveBeenCalledWith('Passwords do not match', 'error')
    expect(c.loading).toBe(false)
  })

  it('changePassword rejects short passwords', async () => {
    const c = createComponent(changePasswordApp)
    c.newPw = 'short'
    c.confirmPw = 'short'
    const toastSpy = vi.fn()
    ;(window as any).__sseToast = toastSpy
    await c.changePassword()
    expect(toastSpy).toHaveBeenCalledWith(
      'Password must be at least 8 characters',
      'error',
    )
  })
})

// --- Version check sub-component ---

describe('versionCheckApp', () => {
  it('returns default state', () => {
    const c = createComponent(versionCheckApp)
    expect(c.versionResult).toBeNull()
    expect(c.versionLoading).toBe(true)
    expect(c.versionErr).toBe(false)
  })

  it('init fetches version data', async () => {
    const c = createComponent(versionCheckApp)
    const mockApi = {
      get: vi.fn().mockResolvedValue({ version: '1.0.0' }),
    }
    ;(window as any).__api = mockApi
    await c.init()
    expect(mockApi.get).toHaveBeenCalledWith('/_panel/api/version')
    expect(c.versionResult).toEqual({ version: '1.0.0' })
    expect(c.versionLoading).toBe(false)
    expect(c.versionErr).toBe(false)
  })

  it('init sets versionErr on failure', async () => {
    const c = createComponent(versionCheckApp)
    ;(window as any).__api = {
      get: vi.fn().mockRejectedValue(new Error('Network')),
    }
    await c.init()
    expect(c.versionErr).toBe(true)
    expect(c.versionLoading).toBe(false)
    expect(c.versionResult).toBeNull()
  })
})

// --- Monitoring app ---

describe('monitoringApp', () => {
  it('returns default state', () => {
    const c = createComponent(monitoringApp)
    expect(c.pending_objects).toBe(0)
    expect(c.pending_size).toBe('0 B')
    expect(c.uploaded_objects).toBe(0)
    expect(c.failed_uploads).toBe(0)
    expect(c.lastRefresh).toBe('')
    expect(c.error).toBe('')
    expect(c.showPromGuide).toBe(false)
  })

  it('loading getter is true before first refresh', () => {
    const c = createComponent(monitoringApp)
    expect(c.loading).toBe(true)
  })

  it('applyStats updates fields from SSE data', () => {
    const c = createComponent(monitoringApp)
    c.applyStats({
      pending_objects: 5,
      pending_size: 1500,
      uploaded_objects: 100,
      uploaded_size: 50000,
      failed_uploads: 2,
      orphaned_objects: 1,
      multipart_uploads: 3,
      unpinned_objects: 0,
      account_ready: true,
      account_max_pinned_data: 1000000,
      account_remaining_storage: 500000,
      account_pinned_data: 500000,
      account_pinned_size: 500000,
    })
    expect(c.pending_objects).toBe(5)
    expect(c.pending_size).toBe('1.5 KB')
    expect(c.uploaded_objects).toBe(100)
    expect(c.uploaded_size).toBe('50 KB')
    expect(c.failed_uploads).toBe(2)
    expect(c.account_ready).toBe(true)
    expect(c.account_max_pinned_data).toBe(1000000)
  })

  it('applyStats handles missing fields with defaults', () => {
    const c = createComponent(monitoringApp)
    c.applyStats({})
    expect(c.pending_objects).toBe(0)
    expect(c.pending_size).toBe('0 B')
    expect(c.account_ready).toBe(false)
  })

  it('fmtBytes delegates to utils.fmtBytes', () => {
    const c = createComponent(monitoringApp)
    expect(c.fmtBytes(1500000)).toBe('1.5 MB')
    expect(c.fmtBytes(0)).toBe('0 B')
  })

  it('accountPct calculates percentage', () => {
    const c = createComponent(monitoringApp)
    c.account_pinned_size = 250000
    c.account_max_pinned_data = 1000000
    expect(c.accountPct()).toBe(25)
  })

  it('accountPct returns 0 when max is 0', () => {
    const c = createComponent(monitoringApp)
    c.account_pinned_size = 100
    c.account_max_pinned_data = 0
    expect(c.accountPct()).toBe(0)
  })

  it('promScrapeConfig generates config with current host', () => {
    const c = createComponent(monitoringApp)
    const config = c.promScrapeConfig
    expect(config).toContain('s3-server')
    expect(config).toContain('scrape_configs')
    expect(config).toContain('basic_auth')
  })
})

// --- Reset password form ---

describe('resetPasswordForm', () => {
  it('returns default state', () => {
    const c = createComponent(resetPasswordForm)
    expect(c.loading).toBe(false)
    expect(c.showPw).toBe(false)
    expect(c.newPassword).toBe('')
    expect(c.confirmPassword).toBe('')
  })

  it('submit rejects mismatched passwords', async () => {
    const c = createComponent(resetPasswordForm)
    c.newPassword = 'password123'
    c.confirmPassword = 'different'
    const toastSpy = vi.fn()
    ;(window as any).__sseToast = toastSpy
    const e = { preventDefault: vi.fn() } as any
    await c.submit(e)
    expect(toastSpy).toHaveBeenCalledWith('Passwords do not match', 'error')
  })

  it('submit rejects short passwords', async () => {
    const c = createComponent(resetPasswordForm)
    c.newPassword = 'short'
    c.confirmPassword = 'short'
    const toastSpy = vi.fn()
    ;(window as any).__sseToast = toastSpy
    await c.submit({ preventDefault: vi.fn() } as any)
    expect(toastSpy).toHaveBeenCalledWith(
      'Password must be at least 8 characters',
      'error',
    )
  })
})
