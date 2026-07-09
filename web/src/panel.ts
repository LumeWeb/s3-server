import '@fontsource/poppins/400.css'
import '@fontsource/poppins/500.css'
import '@fontsource/poppins/600.css'
import '@fontsource/poppins/700.css'
import Alpine from 'alpinejs'
import htmx from 'htmx.org'

import './api'       // sets up window.__api, handleReq, ERROR_MESSAGES
import { initToast, initHtmx, initSSE } from './sse'
import { initErrorDialog } from './error-dialog'
import { initConfirmDialog } from './confirm-dialog'

// Page-level Alpine components
import { bucketPage } from './pages/buckets'
import { dashboardApp } from './pages/dashboard'
import { monitoringApp } from './pages/monitoring'
import { onboardingWizard } from './pages/onboarding'
import { settingsApp, updateControlsApp } from './pages/settings'
import { initLogin } from './pages/login'

// --- onboarding-only: sodium + sia SDK ---
// These are heavy deps; only init if onboarding page is active.
const isOnboarding = !!document.querySelector('[data-onboarding]')

if (isOnboarding) {
  import('./sia-sdk')
}

// --- Promise bridge: resolved here, awaited by inline Alpine components ---
window.__apiReady = new Promise<void>(resolve => {
  window.addEventListener('__api:ready', () => resolve())
})

// --- Start Alpine, init layout-level features ---
window.Alpine = Alpine
window.htmx = htmx

document.addEventListener('alpine:init', () => {
  initToast(Alpine)
  initHtmx(Alpine)
  initErrorDialog(Alpine)
  initConfirmDialog(Alpine)

  // Register page-level Alpine components
  Alpine.data('bucketPage', bucketPage)
  Alpine.data('dashboardApp', dashboardApp)
  Alpine.data('monitoringApp', monitoringApp)
  Alpine.data('onboardingWizard', onboardingWizard)
  Alpine.data('settingsApp', settingsApp)
  Alpine.data('updateControlsApp', updateControlsApp)
  initLogin(Alpine)
})

Alpine.start()

// Process hx-* attributes AFTER Alpine has initialized, so HTMX wires up
// the final DOM nodes (Alpine may recreate nodes during initialization).
htmx.process(document.body)
initSSE()

// Signal that __api is ready (ky client, handleReq, etc.)
window.dispatchEvent(new Event('__api:ready'))
