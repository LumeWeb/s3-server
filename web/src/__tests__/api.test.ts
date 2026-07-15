import { describe, it, expect, vi, beforeEach } from 'vitest'
import { handleReq, ERROR_MESSAGES, DIALOG_CODES, csrfToken, withCsrf } from '../api'

describe('ERROR_MESSAGES', () => {
  it('contains human-readable messages for known error types', () => {
    expect(ERROR_MESSAGES['BACKEND_NOT_INITIALIZED']).toContain('S3 backend')
    expect(ERROR_MESSAGES['PASSWORD_TOO_SHORT']).toContain('8 characters')
    expect(ERROR_MESSAGES['AUTH_REQUIRED']).toContain('Authentication')
  })

  it('has no empty values', () => {
    for (const [key, val] of Object.entries(ERROR_MESSAGES)) {
      expect(val, `ERROR_MESSAGES['${key}'] should not be empty`).toBeTruthy()
    }
  })
})

describe('DIALOG_CODES', () => {
  it('includes INTERNAL_ERROR and NOT_READY', () => {
    expect(DIALOG_CODES.has('INTERNAL_ERROR')).toBe(true)
    expect(DIALOG_CODES.has('NOT_READY')).toBe(true)
  })

  it('does not include validation errors', () => {
    expect(DIALOG_CODES.has('NAME_REQUIRED')).toBe(false)
    expect(DIALOG_CODES.has('PASSWORD_TOO_SHORT')).toBe(false)
  })
})

describe('csrfToken', () => {
  it('returns empty string when no cookie is set', () => {
    document.cookie = ''
    expect(csrfToken()).toBe('')
  })

  it('extracts token from _csrf cookie', () => {
    // happy-dom: set cookie and read it back
    document.cookie = '_csrf=test-token-123; path=/'
    expect(csrfToken()).toBe('test-token-123')
  })
})

describe('withCsrf', () => {
  beforeEach(() => {
    // Clear all cookies
    document.cookie.split(';').forEach(c => {
      const name = c.split('=')[0].trim()
      if (name) document.cookie = `${name}=; expires=Thu, 01 Jan 1970 00:00:00 GMT`
    })
  })

  it('returns empty opts when no CSRF cookie exists', () => {
    // Clear _csrf cookie by setting it to expire in the past
    document.cookie = '_csrf=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/'
    const result = withCsrf()
    expect(result).toEqual({})
  })

  it('adds X-CSRF-Token header when cookie exists', () => {
    document.cookie = '_csrf=abc123; path=/'
    const result = withCsrf()
    expect(result.headers).toEqual({ 'X-CSRF-Token': 'abc123' })
  })

  it('preserves existing headers', () => {
    document.cookie = '_csrf=abc123; path=/'
    const result = withCsrf({ headers: { 'X-Custom': 'yes' } })
    expect(result.headers).toEqual({ 'X-Custom': 'yes', 'X-CSRF-Token': 'abc123' })
  })
})

describe('handleReq', () => {
  it('returns resolved value on success', async () => {
    const mockRes = { status: 200, json: () => Promise.resolve({ data: 'ok' }) }
    const result = await handleReq(Promise.resolve(mockRes as any))
    expect(result).toEqual({ data: 'ok' })
  })

  it('returns undefined for 204 No Content without parsing JSON', async () => {
    const jsonSpy = vi.fn(() => Promise.resolve({}))
    const mockRes = { status: 204, json: jsonSpy }
    const result = await handleReq(Promise.resolve(mockRes as any))
    expect(result).toBeUndefined()
    expect(jsonSpy).not.toHaveBeenCalled()
  })

  it('returns undefined for 204 even when response has empty body', async () => {
    // Regression: flush with no active buckets returns 204 No Content.
    // Previously, calling .json() on the empty body threw
    // "Unexpected end of JSON input".
    const jsonSpy = vi.fn(() => {
      throw new SyntaxError('Unexpected end of JSON input')
    })
    const mockRes = { status: 204, json: jsonSpy }
    const result = await handleReq(Promise.resolve(mockRes as any))
    expect(result).toBeUndefined()
    expect(jsonSpy).not.toHaveBeenCalled()
  })

  it('returns undefined on generic error (without ky HTTPError shape)', async () => {
    const result = await handleReq(Promise.reject(new Error('network failed')))
    expect(result).toBeUndefined()
  })

  it('shows toast with human-readable message for known error type', async () => {
    const toastSpy = vi.fn()
    window.__sseToast = toastSpy

    const error = {
      message: 'raw msg',
      data: { type: 'BACKEND_NOT_INITIALIZED', code: 'SOME_CODE' },
    }
    await handleReq(Promise.reject(error))

    expect(toastSpy).toHaveBeenCalledWith(
      ERROR_MESSAGES['BACKEND_NOT_INITIALIZED'],
      'error',
    )
  })

  it('falls back to raw message for unknown error types', async () => {
    const toastSpy = vi.fn()
    window.__sseToast = toastSpy

    const error = {
      message: 'unknown failure',
      data: { type: 'UNKNOWN_TYPE_XYZ', code: 'SOME_CODE' },
    }
    await handleReq(Promise.reject(error))

    expect(toastSpy).toHaveBeenCalledWith('unknown failure', 'error')
  })

  it('dispatches __api:error event for DIALOG_CODES (INTERNAL_ERROR)', async () => {
    const eventSpy = vi.fn()
    window.addEventListener('__api:error', eventSpy)

    const error = {
      message: 'internal failure',
      data: { type: 'SOME_TYPE', code: 'INTERNAL_ERROR' },
    }
    await handleReq(Promise.reject(error))

    expect(eventSpy).toHaveBeenCalledTimes(1)
    const detail = (eventSpy.mock.calls[0][0] as CustomEvent).detail
    expect(detail.code).toBe('INTERNAL_ERROR')
    expect(detail.type).toBe('SOME_TYPE')

    window.removeEventListener('__api:error', eventSpy)
  })

  it('dispatches __api:error event for NOT_READY', async () => {
    const eventSpy = vi.fn()
    window.addEventListener('__api:error', eventSpy)

    const error = {
      message: 'not ready',
      data: { type: 'BACKEND_NOT_INITIALIZED', code: 'NOT_READY' },
    }
    await handleReq(Promise.reject(error))

    expect(eventSpy).toHaveBeenCalledTimes(1)
    const detail = (eventSpy.mock.calls[0][0] as CustomEvent).detail
    expect(detail.code).toBe('NOT_READY')
    expect(detail.message).toBe(ERROR_MESSAGES['BACKEND_NOT_INITIALIZED'])

    window.removeEventListener('__api:error', eventSpy)
  })

  it('does not show toast when error triggers dialog (INTERNAL_ERROR)', async () => {
    const toastSpy = vi.fn()
    window.__sseToast = toastSpy

    const error = {
      message: 'internal',
      data: { type: 'SOME_TYPE', code: 'INTERNAL_ERROR' },
    }
    await handleReq(Promise.reject(error))

    expect(toastSpy).not.toHaveBeenCalled()
  })
})
