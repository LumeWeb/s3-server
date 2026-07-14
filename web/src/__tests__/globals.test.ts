import { describe, it, expect, vi, beforeEach } from 'vitest'

// Test the globals.ts typed accessors.
// We can't import globals.ts directly because it calls window getters that
// reference runtime singletons. Instead, we test the behavior the accessors
// provide: that calling api(), toast(), etc. delegates to the window globals.

describe('globals accessors', () => {
  beforeEach(() => {
    // Reset window globals
    window.__apiReady = Promise.resolve()
    window.__sseToast = vi.fn()
    window.__sseAction = vi.fn()
    window.__siaReady = Promise.resolve()
    window.__siaSdk = {
      Builder: vi.fn(),
      generateRecoveryPhrase: vi.fn(() => 'test phrase'),
      validateRecoveryPhrase: vi.fn(),
      AppKey: vi.fn(),
    }
  })

  describe('api()', () => {
    it('returns the __api object from window', async () => {
      const mockApi = {
        get: vi.fn().mockResolvedValue({ status: 'ok' }),
        post: vi.fn(),
        put: vi.fn(),
        del: vi.fn(),
      }
      window.__api = mockApi

      // Dynamically import globals to get fresh references
      const { api } = await import('../globals')
      const result = await api()!.get('/test')
      expect(result).toEqual({ status: 'ok' })
      expect(mockApi.get).toHaveBeenCalledWith('/test')
    })
  })

  describe('apiReady()', () => {
    it('returns the __apiReady promise', async () => {
      const { apiReady } = await import('../globals')
      await expect(apiReady()).resolves.toBeUndefined()
    })
  })

  describe('toast()', () => {
    it('calls __sseToast on window', async () => {
      const { toast } = await import('../globals')
      toast('test message', 'success')
      expect(window.__sseToast).toHaveBeenCalledWith('test message', 'success')
    })
  })

  describe('siaReady()', () => {
    it('returns the __siaReady promise', async () => {
      const { siaReady } = await import('../globals')
      await expect(siaReady()).resolves.toBeUndefined()
    })
  })

  describe('siaSdk()', () => {
    it('returns the __siaSdk object from window', async () => {
      const { siaSdk } = await import('../globals')
      const sdk = siaSdk()
      expect(sdk).toBe(window.__siaSdk)
      expect(sdk.generateRecoveryPhrase).toBeDefined()
    })
  })

  describe('apiAction()', () => {
    it('calls __sseAction with processing message, fn, and success message', async () => {
      const { apiAction } = await import('../globals')
      const fn = async () => 'result'
      apiAction('Processing…', fn, 'Done')
      expect(window.__sseAction).toHaveBeenCalledWith('Processing…', fn, 'Done')
    })
  })
})
