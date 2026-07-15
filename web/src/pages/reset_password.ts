// Alpine component: reset password page
// Uses form POST (like login) — the Go handler returns 302 on success,
// 401/403 on failure. No __api or handleReq needed for pre-auth flows.
import { toast } from '../globals'
import { validatePasswordMatch } from '../utils'

export function resetPasswordForm(this: AlpineMagic) {
  return {
    loading: false,
    showPw: false,
    newPassword: '',
    confirmPassword: '',

    async submit(e: SubmitEvent) {
      e.preventDefault()
      const err = validatePasswordMatch(this.newPassword, this.confirmPassword)
      if (err) {
        toast(err, 'error')
        return
      }
      this.loading = true
      try {
        const form = e.target as HTMLFormElement
        const formData = new FormData(form)
        formData.set('new_password', this.newPassword)
        const csrfInput = form.querySelector('input[name="_csrf"]') as HTMLInputElement | null
        if (csrfInput?.value) {
          formData.set('_csrf', csrfInput.value)
        }

        const resp = await fetch('/_panel/reset-password', {
          method: 'POST',
          headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
          body: new URLSearchParams(formData as any).toString(),
        })

        if (resp.ok) {
          toast('Password reset successfully', 'success')
          setTimeout(() => { window.location.href = '/_panel/login' }, 1500)
          return
        }

        if (resp.status === 401) {
          toast('Password reset token is invalid or expired', 'error')
        } else {
          toast('Password reset failed: please try again', 'error')
        }
      } catch {
        toast('Network error: check your connection', 'error')
      }
      this.loading = false
    },
  }
}
