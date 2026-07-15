// Alpine component: bucket lifecycle management
import { api, apiAction, reloadAfter } from '../globals'
import { nextVersioningStatus, bucketLifecycleURL, bucketVersioningURL, bucketFlushURL, normalizeLifecycleRules } from '../utils'

interface BucketComponent extends AlpineMagic {
  showCreate: boolean
  createName: string
  createOwner: string
  createErr: string
  creating: boolean
  showFlush: boolean
  flushBucketName: string
  flushing: boolean
  showLifecycle: boolean
  lifecycleBucket: string
  lifecycleRules: { prefix: string; expiration_days: number; status: string }[]
  lifecycleLoading: boolean
  lifecycleSaving: boolean
  lifecycleErr: string
  showVersioning: boolean
  versioningBucket: string
  versioningNewStatus: string
  init(): void
  openLifecycle(name: string): Promise<void>
  saveLifecycle(): Promise<void>
  deleteLifecycle(): Promise<void>
  toggleVersioning(name: string, currentStatus: string): void
  confirmVersioning(): Promise<void>
  openFlush(name: string): void
  confirmFlush(): Promise<void>
  createBucket(): Promise<void>
}

export function bucketPage(this: BucketComponent) {
  return {
    showCreate: false,
    createName: '',
    createOwner: '',
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

    init() {
      const self = this as BucketComponent
      self.$watch('showCreate', (v: boolean) => {
        if (!v) {
          self.createName = ''
          self.createOwner = ''
          self.createErr = ''
          self.creating = false
        }
      })
      self.$watch('showLifecycle', (v: boolean) => {
        if (!v) {
          self.lifecycleBucket = ''
          self.lifecycleRules = []
          self.lifecycleLoading = false
          self.lifecycleSaving = false
          self.lifecycleErr = ''
        }
      })
    },

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    async openLifecycle(name: string) {
      this.lifecycleBucket = name
      this.lifecycleRules = []
      this.lifecycleErr = ''
      this.lifecycleLoading = true
      this.showLifecycle = true
      try {
        const cfg = await api()!.get(bucketLifecycleURL(name, 'get'))
        this.lifecycleRules = normalizeLifecycleRules(cfg?.rules)
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
            bucketLifecycleURL(this.lifecycleBucket, 'put'),
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
            bucketLifecycleURL(this.lifecycleBucket, 'delete'),
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
      this.versioningNewStatus = nextVersioningStatus(currentStatus)
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
            bucketVersioningURL(name),
            { status: newStatus },
          ),
          'Versioning ' + (newStatus === 'Enabled' ? 'enabled' : 'suspended') + ' for ' + name,
        )
        reloadAfter()
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
            bucketFlushURL(this.flushBucketName),
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
        const res = await apiAction(
          'Creating bucket…',
          () => api()!.post('/_panel/api/buckets', { name: this.createName, owner: this.createOwner }),
          'Bucket "' + this.createName + '" created',
        )
        if (res === undefined) return
        this.showCreate = false
        reloadAfter()
      } catch (e: any) {
        this.createErr = e.message
      } finally {
        this.creating = false
      }
    },
  }
}
