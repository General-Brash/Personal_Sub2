import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

const {
  getProviderStatus,
  listClients,
  getClient,
  listKeys,
  listConsents,
  listAuditEvents,
  disableClient,
  retireKey,
} = vi.hoisted(() => ({
  getProviderStatus: vi.fn(),
  listClients: vi.fn(),
  getClient: vi.fn(),
  listKeys: vi.fn(),
  listConsents: vi.fn(),
  listAuditEvents: vi.fn(),
  disableClient: vi.fn(),
  retireKey: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  OIDC_PROVIDER_PERMISSIONS: {
    providerRead: 'oidc.provider.read',
    clientsRead: 'oidc.clients.read',
    clientsWrite: 'oidc.clients.write',
    clientsSecretRotate: 'oidc.clients.secret.rotate',
    clientsDisable: 'oidc.clients.disable',
    consentsRead: 'oidc.consents.read',
    consentsRevoke: 'oidc.consents.revoke',
    keysRead: 'oidc.keys.read',
    keysRotate: 'oidc.keys.rotate',
    keysRevoke: 'oidc.keys.revoke',
    auditRead: 'oidc.audit.read',
  },
  adminAPI: {
    oidcProvider: {
      getProviderStatus,
      listClients,
      getClient,
      listKeys,
      listConsents,
      listAuditEvents,
      createClient: vi.fn(),
      updateClient: vi.fn(),
      rotateClientSecret: vi.fn(),
      revokeClientSecret: vi.fn(),
      disableClient,
      enableClient: vi.fn(),
      revokeConsent: vi.fn(),
      rotateKey: vi.fn(),
      retireKey,
      revokeKey: vi.fn(),
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }),
  useAuthStore: () => ({ canAdmin: vi.fn(() => true) }),
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ visible: false, run: (action: () => unknown) => action(), onVerified: vi.fn(), onCancel: vi.fn() }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key, locale: ref('en') }),
}))

import OIDCProviderView from '../OIDCProviderView.vue'

const status = {
  enabled: false,
  status: 'disabled' as const,
  issuer: 'https://auth.taffy.edu.kg',
  endpoints: {
    discovery: 'https://auth.taffy.edu.kg/.well-known/openid-configuration',
    authorization: 'https://auth.taffy.edu.kg/oauth/authorize',
    token: 'https://auth.taffy.edu.kg/oauth/token',
    userinfo: 'https://auth.taffy.edu.kg/oauth/userinfo',
    jwks: 'https://auth.taffy.edu.kg/oauth/jwks',
    revocation: 'https://auth.taffy.edu.kg/oauth/revoke',
  },
  supported_scopes: ['openid', 'profile'],
  supported_claims: ['sub', 'preferred_username'],
  security: {
    default_disabled: true,
    secret_one_time_display: true,
    private_key_hidden: true,
    csrf_required: true,
    step_up_required: true,
    admin_session: 'http_only' as const,
  },
}

const client = {
  id: 'client-1',
  client_id: 'client-id',
  name: 'Test RP',
  owner: 'test',
  client_type: 'confidential',
  enabled: true,
  trusted_skip_consent: false,
  redirect_uris: ['https://rp.example/callback'],
  allowed_scopes: ['openid', 'profile'],
  secrets: [],
  created_at: '2026-09-17T00:00:00Z',
  updated_at: '2026-09-17T00:00:00Z',
  policy_version: 1,
  version: 1,
}

const activeKey = {
  kid: 'kid-1',
  alg: 'RS256',
  fingerprint: 'key-fingerprint',
  status: 'active' as const,
  not_before: '2026-09-17T00:00:00Z',
  not_after: '2027-09-17T00:00:00Z',
}

function mountView() {
  return mount(OIDCProviderView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TotpStepUpDialog: true,
      },
    },
  })
}

