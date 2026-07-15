import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { dashboardApp } from '../src/pages/dashboard'
import { S3Status } from '../src/pages/status'
import { stubGlobals, mountAlpine, setupAlpine, beforeEachHook, afterEachHook, settle } from './helpers'

setupAlpine({ dashboardApp })

beforeEach(() => {
  beforeEachHook()
  // Dashboard init() expects realistic API data
  stubGlobals({ getResponse: { s3_status: 'stopped', key_count: 0, version: '' } })
})
afterEach(afterEachHook)

describe('Dashboard — real Alpine.js in browser', () => {
  it('renders component state via x-text directives', async () => {
    const container = await mountAlpine(`
      <div x-data="dashboardApp">
        <span data-testid="status" x-text="s3Status"></span>
        <span data-testid="keyCount" x-text="keyCount"></span>
        <span data-testid="uptime" x-text="uptime"></span>
      </div>
    `, 50)

    const status = container.querySelector('[data-testid="status"]')! as HTMLElement
    const keyCount = container.querySelector('[data-testid="keyCount"]')! as HTMLElement

    expect(status.textContent).toBe(S3Status.Stopped)
    expect(keyCount.textContent).toBe('0')
  })

  it('x-show hides start button when running, shows stop button', async () => {
    const container = await mountAlpine(`
      <div x-data="dashboardApp">
        <button data-testid="startBtn" x-show="!isRunning">Start</button>
        <button data-testid="stopBtn" x-show="isRunning">Stop</button>
      </div>
    `)

    const startBtn = container.querySelector('[data-testid="startBtn"]')! as HTMLElement
    const stopBtn = container.querySelector('[data-testid="stopBtn"]')! as HTMLElement

    expect(startBtn.style.display).not.toBe('none')
    expect(stopBtn.style.display).toBe('none')
  })

  it('x-text updates reactively when state changes', async () => {
    const container = await mountAlpine(`
      <div x-data="dashboardApp">
        <span data-testid="status" x-text="s3Status"></span>
      </div>
    `)

    const status = container.querySelector('[data-testid="status"]')! as HTMLElement

    await settle(50)
    expect(status.textContent).toBe(S3Status.Stopped)

    const data = (container.firstElementChild as any)._x_dataStack[0]
    data.s3Status = S3Status.Running
    await settle(50)

    expect(status.textContent).toBe(S3Status.Running)
  })

  it('init() fetches status and updates DOM reactively', async () => {
    const { apiClient } = stubGlobals({
      getResponse: { s3_status: 'stopped', key_count: 0, version: '' },
    })
    apiClient.get = vi.fn().mockImplementation((url: string) => {
      if (url === '/_panel/api/status') {
        return Promise.resolve({
          s3_status: 'running',
          init_error: '',
          key_count: 7,
          version: 'v3.2.1',
        })
      }
      return Promise.resolve({})
    }) as any

    const container = await mountAlpine(`
      <div x-data="dashboardApp" x-init="init()">
        <span data-testid="status" x-text="s3Status"></span>
        <span data-testid="keyCount" x-text="keyCount"></span>
        <span data-testid="version" x-text="version"></span>
      </div>
    `, 100)

    const status = container.querySelector('[data-testid="status"]')! as HTMLElement
    const keyCount = container.querySelector('[data-testid="keyCount"]')! as HTMLElement
    const version = container.querySelector('[data-testid="version"]')! as HTMLElement

    expect(status.textContent).toBe('running')
    expect(keyCount.textContent).toBe('7')
    expect(version.textContent).toBe('v3.2.1')
  })
})

describe('Dashboard — real DOM event handling', () => {
  it('SSE custom event updates Alpine state reactively', async () => {
    const container = await mountAlpine(`
      <div x-data="dashboardApp" x-init="init()">
        <span data-testid="status" x-text="s3Status"></span>
        <span data-testid="keyCount" x-text="keyCount"></span>
      </div>
    `, 100)

    const status = container.querySelector('[data-testid="status"]')! as HTMLElement
    const keyCount = container.querySelector('[data-testid="keyCount"]')! as HTMLElement

    expect(status.textContent).toBe(S3Status.Stopped)

    window.dispatchEvent(new CustomEvent('sse:dashboard', {
      detail: {
        s3_status: 'running',
        key_count: 15,
      },
    }))

    await settle(50)

    expect(status.textContent).toBe('running')
    expect(keyCount.textContent).toBe('15')
  })
})
