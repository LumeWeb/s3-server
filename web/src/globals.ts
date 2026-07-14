// Typed accessors for window globals set by api.ts, sse.ts, panel.ts, and sia-sdk.ts.
// Eliminates (window as any) casts across page components.

// --- Types ---

export type ToastType = 'info' | 'success' | 'error'

export interface ApiClient {
  get: (url: string) => Promise<any>
  post: (url: string, json?: any) => Promise<any>
  put: (url: string, json?: any) => Promise<any>
  del: (url: string) => Promise<any>
}

export interface SiaSdk {
  Builder: any
  generateRecoveryPhrase: () => string
  validateRecoveryPhrase: (phrase: string) => boolean
  AppKey: any
}

// --- Window global declarations ---

declare global {
  interface Window {
    __api?: ApiClient
    __apiReady?: Promise<void>
    __sseReady?: Promise<void>
    __sseToast?: (msg: string, type?: ToastType) => void
    __sseProcessing?: (msg: string) => void
    __sseAction?: (processingMsg: string, fn: () => Promise<any>, successMsg?: string) => Promise<any>
    __reloadAfter?: (ms?: number) => void
    __copyToClipboard?: (text: string, successMsg?: string) => Promise<void>
    __dialogHandlers?: Map<string, () => void>
    __onDialogConfirm?: (source: string, handler: () => void) => void
    __sseEs?: EventSource
    __siaSdk?: SiaSdk
    __siaReady?: Promise<void>
    sodium?: any
    Alpine?: any
  }
}

// --- Typed accessors ---

/** The API client for making panel requests. Set by api.ts on init. */
export function api(): Window['__api'] {
  return window.__api
}

/** Resolves when the API client is ready (panel.ts dispatches __api:ready). */
export function apiReady(): Promise<void> {
  return window.__apiReady ?? Promise.resolve()
}

/** Resolves when the SSE connection is ready. */
export function sseReady(): Promise<void> {
  return window.__sseReady ?? Promise.resolve()
}

/** Shows a toast notification. */
export function toast(msg: string, type: ToastType = 'info'): void {
  window.__sseToast?.(msg, type)
}

/** Shows a processing toast (with spinner). */
export function processing(msg: string): void {
  window.__sseProcessing?.(msg)
}

/**
 * Wraps an async action with processing toast + success/error toasts.
 * Returns the result on success, throws on error.
 */
export async function apiAction<T = any>(
  processingMsg: string,
  fn: () => Promise<T>,
  successMsg?: string,
): Promise<T> {
  return window.__sseAction!(processingMsg, fn, successMsg)
}

/** Resolves when the Sia SDK is ready (onboarding page only). */
export function siaReady(): Promise<void> {
  return window.__siaReady ?? Promise.resolve()
}

/** Reloads the page after a short delay (lets success toasts finish). */
export function reloadAfter(ms = 800): void {
  setTimeout(() => window.location.reload(), ms)
}

/** Copies text to clipboard with HTTP fallback. Shows a toast. */
export function copyToClipboard(text: string, successMsg?: string): Promise<void> {
  return window.__copyToClipboard!(text, successMsg)
}

/**
 * Registers a handler for a dialog-confirm event with the given source name.
 * The Dialog component dispatches `dialog-confirm` when the user clicks the
 * confirm button. This replaces inline `@dialog-confirm.window` Alpine handlers.
 */
export function onDialogConfirm(source: string, handler: () => void): void {
  window.__onDialogConfirm?.(source, handler)
}

/** The Sia SDK object (onboarding page only). */
export function siaSdk(): SiaSdk {
  return window.__siaSdk!
}
