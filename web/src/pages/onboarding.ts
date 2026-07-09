// Alpine component: onboarding wizard — powered by robot3 FSM
/* eslint-disable @typescript-eslint/no-explicit-any */

import { createMachine, interpret, state, transition, reduce } from 'robot3'
import { api, apiReady, toast, siaReady, siaSdk } from '../globals'

// --- FSM definition ---------------------------------------------------------

// Event names — enum, not string literals, so renames are caught at compile time.
const Event = {
  PasswordSet: 'passwordSet',
  SiaStepChange: 'siaStepChange',
  AppKeySubmitted: 'appKeySubmitted',
  Back: 'back',
  CredentialsGenerated: 'credentialsGenerated',
  GoToDashboard: 'goToDashboard',
} as const

// Server onboarding state values — must match Go OnboardingState constants.
const ServerState = {
  Pending: 'pending',
  AdminSet: 'admin_password_set',
  AppKeySet: 'app_key_set',
  Complete: 'complete',
} as const

// FSM state names (robot3 machine keys).
const FsmState = {
  Password: 'password',
  Connect: 'connect',
  Finish: 'finish',
  Complete: 'complete',
} as const

type OnboardingContext = {
  loading: boolean
  siaStep: string
  recoveryPhrase: string
  phraseSaved: boolean
  showConfirmKey: boolean
  showSecretKey: boolean
  copiedField: string
  generatedCredentials: { accessKey: string; secretKey: string }
  // Stored API data needed later (not for display)
  siaSdk: any
}

const onboardingMachine = createMachine(
  {
    // Step 0: Set admin password
    password: state(
      transition(
        Event.PasswordSet,
        'connect',
        reduce((ctx: any) => ({ ...ctx, loading: false })),
      ),
    ),
    // Step 1: Connect to Sia (has sub-steps: connect → waiting → recovery)
    connect: state(
      transition(Event.SiaStepChange, 'connect'),
      transition(Event.AppKeySubmitted, 'finish'),
      transition(Event.Back, 'password'),
    ),
    // Step 2: Finish setup — generate credentials
    finish: state(
      transition(
        Event.CredentialsGenerated,
        'finish',
        reduce((ctx: any, ev: any) => ({
          ...ctx,
          loading: false,
          generatedCredentials: {
            accessKey: ev.accessKey || '',
            secretKey: ev.secretKey || '',
          },
        })),
      ),
      transition(Event.GoToDashboard, 'complete'),
    ),
    // Terminal state — redirect happens in Alpine watcher
    complete: state(),
  },
  (): OnboardingContext => ({
    loading: false,
    siaStep: 'connect',
    recoveryPhrase: '',
    phraseSaved: false,
    showConfirmKey: false,
    showSecretKey: false,
    showPw: false,
    copiedField: '',
    generatedCredentials: { accessKey: '', secretKey: '' },
    siaSdk: null,
  }),
)

// Map FSM state names → step indices for the step indicator
const STEP_MAP: Record<string, number> = {
  [FsmState.Password]: 0,
  [FsmState.Connect]: 1,
  [FsmState.Finish]: 2,
  [FsmState.Complete]: 3,
}

// Map server onboarding states → step indices (for restoreFromServer comparison)
const SERVER_STEP_MAP: Record<string, number> = {
  [ServerState.Pending]: 0,
  [ServerState.AdminSet]: 1,
  [ServerState.AppKeySet]: 2,
  [ServerState.Complete]: 3,
}

const STEP_LABELS = ['Password', 'Connect', 'Finish']

// --- Alpine component -------------------------------------------------------

