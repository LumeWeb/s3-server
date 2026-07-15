import { vi } from 'vitest'

// Shared helpers for onboarding tests — stubs the browser globals that
// onboardingWizard expects (siaSdk, sseAction, etc.)
// Used by both onboarding.test.ts and onboarding-extended.test.ts.

export function stubOnboardingGlobals(overrides: Record<string, any> = {}) {
  window.__apiReady = Promise.resolve()
  window.__sseToast = overrides.__sseToast ?? vi.fn()
  window.__sseAction = overrides.__sseAction ?? vi.fn()
  window.__sseReady = Promise.resolve()
  window.__siaReady = Promise.resolve()
  window.__siaSdk = overrides.__siaSdk ?? {
    Builder: vi.fn(),
    generateRecoveryPhrase: vi.fn(() => 'word '.repeat(12).trim()),
    validateRecoveryPhrase: vi.fn(),
    AppKey: vi.fn(),
  }
  window.history.replaceState({}, '', '/_panel/onboarding/password')
}
