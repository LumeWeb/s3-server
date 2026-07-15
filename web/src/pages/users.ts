// Alpine component: users page
import { api, apiAction, reloadAfter } from '../globals'

export function usersApp(this: AlpineMagic) {
  return {
    showCreate: false,
    createName: '',
    createErr: '',
    creating: false,
    showDelete: false,
    deleteName: '',

    async createUser() {
      this.creating = true
      this.createErr = ''
      try {
        await apiAction(
          'Creating user\u2026',
          () => api()!.post('/_panel/api/users', { name: this.createName }),
          'User created',
        )
        this.showCreate = false
        reloadAfter()
      } catch (e: any) {
        this.createErr = e.message
      } finally {
        this.creating = false
      }
    },

    async confirmDelete() {
      try {
        await apiAction(
          'Deleting user\u2026',
          () => api()!.del('/_panel/api/users/' + encodeURIComponent(this.deleteName)),
          'User deleted',
        )
        reloadAfter()
      } catch {
        // toast already shown
      }
    },
  }
}
