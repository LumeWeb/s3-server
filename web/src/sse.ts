import { RELOAD_DELAY } from './globals'

// --- Toast ---

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
      if (result === undefined) return result // error already handled by handleReq
      if (successMsg) { window.__sseToast!(successMsg, 'success') }
      return result
    } catch (e: any) {
      window.__sseToast!(e.message || 'Action failed', 'error')
      throw e
    }
  }

  // DRY helper: reload after a short delay (lets success toasts finish).
  // Usage in templ: window.__reloadAfter()
  window.__reloadAfter = function (ms?: number) {
    setTimeout(() => window.location.reload(), ms ?? RELOAD_DELAY)
  }

  // DRY helper: copy text to clipboard with HTTP fallback for non-secure contexts.
  // Uses navigator.clipboard when available (HTTPS/localhost), falls back to
  // hidden textarea + execCommand('copy') for HTTP.
  // Usage in templ: window.__copyToClipboard('value', 'Copied ID')
  window.__copyToClipboard = async function (text: string, successMsg?: string) {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text)
      } else {
        const ta = document.createElement('textarea')
        ta.value = text
        ta.style.position = 'fixed'
        ta.style.left = '-9999px'
        document.body.appendChild(ta)
        ta.select()
        document.execCommand('copy')
        document.body.removeChild(ta)
      }
      window.__sseToast!(successMsg || 'Copied to clipboard', 'success')
    } catch (_) {
      window.__sseToast!('Failed to copy', 'error')
    }
  }

  // --- Dialog confirm handler registry ---
  //
  // Dialog components dispatch a `dialog-confirm` CustomEvent with
  // { source: '<showProp>' }. Instead of each page writing an inline
  // @dialog-confirm.window="if ($event.detail?.source === '...') { ... }"
  // handler, pages register named handlers via window.__onDialogConfirm.
  //
  // Registration is idempotent: re-registering replaces the handler
  // (important for navigations that re-evaluate page scripts).
  window.__dialogHandlers = new Map<string, () => void>()
  window.__onDialogConfirm = function (source: string, handler: () => void) {
    window.__dialogHandlers!.set(source, handler)
  }
  window.addEventListener('dialog-confirm', (e: Event) => {
    const detail = (e as CustomEvent).detail
    if (!detail?.source) return
    const handler = window.__dialogHandlers!.get(detail.source)
    if (handler) handler()
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

    // Prevent multiple EventSource connections
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
