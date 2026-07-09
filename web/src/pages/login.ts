// Login page — Alpine component registered as global for x-data binding.
// Validation errors use toast (per UX rules); critical failures use modal.

import type { Alpine } from 'alpinejs'
import { toast } from '../globals'

export function initLogin(Alpine: Alpine) {
  Alpine.data('loginForm', () => ({
    loading: false,

    async submit(e: SubmitEvent) {
      e.preventDefault()
      const form = e.target as HTMLFormElement
      const formData = new FormData(form)

      this.loading = true
      try {
        const resp = await fetch('/_panel/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
          body: new URLSearchParams(formData as any).toString(),
        })

        if (resp.ok) {
          window.location.href = '/_panel'
          return
        }

        if (resp.status === 401) {
          toast('Incorrect password', 'error')
        } else {
          toast('Login failed — please try again', 'error')
        }
      } catch {
        toast('Network error — check your connection', 'error')
      }
      this.loading = false
    },
  }))
}
