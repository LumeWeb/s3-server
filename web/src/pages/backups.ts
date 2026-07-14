// Alpine component: backups page
// eslint-disable-next-line @typescript-eslint/no-explicit-any
import { api, reloadAfter, toast } from '../globals'

export function backupsApp(this: any) {
  return {
    backing: false,
    deleting: '' as string,
    backupErr: '',

    async backupNow() {
      this.backing = true
      this.backupErr = ''
      try {
        await api()!.post('/_panel/api/backups')
        reloadAfter()
      } catch (e: any) {
        this.backupErr = e.message
      } finally {
        this.backing = false
      }
    },

    async deleteBackup(filename: string) {
      this.deleting = filename
      try {
        await api()!.delete(`/_panel/api/backups/${filename}`)
        toast('Backup deleted', 'success')
        reloadAfter(1500)
      } catch (e: any) {
        toast(e.message, 'error')
      } finally {
        this.deleting = ''
      }
    },
  }
}
