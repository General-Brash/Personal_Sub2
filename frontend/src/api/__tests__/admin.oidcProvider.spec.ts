import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('../client', () => ({
  apiClient: { get, post, put },
}))

import {
  OIDC_ADMIN_CSRF_COOKIE,
  OIDC_ADMIN_CSRF_HEADER,
  createClient,
  disableClient,
  enableClient,
  getOidcAdminCsrfToken,
  getProviderStatus,
  listAuditEvents,
  listClients,
  listConsents,
  listKeys,
  revokeClientSecret,
  revokeConsent,
  revokeKey,
  retireKey,
  rotateClientSecret,
  rotateKey,
  updateClient,
} from '@/api/admin/oidcProvider'

const client = {
  id: 'client-1',
  client_id: 'client-abc',
  name: 'Test RP',
  owner: 'test',
  client_type: 'confidential',
  enabled: true,
  trusted_skip_consent: false,
  redirect_uris: ['https://rp.example/callback'],
  allowed_scopes: ['openid'],
  created_at: '2026-09-17T00:00:00Z',
  updated_at: '2026-09-17T00:00:00Z',
  policy_version: 1,
  version: 1,
}

const draft = {
  name: 'Test RP',
  owner: 'test',
  redirect_uris: ['https://rp.example/callback'],
  allowed_scopes: ['openid'],
  trusted_skip_consent: false,
  reason: 'reviewed client',
}

let cookieValue = ''

