// Alpine component: settings page
// Server config is passed via <script type="application/json" id="settings-data">
/* eslint-disable @typescript-eslint/no-explicit-any */

import { api, apiAction, reloadAfter, toast } from '../globals'

interface SettingsData {
  s3: {
    directory: string
    indexer_url: string
    available_indexers: string[]
    indexer_selection: string
    custom_indexer: string
    host_bases_input: string
    disk_usage_limit: number
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
}

function readSettingsData(): SettingsData {
  const el = document.getElementById('settings-data')
  if (!el || !el.textContent) {
    return {
      s3: { directory: '', indexer_url: '', available_indexers: [], indexer_selection: '', custom_indexer: '', host_bases_input: '', disk_usage_limit: 0, upload_waste_pct: 0.1 },
      ssl: { mode: '', acme_email: '', acme_dir_url: '' },
      log: { level: 'info', format: 'json' },
    }
  }
  return JSON.parse(el.textContent)
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function settingsApp(this: any) {
  const data = readSettingsData()
  return {
    s3: data.s3,
    ssl: data.ssl,
    log: data.log,
    s3Saving: false,
    sslSaving: false,
    logSaving: false,

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
      const hostBases = this.s3.host_bases_input.split(',').map((s: string) => s.trim()).filter((s: string) => s.length > 0)
      const indexerURL = this.s3.indexer_selection === '__custom__' ? this.s3.custom_indexer : this.s3.indexer_selection
      this.saveConfig('s3Saving', 'S3', '/_panel/api/s3-config', {
        directory: this.s3.directory,
        indexer_url: indexerURL,
        host_bases: hostBases,
        disk_usage_limit: this.s3.disk_usage_limit,
        upload_waste_pct: this.s3.upload_waste_pct,
      })
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

function readUpdateData(): UpdateData {
  const el = document.getElementById('settings-data')
  if (!el || !el.textContent) {
    return { sidecar_mode: false, auto_update_on: false, last_digest: '', updater_log: [] }
  }
  const parsed = JSON.parse(el.textContent)
  return parsed.update || { sidecar_mode: false, auto_update_on: false, last_digest: '', updater_log: [] }
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function updateControlsApp(this: any) {
  return {
    sidecarMode: false,
    autoUpdateOn: false,
    lastDigest: '',
    updaterLog: [] as string[],
    toggleLoading: false,
    triggerLoading: false,

    init() {
      const data = readUpdateData()
      this.sidecarMode = data.sidecar_mode
      this.autoUpdateOn = data.auto_update_on
      this.lastDigest = data.last_digest
      this.updaterLog = data.updater_log || []
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

// Flush all buckets sub-component
export function flushAllApp(this: any) {
  return {
    flushing: false,
    showFlush: false,

    async confirmFlush() {
      try {
        await apiAction(
          'Flushing all buckets\u2026',
          () => api()!.post('/_panel/api/system/flush'),
          'Buckets flushed',
        )
      } finally {
        this.flushing = false
      }
    },
  }
}

// Restart backend sub-component
export function restartApp(this: any) {
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
export function changePasswordApp(this: any) {
  return {
    showChangePw: false,
    loading: false,
    showPw: false,
    currentPw: '',
    newPw: '',
    confirmPw: '',

    async changePassword() {
      if (this.newPw !== this.confirmPw) {
        toast('Passwords do not match', 'error')
        return
      }
      if (this.newPw.length < 8) {
        toast('Password must be at least 8 characters', 'error')
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
export function versionCheckApp(this: any) {
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

// Nav flush sub-component (layout-level)
export function navFlushApp(this: any) {
  return {
    showFlush: false,
    navOpen: false,

    async confirmFlush() {
      try {
        await apiAction(
          'Flushing pending objects\u2026',
          () => api()!.post('/_panel/api/system/flush'),
          'All pending objects flushed to the network',
        )
      } catch {
        // toast already shown
      }
    },
  }
}
