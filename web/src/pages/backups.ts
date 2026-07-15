// Alpine component: backups page
import { api, reloadAfter, toast } from '../globals'

export function backupsApp(this: AlpineMagic) {
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
        await api()!.del(`/_panel/api/backups/${filename}`)
        toast('Backup deleted', 'success')
        reloadAfter()
      } catch (e: any) {
        toast(e.message, 'error')
      } finally {
        this.deleting = ''
      }
    },
  }
}
