// Alpine component: settings page
// Server config is passed via <script type="application/json" id="settings-data">
/* eslint-disable @typescript-eslint/no-explicit-any */

import { api, apiAction } from '../globals'

interface SettingsData {
  s3: {
    directory: string
    indexer_url: string
    available_indexers: string[]
    indexer_selection: string
    custom_indexer: string
    host_bases_input: string
  }
  ssl: {
    mode: string
    acme_email: string
    acme_dir_url: string
  }
}

function readSettingsData(): SettingsData {
  const el = document.getElementById('settings-data')
  if (!el || !el.textContent) {
    return {
      s3: { directory: '', indexer_url: '', available_indexers: [], indexer_selection: '', custom_indexer: '', host_bases_input: '' },
      ssl: { mode: '', acme_email: '', acme_dir_url: '' },
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
    s3Saving: false,
    sslSaving: false,

    async saveS3Config() {
      this.s3Saving = true
      try {
        const hostBases = this.s3.host_bases_input.split(',').map((s: string) => s.trim()).filter((s: string) => s.length > 0)
        const indexerURL = this.s3.indexer_selection === '__custom__' ? this.s3.custom_indexer : this.s3.indexer_selection
        await apiAction(
          'Saving S3 config…',
          () => api()!.put('/_panel/api/s3-config', {
            directory: this.s3.directory,
            indexer_url: indexerURL,
            available_indexers: this.s3.available_indexers,
            host_bases: hostBases,
          }),
          'Saved. Backend restarting.',
        )
      } catch {
        // toast already shown by apiAction
      } finally {
        this.s3Saving = false
      }
    },

    async saveSSLConfig() {
      this.sslSaving = true
      try {
        await apiAction(
          'Saving SSL config…',
          () => api()!.put('/_panel/api/ssl-config', this.ssl),
          'Saved. Backend restarting.',
        )
      } catch {
        // toast already shown by apiAction
      } finally {
        this.sslSaving = false
      }
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
          'Triggering update…',
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
