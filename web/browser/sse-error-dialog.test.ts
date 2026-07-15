import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { initToast, initSSE } from '../src/sse'
import { DIALOG_TITLES } from '../src/api'
import { initErrorDialog } from '../src/error-dialog'

// Minimal Alpine mock: $data returns a reactive-ish object
function mockAlpine(data: Record<string, any> = {}) {
  const store = { ...data }
  return {
    $data: () => store,
  }
}

describe('sse.ts — initToast', () => {
  let toastEl: HTMLElement
  let alpine: any

  beforeEach(() => {
    // Clean up any window globals from previous tests
    delete (window as any).__sseToast
    delete (window as any).__sseProcessing
    delete (window as any).__sseAction
    delete (window as any).__reloadAfter
    delete (window as any).__copyToClipboard
    delete (window as any).__dialogHandlers
    delete (window as any).__onDialogConfirm
    toastEl = document.createElement('div')
    toastEl.id = 'sse-toast'
    document.body.appendChild(toastEl)
    alpine = mockAlpine({ processing: false, msg: '', type: '', show: false })
  })

  afterEach(() => {
    // Clean up window globals set by initToast
    delete (window as any).__sseToast
    delete (window as any).__sseProcessing
    delete (window as any).__sseAction
    delete (window as any).__reloadAfter
    delete (window as any).__copyToClipboard
    delete (window as any).__dialogHandlers
    delete (window as any).__onDialogConfirm
  })

  it('returns early if toast element does not exist', () => {
    toastEl.remove()
    initToast({})
    expect(window.__sseToast).toBeUndefined()
  })

  it('sets up __sseToast global', () => {
    initToast(alpine)
    expect(typeof window.__sseToast).toBe('function')
  })

  it('__sseToast sets msg, type, show=true and auto-hides after 5s', async () => {
    initToast(alpine)

    window.__sseToast!('Hello', 'success')

    expect(alpine.$data().processing).toBe(false)
    expect(alpine.$data().msg).toBe('Hello')
    expect(alpine.$data().type).toBe('success')
    expect(alpine.$data().show).toBe(true)

    // Wait for 5s auto-hide (real browser timing)
    await vi.waitFor(() => {
      expect(alpine.$data().show).toBe(false)
    }, { timeout: 6000, interval: 500 })
  })

  it('__sseToast defaults type to "info"', () => {
    initToast(alpine)
    window.__sseToast!('Test')
    expect(alpine.$data().type).toBe('info')
  })

  it('__sseProcessing sets processing=true and shows message', () => {
    initToast(alpine)
    window.__sseProcessing!('Loading…')
    expect(alpine.$data().processing).toBe(true)
    expect(alpine.$data().msg).toBe('Loading…')
    expect(alpine.$data().show).toBe(true)
  })

  it('__sseAction wraps fn with processing + success toast', async () => {
    initToast(alpine)
    const fn = vi.fn().mockResolvedValue({ ok: true })

    const result = await window.__sseAction!('Working…', fn, 'Done!')

    expect(fn).toHaveBeenCalled()
    expect(result).toEqual({ ok: true })
    expect(alpine.$data().processing).toBe(false)
    expect(alpine.$data().msg).toBe('Done!')
    expect(alpine.$data().type).toBe('success')
  })

  it('__sseAction does not show success toast when successMsg is omitted', async () => {
    initToast(alpine)
    const fn = vi.fn().mockResolvedValue({ ok: true })

    await window.__sseAction!('Working…', fn)

    expect(alpine.$data().msg).toBe('Working…') // still processing msg
    expect(alpine.$data().type).toBe('info')
  })

  it('__sseAction shows error toast on fn rejection and rethrows', async () => {
    initToast(alpine)
    const fn = vi.fn().mockRejectedValue(new Error('Boom'))

    await expect(window.__sseAction!('Working…', fn, 'Done')).rejects.toThrow('Boom')

    expect(alpine.$data().msg).toBe('Boom')
    expect(alpine.$data().type).toBe('error')
  })

  it('__sseAction returns undefined result silently (error already handled)', async () => {
    initToast(alpine)
    const fn = vi.fn().mockResolvedValue(undefined)

    const result = await window.__sseAction!('Working…', fn, 'Done')

    expect(result).toBeUndefined()
    // success toast should NOT fire when result is undefined
    expect(alpine.$data().msg).toBe('Working…')
  })

  it('__reloadAfter calls window.location.reload after delay', () => {
    const reloadSpy = vi.fn()
    initToast(alpine)

    expect(typeof window.__reloadAfter).toBe('function')

    // Track the timer
    const timeoutSpy = vi.spyOn(window, 'setTimeout')
    window.__reloadAfter!(100)
    expect(timeoutSpy).toHaveBeenCalledWith(expect.any(Function), 100)
    timeoutSpy.mockRestore()
  })

  it('__reloadAfter uses default RELOAD_DELAY when no arg', () => {
    initToast(alpine)
    const timeoutSpy = vi.spyOn(window, 'setTimeout')
    window.__reloadAfter!()
    expect(timeoutSpy).toHaveBeenCalled()
    const delay = timeoutSpy.mock.calls[0][1]
    expect(delay).toBeGreaterThan(0)
    timeoutSpy.mockRestore()
  })

  it('__copyToClipboard uses navigator.clipboard when available', async () => {
    initToast(alpine)
    const writeTextSpy = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: writeTextSpy },
      configurable: true,
    })
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })

    await window.__copyToClipboard!('test-value', 'Copied!')

    expect(writeTextSpy).toHaveBeenCalledWith('test-value')
    expect(alpine.$data().msg).toBe('Copied!')
    expect(alpine.$data().type).toBe('success')
  })

  it('__copyToClipboard falls back to textarea + execCommand on non-secure context', async () => {
    initToast(alpine)
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true })
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
    const execSpy = vi.fn()
    document.execCommand = execSpy

    await window.__copyToClipboard!('fallback-value')

    expect(execSpy).toHaveBeenCalledWith('copy')
    expect(alpine.$data().msg).toBe('Copied to clipboard')
    expect(alpine.$data().type).toBe('success')
  })

  it('__onDialogConfirm registers handler fired on dialog-confirm event', () => {
    initToast(alpine)
    const handler = vi.fn()
    window.__onDialogConfirm!('delete-key', handler)

    window.dispatchEvent(new CustomEvent('dialog-confirm', { detail: { source: 'delete-key' } }))
    expect(handler).toHaveBeenCalled()
  })

  it('dialog-confirm event with no source does nothing', () => {
    initToast(alpine)
    const handler = vi.fn()
    window.__onDialogConfirm!('some-source', handler)

    window.dispatchEvent(new CustomEvent('dialog-confirm', { detail: {} }))
    expect(handler).not.toHaveBeenCalled()
  })

  it('dialog-confirm re-registration replaces previous handler', () => {
    initToast(alpine)
    const handler1 = vi.fn()
    const handler2 = vi.fn()
    window.__onDialogConfirm!('source', handler1)
    window.__onDialogConfirm!('source', handler2)

    window.dispatchEvent(new CustomEvent('dialog-confirm', { detail: { source: 'source' } }))
    expect(handler1).not.toHaveBeenCalled()
    expect(handler2).toHaveBeenCalled()
  })
})

