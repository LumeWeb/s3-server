// Alpine component: settings page
// Server config is passed via <script type="application/json" id="settings-data">
/* eslint-disable @typescript-eslint/no-explicit-any */

import { api, apiAction, reloadAfter, toast } from '../globals'
import { buildS3ConfigPayload, readJSONScript, validatePasswordMatch } from '../utils'

export const DiskLimitMode = {
  AUTO: 'auto',
  UNLIMITED: 'unlimited',
  CUSTOM: 'custom',
} as const

export type DiskLimitModeType = (typeof DiskLimitMode)[keyof typeof DiskLimitMode]

interface SettingsData {
  s3: {
    directory: string
    indexer_url: string
    available_indexers: string[]
    indexer_selection: string
    custom_indexer: string
    host_bases_input: string
    disk_usage_limit: string
    disk_usage_limit_mode: DiskLimitModeType
    disk_usage_limit_custom: number
    upload_waste_pct: number
  }
  ssl: {
    mode: string
    acme_email: string
    acme_dir_url: string
  }
  log: {
    level: string
    format: string
  }
  update?: {
    sidecar_mode: boolean
    auto_update_on: boolean
    last_digest: string
    updater_log: string[]
  }
}

const fallbackSettings: SettingsData = {
  s3: { directory: '', indexer_url: '', available_indexers: [], indexer_selection: '', custom_indexer: '', host_bases_input: '', disk_usage_limit: DiskLimitMode.AUTO, disk_usage_limit_mode: DiskLimitMode.AUTO, disk_usage_limit_custom: 100, upload_waste_pct: 0.1 },
  ssl: { mode: '', acme_email: '', acme_dir_url: '' },
  log: { level: 'info', format: 'json' },
}

const fallbackUpdate = { sidecar_mode: false, auto_update_on: false, last_digest: '', updater_log: [] }

export function settingsApp(this: AlpineMagic) {
  const data = readJSONScript<SettingsData>('settings-data', fallbackSettings)

  // Normalize the disk-limit UI state from the persisted string value.
  const limit = String(data.s3.disk_usage_limit ?? DiskLimitMode.AUTO)
  let mode: DiskLimitModeType = DiskLimitMode.AUTO
  let custom = 100
  if (limit === DiskLimitMode.AUTO) {
    mode = DiskLimitMode.AUTO
  } else if (limit === '0') {
    mode = DiskLimitMode.UNLIMITED
  } else {
    mode = DiskLimitMode.CUSTOM
    custom = Number(limit) || 100
  }
  data.s3.disk_usage_limit = limit
  data.s3.disk_usage_limit_mode = mode
  data.s3.disk_usage_limit_custom = custom

  return {
    s3: data.s3,
    ssl: data.ssl,
    log: data.log,
    s3Saving: false,
    sslSaving: false,
    logSaving: false,

    updateDiskLimitMode() {
      const mode = (this as any).s3.disk_usage_limit_mode
      if (mode === DiskLimitMode.AUTO) {
        ;(this as any).s3.disk_usage_limit = DiskLimitMode.AUTO
      } else if (mode === DiskLimitMode.UNLIMITED) {
        ;(this as any).s3.disk_usage_limit = '0'
      } else if (mode === DiskLimitMode.CUSTOM) {
        const custom = Number((this as any).s3.disk_usage_limit_custom)
        if (!custom || custom < 1) {
          // avoid emitting "0" which the backend treats as unlimited
          ;(this as any).s3.disk_usage_limit = DiskLimitMode.AUTO
        } else {
          ;(this as any).s3.disk_usage_limit = String(custom)
        }
      }
    },

    updateDiskLimitCustom() {
      if ((this as any).s3.disk_usage_limit_mode === DiskLimitMode.CUSTOM) {
        const custom = Number((this as any).s3.disk_usage_limit_custom)
        if (!custom || custom < 1) {
          // avoid emitting "0" which the backend treats as unlimited
          ;(this as any).s3.disk_usage_limit = DiskLimitMode.AUTO
        } else {
          ;(this as any).s3.disk_usage_limit = String(custom)
        }
      }
    },

    async saveConfig(
      flag: 's3Saving' | 'sslSaving' | 'logSaving',
      label: string,
      url: string,
      payload: any,
    ) {
      (this as any)[flag] = true
      try {
        await apiAction(
          `Saving ${label} config…`,
          () => api()!.put(url, payload),
          'Saved. Backend restart required.',
        )
      } catch {
        // toast already shown by apiAction
      } finally {
        (this as any)[flag] = false
      }
    },

    async saveS3Config() {
      const payload = buildS3ConfigPayload(this.s3)
      this.saveConfig('s3Saving', 'S3', '/_panel/api/s3-config', payload)
    },

    async saveSSLConfig() {
      this.saveConfig('sslSaving', 'SSL', '/_panel/api/ssl-config', this.ssl)
    },

    async saveLogConfig() {
      this.saveConfig('logSaving', 'log', '/_panel/api/log-config', this.log)
    },
  }
}

