import { DIALOG_TITLES } from './api'

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function initErrorDialog(Alpine: any) {
  const dialog = document.getElementById('error-dialog')
  if (!dialog) return

  window.addEventListener('__api:error', (e: Event) => {
    const alpine = Alpine.$data(dialog)
    const d = (e as CustomEvent).detail || {}
    const code = d.code || ''
    const msg = d.message || 'An unexpected error occurred.'
    const raw = d.raw || ''
    alpine.title = DIALOG_TITLES[code] || 'Error'
    alpine.message = msg
    alpine.detail = (raw && raw !== msg) ? raw : ''
    alpine.show = true
  })
}