export function onboardingWizard(this: any) {
  // Declare alpine first so the onChange callback can reference it without TDZ.
  const alpine = {
    // --- Reactive properties (synced from robot3 service) -------------------
    steps: STEP_LABELS,
    currentStep: 0,
    siaStep: 'connect',
    loading: false,
    recoveryPhrase: '',
    phraseSaved: false,
    phraseCopied: false,
    showConfirmKey: false,
    showSecretKey: false,
    showPw: false,
    copiedField: '',
    generatedCredentials: { accessKey: '', secretKey: '' },
    // --- Local UI state (not FSM-managed) -----------------------------------
    adminPassword: '',
    adminPasswordConfirm: '',
    siaConfig: null as any,
    indexerSelection: '',
    customIndexer: '',
    insecureContext: false,
    // Not displayed — kept for API calls
    siaSdk: null as any,

    // --- Lifecycle ----------------------------------------------------------
    async init() {
      // Capture Alpine's reactive proxy for this component. Alpine wraps the
      // returned object in Alpine.reactive(), creating a proxy. We need to
      // write through THAT proxy (not the raw `alpine` object) so Alpine's
      // directives detect the changes. `this` inside init IS that proxy.
      ;(this as any)._reactive = this

      // Don't block Alpine initialization on __apiReady — just fire and forget
      // so x-cloak is removed immediately and the password UI is visible.
      this.insecureContext = !window.isSecureContext

      this.$el.addEventListener('copied-key', () => {
        this.phraseCopied = true
        setTimeout(() => {
          this.phraseCopied = false
        }, 3000)
      })

      this.$el.addEventListener('dialog-confirm', () => {
        // Dialog already closes itself by setting showConfirmKey = false
        // before dispatching this event — don't guard on showConfirmKey.
        this.submitAppKey()
      })

      // Restore onboarding progress from server state
      this.restoreFromServer()
      // Load Sia config (non-blocking)
      this.loadSiaConfig()
    },

    async restoreFromServer() {
      try {
        await apiReady()
        const data = await api()!.get('/_panel/api/onboarding/status')
        if (!data) return
        if (data.state === ServerState.Complete) {
          window.location.href = '/_panel/dashboard'
          return
        }
        // Only restore forward — never jump the user backwards or to a state
        // they've already passed. This prevents the race where restoreFromServer
        // resolves after the user has already started interacting.
        const serverStep = SERVER_STEP_MAP[data.state] ?? 0
        const currentStep = STEP_MAP[this._service.machine.current] ?? 0
        if (serverStep > currentStep) {
          if (data.state === ServerState.AppKeySet) {
            this._service.send(Event.PasswordSet)
            this._service.send(Event.AppKeySubmitted)
          } else if (data.state === ServerState.AdminSet) {
            this._service.send(Event.PasswordSet)
          }
        }
      } catch (e: any) {
        // Swallow — stay on step 0 (password)
      }
    },

    async loadSiaConfig() {
      try {
        await apiReady()
        this.siaConfig = await api()!.get('/_panel/api/onboarding/config')
        this.indexerSelection =
          this.siaConfig.indexer_url ||
          (this.siaConfig.available_indexers && this.siaConfig.available_indexers[0]) ||
          ''
      } catch (e: any) {
        toast('Failed to load onboarding config: ' + e.message, 'error')
      }
    },

    // --- Step 0: Admin password --------------------------------------------
    async setAdminPassword() {
      if (this.adminPassword.length < 8) {
        toast('Password must be at least 8 characters', 'error')
        return
      }
      if (this.adminPassword !== this.adminPasswordConfirm) {
        toast('Passwords do not match', 'error')
        return
      }
      this.loading = true
      try {
        const data = await api()!.post('/_panel/api/onboarding/admin-password', {
          password: this.adminPassword,
        })
        if (data === undefined) {
          this.loading = false
          return // error toast shown by handleReq
        }
        this._service.send(Event.PasswordSet)
      } catch (e: any) {
        this.loading = false
        toast(e.message, 'error')
      }
    },

    // --- Step 1: Sia connection --------------------------------------------
    selectedIndexerURL() {
      if (this.indexerSelection === '__custom__') return this.customIndexer
      return this.indexerSelection
    },

    normalizeIndexerURL(url: string) {
      let u = url.trim()
      if (!/^https?:\/\//i.test(u)) u = 'https://' + u
      return u.replace(/\/+$/, '')
    },

    async connectToSia() {
      this.loading = true
      try {
        await siaReady()
        if (!this.siaConfig) throw new Error('Sia config not loaded')
        const rawURL = this.selectedIndexerURL()
        if (!rawURL) throw new Error('Select an indexer or enter a custom URL')
        const indexerURL = this.normalizeIndexerURL(rawURL)

        // Pre-validate reachability; WASM SDK error is opaque when host is unreachable
        try {
          await fetch(indexerURL, {
            method: 'HEAD',
            mode: 'no-cors',
            signal: AbortSignal.timeout(8000),
          })
        } catch (e: any) {
          if (e.name === 'TimeoutError')
            throw new Error(
              `Indexer "${indexerURL}" did not respond (timed out). Check the URL or try a different indexer.`,
            )
          throw new Error(
            `Could not reach indexer "${indexerURL}". Verify the URL is correct and the service is online.`,
          )
        }

        const { Builder, generateRecoveryPhrase, validateRecoveryPhrase } =
          siaSdk()
        const appMeta = {
          appId: this.siaConfig.app_id,
          name: this.siaConfig.app_name,
          description: this.siaConfig.app_description,
          logoUrl: this.siaConfig.logo_url,
          serviceUrl: this.siaConfig.service_url,
          callbackUrl: this.siaConfig.callback_url,
        }
        const builder = new Builder(indexerURL, appMeta)
        try {
          await builder.requestConnection()
        } catch (e: any) {
          const msg = e.message || ''
          if (/invalid character/i.test(msg))
            throw new Error(
              `Indexer "${indexerURL}" returned an unexpected response. It may be down or not a valid Sia indexer.`,
            )
          if (/builder error/i.test(msg))
            throw new Error(
              `Failed to connect to indexer "${indexerURL}". The URL may be invalid or the service unavailable.`,
            )
          if (/error sending request/i.test(msg))
            throw new Error(
              `Connection blocked: indexer "${indexerURL}" rejected the request. This is typically a CORS or network issue. Ensure the indexer allows requests from this origin.`,
            )
          throw new Error(`Connection failed: ${msg}`)
        }
        window.open(builder.responseUrl(), '_blank')
        this.siaStep = 'waiting'

        try {
          await builder.waitForApproval()
        } catch (e: any) {
          const msg = (e.message || '').toLowerCase()
          if (/error sending request|failed to fetch|networkerror/.test(msg))
            throw new Error(
              `Approval status check to "${indexerURL}" was blocked. This is typically a CORS issue on the portal proxy.`,
            )
          throw new Error(`Approval check failed: ${e.message}`)
        }
        this.recoveryPhrase = generateRecoveryPhrase()
        validateRecoveryPhrase(this.recoveryPhrase)
        this.siaSdk = await builder.register(this.recoveryPhrase)
        this.siaStep = 'recovery'
      } catch (e: any) {
        this.siaStep = 'connect'
        toast(e.message, 'error')
      } finally {
        this.loading = false
      }
    },

    async submitAppKey() {
      this.loading = true
      try {
        const appKeySeed = this.siaSdk.appKey().export()
        const pubKeyData = await api()!.get('/_panel/api/onboarding/public-key')
        if (pubKeyData === undefined) {
          this.loading = false
          return // error toast shown by handleReq
        }
        const pubKeyBytes = Uint8Array.from(atob(pubKeyData.public_key), (c) => c.charCodeAt(0))
        if (typeof window.sodium == 'undefined' || !window.sodium) {
          throw new Error(
            'Could not load the encryption library (libsodium). Please refresh the page. If the problem persists, check the browser console for details.',
          )
        }
        await window.sodium.ready
        const sealed = window.sodium.crypto_box_seal(appKeySeed, pubKeyBytes)
        const result = await api()!.post('/_panel/api/onboarding/app-key', {
          encrypted_app_key: btoa(String.fromCharCode(...sealed)),
          indexer_url: this.selectedIndexerURL(),
        })
        if (result === undefined) {
          this.loading = false
          return // error dialog shown by handleReq
        }
        this._service.send(Event.AppKeySubmitted)
      } catch (e: any) {
        this.loading = false
        toast(e.message, 'error')
      }
    },

    // --- Step 2: Finish setup (auto-generate credentials) ------------------
    async finishSetup() {
      this.loading = true
      try {
        const data = await api()!.post('/_panel/api/onboarding/access-keys', {})
        if (data === undefined) {
          this.loading = false
          return
        }
        this._service.send({
          type: Event.CredentialsGenerated,
          accessKey: data.access_key || '',
          secretKey: data.secret_key || '',
        })
      } catch (e: any) {
        this.loading = false
        toast(e.message, 'error')
      }
    },

    goToDashboard() {
      this._service.send(Event.GoToDashboard)
    },

    // --- Reset onboarding ---------------------------------------------------
    async resetOnboarding() {
      this.loading = true
      try {
        await api()!.post('/_panel/api/onboarding/reset', {})
        // Reload the page to get a fresh Alpine state + FSM
        window.location.reload()
      } catch (e: any) {
        this.loading = false
        toast(e.message, 'error')
      }
    },

    // --- Clipboard helpers --------------------------------------------------
    copyToClipboard(text: string, field?: string) {
      navigator.clipboard
        .writeText(text)
        .then(() => {
          if (field) {
            const r = (this as any)._reactive || this
            r.copiedField = field
            setTimeout(() => { r.copiedField = '' }, 2000)
          }
          toast('Copied to clipboard', 'success')
        })
        .catch(() => {
          toast('Failed to copy', 'error')
        })
    },

    copyAllCredentials() {
      const text = `Access Key: ${this.generatedCredentials.accessKey}\nSecret Key: ${this.generatedCredentials.secretKey}`
      this.copyToClipboard(text)
    },
  }

  // Create the robot3 service — must be after `alpine` is defined so the
  // onChange callback can safely reference it (no TDZ).
  // We write through `alpine._reactive` (Alpine's reactive proxy, captured
  // in init()) so Alpine's directives detect the changes. Writing to the
  // raw `alpine` object bypasses Alpine's reactivity.
  const service = interpret(onboardingMachine, (svc: any) => {
    const fs = svc.machine.current
    const ctx = svc.context as OnboardingContext
    const r = (alpine as any)._reactive || alpine // fallback to raw before init
    r.currentStep = STEP_MAP[fs] ?? 0
    r.loading = ctx.loading
    r.siaStep = ctx.siaStep
    r.recoveryPhrase = ctx.recoveryPhrase
    r.phraseSaved = ctx.phraseSaved
    r.showConfirmKey = ctx.showConfirmKey
    r.generatedCredentials = ctx.generatedCredentials
    if (fs === FsmState.Complete) {
      window.location.href = '/_panel/dashboard'
    }
  })

  // Expose service so Alpine methods can call this._service.send()
  ;(alpine as any)._service = service

  return alpine
}
