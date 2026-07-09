// Sodium + Sia SDK initialization — only loaded on the onboarding page.
import _sodium from 'libsodium-wrappers'
import { initSia, Builder, generateRecoveryPhrase, validateRecoveryPhrase, AppKey } from '@siafoundation/sia-storage'

// --- sodium ---
;(async () => {
  await _sodium.ready
  window.sodium = _sodium
})()

// --- sia SDK ---
window.__siaSdk = { Builder, generateRecoveryPhrase, validateRecoveryPhrase, AppKey }

;(async () => {
  try {
    await initSia()
    const w = window as Window & typeof globalThis & {
      __siaReadyResolve?: () => void
      __siaReadyReject?: (e: unknown) => void
    }
    if (typeof w.__siaReadyResolve === 'function') {
      w.__siaReadyResolve()
    } else {
      window.__siaReady = Promise.resolve()
    }
  } catch (e) {
    console.error('Failed to init sia SDK:', e)
    const w = window as Window & typeof globalThis & {
      __siaReadyResolve?: () => void
      __siaReadyReject?: (e: unknown) => void
    }
    if (typeof w.__siaReadyReject === 'function') {
      w.__siaReadyReject(e)
    } else {
      window.__siaReady = Promise.reject(e)
    }
  }
})()
