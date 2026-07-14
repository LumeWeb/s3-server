// Alpine component: reset password page
// eslint-disable-next-line @typescript-eslint/no-explicit-any
import { toast } from '../globals'

export function resetPasswordForm(this: any) {
  return {
    loading: false,
    showPw: false,
    newPassword: '',
    confirmPassword: '',

    async submit(e: SubmitEvent) {
      e.preventDefault()
      if (this.newPassword !== this.confirmPassword) {
        toast('Passwords do not match', 'error')
        return
      }
      if (this.newPassword.length < 8) {
        toast('Password must be at least 8 characters', 'error')
        return
      }
      this.loading = true
      try {
        const csrf = document.querySelector('input[name="_csrf"]') as HTMLInputElement | null
        const resp = await fetch('/_panel/api/password/reset', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrf?.value || '',
          },
          body: JSON.stringify({ new_password: this.newPassword }),
        })
        if (resp.ok) {
          toast('Password reset successfully', 'success')
          setTimeout(() => { window.location.href = '/_panel/login' }, 1500)
        } else {
          const data = await resp.json().catch(() => ({}))
          toast(data.message || 'Reset failed', 'error')
        }
      } catch {
        toast('Network error', 'error')
      }
      this.loading = false
    },
  }
}
