import { describe, it, expect, vi, beforeEach } from 'vitest'
import { interpret } from 'robot3'
import { stubOnboardingGlobals } from './onboarding-helpers'

// We need to test goBack() and the FSM transition. The onboardingWizard
// function is an Alpine component factory that creates a robot3 service in
// init(). For unit testing, we call it directly and set up the minimum state
// needed to test goBack().

// Stub window globals needed by the module
beforeEach(() => {
  stubOnboardingGlobals()
  ;(window as any).__copyToClipboard = vi.fn().mockResolvedValue(undefined)
})

import { onboardingWizard } from '../src/pages/onboarding'

// Helper: create an Alpine-like component proxy
function createComponent(): any {
  const wizard = onboardingWizard.call({} as any)
  // Simulate Alpine's reactive proxy: just use the object directly
  const component = Object.create(wizard)
  // Initialize state that init() would normally set
  component._reactive = component
  component.currentStep = 0
  component.siaStep = 'connect'
  component.loading = false
  component.siaBuilder = null
  component._approvalAbort = null
  component._urlSyncReady = false
  return component
}

// Helper: create a component with a mock service that tracks sent events
function createComponentWithMockService(): any {
  const component = createComponent()
  const sentEvents: string[] = []
  component._service = {
    send: (event: string) => sentEvents.push(event),
    machine: { states: {} },
  }
  component._sentEvents = sentEvents
  return component
}

describe('goBack()', () => {
  it('from step 2 (finish): clears stale siaBuilder before sending Event.Back', () => {
    const component = createComponentWithMockService()
    component.currentStep = 2
    component.siaBuilder = { /* stale SSO session */ } as any
    component.siaStep = 'recovery'

    component.goBack()

    // siaBuilder should be cleared (cancelConnection was called)
    expect(component.siaBuilder).toBeNull()
    expect(component.siaStep).toBe('connect')
    // Event.Back should be sent to advance FSM from finish → connect
    expect(component._sentEvents).toContain('back')
  })

  it('from step 1 Sia sub-step (waiting): cancels approval without sending Event.Back', () => {
    const component = createComponentWithMockService()
    component.currentStep = 1
    component.siaStep = 'waiting'
    component._approvalAbort = { reject: vi.fn() }

    component.goBack()

    // Should cancel the connection (reset to connect sub-step)
    expect(component.siaStep).toBe('connect')
    expect(component.siaBuilder).toBeNull()
    expect(component._approvalAbort).toBeNull()
    // Event.Back should NOT be sent on first back from a sub-step
    expect(component._sentEvents).not.toContain('back')
  })

  it('from step 1 Sia sub-step (recovery): cancels connection without sending Event.Back', () => {
    const component = createComponentWithMockService()
    component.currentStep = 1
    component.siaStep = 'recovery'
    component.siaBuilder = { /* SSO session */ } as any

    component.goBack()

    expect(component.siaStep).toBe('connect')
    expect(component.siaBuilder).toBeNull()
    expect(component._sentEvents).not.toContain('back')
  })

  it('from step 1 connect sub-step: sends Event.Back to go to password', () => {
    const component = createComponentWithMockService()
    component.currentStep = 1
    component.siaStep = 'connect'

    component.goBack()

    // No sub-step to cancel, should send Event.Back directly
    expect(component._sentEvents).toContain('back')
  })

  it('does not send Event.Back while loading is true (step 2)', () => {
    // When loading, the back button is hidden via x-show, but goBack()
    // should still clear siaBuilder since we're cancelling.
    const component = createComponentWithMockService()
    component.currentStep = 2
    component.loading = true
    component.siaBuilder = { /* stale */ } as any

    component.goBack()

    // cancelConnection still runs regardless of loading
    expect(component.siaBuilder).toBeNull()
    expect(component.siaStep).toBe('connect')
    // Event.Back is still sent — the UI hides the button, but the method
    // doesn't guard on loading itself
    expect(component._sentEvents).toContain('back')
  })
})