interface UpdateData {
  sidecar_mode: boolean
  auto_update_on: boolean
  last_digest: string
  updater_log: string[]
}

export function updateControlsApp(this: AlpineMagic) {
  return {
    sidecarMode: false,
    autoUpdateOn: false,
    lastDigest: '',
    updaterLog: [] as string[],
    toggleLoading: false,
    triggerLoading: false,

    init() {
      const data = readJSONScript<SettingsData>('settings-data', fallbackSettings)
      const update = (data.update || fallbackUpdate) as UpdateData
      this.sidecarMode = update.sidecar_mode
      this.autoUpdateOn = update.auto_update_on
      this.lastDigest = update.last_digest
      this.updaterLog = update.updater_log || []
    },

    async toggleAutoUpdate() {
      this.toggleLoading = true
      const newEnabled = !this.autoUpdateOn
      try {
        await apiAction(
          'Toggling auto-update…',
          () => api()!.post('/_panel/api/update/toggle', { enabled: newEnabled }),
          'Auto-update ' + (newEnabled ? 'enabled' : 'disabled'),
        )
        this.autoUpdateOn = newEnabled
      } catch {
        // toast already shown by apiAction
      } finally {
        this.toggleLoading = false
      }
    },

    async triggerUpdate() {
      this.triggerLoading = true
      try {
        await apiAction(
          'Triggering update\u2026',
          () => api()!.post('/_panel/api/update/trigger'),
          'Update triggered',
        )
      } catch {
        // toast already shown by apiAction
      } finally {
        this.triggerLoading = false
      }
    },
  }
}

// Flush all buckets sub-component (settings page only).
// Does NOT poll or show toasts — navFlushApp (always mounted) owns the
// single poller and all user-facing notifications. This component only
// tracks the flushing flag for button disabled state via SSE + one-time
// sync on init.
export function flushAllApp(this: AlpineMagic) {
  return {
    flushing: false,
    showFlushAll: false,
    onFlush: null as ((e: any) => void) | null,

    init() {
      this.onFlush = (e: any) => {
        const data = e.detail
        if (data.status === 'running') {
          this.flushing = true
        } else if (data.status === 'complete' || data.status === 'error') {
          this.flushing = false
        }
      }
      window.addEventListener('sse:flush', this.onFlush)
      this.syncFlushStatus()
    },

    destroy() {
      if (this.onFlush) {
        window.removeEventListener('sse:flush', this.onFlush)
        this.onFlush = null
      }
    },

    async syncFlushStatus() {
      try {
        const res = await api()!.get('/_panel/api/system/flush')
        this.flushing = res?.status === 'running'
      } catch {
        // ignore — SSE will handle updates
      }
    },

    async confirmFlush() {
      this.flushing = true
      const res = await api()!.post('/_panel/api/system/flush')
      if (res === undefined) {
        // handleReq already showed an error toast.
        // Reset and let sync check if a flush is still running (e.g., 409).
        this.flushing = false
        await this.syncFlushStatus()
      }
    },
  }
}

// Restart backend sub-component
export function restartApp(this: AlpineMagic) {
  return {
    restarting: false,
    showRestart: false,

    async confirmRestart() {
      try {
        await apiAction(
          'Restarting backend\u2026',
          () => api()!.post('/_panel/api/system/restart'),
          'Backend restart initiated',
        )
      } finally {
        this.restarting = false
      }
    },
  }
}

