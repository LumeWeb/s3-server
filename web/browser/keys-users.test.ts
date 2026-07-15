import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { keysApp } from '../src/pages/keys'
import { usersApp } from '../src/pages/users'
import { stubGlobals, mountAlpine, setupAlpine, beforeEachHook, settle } from './helpers'

setupAlpine({ keysApp, usersApp })

// Track pending timers so we can clear them before they fire reload()
let pendingTimers: number[] = []
const originalSetTimeout = window.setTimeout

beforeEach(() => {
  beforeEachHook()
  // Wrap setTimeout to track timers — we'll clear reloadAfter timers in afterEach
  window.setTimeout = ((fn: Function, ms?: number, ...args: any[]) => {
    const id = originalSetTimeout(fn, ms, ...args)
    pendingTimers.push(id)
    return id
  }) as any
})

afterEach(() => {
  pendingTimers.forEach(id => clearTimeout(id))
  pendingTimers = []
  window.setTimeout = originalSetTimeout
  document.body.innerHTML = ''
})

/** Wait with tracked setTimeout so timers are cleaned up */
async function settleTracked(delay = 50): Promise<void> {
  await new Promise(resolve => originalSetTimeout(resolve, delay))
}

describe('Keys page — real Alpine.js in browser', () => {
  it('renders initial state with showDelete=false and empty deleteAccessKey', async () => {
    const container = await mountAlpine(`
      <div x-data="keysApp">
        <span data-testid="showDelete" x-text="showDelete"></span>
        <span data-testid="accessKey" x-text="deleteAccessKey"></span>
        <span data-testid="backend" x-text="backendRunning"></span>
      </div>
    `)

    await settleTracked(100)

    const showDelete = container.querySelector('[data-testid="showDelete"]')! as HTMLElement
    const accessKey = container.querySelector('[data-testid="accessKey"]')! as HTMLElement
    const backend = container.querySelector('[data-testid="backend"]')! as HTMLElement

    expect(showDelete.textContent).toBe('false')
    expect(accessKey.textContent).toBe('')
    expect(backend.textContent).toBe('false')
  })

  it('x-show toggles delete confirmation based on showDelete', async () => {
    const container = await mountAlpine(`
      <div x-data="keysApp">
        <div data-testid="deletePanel" x-show="showDelete">Delete panel</div>
      </div>
    `)

    const panel = container.querySelector('[data-testid="deletePanel"]')! as HTMLElement
    expect(panel.style.display).toBe('none')

    const data = (container.firstElementChild as any)._x_dataStack[0]
    data.showDelete = true
    await settleTracked(50)

    expect(panel.style.display).not.toBe('none')
  })

  it('generateKey calls api.post with user_name', async () => {
    const { apiClient } = stubGlobals()

    const container = await mountAlpine(`
      <div x-data="keysApp">
      </div>
    `)

    await settleTracked(100)

    const data = (container.firstElementChild as any)._x_dataStack[0]
    await data.generateKey('alice')

    expect(apiClient.post).toHaveBeenCalledWith('/_panel/api/keys', { user_name: 'alice' })
  })

  it('confirmDelete calls api.del with encoded key', async () => {
    const { apiClient } = stubGlobals()

    const container = await mountAlpine(`
      <div x-data="keysApp">
      </div>
    `)

    await settleTracked(100)

    const data = (container.firstElementChild as any)._x_dataStack[0]
    data.deleteAccessKey = 'AKIA-TEST+key='
    await settle(50)

    await data.confirmDelete()

    expect(apiClient.del).toHaveBeenCalledWith('/_panel/api/keys/AKIA-TEST%2Bkey%3D')
  })

  it('backendRunning prop is passed through from x-data', async () => {
    const container = await mountAlpine(`
      <div x-data="keysApp({ backendRunning: true })">
        <span data-testid="backend" x-text="backendRunning"></span>
      </div>
    `)

    await settleTracked(100)

    const backend = container.querySelector('[data-testid="backend"]')! as HTMLElement
    expect(backend.textContent).toBe('true')
  })
})

describe('Users page — real Alpine.js in browser', () => {
  it('renders initial state with showCreate=false and empty createName', async () => {
    const container = await mountAlpine(`
      <div x-data="usersApp">
        <span data-testid="showCreate" x-text="showCreate"></span>
        <span data-testid="createName" x-text="createName"></span>
        <span data-testid="creating" x-text="creating"></span>
        <span data-testid="createErr" x-text="createErr"></span>
      </div>
    `)

    await settleTracked(100)

    const showCreate = container.querySelector('[data-testid="showCreate"]')! as HTMLElement
    const createName = container.querySelector('[data-testid="createName"]')! as HTMLElement
    const creating = container.querySelector('[data-testid="creating"]')! as HTMLElement
    const createErr = container.querySelector('[data-testid="createErr"]')! as HTMLElement

    expect(showCreate.textContent).toBe('false')
    expect(createName.textContent).toBe('')
    expect(creating.textContent).toBe('false')
    expect(createErr.textContent).toBe('')
  })

  it('x-show toggles create user panel', async () => {
    const container = await mountAlpine(`
      <div x-data="usersApp">
        <div data-testid="createPanel" x-show="showCreate">Create</div>
      </div>
    `)

    const panel = container.querySelector('[data-testid="createPanel"]')! as HTMLElement
    expect(panel.style.display).toBe('none')

    const data = (container.firstElementChild as any)._x_dataStack[0]
    data.showCreate = true
    await settleTracked(50)

    expect(panel.style.display).not.toBe('none')
  })

  it('createUser calls api.post with name', async () => {
    const { apiClient } = stubGlobals()

    const container = await mountAlpine(`
      <div x-data="usersApp">
      </div>
    `)

    await settleTracked(100)

    const data = (container.firstElementChild as any)._x_dataStack[0]
    data.createName = 'bob'
    await settle(50)

    await data.createUser()

    expect(apiClient.post).toHaveBeenCalledWith('/_panel/api/users', { name: 'bob' })
    expect(data.creating).toBe(false)
  })

  it('confirmDelete calls api.del with encoded name', async () => {
    const { apiClient } = stubGlobals()

    const container = await mountAlpine(`
      <div x-data="usersApp">
      </div>
    `)

    await settleTracked(100)

    const data = (container.firstElementChild as any)._x_dataStack[0]
    data.deleteName = 'user name+test'
    await settle(50)

    await data.confirmDelete()

    expect(apiClient.del).toHaveBeenCalledWith('/_panel/api/users/user%20name%2Btest')
  })
})
