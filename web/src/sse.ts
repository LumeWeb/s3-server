// --- Toast ---

import { ERROR_MESSAGES } from './api'

let toastTimer: ReturnType<typeof setTimeout> | null = null

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function initToast(Alpine: any) {
  const toast = document.getElementById('sse-toast')
  if (!toast) return

  window.__sseToast = function (msg: string, type?: string) {
    const alpine = (Alpine as any).$data(toast)
    alpine.processing = false
    alpine.msg = msg
    alpine.type = type || 'info'
    alpine.show = true
    if (toastTimer) clearTimeout(toastTimer)
    toastTimer = setTimeout(() => { alpine.show = false }, 5000)
  }

  window.__sseProcessing = function (msg: string) {
    const alpine = (Alpine as any).$data(toast)
    alpine.processing = true
    alpine.msg = msg
    alpine.type = 'info'
    alpine.show = true
    if (toastTimer) clearTimeout(toastTimer)
  }

  // DRY helper: wraps an async action with processing toast + success/error toasts.
  // Usage: await apiAction('Creating bucket…', () => api().post(...), 'Bucket created')
  window.__sseAction = async function (processingMsg: string, fn: () => Promise<any>, successMsg?: string) {
    window.__sseProcessing!(processingMsg)
    try {
      const result = await fn()
      if (successMsg) { window.__sseToast!(successMsg, 'success') }
      return result
    } catch (e: any) {
      window.__sseToast!(e.message || 'Action failed', 'error')
      throw e
    }
  }
}

// --- htmx progress bar + error handling ---

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function initHtmx(Alpine: any) {
  const bar = document.getElementById('htmx-indicator')
  const toast = document.getElementById('sse-toast')

  if (bar) {
    document.body.addEventListener('htmx:beforeRequest', () => {
      bar.style.opacity = '1'
      bar.style.width = '0%'
      bar.style.transition = 'width 0.3s ease'
      bar.style.width = '70%'
    })
    document.body.addEventListener('htmx:afterRequest', () => {
      bar.style.width = '100%'
      setTimeout(() => { bar.style.opacity = '0' }, 200)
    })
  }

  document.body.addEventListener('htmx:responseError', (e: Event) => {
    if (!toast) return
    const alpine = (Alpine as any).$data(toast)
    alpine.msg = 'Request failed'
    const detail = (e as any).detail
    if (detail && detail.xhr) {
      try {
        const d = JSON.parse(detail.xhr.responseText)
        const type = d.type || ''
        alpine.msg = (ERROR_MESSAGES as Record<string, string>)[type] || d.message || d.error || 'Request failed'
      } catch (_) { /* not JSON */ }
    }
    alpine.type = 'error'
    alpine.show = true
    if (toastTimer) clearTimeout(toastTimer)
    toastTimer = setTimeout(() => { alpine.show = false }, 5000)
  })

  // CSRF header injection for htmx
  document.addEventListener('htmx:configRequest', (event: Event) => {
    const csrf = document.cookie.split('; ').find(row => row.startsWith('_csrf='))
    if (csrf) {
      (event as any).detail.headers['X-CSRF-Token'] = csrf.split('=')[1]
    }
  })
}

// --- SSE ---

export function initSSE() {
  window.__sseReady = new Promise<void>(resolve => {
    window.addEventListener('__sse:ready', () => resolve())
  })

  let sseInitialized = false

  window.addEventListener('__api:ready', () => {
    // Skip SSE on pages where the user isn't authenticated yet
    if (document.querySelector('[data-onboarding]') || document.querySelector('[data-login]')) return

    // Prevent multiple EventSource connections — if __api:ready fires
    // more than once (e.g. after htmx boosted navigation re-evaluates scripts),
    // don't create a new EventSource.
    if (sseInitialized) return
    sseInitialized = true

    // Close any existing EventSource before opening a new one
    const prev = window.__sseEs
    if (prev) prev.close()

    const es = new EventSource('/_panel/api/events')
    window.__sseEs = es

    es.addEventListener('dashboard', (e) => {
      try {
        const data = JSON.parse(e.data)
        window.dispatchEvent(new CustomEvent('sse:dashboard', { detail: data }))
      } catch (_) { /* ignore parse errors */ }
    })

    es.addEventListener('keys', (e) => {
      try {
        const data = JSON.parse(e.data)
        window.dispatchEvent(new CustomEvent('sse:keys', { detail: data }))
        if (data.action === 'created' || data.action === 'deleted') {
          window.__sseToast?.('Access key ' + data.action, 'info')
        }
      } catch (_) { /* ignore */ }
    })

    es.addEventListener('buckets', (e) => {
      try {
        const data = JSON.parse(e.data)
        window.dispatchEvent(new CustomEvent('sse:buckets', { detail: data }))
        window.__sseToast?.('Bucket "' + data.name + '" ' + data.action, 'info')
      } catch (_) { /* ignore */ }
    })

    es.addEventListener('stats', (e) => {
      try {
        const data = JSON.parse(e.data)
        window.dispatchEvent(new CustomEvent('sse:stats', { detail: data }))
      } catch (_) { /* ignore */ }
    })

    es.onerror = () => {
      // EventSource auto-reconnects; no action needed
    }

    // Close SSE connection when the page unloads (navigation)
    window.addEventListener('pagehide', () => es.close(), { once: true })

    window.dispatchEvent(new Event('__sse:ready'))
  })
}
