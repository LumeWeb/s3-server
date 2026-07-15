// Browser test setup: runs in the real browser before each test file.
// Sets up window globals that panel.ts expects, similar to what the Go backend
// would inject via templ templates.

import '../src/globals'

// Stub SSE globals — real SSE EventSource isn't needed for most tests
(window as any).__sseReady = Promise.resolve()
;(window as any).__sseProcessing = () => {}
;(window as any).__sseToast = () => {}
;(window as any).__sseAction = async (_msg: string, fn: () => Promise<any>) => fn()
;(window as any).__reloadAfter = () => {}
;(window as any).__siaReady = Promise.resolve()

// Default API stub — tests override via page.evaluate or route interception
;(window as any).__api = {
  get: () => Promise.resolve({}),
  post: () => Promise.resolve({}),
  put: () => Promise.resolve({}),
  del: () => Promise.resolve({}),
}

// Global DOM cleanup after each test — prevents leakage between tests.
import { afterEach } from 'vitest'

afterEach(() => {
  document.body.innerHTML = ''
  // Clear any pending timers (e.g., reloadAfter's setTimeout) to prevent
  // iframe navigation between tests
  let id = window.setTimeout(() => {}, 0)
  while (id--) window.clearTimeout(id)
})
