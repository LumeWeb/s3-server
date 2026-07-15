import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import Alpine from 'alpinejs'
import { initLogin } from '../src/pages/login'
import { stubGlobals, mountAlpine, beforeEachHook, afterEachHook, settle } from './helpers'

initLogin(Alpine)

beforeEach(beforeEachHook)
afterEach(afterEachHook)

describe('Login page — real Alpine.js in browser', () => {
  it('renders login form with loading=false and showPw=false', async () => {
    const container = await mountAlpine(`
      <div x-data="loginForm">
        <span data-testid="loading" x-text="loading"></span>
        <span data-testid="showPw" x-text="showPw"></span>
      </div>
    `, 100)

    const loading = container.querySelector('[data-testid="loading"]')! as HTMLElement
    const showPw = container.querySelector('[data-testid="showPw"]')! as HTMLElement

    expect(loading.textContent).toBe('false')
    expect(showPw.textContent).toBe('false')
  })

  it('x-show toggles password visibility', async () => {
    const container = await mountAlpine(`
      <div x-data="loginForm">
        <input data-testid="pw" :type="showPw ? 'text' : 'password'" />
        <button data-testid="toggle" @click="showPw = !showPw">Toggle</button>
      </div>
    `, 100)

    const pw = container.querySelector('[data-testid="pw"]')! as HTMLInputElement
    expect(pw.type).toBe('password')

    const data = (container.firstElementChild as any)._x_dataStack[0]
    data.showPw = true
    await settle(50)

    expect(pw.type).toBe('text')
  })

  it('submit sends form data via fetch with correct payload', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
    })
    ;(globalThis as any).fetch = fetchSpy

    const container = await mountAlpine(`
      <form x-data="loginForm">
        <input name="email" value="admin@test.com" />
        <input name="password" value="secret" />
      </form>
    `, 100)

    const form = container.querySelector('form')! as HTMLFormElement
    const data = (container.firstElementChild as any)._x_dataStack[0]

    const mockEvent = { preventDefault: vi.fn(), target: form }
    await data.submit(mockEvent)

    expect(fetchSpy).toHaveBeenCalledWith('/_panel/login', expect.objectContaining({
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    }))

    const callArgs = fetchSpy.mock.calls[0]
    const body = callArgs[1].body as string
    expect(body).toContain('email=admin')
    expect(body).toContain('password=secret')
  })

  it('shows toast on 401 unauthorized', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
    })
    ;(globalThis as any).fetch = fetchSpy

    const toastSpy = vi.fn()
    stubGlobals({ sseToast: toastSpy })

    const container = await mountAlpine(`
      <form x-data="loginForm">
        <input name="email" value="admin@test.com" />
        <input name="password" value="wrong" />
      </form>
    `, 100)

    const form = container.querySelector('form')! as HTMLFormElement
    const data = (container.firstElementChild as any)._x_dataStack[0]

    const mockEvent = { preventDefault: vi.fn(), target: form }
    await data.submit(mockEvent)

    expect(toastSpy).toHaveBeenCalledWith('Incorrect password', 'error')
  })

  it('shows generic error toast on non-401 failure', async () => {
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
    })
    ;(globalThis as any).fetch = fetchSpy

    const toastSpy = vi.fn()
    stubGlobals({ sseToast: toastSpy })

    const container = await mountAlpine(`
      <form x-data="loginForm">
        <input name="email" value="admin@test.com" />
        <input name="password" value="secret" />
      </form>
    `, 100)

    const form = container.querySelector('form')! as HTMLFormElement
    const data = (container.firstElementChild as any)._x_dataStack[0]

    const mockEvent = { preventDefault: vi.fn(), target: form }
    await data.submit(mockEvent)

    expect(toastSpy).toHaveBeenCalledWith('Login failed: please try again', 'error')
  })

  it('shows network error toast on fetch rejection', async () => {
    const fetchSpy = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'))
    ;(globalThis as any).fetch = fetchSpy

    const toastSpy = vi.fn()
    stubGlobals({ sseToast: toastSpy })

    const container = await mountAlpine(`
      <form x-data="loginForm">
        <input name="email" value="admin@test.com" />
        <input name="password" value="secret" />
      </form>
    `, 100)

    const form = container.querySelector('form')! as HTMLFormElement
    const data = (container.firstElementChild as any)._x_dataStack[0]

    const mockEvent = { preventDefault: vi.fn(), target: form }
    await data.submit(mockEvent)

    expect(toastSpy).toHaveBeenCalledWith('Network error: check your connection', 'error')
  })
})
