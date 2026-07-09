// Alpine component: bucket lifecycle management
// eslint-disable-next-line @typescript-eslint/no-explicit-any
import { api, apiAction } from '../globals'

export function bucketPage(this: any) {
  return {
    showCreate: false,
    createName: '',
    createErr: '',
    creating: false,

    showFlush: false,
    flushBucketName: '',
    flushing: false,

    showLifecycle: false,
    lifecycleBucket: '',
    lifecycleRules: [] as { prefix: string; expiration_days: number; status: string }[],
    lifecycleLoading: false,
    lifecycleSaving: false,
    lifecycleErr: '',

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    async openLifecycle(name: string) {
      this.lifecycleBucket = name
      this.lifecycleRules = []
      this.lifecycleErr = ''
      this.lifecycleLoading = true
      this.showLifecycle = true
      try {
        const cfg = await api()!.get('/_panel/api/buckets/' + encodeURIComponent(name) + '/lifecycle')
        this.lifecycleRules = (cfg?.rules || []).map((r: any) => ({
          prefix: r.prefix || '',
          expiration_days: r.expiration_days || 30,
          status: r.status || 'Enabled',
        }))
      } catch (e: any) {
        this.lifecycleErr = e.message
      } finally {
        this.lifecycleLoading = false
      }
    },

    async saveLifecycle() {
      this.lifecycleErr = ''
      this.lifecycleSaving = true
      try {
        await apiAction(
          'Saving lifecycle rules…',
          () => api()!.put(
            '/_panel/api/buckets/' + encodeURIComponent(this.lifecycleBucket) + '/lifecycle',
            { rules: this.lifecycleRules },
          ),
          'Lifecycle rules saved',
        )
        this.showLifecycle = false
      } catch {
        // toast already shown by apiAction
      } finally {
        this.lifecycleSaving = false
      }
    },

    async deleteLifecycle() {
      this.lifecycleErr = ''
      this.lifecycleSaving = true
      try {
        await apiAction(
          'Deleting lifecycle rules…',
          () => api()!.del(
            '/_panel/api/buckets/' + encodeURIComponent(this.lifecycleBucket) + '/lifecycle',
          ),
          'Lifecycle rules deleted',
        )
        this.lifecycleRules = []
        this.showLifecycle = false
      } catch {
        // toast already shown by apiAction
      } finally {
        this.lifecycleSaving = false
      }
    },

    showVersioning: false,
    versioningBucket: '',
    versioningNewStatus: '',

    toggleVersioning(name: string, currentStatus: string) {
      this.versioningBucket = name
      this.versioningNewStatus = currentStatus === 'Enabled' ? 'Suspended' : 'Enabled'
      this.showVersioning = true
    },

    async confirmVersioning() {
      const name = this.versioningBucket
      const newStatus = this.versioningNewStatus
      this.showVersioning = false
      try {
        await apiAction(
          'Versioning ' + (newStatus === 'Enabled' ? 'enabling' : 'suspending') + '…',
          () => api()!.put(
            '/_panel/api/buckets/' + encodeURIComponent(name) + '/versioning',
            { status: newStatus },
          ),
          'Versioning ' + (newStatus === 'Enabled' ? 'enabled' : 'suspended') + ' for ' + name,
        )
        setTimeout(() => window.location.reload(), 800)
      } catch {
        // toast already shown by apiAction
      }
    },

    openFlush(name: string) {
      this.flushBucketName = name
      this.showFlush = true
    },

    async confirmFlush() {
      this.showFlush = false
      try {
        await apiAction(
          'Flushing "' + this.flushBucketName + '"…',
          () => api()!.post(
            '/_panel/api/buckets/' + encodeURIComponent(this.flushBucketName) + '/flush',
          ),
          'Bucket "' + this.flushBucketName + '" flushed',
        )
      } catch {
        // toast already shown by apiAction
      }
    },

    async createBucket() {
      this.creating = true
      this.createErr = ''
      try {
        await apiAction(
          'Creating bucket…',
          () => api()!.post('/_panel/api/buckets', { name: this.createName }),
          'Bucket "' + this.createName + '" created',
        )
        this.showCreate = false
      } catch (e: any) {
        this.createErr = e.message
      } finally {
        this.creating = false
      }
    },
  }
}
