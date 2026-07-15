// Alpine component: keys page
import { api, apiAction, reloadAfter } from '../globals'

export function keysApp(this: AlpineMagic, props: { backendRunning: boolean } = { backendRunning: false }) {
  return {
    showDelete: false,
    deleteAccessKey: '',
    generating: {} as Record<string, boolean>,
    backendRunning: props.backendRunning,

    async generateKey(userName: string) {
      this.generating[userName] = true
      try {
        await apiAction(
          'Generating key\u2026',
          () => api()!.post('/_panel/api/keys', { user_name: userName }),
          'Access key generated',
        )
        reloadAfter()
      } catch {
        this.generating[userName] = false
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
