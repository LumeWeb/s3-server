// Global confirm dialog — intercepts htmx:confirm to replace native confirm()
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function initConfirmDialog(Alpine: any) {
  const dialog = document.getElementById('confirm-dialog')
  if (!dialog) return

  document.body.addEventListener('htmx:confirm', (e: Event) => {
    const detail = (e as any).detail
    if (!detail?.question) return

    // Prevent the default native confirm()
    e.preventDefault()

    // Determine variant from the triggering element's data-confirm-variant
    const elt = detail.elt as HTMLElement | undefined
    const variant = elt?.getAttribute('data-confirm-variant') || 'warning'

    // Derive a title from data-confirm-title, or infer from variant
    const title = elt?.getAttribute('data-confirm-title')
      || (variant === 'danger' ? 'Confirm Deletion'
        : variant === 'info' ? 'Please Confirm'
        : 'Confirm')

    const alpine = Alpine.$data(dialog)
    alpine.title = title
    alpine.message = detail.question
    alpine.detail = ''
    alpine.variant = variant
    alpine.show = true

    // Resolve the promise when the user clicks Confirm/Cancel
    alpine.resolve = (confirmed: boolean) => {
      if (confirmed) {
        detail.issueRequest(true)
      }
      alpine.resolve = null
    }
  }, { capture: true })
}