describe('sse.ts — initSSE', () => {
  let origAddEventListener: typeof window.addEventListener

  beforeEach(() => {
    origAddEventListener = window.addEventListener
  })

  afterEach(() => {
    window.addEventListener = origAddEventListener
    delete (window as any).__sseReady
    delete (window as any).__sseEs
  })

  it('sets __sseReady to a Promise', () => {
    initSSE()
    expect(window.__sseReady).toBeInstanceOf(Promise)
  })

  it('__sseReady resolves when __sse:ready event fires', async () => {
    initSSE()
    const ready = window.__sseReady!
    window.dispatchEvent(new Event('__sse:ready'))
    await expect(ready).resolves.toBeUndefined()
  })
})

describe('error-dialog.ts — initErrorDialog', () => {
  let dialogEl: HTMLElement
  let alpine: any

  beforeEach(() => {
    dialogEl = document.createElement('div')
    dialogEl.id = 'error-dialog'
    document.body.appendChild(dialogEl)
    alpine = mockAlpine({ title: '', message: '', detail: '', show: false })
  })

  afterEach(() => {
  })

  it('returns early if error-dialog element does not exist', () => {
    dialogEl.remove()
    initErrorDialog({})
    // No error thrown — just exits silently
    expect(true).toBe(true)
  })

  it('listens for __api:error and shows dialog with title/message', () => {
    initErrorDialog(alpine)

    window.dispatchEvent(new CustomEvent('__api:error', {
      detail: { code: 'INTERNAL_ERROR', message: 'Something broke', raw: 'stack trace' },
    }))

    expect(alpine.$data().title).toBe(DIALOG_TITLES.INTERNAL_ERROR)
    expect(alpine.$data().message).toBe('Something broke')
    expect(alpine.$data().detail).toBe('stack trace')
    expect(alpine.$data().show).toBe(true)
  })

  it('uses "Error" as default title for unknown codes', () => {
    initErrorDialog(alpine)

    window.dispatchEvent(new CustomEvent('__api:error', {
      detail: { code: 'UNKNOWN_CODE', message: 'Hmm', raw: '' },
    }))

    expect(alpine.$data().title).toBe('Error')
  })

  it('defaults message to "An unexpected error occurred."', () => {
    initErrorDialog(alpine)

    window.dispatchEvent(new CustomEvent('__api:error', {
      detail: {},
    }))

    expect(alpine.$data().message).toBe('An unexpected error occurred.')
  })

  it('omits detail when raw equals message', () => {
    initErrorDialog(alpine)

    window.dispatchEvent(new CustomEvent('__api:error', {
      detail: { code: '', message: 'Same text', raw: 'Same text' },
    }))

    expect(alpine.$data().detail).toBe('')
  })

  it('omits detail when raw is empty', () => {
    initErrorDialog(alpine)

    window.dispatchEvent(new CustomEvent('__api:error', {
      detail: { message: 'Error', raw: '' },
    }))

    expect(alpine.$data().detail).toBe('')
  })

  it('shows detail when raw differs from message', () => {
    initErrorDialog(alpine)

    window.dispatchEvent(new CustomEvent('__api:error', {
      detail: { message: 'User-friendly msg', raw: 'goroutine panic: ...' },
    }))

    expect(alpine.$data().detail).toBe('goroutine panic: ...')
  })
})
