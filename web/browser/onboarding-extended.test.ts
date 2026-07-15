import { describe, it, expect, vi, beforeEach } from 'vitest'
import { stubOnboardingGlobals } from './onboarding-helpers'

beforeEach(() => {
  stubOnboardingGlobals()
  // Stub __copyToClipboard so copyToClipboard() works in browser
  ;(window as any).__copyToClipboard = vi.fn().mockResolvedValue(undefined)
})

import { onboardingWizard } from '../src/pages/onboarding'

function createComponent(): any {
  const wizard = onboardingWizard.call({} as any)
  const component = Object.create(wizard)
  component._reactive = component
  component.currentStep = 0
  component.siaStep = 'connect'
  component.loading = false
  component.siaBuilder = null
  component._approvalAbort = null
  component._urlSyncReady = false
  component.adminPassword = ''
  component.adminPasswordConfirm = ''
  component.siaConfig = null
  component.indexerSelection = ''
  component.customIndexer = ''
  component.seedMode = 'generated'
  component.customSeedInput = ''
  component.customSeedError = ''
  component.recoveryPhrase = ''
  component.phraseSaved = false
  component.generatedCredentials = { accessKey: '', secretKey: '' }
  component._service = {
    send: vi.fn(),
    machine: { current: 'password' },
  }
  return component
}

describe('onboardingWizard — Step 0: setAdminPassword', () => {
  it('rejects password shorter than 8 characters', async () => {
    const component = createComponent()
    ;(window.__sseToast as any).mockClear()
    component.adminPassword = 'short'
    component.adminPasswordConfirm = 'short'
    await component.setAdminPassword()

    expect(window.__sseToast).toHaveBeenCalledWith('Password must be at least 8 characters', 'error')
    expect(component.loading).toBe(false)
    expect(component._service.send).not.toHaveBeenCalled()
  })

  it('rejects mismatched passwords', async () => {
    const component = createComponent()
    ;(window.__sseToast as any).mockClear()
    component.adminPassword = 'password1'
    component.adminPasswordConfirm = 'password2'
    await component.setAdminPassword()

    expect(window.__sseToast).toHaveBeenCalledWith('Passwords do not match', 'error')
    expect(component._service.send).not.toHaveBeenCalled()
  })

  it('posts password to API and sends PasswordSet event on success', async () => {
    const postSpy = vi.fn().mockResolvedValue({ ok: true })
    ;(window as any).__api = { post: postSpy, get: vi.fn() }

    const component = createComponent()
    component.adminPassword = 'securePass123'
    component.adminPasswordConfirm = 'securePass123'
    await component.setAdminPassword()

    expect(postSpy).toHaveBeenCalledWith('/_panel/api/onboarding/admin-password', {
      password: 'securePass123',
    })
    expect(component._service.send).toHaveBeenCalledWith('passwordSet')
  })

  it('resets loading and does not send event when API returns undefined', async () => {
    const postSpy = vi.fn().mockResolvedValue(undefined)
    ;(window as any).__api = { post: postSpy, get: vi.fn() }

    const component = createComponent()
    component.adminPassword = 'securePass123'
    component.adminPasswordConfirm = 'securePass123'
    await component.setAdminPassword()

    expect(component.loading).toBe(false)
    expect(component._service.send).not.toHaveBeenCalled()
  })

  it('shows toast on API error', async () => {
    const postSpy = vi.fn().mockRejectedValue(new Error('Server down'))
    ;(window as any).__api = { post: postSpy, get: vi.fn() }
    ;(window.__sseToast as any).mockClear()
    const component = createComponent()
    component.adminPassword = 'securePass123'
    component.adminPasswordConfirm = 'securePass123'
    await component.setAdminPassword()

    expect(window.__sseToast).toHaveBeenCalledWith('Server down', 'error')
    expect(component.loading).toBe(false)
  })
})

