import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

import { keysApp } from '../src/pages/keys'
import { usersApp } from '../src/pages/users'
import { backupsApp } from '../src/pages/backups'
import { dashboardApp } from '../src/pages/dashboard'
import { S3Status } from '../src/pages/status'

// --- Helper: create an Alpine-like component proxy ---

function createComponent(factory: (...args: any[]) => any, ...args: any[]): any {
  const obj = factory.call({} as any, ...args)
  const component = Object.create(obj)
  component.$watch = vi.fn()
  component.$el = document.createElement('div')
  component.$dispatch = vi.fn()
  component.$refs = {}
  component.$nextTick = vi.fn((cb: () => void) => Promise.resolve().then(cb))
  return component
}

// --- Stubs for window globals ---

function stubApi(overrides: Partial<ApiClient> = {}): ApiClient {
  return {
    get: vi.fn().mockResolvedValue({}),
    post: vi.fn().mockResolvedValue({}),
    put: vi.fn().mockResolvedValue({}),
    del: vi.fn().mockResolvedValue({}),
    ...overrides,
  }
}

interface ApiClient {
  get: (url: string) => Promise<any>
  post: (url: string, json?: any) => Promise<any>
  put: (url: string, json?: any) => Promise<any>
  del: (url: string) => Promise<any>
}



let reloadSpy: ReturnType<typeof vi.spyOn>

function stubGlobals(overrides: Record<string, any> = {}) {
  const apiClient = overrides.__api ?? stubApi()
  window.__api = apiClient
  window.__apiReady = Promise.resolve()
  window.__sseReady = Promise.resolve()
  window.__sseToast = overrides.__sseToast ?? vi.fn()
  window.__sseProcessing = overrides.__sseProcessing ?? vi.fn()
  window.__sseAction = overrides.__sseAction ?? vi.fn(async (msg: string, fn: () => Promise<any>) => fn())
  reloadSpy = vi.spyOn(window, 'setTimeout')
  return { apiClient }
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  reloadSpy?.mockRestore()
})

// ===================== keysApp =====================

describe('keysApp', () => {
  it('returns correct initial state', () => {
    const c = createComponent(keysApp, { backendRunning: true })
    expect(c.showDelete).toBe(false)
    expect(c.deleteAccessKey).toBe('')
    expect(c.generating).toEqual({})
    expect(c.backendRunning).toBe(true)
  })

  it('returns default backendRunning=false when no props', () => {
    const c = createComponent(keysApp)
    expect(c.backendRunning).toBe(false)
  })

  it('generateKey calls api.post with user_name', async () => {
    const { apiClient } = stubGlobals()
    const c = createComponent(keysApp)

    await c.generateKey('alice')

    expect(apiClient.post).toHaveBeenCalledWith('/_panel/api/keys', { user_name: 'alice' })
    expect(reloadSpy).toHaveBeenCalled()
  })

  it('generateKey sets generating flag during request', async () => {
    let resolvePost: () => void
    stubGlobals({
      __api: {
        ...stubApi(),
        post: vi.fn(() => new Promise(r => { resolvePost = () => r({}) })),
      },
      __sseAction: vi.fn(async (_msg: string, fn: () => Promise<any>) => fn()),
    })
    const c = createComponent(keysApp)

    const promise = c.generateKey('bob')
    expect(c.generating['bob']).toBe(true)
    resolvePost!()
    await promise
  })

  it('generateKey clears generating flag on error', async () => {
    stubGlobals({
      __sseAction: vi.fn(async () => { throw new Error('fail') }),
    })
    const c = createComponent(keysApp)

    await c.generateKey('charlie')
    expect(c.generating['charlie']).toBe(false)
  })

  it('confirmDelete calls api.del with encoded access key', async () => {
    const { apiClient } = stubGlobals()
    const c = createComponent(keysApp)
    c.deleteAccessKey = 'AKIA123/abc'

    await c.confirmDelete()

    expect(apiClient.del).toHaveBeenCalledWith('/_panel/api/keys/AKIA123%2Fabc')
    expect(reloadSpy).toHaveBeenCalled()
  })
})

