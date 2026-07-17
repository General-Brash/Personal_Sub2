import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import {
  batchUpdateLimits,
  bindUserAuthIdentity,
  getTemporaryCredits,
  grantTemporaryCredit,
  type AdminBindAuthIdentityRequest,
  type AdminBoundAuthIdentity,
  type BatchUpdateUserLimitsRequest,
  type BatchUpdateUserLimitsResponse,
  type GrantTemporaryCreditRequest,
  type GrantTemporaryCreditResult,
  type TemporaryCreditAuditItem,
} from '@/api/admin/users'

type Assert<T extends true> = T
type IsExact<T, U> = (
  (<G>() => G extends T ? 1 : 2) extends (<G>() => G extends U ? 1 : 2)
    ? ((<G>() => G extends U ? 1 : 2) extends (<G>() => G extends T ? 1 : 2) ? true : false)
    : false
)

type ExpectedAdminBindAuthIdentityRequest = {
  provider_type: string
  provider_key: string
  provider_subject: string
  issuer?: string
  metadata?: Record<string, unknown>
  channel?: {
    channel: string
    channel_app_id: string
    channel_subject: string
    metadata?: Record<string, unknown>
  }
}

type ExpectedAdminBoundAuthIdentity = {
  user_id: number
  provider_type: string
  provider_key: string
  provider_subject: string
  verified_at?: string | null
  issuer?: string | null
  metadata: Record<string, unknown> | null
  created_at: string
  updated_at: string
  channel?: {
    channel: string
    channel_app_id: string
    channel_subject: string
    metadata: Record<string, unknown> | null
    created_at: string
    updated_at: string
  } | null
}

const requestContractExact: Assert<
  IsExact<AdminBindAuthIdentityRequest, ExpectedAdminBindAuthIdentityRequest>
> = true
const responseContractExact: Assert<
  IsExact<AdminBoundAuthIdentity, ExpectedAdminBoundAuthIdentity>
> = true
const batchRequestContractExact: Assert<
  IsExact<
    BatchUpdateUserLimitsRequest,
    {
      user_ids: number[]
      all?: boolean
      concurrency?: number
      rpm_limit?: number
    }
  >
> = true
const batchResponseContractExact: Assert<
  IsExact<BatchUpdateUserLimitsResponse, { affected: number }>
> = true

describe('admin users api auth identity binding', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('posts the backend-compatible auth identity bind payload and returns the backend response shape', async () => {
    const payload: AdminBindAuthIdentityRequest = {
      provider_type: 'wechat',
      provider_key: 'wechat-main',
      provider_subject: 'union-123',
      metadata: { source: 'admin-repair' },
      channel: {
        channel: 'open',
        channel_app_id: 'wx-open',
        channel_subject: 'openid-123',
        metadata: { scene: 'migration' },
      },
    }

    const response: AdminBoundAuthIdentity = {
      user_id: 9,
      provider_type: 'wechat',
      provider_key: 'wechat-main',
      provider_subject: 'union-123',
      verified_at: '2026-04-22T00:00:00Z',
      issuer: null,
      metadata: { source: 'admin-repair' },
      created_at: '2026-04-22T00:00:00Z',
      updated_at: '2026-04-22T00:00:00Z',
      channel: {
        channel: 'open',
        channel_app_id: 'wx-open',
        channel_subject: 'openid-123',
        metadata: { scene: 'migration' },
        created_at: '2026-04-22T00:00:00Z',
        updated_at: '2026-04-22T00:00:00Z',
      },
    }
    post.mockResolvedValue({ data: response })

    const result = await bindUserAuthIdentity(9, payload)

    expect(post).toHaveBeenCalledWith('/admin/users/9/auth-identities', payload)
    expect(result).toEqual(response)
  })

  it('keeps bind auth identity request and response types aligned with the backend contract', () => {
    expect(requestContractExact).toBe(true)
    expect(responseContractExact).toBe(true)
  })

  it('posts batch limit updates once with only the supplied limit fields', async () => {
    const request: BatchUpdateUserLimitsRequest = {
      user_ids: [4, 7],
      all: false,
      rpm_limit: 0,
    }
    post.mockResolvedValue({ data: { affected: 2 } satisfies BatchUpdateUserLimitsResponse })

    const result = await batchUpdateLimits(request)

    expect(post).toHaveBeenCalledWith('/admin/users/batch-limits', request)
    expect(result).toEqual({ affected: 2 })
    expect(batchRequestContractExact).toBe(true)
    expect(batchResponseContractExact).toBe(true)
  })
})

describe('admin users temporary credit api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('keeps the grant amount as a strict decimal string and sends the idempotency key', async () => {
    const request: GrantTemporaryCreditRequest = {
      amount: '0.00000001',
      notes: 'manual adjustment',
    }
    const response: GrantTemporaryCreditResult = {
      temporary_credit_grant_id: 18,
      amount: '0.00000001',
      remaining_amount: '0.00000001',
      expires_at: '2026-07-17T16:00:00Z',
      notes: 'manual adjustment',
    }
    post.mockResolvedValue({ data: response })

    const result = await grantTemporaryCredit(9, request, 'admin-grant-9-20260716')

    expect(post).toHaveBeenCalledWith(
      '/admin/users/9/temporary-credits',
      request,
      { headers: { 'Idempotency-Key': 'admin-grant-9-20260716' } },
    )
    expect(result).toEqual(response)
    expect(typeof post.mock.calls[0]?.[1]?.amount).toBe('string')
  })

  it('preserves nullable audit fields, fixed amount strings, and pagination', async () => {
    const item: TemporaryCreditAuditItem = {
      id: 18,
      user_id: 9,
      source: 'checkin',
      checkin_id: 5,
      amount: '1.25000000',
      remaining_amount: '0.25000000',
      expires_at: '2026-07-17T16:00:00Z',
      notes: '',
      granted_by: null,
      created_at: '2026-07-16T03:00:00Z',
      updated_at: '2026-07-16T04:00:00Z',
    }
    const response = {
      items: [item],
      total: 1,
      page: 2,
      page_size: 20,
      pages: 3,
    }
    get.mockResolvedValue({ data: response })

    const result = await getTemporaryCredits(9, 2, 20)

    expect(get).toHaveBeenCalledWith('/admin/users/9/temporary-credits', {
      params: { page: 2, page_size: 20 },
    })
    expect(result).toEqual(response)
    expect(result.items[0]?.checkin_id).toBe(5)
    expect(result.items[0]?.granted_by).toBeNull()
    expect(result.items[0]?.amount).toBe('1.25000000')
  })
})
