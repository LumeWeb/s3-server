// Alpine component: monitoring stats + account info
import { apiReady } from '../globals'
import { fmtBytes, accountPct } from '../utils'

export function monitoringApp(this: AlpineMagic) {
  return {
    pending_objects: 0,
    pending_size: '0 B',
    pending_size_bytes: 0,
    uploaded_objects: 0,
    uploaded_size: '0 B',
    failed_uploads: 0,
    orphaned_objects: 0,
    multipart_uploads: 0,
    unpinned_objects: 0,

    // account fields: null until first SSE update
    account_ready: null as boolean | null,
    account_max_pinned_data: 0,
    account_remaining_storage: 0,
    account_pinned_data: 0,
    account_pinned_size: 0,

    lastRefresh: '',
    error: '',
    showPromGuide: false,

    get promScrapeConfig() {
      const host = window.location.hostname
      const port = window.location.port || (window.location.protocol === 'https:' ? '443' : '80')
      const scheme = window.location.protocol === 'https:' ? 'https' : 'http'
      return `scrape_configs:
  - job_name: "s3-server"
    scheme: ${scheme}
    basic_auth:
      username: admin
      password: <your-admin-password>
    static_configs:
      - targets: ["${host}:${port}"]
    metrics_path: /prometheus`
    },

    get loading() {
      return !this.lastRefresh && !this.error
    },

    async init() {
      // Don't block forever if backend isn't ready
      await Promise.race([
        apiReady(),
        new Promise((_, reject) => setTimeout(() => reject(new Error('Backend not ready')), 5000)),
      ]).catch(() => {})
      // listen for real-time SSE stats events (sole data channel)
      window.addEventListener('sse:stats', (e: any) => {
        try {
          this.applyStats(e.detail)
          this.error = ''
          this.lastRefresh = new Date().toLocaleTimeString()
        } catch (err) {}
      })
      // SSE is the sole source: the server publishes every 5s.
      // First render happens on the next SSE stats push (within 5s).
    },

    applyStats(data: any) {
      this.pending_objects = data.pending_objects ?? 0
      this.pending_size = fmtBytes(data.pending_size ?? 0)
      this.pending_size_bytes = data.pending_size ?? 0
      this.uploaded_objects = data.uploaded_objects ?? 0
      this.uploaded_size = fmtBytes(data.uploaded_size ?? 0)
      this.failed_uploads = data.failed_uploads ?? 0
      this.orphaned_objects = data.orphaned_objects ?? 0
      this.multipart_uploads = data.multipart_uploads ?? 0
      this.unpinned_objects = data.unpinned_objects ?? 0

      this.account_ready = data.account_ready ?? false
      this.account_max_pinned_data = data.account_max_pinned_data ?? 0
      this.account_remaining_storage = data.account_remaining_storage ?? 0
      this.account_pinned_data = data.account_pinned_data ?? 0
      this.account_pinned_size = data.account_pinned_size ?? 0
    },

    fmtBytes(bytes: number) {
      return fmtBytes(bytes)
    },

    // Alias used by the account section in templ (name must match the
    // x-text / :class expressions in monitoring.templ).
    fmtAccountBytes(bytes: number) {
      return fmtBytes(bytes)
    },

    accountPct() {
      return accountPct(this.account_pinned_size, this.account_max_pinned_data)
    },
  }
}
