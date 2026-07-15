import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// Mock ky so we can control what handleReq sees without real HTTP.
// Must use vi.hoisted because vi.mock is hoisted above const declarations.
// The ky instance is both callable (for GET) and has method properties.
const mockKyInstance = vi.hoisted(() => {
  const fn = vi.fn() as any
  fn.get = vi.fn()
  fn.post = vi.fn()
  fn.put = vi.fn()
  fn.delete = vi.fn()
  return fn
})

vi.mock('ky', () => ({
  default: {
    create: vi.fn(() => mockKyInstance),
  },
}))

// Import after mock is set up — api.ts calls ky.create() at module level
import { ERROR_MESSAGES, DIALOG_CODES, DIALOG_TITLES } from '../src/api'

// Re-import the module side-effect to reset window.__api (set once at module level)
// and reset mocks before each test
beforeEach(async () => {
  vi.clearAllMocks()
  vi.resetModules()
  // Re-import api.ts so it re-assigns window.__api via ky.create()
  await import('../src/api')
  window.__sseToast = vi.fn()
})

afterEach(() => {
  vi.restoreAllMocks()
})

// Helper: create a Response-like object that handleReq can process.
// ky wraps fetch responses — res.ok, res.status, res.json()
function mockResponse(status: number, body?: any) {
  const res: any = {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  }
  return Promise.resolve(res)
}

// ky throws an HTTPError on non-2xx with .data (pre-parsed body) and .message
function mockHttpError(status: number, body: any, message?: string): Error {
  const err: any = new Error(message || body.message || body.error || 'Request failed')
  err.data = body
  return err
}

// Network error (fetch throws, no response object, no .data)
function mockNetworkError(message: string): Error {
  return new Error(message)
}

describe('handleReq — success', () => {
  it('parses JSON body on 200 response', async () => {
    mockKyInstance.mockReturnValue(mockResponse(200, { status: 'running' }))
    const result = await (window as any).__api.get('/_panel/api/status')
    expect(result).toEqual({ status: 'running' })
  })

  it('returns undefined for 204 No Content without parsing body', async () => {
    mockKyInstance.post.mockReturnValue(mockResponse(204))
    const result = await (window as any).__api.post('/_panel/api/keys', { user_name: 'admin' })
    expect(result).toBeUndefined()
  })

  it('passes through body for 201 Created', async () => {
    mockKyInstance.post.mockReturnValue(mockResponse(201, { access_key: 'AKIA123' }))
    const result = await (window as any).__api.post('/_panel/api/keys')
    expect(result).toEqual({ access_key: 'AKIA123' })
  })
})

describe('handleReq — error mapping', () => {
  it('shows toast with ERROR_MESSAGES mapping for known type (non-CONFLICT/VALIDATION)', async () => {
    mockKyInstance.post.mockRejectedValue(mockHttpError(500, {
      code: 'SOME_CODE',
      type: 'PASSWORD_TOO_SHORT',
      message: 'password too short',
    }))

    await (window as any).__api.post('/_panel/api/onboarding/admin-password')

    expect(window.__sseToast).toHaveBeenCalledWith(
      ERROR_MESSAGES['PASSWORD_TOO_SHORT'],
      'error',
    )
  })

  it('uses raw message for CONFLICT errors (server provides user-friendly msg)', async () => {
    const err = mockHttpError(409, {
      code: 'CONFLICT',
      type: 'BUCKET_EXISTS',
      message: 'A bucket with this name already exists',
    })
    mockKyInstance.post.mockRejectedValue(err)

    await (window as any).__api.post('/_panel/api/buckets')

    expect(window.__sseToast).toHaveBeenCalledWith(
      'A bucket with this name already exists',
      'error',
    )
  })

  it('uses raw message for VALIDATION_ERROR code (server provides msg)', async () => {
    const err = mockHttpError(400, {
      code: 'VALIDATION_ERROR',
      type: 'CUSTOM_FIELD',
      message: 'The indexer URL is invalid',
    })
    mockKyInstance.put.mockRejectedValue(err)

    await (window as any).__api.put('/_panel/api/s3-config')

    expect(window.__sseToast).toHaveBeenCalledWith(
      'The indexer URL is invalid',
      'error',
    )
  })

  it('falls back to raw message when type is not in ERROR_MESSAGES', async () => {
    const err = mockHttpError(500, {
      code: 'SOME_CODE',
      type: 'UNKNOWN_TYPE',
      message: 'Something went wrong',
    })
    mockKyInstance.mockRejectedValue(err)

    await (window as any).__api.get('/_panel/api/status')

    expect(window.__sseToast).toHaveBeenCalledWith('Something went wrong', 'error')
  })

  it('falls back to body.error when message is missing', async () => {
    const err = mockHttpError(500, {
      code: '',
      type: '',
      error: 'Internal failure',
    })
    mockKyInstance.delete.mockRejectedValue(err)

    await (window as any).__api.del('/_panel/api/keys/AKIA123')

    expect(window.__sseToast).toHaveBeenCalledWith('Internal failure', 'error')
  })

  it('falls back to e.message when body is empty', async () => {
    const err = mockNetworkError('TypeError: Failed to fetch')
    mockKyInstance.mockRejectedValue(err)

    await (window as any).__api.get('/_panel/api/status')

    expect(window.__sseToast).toHaveBeenCalledWith('TypeError: Failed to fetch', 'error')
  })

  it('falls back to "Request failed" when no message available', async () => {
    // No .data, no .message
    const err: any = new Error()
    err.message = ''
    mockKyInstance.mockRejectedValue(err)

    await (window as any).__api.get('/_panel/api/status')

    expect(window.__sseToast).toHaveBeenCalledWith('Request failed', 'error')
  })
})

