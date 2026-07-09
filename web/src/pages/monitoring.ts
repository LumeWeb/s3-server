// Alpine component: monitoring stats
// eslint-disable-next-line @typescript-eslint/no-explicit-any
import { api, apiReady } from '../globals'

export function monitoringApp(this: any) {
  return {
    pending_objects: 0,
    pending_size: '0 B',
    uploaded_objects: 0,
    uploaded_size: '0 B',
    failed_uploads: 0,
    orphaned_objects: 0,
    multipart_uploads: 0,
    unpinned_objects: 0,
    lastRefresh: '',
    error: '',
    _pollTimer: null as ReturnType<typeof setInterval> | null,

    async init() {
      // Don't block forever if backend isn't ready
      await Promise.race([
        apiReady(),
        new Promise((_, reject) => setTimeout(() => reject(new Error('Backend not ready')), 5000)),
      ]).catch(() => {})
      // initial fetch
      await this.refresh()
      // listen for real-time SSE stats events
      window.addEventListener('sse:stats', (e: any) => {
        try {
          this.applyStats(e.detail)
          this.error = ''
          this.lastRefresh = new Date().toLocaleTimeString()
        } catch (err) {}
      })
      // fallback polling in case SSE drops
      this._pollTimer = setInterval(() => this.refresh(), 15000)
    },

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    applyStats(data: any) {
      this.pending_objects = data.pending_objects ?? 0
      this.pending_size = this.fmtBytes(data.pending_size ?? 0)
      this.uploaded_objects = data.uploaded_objects ?? 0
      this.uploaded_size = this.fmtBytes(data.uploaded_size ?? 0)
      this.failed_uploads = data.failed_uploads ?? 0
      this.orphaned_objects = data.orphaned_objects ?? 0
      this.multipart_uploads = data.multipart_uploads ?? 0
      this.unpinned_objects = data.unpinned_objects ?? 0
    },

    async refresh() {
      try {
        const data = await api()!.get('/_panel/api/admin/stats')
        this.applyStats(data)
        this.error = ''
        this.lastRefresh = new Date().toLocaleTimeString()
      } catch (e: any) {
        this.error = e.message || 'Failed to fetch stats'
      }
    },

    fmtBytes(b: number) {
      if (b < 1024) return b + ' B'
      const units = ['KB', 'MB', 'GB', 'TB', 'PB']
      let u = -1
      do { b /= 1024; u++ } while (b >= 1024 && u < units.length - 1)
      return b.toFixed(1) + ' ' + units[u]
    },
  }
}
