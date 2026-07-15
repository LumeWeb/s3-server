import { vi } from 'vitest'
import Alpine from 'alpinejs'
import focus from '@alpinejs/focus'

// Shared browser test helpers — used by all browser/*.test.ts files.
// Eliminates ~70 lines of duplicated stubGlobals/mountAlpine/Alpine setup.

Alpine.plugin(focus)

let alpineStarted = false

/** Default mock API responses. Tests can override per-call with vi.fn().mockReturnValue(...). */
const DEFAULT_API_RESPONSE: Record<string, any> = {}

/**
 * Stub browser globals (__api, __sseAction, __sseToast, etc.) to avoid
 * real HTTP and SSE in browser tests.
 *
 * @param overrides - Override specific globals or API responses.
 *   - `{ getResponse: {...} }` — return custom data from api.get()
 *   - `{ apiClient: {...} }` — replace the entire API client
 *   - `{ sseAction: fn }` — override __sseAction
 *   - `{ sseToast: fn }` — override __sseToast
 */
export function stubGlobals(overrides: Record<string, any> = {}) {
  const apiClient = overrides.apiClient ?? {
    get: vi.fn().mockResolvedValue(overrides.getResponse ?? DEFAULT_API_RESPONSE),
    post: vi.fn().mockResolvedValue({}),
    put: vi.fn().mockResolvedValue({}),
    del: vi.fn().mockResolvedValue({}),
  }
  ;(window as any).__api = apiClient
  ;(window as any).__apiReady = Promise.resolve()
  ;(window as any).__sseReady = Promise.resolve()
  ;(window as any).__sseToast = overrides.sseToast ?? vi.fn()
  ;(window as any).__sseProcessing = vi.fn()
  ;(window as any).__sseAction = overrides.sseAction ?? vi.fn(async (_msg: string, fn: () => Promise<any>) => fn())
  return { apiClient }
}

/**
 * Mount Alpine markup into the DOM and wait for directives to process.
 * Uses Alpine.initTree() for dynamically added elements.
 */
export async function mountAlpine(html: string, delay = 20): Promise<HTMLElement> {
  const el = document.createElement('div')
  el.innerHTML = html
  document.body.appendChild(el)
  Alpine.initTree(el)
  await Alpine.nextTick()
  await new Promise(resolve => setTimeout(resolve, delay))
  return el
}

/**
 * Register Alpine component(s). Call at module level — Alpine.data() must
 * be called before Alpine.start() (which happens in beforeEachHook).
 */
export function setupAlpine(components: Record<string, () => any>): void {
  for (const [name, fn] of Object.entries(components)) {
    Alpine.data(name, fn)
  }
}

/** Reset state between tests: clear DOM, clear mocks, start Alpine if needed. */
export function beforeEachHook(): void {
  document.body.innerHTML = ''
  vi.clearAllMocks()
  stubGlobals()
  if (!alpineStarted) {
    Alpine.start()
    alpineStarted = true
  }
}

/** Cleanup after tests: clear DOM. */
export function afterEachHook(): void {
  document.body.innerHTML = ''
}

/** Wait for Alpine to settle + a short delay for async ops. */
export async function settle(delay = 50): Promise<void> {
  await Alpine.nextTick()
  await new Promise(resolve => setTimeout(resolve, delay))
}