// ===================== usersApp =====================

describe('usersApp', () => {
  it('returns correct initial state', () => {
    const c = createComponent(usersApp)
    expect(c.showCreate).toBe(false)
    expect(c.createName).toBe('')
    expect(c.createErr).toBe('')
    expect(c.creating).toBe(false)
    expect(c.showDelete).toBe(false)
    expect(c.deleteName).toBe('')
  })

  it('createUser calls api.post with name', async () => {
    const { apiClient } = stubGlobals()
    const c = createComponent(usersApp)
    c.createName = 'alice'

    await c.createUser()

    expect(apiClient.post).toHaveBeenCalledWith('/_panel/api/users', { name: 'alice' })
    expect(c.showCreate).toBe(false)
    expect(reloadSpy).toHaveBeenCalled()
  })

  it('createUser sets createErr on failure', async () => {
    stubGlobals({
      __sseAction: vi.fn(async () => { throw new Error('User exists') }),
    })
    const c = createComponent(usersApp)
    c.createName = 'alice'
    c.showCreate = true

    await c.createUser()

    expect(c.createErr).toBe('User exists')
    expect(c.creating).toBe(false)
    expect(c.showCreate).toBe(true) // stays open on error
  })

  it('confirmDelete calls api.del with encoded name', async () => {
    const { apiClient } = stubGlobals()
    const c = createComponent(usersApp)
    c.deleteName = 'alice/test'

    await c.confirmDelete()

    expect(apiClient.del).toHaveBeenCalledWith('/_panel/api/users/alice%2Ftest')
    expect(reloadSpy).toHaveBeenCalled()
  })
})

// ===================== backupsApp =====================

describe('backupsApp', () => {
  it('returns correct initial state', () => {
    const c = createComponent(backupsApp)
    expect(c.backing).toBe(false)
    expect(c.deleting).toBe('')
    expect(c.backupErr).toBe('')
  })

  it('backupNow calls api.post', async () => {
    const { apiClient } = stubGlobals()
    const c = createComponent(backupsApp)

    await c.backupNow()

    expect(apiClient.post).toHaveBeenCalledWith('/_panel/api/backups')
    expect(c.backing).toBe(false)
    expect(reloadSpy).toHaveBeenCalled()
  })

  it('backupNow sets backupErr on failure', async () => {
    stubGlobals({
      __api: {
        ...stubApi(),
        post: vi.fn().mockRejectedValue(new Error('disk full')),
      },
    })
    const c = createComponent(backupsApp)

    await c.backupNow()

    expect(c.backupErr).toBe('disk full')
    expect(c.backing).toBe(false)
  })

  it('deleteBackup calls api.del with filename', async () => {
    const { apiClient } = stubGlobals()
    const c = createComponent(backupsApp)

    await c.deleteBackup('backup-2026-01.tar.gz')

    expect(apiClient.del).toHaveBeenCalledWith('/_panel/api/backups/backup-2026-01.tar.gz')
    expect(c.deleting).toBe('')
    expect(window.__sseToast).toHaveBeenCalledWith('Backup deleted', 'success')
  })

  it('deleteBackup shows error toast on failure', async () => {
    stubGlobals({
      __api: {
        ...stubApi(),
        del: vi.fn().mockRejectedValue(new Error('not found')),
      },
    })
    const c = createComponent(backupsApp)

    await c.deleteBackup('missing.tar.gz')

    expect(window.__sseToast).toHaveBeenCalledWith('not found', 'error')
    expect(c.deleting).toBe('')
  })
})

// ===================== dashboardApp =====================

