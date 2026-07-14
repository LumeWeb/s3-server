import '@fontsource/poppins/400.css'
import '@fontsource/poppins/500.css'
import '@fontsource/poppins/600.css'
import '@fontsource/poppins/700.css'
import Alpine from 'alpinejs'
import focus from '@alpinejs/focus'

import './api'       // sets up window.__api, handleReq, ERROR_MESSAGES
import { initToast, initSSE } from './sse'
import { initErrorDialog } from './error-dialog'

// Page-level Alpine components
import { bucketPage } from './pages/buckets'
import { dashboardApp } from './pages/dashboard'
import { monitoringApp } from './pages/monitoring'
import { onboardingWizard } from './pages/onboarding'
import { settingsApp, updateControlsApp, flushAllApp, restartApp, changePasswordApp, versionCheckApp, navFlushApp } from './pages/settings'
import { initLogin } from './pages/login'
import { keysApp } from './pages/keys'
import { usersApp } from './pages/users'
import { backupsApp } from './pages/backups'
import { resetPasswordForm } from './pages/reset_password'

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

Alpine.plugin(focus)

document.addEventListener('alpine:init', () => {
  initToast(Alpine)
  initErrorDialog(Alpine)

  // Register page-level Alpine components
  Alpine.data('bucketPage', bucketPage)
  Alpine.data('dashboardApp', dashboardApp)
  Alpine.data('monitoringApp', monitoringApp)
  Alpine.data('onboardingWizard', onboardingWizard)
  Alpine.data('settingsApp', settingsApp)
  Alpine.data('updateControlsApp', updateControlsApp)
  Alpine.data('flushAllApp', flushAllApp)
  Alpine.data('restartApp', restartApp)
  Alpine.data('changePasswordApp', changePasswordApp)
  Alpine.data('versionCheckApp', versionCheckApp)
  Alpine.data('navFlushApp', navFlushApp)
  Alpine.data('keysApp', keysApp)
  Alpine.data('usersApp', usersApp)
  Alpine.data('backupsApp', backupsApp)
  Alpine.data('resetPasswordForm', resetPasswordForm)
  initLogin(Alpine)
})

Alpine.start()
initSSE()

// Signal that __api is ready (ky client, handleReq, etc.)
window.dispatchEvent(new Event('__api:ready'))