describe('OIDCProviderView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getProviderStatus.mockResolvedValue(status)
    listClients.mockResolvedValue([client])
    getClient.mockResolvedValue(client)
    listKeys.mockResolvedValue([])
    listConsents.mockResolvedValue([])
    listAuditEvents.mockResolvedValue([])
    disableClient.mockResolvedValue(undefined)
    retireKey.mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('shows the disabled-by-default boundary and fixed issuer without using upstream settings', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('admin.oidcProvider.disabledNotice')
    expect(wrapper.text()).toContain('https://auth.taffy.edu.kg')
    expect(wrapper.text()).toContain('admin.oidcProvider.security.privateKeyHidden')
    expect(wrapper.text()).toContain('admin.oidcProvider.clients.title')
    expect(getProviderStatus).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('renders a clear section error when an expected backend endpoint is unavailable', async () => {
    getProviderStatus.mockRejectedValue({ status: 404, code: 'NOT_FOUND', message: 'route unavailable' })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('admin.oidcProvider.errors.status [NOT_FOUND] (404): route unavailable')
    expect(wrapper.text()).toContain('admin.oidcProvider.backendNotice')
    wrapper.unmount()
  })

  it('refreshes the client after a 204 disable mutation instead of using a response body', async () => {
    const wrapper = mountView()
    await flushPromises()

    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const reason = wrapper.find('textarea')
    await reason.setValue('disable reason')
    const disableButton = wrapper.findAll('button').find((button) => button.text().includes('admin.oidcProvider.clients.disable'))
    expect(disableButton).toBeDefined()

    await disableButton!.trigger('click')
    await flushPromises()

    expect(disableClient).toHaveBeenCalledWith('client-1', { reason: 'disable reason' })
    expect(getClient).toHaveBeenCalledTimes(2)
    expect(listClients).toHaveBeenCalledTimes(2)
    expect(confirm).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('refreshes keys and provider status after a 204 key status mutation', async () => {
    listKeys.mockResolvedValue([activeKey])
    const wrapper = mountView()
    await flushPromises()

    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    vi.spyOn(window, 'prompt').mockReturnValue('retire reason')
    const retireButton = wrapper.findAll('button').find((button) => button.text().includes('admin.oidcProvider.keys.retire'))
    expect(retireButton).toBeDefined()

    await retireButton!.trigger('click')
    await flushPromises()

    expect(retireKey).toHaveBeenCalledWith('kid-1', { reason: 'retire reason' })
    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(getProviderStatus).toHaveBeenCalledTimes(2)
    expect(confirm).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('distinguishes expired and near-expiry active secrets from usable ones', async () => {
    const now = Date.now()
    getClient.mockResolvedValue({ ...client, secrets: [
      { id: 'expired', fingerprint: 'expired-fp', status: 'active', not_before: new Date(now - 10 * 86400000).toISOString(), expires_at: new Date(now - 1000).toISOString() },
      { id: 'soon', fingerprint: 'soon-fp', status: 'active', not_before: new Date(now - 86400000).toISOString(), expires_at: new Date(now + 86400000).toISOString() },
    ] })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('Expired — unusable')
    expect(wrapper.text()).toContain('Expires within 7 days — plan rotation')
    expect(wrapper.text()).not.toContain('No currently usable secret')
    wrapper.unmount()
  })

  it('updates availability and the no-usable-secret alert as time crosses expiry without refetching', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-23T12:00:00Z'))
    getClient.mockResolvedValue({ ...client, secrets: [
      { id: 'boundary', fingerprint: 'boundary-fp', status: 'active', not_before: '2026-09-22T12:00:00Z', expires_at: '2026-09-23T12:00:02Z' },
    ] })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('Expires within 7 days — plan rotation')
    expect(wrapper.text()).not.toContain('No currently usable secret')

    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.text()).toContain('Expired — unusable')
    expect(wrapper.find('[role="alert"]').text()).toContain('No currently usable secret')
    expect(getClient).toHaveBeenCalledOnce()
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('warns when an enabled client has no secrets', async () => {
    getClient.mockResolvedValue({ ...client, secrets: [] })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('No currently usable secret')
    expect(wrapper.text()).toContain('admin.oidcProvider.clients.noSecrets')
    wrapper.unmount()
  })

  it('warns when every client secret is expired despite an active status', async () => {
    getClient.mockResolvedValue({ ...client, secrets: [
      { id: 'expired', fingerprint: 'expired-fp', status: 'active', not_before: '2026-01-01T00:00:00Z', expires_at: '2026-01-02T00:00:00Z' },
    ] })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('No currently usable secret')
    expect(wrapper.text()).toContain('Expired — unusable')
    wrapper.unmount()
  })

  it('labels the old secret in a rotation overlap without prompting another rotation', async () => {
    const now = Date.now()
    getClient.mockResolvedValue({ ...client, secrets: [
      { id: 'new', fingerprint: 'new-fp', status: 'active', not_before: new Date(now - 1000).toISOString(), expires_at: new Date(now + 90 * 86400000).toISOString() },
      { id: 'old', fingerprint: 'old-fp', status: 'retiring', not_before: new Date(now - 86400000).toISOString(), expires_at: new Date(now + 3600000).toISOString() },
    ] })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('active · Usable')
    expect(wrapper.text()).toContain('retiring · Rotation overlap — usable until it expires automatically')
    expect(wrapper.text()).not.toContain('plan rotation')
    expect(wrapper.text()).not.toContain('No currently usable secret')
    wrapper.unmount()
  })

  it('still prompts rotation when a retiring secret is the only usable one', async () => {
    const now = Date.now()
    getClient.mockResolvedValue({ ...client, secrets: [
      { id: 'old', fingerprint: 'old-fp', status: 'retiring', not_before: new Date(now - 86400000).toISOString(), expires_at: new Date(now + 3600000).toISOString() },
    ] })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('retiring · Expires within 7 days — plan rotation')
    expect(wrapper.text()).not.toContain('No currently usable secret')
    wrapper.unmount()
  })

  it('does not write secret or token material to browser storage in the view source', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.html()).not.toContain('localStorage')
    expect(wrapper.html()).not.toContain('private_key')
    wrapper.unmount()
  })
})