// Change password sub-component
export function changePasswordApp(this: AlpineMagic) {
  return {
    showChangePw: false,
    loading: false,
    showPw: false,
    currentPw: '',
    newPw: '',
    confirmPw: '',

    async changePassword() {
      const err = validatePasswordMatch(this.newPw, this.confirmPw)
      if (err) {
        toast(err, 'error')
        return
      }
      this.loading = true
      try {
        await apiAction(
          'Changing password\u2026',
          () => api()!.post('/_panel/api/password/change', {
            current_password: this.currentPw,
            new_password: this.newPw,
          }),
          'Password changed successfully',
        )
        this.showChangePw = false
        this.currentPw = ''
        this.newPw = ''
        this.confirmPw = ''
      } catch {
        // toast already shown by apiAction
      }
      this.loading = false
    },
  }
}

// Version check sub-component (non-sidecar mode only)
export function versionCheckApp(this: AlpineMagic) {
  return {
    versionResult: null as any,
    versionLoading: true,
    versionErr: false,

    async init() {
      try {
        this.versionResult = await api()!.get('/_panel/api/version')
      } catch {
        this.versionErr = true
      } finally {
        this.versionLoading = false
      }
    },
  }
}

// Nav flush sub-component (layout-level, always mounted).
// Owns flush polling fallback and user-facing toasts.
export function navFlushApp(this: AlpineMagic) {
  return {
    showFlush: false,
    showSignOut: false,
    navOpen: false,
    flushing: false,
    flushPollTimer: null as ReturnType<typeof setInterval> | null,
    onFlush: null as ((e: any) => void) | null,

    init() {
      this.onFlush = (e: any) => this.handleFlushEvent(e.detail)
      window.addEventListener('sse:flush', this.onFlush)
      this.syncFlushStatus()
    },

    destroy() {
      if (this.onFlush) {
        window.removeEventListener('sse:flush', this.onFlush)
        this.onFlush = null
      }
      this.stopFlushPoll()
    },

    handleFlushEvent(data: any) {
      if (data.status === 'running') {
        this.flushing = true
        this.startFlushPoll()
      } else if (data.status === 'complete') {
        this.flushing = false
        this.stopFlushPoll()
        toast('All pending objects flushed to the network', 'success')
      } else if (data.status === 'error') {
        this.flushing = false
        this.stopFlushPoll()
        toast(data.message || 'Flush failed', 'error')
      }
    },

    // syncFlushStatus is called on page load to recover state.
    // It must NOT toast on terminal statuses — those may be stale from a
    // previous flush. Only the poll callback and SSE handler toast, since
    // they only fire after we've observed a running→terminal transition.
    async syncFlushStatus() {
      try {
        const res = await api()!.get('/_panel/api/system/flush')
        if (res?.status === 'running') {
          this.flushing = true
          this.startFlushPoll()
        } else {
          this.flushing = false
        }
      } catch {
        // ignore
      }
    },

    startFlushPoll() {
      if (this.flushPollTimer) return
      this.flushPollTimer = setInterval(async () => {
        try {
          const res = await api()!.get('/_panel/api/system/flush')
          if (res?.status === 'complete' || res?.status === 'error' || res?.status === 'idle') {
            this.stopFlushPoll()
            this.flushing = false
            if (res.status === 'complete') {
              toast('All pending objects flushed to the network', 'success')
            } else if (res.status === 'error') {
              toast(res.message || 'Flush failed', 'error')
            }
          }
        } catch {
          // keep polling — transient network errors
        }
      }, 3000)
    },

    stopFlushPoll() {
      if (this.flushPollTimer) {
        clearInterval(this.flushPollTimer)
        this.flushPollTimer = null
      }
    },

    async confirmFlush() {
      this.flushing = true
      const res = await api()!.post('/_panel/api/system/flush')
      if (res !== undefined) {
        toast('Flush started. Monitor the monitoring page for progress.', 'info')
        this.startFlushPoll()
      } else {
        // handleReq already showed an error toast.
        // Reset and let sync check if a flush is still running (e.g., 409).
        this.flushing = false
        await this.syncFlushStatus()
      }
    },

    confirmSignOut() {
      const form = document.querySelector('#logout-form') as HTMLFormElement | null
      if (form) form.submit()
    },
  }
}