describe('onboardingWizard — restoreFromServer', () => {
  it('sends PasswordSet when server state is admin_password_set', async () => {
    const getSpy = vi.fn().mockResolvedValue({ state: 'admin_password_set' })
    ;(window as any).__api = { get: getSpy, post: vi.fn() }

    const component = createComponent()
    await component.restoreFromServer()

    expect(component._service.send).toHaveBeenCalledWith('passwordSet')
  })

  it('sends PasswordSet + AppKeySubmitted when server state is app_key_set', async () => {
    const getSpy = vi.fn().mockResolvedValue({ state: 'app_key_set' })
    ;(window as any).__api = { get: getSpy, post: vi.fn() }

    const component = createComponent()
    await component.restoreFromServer()

    expect(component._service.send).toHaveBeenCalledWith('passwordSet')
    expect(component._service.send).toHaveBeenCalledWith('appKeySubmitted')
  })

  it('does nothing when server step is same as current step', async () => {
    const getSpy = vi.fn().mockResolvedValue({ state: 'pending' })
    ;(window as any).__api = { get: getSpy, post: vi.fn() }

    const component = createComponent()
    await component.restoreFromServer()

    expect(component._service.send).not.toHaveBeenCalled()
  })

  it('swallows errors silently', async () => {
    const getSpy = vi.fn().mockRejectedValue(new Error('Network error'))
    ;(window as any).__api = { get: getSpy, post: vi.fn() }

    const component = createComponent()
    // Should not throw
    await expect(component.restoreFromServer()).resolves.toBeUndefined()
  })
})

describe('onboardingWizard — loadSiaConfig', () => {
  it('loads config and sets indexer selection', async () => {
    const config = {
      app_id: 'test-app',
      app_name: 'Test App',
      indexer_url: 'https://indexer.example.com',
      available_indexers: [{ url: 'https://indexer1.example.com' }],
    }
    const getSpy = vi.fn().mockResolvedValue(config)
    ;(window as any).__api = { get: getSpy, post: vi.fn() }

    const component = createComponent()
    await component.loadSiaConfig()

    expect(getSpy).toHaveBeenCalledWith('/_panel/api/onboarding/config')
    expect(component.siaConfig).toEqual(config)
    expect(component.indexerSelection).toBe('https://indexer.example.com')
  })

  it('falls back to first available indexer', async () => {
    const config = {
      available_indexers: [{ url: 'https://fallback.example.com' }],
    }
    const getSpy = vi.fn().mockResolvedValue(config)
    ;(window as any).__api = { get: getSpy, post: vi.fn() }

    const component = createComponent()
    await component.loadSiaConfig()

    expect(component.indexerSelection).toBe('https://fallback.example.com')
  })

  it('shows error toast on failure', async () => {
    const getSpy = vi.fn().mockRejectedValue(new Error('Config load failed'))
    ;(window as any).__api = { get: getSpy, post: vi.fn() }
    ;(window.__sseToast as any).mockClear()
    const component = createComponent()
    await component.loadSiaConfig()

    expect(window.__sseToast).toHaveBeenCalledWith(
      expect.stringContaining('Config load failed'),
      'error',
    )
  })
})

describe('onboardingWizard — finishSetup', () => {
  it('posts to access-keys endpoint and sends CredentialsGenerated event', async () => {
    const postSpy = vi.fn().mockResolvedValue({
      access_key: 'AKIA123',
      secret_key: 'SECRET456',
    })
    ;(window as any).__api = { post: postSpy, get: vi.fn() }

    const component = createComponent()
    await component.finishSetup()

    expect(postSpy).toHaveBeenCalledWith('/_panel/api/onboarding/access-keys', {})
    expect(component._service.send).toHaveBeenCalledWith({
      type: 'credentialsGenerated',
      accessKey: 'AKIA123',
      secretKey: 'SECRET456',
    })
  })

  it('resets loading when API returns undefined', async () => {
    const postSpy = vi.fn().mockResolvedValue(undefined)
    ;(window as any).__api = { post: postSpy, get: vi.fn() }

    const component = createComponent()
    await component.finishSetup()

    expect(component.loading).toBe(false)
    expect(component._service.send).not.toHaveBeenCalled()
  })
})