describe('dashboardApp', () => {
  it('returns correct initial state', () => {
    const c = createComponent(dashboardApp)
    expect(c.s3Status).toBe(S3Status.Stopped)
    expect(c.initError).toBe('')
    expect(c.isRunning).toBe(false)
    expect(c.keyCount).toBe(0)
    expect(c.uptime).toBe('0s')
    expect(c.version).toBe('')
    expect(c.updateAvailable).toBe(false)
    expect(c.latestVersion).toBe('')
    expect(c.generating).toBe(false)
  })

  it('isRunning returns true when s3Status is running', () => {
    const c = createComponent(dashboardApp)
    c.s3Status = S3Status.Running
    expect(c.isRunning).toBe(true)
  })

  it('init fetches status and version data', async () => {
    const { apiClient } = stubGlobals({
      __api: {
        get: vi.fn().mockImplementation((url: string) => {
          if (url === '/_panel/api/status') {
            return Promise.resolve({
              s3_status: 'running',
              init_error: '',
              key_count: 5,
              version: 'v1.0.0',
            })
          }
          if (url === '/_panel/api/version') {
            return Promise.resolve({
              update_available: true,
              latest_version: 'v1.1.0',
            })
          }
          return Promise.resolve({})
        }),
        post: vi.fn(),
        put: vi.fn(),
        del: vi.fn(),
      },
    })
    const addEventListenerSpy = vi.spyOn(window, 'addEventListener')

    const c = createComponent(dashboardApp)
    await c.init()

    expect(apiClient.get).toHaveBeenCalledWith('/_panel/api/status')
    expect(apiClient.get).toHaveBeenCalledWith('/_panel/api/version')
    expect(c.s3Status).toBe('running')
    expect(c.keyCount).toBe(5)
    expect(c.version).toBe('v1.0.0')
    expect(c.updateAvailable).toBe(true)
    expect(c.latestVersion).toBe('v1.1.0')
    expect(addEventListenerSpy).toHaveBeenCalledWith('sse:dashboard', expect.any(Function))
  })

  it('sse:dashboard event updates state', async () => {
    stubGlobals({
      __api: {
        get: vi.fn().mockResolvedValue({}),
        post: vi.fn(),
        put: vi.fn(),
        del: vi.fn(),
      },
    })

    const c = createComponent(dashboardApp)
    await c.init()

    // Simulate SSE event
    const event = new CustomEvent('sse:dashboard', {
      detail: {
        s3_status: 'error',
        init_error: 'failed',
        key_count: 10,
        uptime: '5m',
        version: 'v2.0.0',
      },
    })
    window.dispatchEvent(event)

    expect(c.s3Status).toBe('error')
    expect(c.initError).toBe('failed')
    expect(c.keyCount).toBe(10)
    expect(c.uptime).toBe('5m')
    expect(c.version).toBe('v2.0.0')
  })

  it('generateKey calls api.post with default user', async () => {
    const { apiClient } = stubGlobals()
    const c = createComponent(dashboardApp)

    await c.generateKey()

    expect(apiClient.post).toHaveBeenCalledWith('/_panel/api/keys', { user_name: 'default' })
    // generating stays true on success (page reloads via reloadAfter)
    expect(reloadSpy).toHaveBeenCalled()
  })

  it('generateKey clears generating flag on error', async () => {
    stubGlobals({
      __sseAction: vi.fn(async () => { throw new Error('fail') }),
    })
    const c = createComponent(dashboardApp)

    await c.generateKey()

    expect(c.generating).toBe(false)
  })

  it('has showDelete and deleteAccessKey initial state', () => {
    const c = createComponent(dashboardApp)
    expect(c.showDelete).toBe(false)
    expect(c.deleteAccessKey).toBe('')
  })

  it('confirmDelete calls api.del with encoded access key', async () => {
    const { apiClient } = stubGlobals()
    const c = createComponent(dashboardApp)
    c.deleteAccessKey = 'AKIA123/abc'

    await c.confirmDelete()

    expect(apiClient.del).toHaveBeenCalledWith('/_panel/api/keys/AKIA123%2Fabc')
    expect(reloadSpy).toHaveBeenCalled()
  })

  it('confirmDelete handles errors without throwing', async () => {
    stubGlobals({
      __sseAction: vi.fn(async () => { throw new Error('Network error') }),
    })
    const c = createComponent(dashboardApp)
    c.deleteAccessKey = 'AKIA456'

    await c.confirmDelete()
    // should not throw
  })
})
