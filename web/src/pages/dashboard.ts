// Alpine component: dashboard status
import { S3Status } from './status'
import { api, apiReady, apiAction, reloadAfter } from '../globals'

export function dashboardApp(this: AlpineMagic) {
  return {
    s3Status: S3Status.Stopped,
    initError: '',
    get isRunning() { return this.s3Status === S3Status.Running },
    keyCount: 0,
    uptime: '0s',
    version: '',
    updateAvailable: false,
    latestVersion: '',
    generating: false,
    showDelete: false,
    deleteAccessKey: '',

    async init() {
      await apiReady()
      try {
        const data = await api()!.get('/_panel/api/status')
        this.s3Status = data.s3_status
        this.initError = data.init_error || ''
        this.keyCount = data.key_count
        this.version = data.version
      } catch (e) {}

      try {
        const data = await api()!.get('/_panel/api/version')
        this.updateAvailable = data.update_available
        this.latestVersion = data.latest_version
      } catch (e) {}

      // listen for SSE dashboard events dispatched by panel.ts
      window.addEventListener('sse:dashboard', (e: any) => {
        try {
          const data = e.detail
          if (data.s3_status) this.s3Status = data.s3_status
          if (data.init_error !== undefined) this.initError = data.init_error
          if (data.key_count !== undefined) this.keyCount = data.key_count
          if (data.uptime) this.uptime = data.uptime
          if (data.version) this.version = data.version
        } catch (err) {}
      })
    },

    async generateKey() {
      this.generating = true
      try {
        await apiAction(
          'Generating key\u2026',
          () => api()!.post('/_panel/api/keys', { user_name: 'default' }),
          'Access key generated',
        )
        reloadAfter()
      } catch {
        this.generating = false
      }
    },

    async confirmDelete() {
      try {
        await apiAction(
          'Deleting access key\u2026',
          () => api()!.del('/_panel/api/keys/' + encodeURIComponent(this.deleteAccessKey)),
          'Access key deleted',
        )
        reloadAfter()
      } catch {
        // toast already shown
      }
    },
  }
}