describe('handleReq — dialog codes (INTERNAL_ERROR, NOT_READY)', () => {
  it('dispatches __api:error event for INTERNAL_ERROR instead of toast', async () => {
    const err = mockHttpError(500, {
      code: 'INTERNAL_ERROR',
      type: 'DATABASE_OPEN_FAILED',
      message: 'database is locked',
    })
    mockKyInstance.mockRejectedValue(err)

    const handler = vi.fn()
    window.addEventListener('__api:error', handler)

    await (window as any).__api.get('/_panel/api/status')

    expect(window.__sseToast).not.toHaveBeenCalled()
    expect(handler).toHaveBeenCalledTimes(1)
    const detail = (handler.mock.calls[0][0] as CustomEvent).detail
    expect(detail.code).toBe('INTERNAL_ERROR')
    expect(detail.type).toBe('DATABASE_OPEN_FAILED')
    expect(detail.message).toBe(ERROR_MESSAGES['DATABASE_OPEN_FAILED'])
    expect(detail.raw).toBe('database is locked')
  })

  it('dispatches __api:error event for NOT_READY', async () => {
    const err = mockHttpError(503, {
      code: 'NOT_READY',
      type: 'BACKEND_NOT_INITIALIZED',
      message: 'backend is not running',
    })
    mockKyInstance.post.mockRejectedValue(err)

    const handler = vi.fn()
    window.addEventListener('__api:error', handler)

    await (window as any).__api.post('/_panel/api/system/restart')

    expect(window.__sseToast).not.toHaveBeenCalled()
    expect(handler).toHaveBeenCalledTimes(1)
    const detail = (handler.mock.calls[0][0] as CustomEvent).detail
    expect(detail.code).toBe('NOT_READY')
    expect(detail.message).toBe(ERROR_MESSAGES['BACKEND_NOT_INITIALIZED'])
  })
})

describe('handleReq — CSRF', () => {
  it('window.__api methods are defined after module import', () => {
    expect((window as any).__api.get).toBeTypeOf('function')
    expect((window as any).__api.post).toBeTypeOf('function')
    expect((window as any).__api.put).toBeTypeOf('function')
    expect((window as any).__api.del).toBeTypeOf('function')
  })
})

describe('api.ts — exports', () => {
  it('ERROR_MESSAGES contains expected mappings', () => {
    expect(ERROR_MESSAGES['PASSWORD_TOO_SHORT']).toContain('8 characters')
    expect(ERROR_MESSAGES['BACKEND_NOT_INITIALIZED']).toContain('not running')
    expect(ERROR_MESSAGES['AUTH_REQUIRED']).toContain('log in')
  })

  it('DIALOG_CODES contains INTERNAL_ERROR and NOT_READY', () => {
    expect(DIALOG_CODES.has('INTERNAL_ERROR')).toBe(true)
    expect(DIALOG_CODES.has('NOT_READY')).toBe(true)
    expect(DIALOG_CODES.has('CONFLICT')).toBe(false)
  })

  it('DIALOG_TITLES maps codes to display titles', () => {
    expect(DIALOG_TITLES.INTERNAL_ERROR).toBe('Server Error')
    expect(DIALOG_TITLES.NOT_READY).toBe('Service Unavailable')
  })
})