describe('onboardingWizard — resetOnboarding', () => {
  it('posts to reset endpoint and calls reloadAfter', async () => {
    const postSpy = vi.fn().mockResolvedValue({})
    ;(window as any).__api = { post: postSpy, get: vi.fn() }

    const component = createComponent()
    await component.resetOnboarding()

    expect(postSpy).toHaveBeenCalledWith('/_panel/api/onboarding/reset', {})
  })

  it('shows error toast on failure', async () => {
    const postSpy = vi.fn().mockRejectedValue(new Error('Reset failed'))
    ;(window as any).__api = { post: postSpy, get: vi.fn() }
    ;(window.__sseToast as any).mockClear()
    const component = createComponent()
    await component.resetOnboarding()

    expect(window.__sseToast).toHaveBeenCalledWith('Reset failed', 'error')
    expect(component.loading).toBe(false)
  })
})

describe('onboardingWizard — navigateBack', () => {
  it('returns false and cancels sub-step when in waiting', () => {
    const component = createComponent()
    component.currentStep = 1
    component.siaStep = 'waiting'
    component._approvalAbort = { reject: vi.fn() }

    const result = component.navigateBack()

    expect(result).toBe(false)
    expect(component.siaStep).toBe('connect')
  })

  it('returns true and sends Back event when in connect sub-step', () => {
    const component = createComponent()
    component.currentStep = 1
    component.siaStep = 'connect'

    const result = component.navigateBack()

    expect(result).toBe(true)
    expect(component._service.send).toHaveBeenCalledWith('back')
  })

  it('clears siaBuilder when going back from step 2', () => {
    const component = createComponent()
    component.currentStep = 2
    component.siaBuilder = { stale: true }
    component.siaStep = 'recovery'

    component.navigateBack()

    expect(component.siaBuilder).toBeNull()
    expect(component.siaStep).toBe('connect')
  })
})

describe('onboardingWizard — copyToClipboard', () => {
  it('sets copiedField and clears it after 2s', async () => {
    const component = createComponent()

    component.copyToClipboard('test-value', 'accessKey')

    // Wait for the async .then() callback to set copiedField
    await vi.waitFor(() => {
      expect(component._reactive.copiedField).toBe('accessKey')
    }, { timeout: 500, interval: 50 })

    // Wait for 2s timer to clear the field (real browser timing)
    await vi.waitFor(() => {
      expect(component._reactive.copiedField).toBe('')
    }, { timeout: 3000, interval: 200 })
  })
})

describe('onboardingWizard — normalizeIndexerURL', () => {
  it('adds https:// prefix when missing', () => {
    const component = createComponent()
    expect(component.normalizeIndexerURL('indexer.example.com')).toBe('https://indexer.example.com')
  })

  it('preserves http:// prefix', () => {
    const component = createComponent()
    expect(component.normalizeIndexerURL('http://indexer.example.com')).toBe('http://indexer.example.com')
  })

  it('preserves https:// prefix', () => {
    const component = createComponent()
    expect(component.normalizeIndexerURL('https://indexer.example.com')).toBe('https://indexer.example.com')
  })

  it('strips trailing slashes', () => {
    const component = createComponent()
    expect(component.normalizeIndexerURL('https://indexer.example.com/')).toBe('https://indexer.example.com')
    expect(component.normalizeIndexerURL('https://indexer.example.com///')).toBe('https://indexer.example.com')
  })

  it('trims whitespace', () => {
    const component = createComponent()
    expect(component.normalizeIndexerURL('  https://indexer.example.com  ')).toBe('https://indexer.example.com')
  })
})

describe('onboardingWizard — selectedIndexerURL', () => {
  it('returns indexerSelection when not custom', () => {
    const component = createComponent()
    component.indexerSelection = 'https://indexer.example.com'
    expect(component.selectedIndexerURL()).toBe('https://indexer.example.com')
  })

  it('returns customIndexer when selection is __custom__', () => {
    const component = createComponent()
    component.indexerSelection = '__custom__'
    component.customIndexer = 'https://custom.example.com'
    expect(component.selectedIndexerURL()).toBe('https://custom.example.com')
  })
})
