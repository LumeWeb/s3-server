// Test setup — runs before every test file.
// Stubs browser-only globals that aren't available in happy-dom.

import { afterAll, afterEach, beforeAll } from 'vitest'
import { setupServer } from 'msw/node'
import { handlers } from './handlers'

// Start MSW server for Node tests
export const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

// Stub window properties that are set at runtime by panel.ts/sia-sdk.ts
// so tested modules don't crash when accessing globals.
if (typeof window !== 'undefined') {
  // Mark api as ready immediately in tests
  window.__apiReady = Promise.resolve()

  // Stub __sseToast so api.ts handleReq can call it
  window.__sseToast = () => {}

  // Stub SSE globals
  window.__sseReady = false
  window.__sseProcessing = false
  window.__sseEs = undefined

  // Stub __api with a passthrough that tests will override via MSW
  window.__api = {
    get: () => Promise.resolve({}),
    post: () => Promise.resolve({}),
    put: () => Promise.resolve({}),
    del: () => Promise.resolve({}),
  }

  // Stub SIA SDK globals
  window.__siaSdk = undefined
  window.__siaReady = false

  // Stub Alpine/htmx (not needed in most unit tests)
  window.Alpine = { $data: (el: unknown) => ({}), data: () => {} } as any
  window.htmx = undefined
}