describe('OIDC Provider admin API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    put.mockReset()
    cookieValue = ''
    Object.defineProperty(document, 'cookie', {
      configurable: true,
      get: () => cookieValue,
      set: (value: string) => {
        cookieValue = value.split(';', 1)[0]
      },
    })
    document.cookie = `${OIDC_ADMIN_CSRF_COOKIE}=csrf-token; Path=/`
  })

  it('keeps Provider status on a dedicated admin endpoint and reads the non-HttpOnly CSRF cookie', async () => {
    const status = {
      enabled: false,
      status: 'disabled',
      issuer: 'https://auth.taffy.edu.kg',
      endpoints: {
        discovery: 'https://auth.taffy.edu.kg/.well-known/openid-configuration',
        authorization: 'https://auth.taffy.edu.kg/oauth/authorize',
        token: 'https://auth.taffy.edu.kg/oauth/token',
        userinfo: 'https://auth.taffy.edu.kg/oauth/userinfo',
        jwks: 'https://auth.taffy.edu.kg/oauth/jwks',
        revocation: 'https://auth.taffy.edu.kg/oauth/revoke',
      },
      supported_scopes: ['openid'],
      supported_claims: ['sub'],
      security: {
        default_disabled: true,
        secret_one_time_display: true,
        private_key_hidden: true,
        csrf_required: true,
        step_up_required: true,
        admin_session: 'http_only',
      },
    }
    get.mockResolvedValue({ data: status })

    await expect(getProviderStatus()).resolves.toEqual(status)
    expect(getOidcAdminCsrfToken()).toBe('csrf-token')
    expect(get).toHaveBeenCalledWith('/admin/oidc-provider/status')
  })

  it('requires the canonical {items: [...]} collection envelope', async () => {
    get.mockResolvedValue({
      data: {
        items: [client],
      },
    })

    await expect(listClients()).resolves.toEqual([client])
    expect(get).toHaveBeenCalledWith('/admin/oidc-provider/clients')

    get.mockResolvedValue({ data: [client] })
    await expect(listClients()).rejects.toThrow('Invalid clients response')
  })

  it('returns the canonical nested create response and sends CSRF without storage writes', async () => {
    const response = {
      data: {
        client,
        client_secret: 'one-time-secret',
        secret: {
          id: 'secret-1',
          fingerprint: 'fingerprint',
          not_before: '2026-09-17T00:00:00Z',
          expires_at: '2026-09-18T00:00:00Z',
        },
      },
    }
    post.mockResolvedValue(response)
    const storageSetItem = vi.spyOn(Storage.prototype, 'setItem')

    const result = await createClient(draft)

    expect(result.client).toEqual(client)
    expect(result.secret.fingerprint).toBe('fingerprint')
    expect(post).toHaveBeenCalledWith(
      '/admin/oidc-provider/clients',
      expect.objectContaining({ name: 'Test RP', reason: 'reviewed client', request_id: expect.any(String) }),
      { headers: { [OIDC_ADMIN_CSRF_HEADER]: 'csrf-token' } },
    )
    expect(storageSetItem).not.toHaveBeenCalled()
    storageSetItem.mockRestore()
  })

  it('keeps client PUT JSON, If-Match, reason/request_id, and CSRF together', async () => {
    put.mockResolvedValue({ data: { ...client, version: 3 } })

    await updateClient('client-1', { ...draft, version: 3 })

    expect(put).toHaveBeenCalledWith(
      '/admin/oidc-provider/clients/client-1',
      expect.objectContaining({ name: 'Test RP', reason: 'reviewed client', request_id: expect.any(String) }),
      { headers: { [OIDC_ADMIN_CSRF_HEADER]: 'csrf-token', 'If-Match': '3' } },
    )
  })

  it('returns the canonical nested rotate response', async () => {
    post.mockResolvedValue({
      data: {
        client_secret: 'rotated-secret',
        secret: {
          id: 'secret-2',
          fingerprint: 'rotated-fingerprint',
          not_before: '2026-09-17T00:00:00Z',
          expires_at: '2026-09-18T00:00:00Z',
        },
      },
    })

    const result = await rotateClientSecret('client/1', { reason: 'scheduled rotation' })

    expect(result.secret.id).toBe('secret-2')
    expect(post).toHaveBeenCalledWith(
      '/admin/oidc-provider/clients/client%2F1/secrets',
      expect.objectContaining({ reason: 'scheduled rotation', request_id: expect.any(String) }),
      { headers: { [OIDC_ADMIN_CSRF_HEADER]: 'csrf-token' } },
    )
  })

  it('treats revoke, enable, disable, consent, and key status mutations as 204 actions', async () => {
    post.mockResolvedValue({ status: 204, data: undefined })

    await expect(revokeClientSecret('client-1', 'secret-1', { reason: 'revoke secret' })).resolves.toBeUndefined()
    await expect(enableClient('client-1', { reason: 'enable client' })).resolves.toBeUndefined()
    await expect(disableClient('client-1', { reason: 'disable client' })).resolves.toBeUndefined()
    await expect(revokeConsent('consent-1', { reason: 'revoke consent' })).resolves.toBeUndefined()
    await expect(retireKey('kid/one', { reason: 'retire key' })).resolves.toBeUndefined()
    await expect(revokeKey('kid/one', { reason: 'revoke key' })).resolves.toBeUndefined()

    expect(post).toHaveBeenCalledTimes(6)
    for (const [, body, config] of post.mock.calls) {
      expect(body).toEqual(expect.objectContaining({ reason: expect.any(String), request_id: expect.any(String) }))
      expect(config).toEqual({ headers: { [OIDC_ADMIN_CSRF_HEADER]: 'csrf-token' } })
    }
  })

  it('uses canonical envelopes for consents, keys, and audit events', async () => {
    get.mockResolvedValue({ data: { items: [] } })

    await expect(listConsents()).resolves.toEqual([])
    await expect(listKeys()).resolves.toEqual([])
    await expect(listAuditEvents()).resolves.toEqual([])

    expect(get).toHaveBeenNthCalledWith(1, '/admin/oidc-provider/consents')
    expect(get).toHaveBeenNthCalledWith(2, '/admin/oidc-provider/keys')
    expect(get).toHaveBeenNthCalledWith(3, '/admin/oidc-provider/audit-events')
  })

  it('keeps key rotation as a JSON mutation with CSRF', async () => {
    post.mockResolvedValue({ data: { kid: 'kid-2', fingerprint: 'fp', status: 'active' } })

    await rotateKey({ reason: 'scheduled key rotation' })

    expect(post).toHaveBeenCalledWith(
      '/admin/oidc-provider/keys/rotate',
      expect.objectContaining({ reason: 'scheduled key rotation', request_id: expect.any(String) }),
      { headers: { [OIDC_ADMIN_CSRF_HEADER]: 'csrf-token' } },
    )
  })
})
