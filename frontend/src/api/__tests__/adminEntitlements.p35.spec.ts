import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getUserEntitlement, previewEntitlementChange, applyEntitlementChange, updateUserEntitlement, updateEntitlementPolicy } from '../adminEntitlements'
const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('../client', () => ({ apiClient: client }))
beforeEach(() => vi.resetAllMocks())
describe('P3.5 entitlement wire contract', () => {
  it('normalizes only null collections and preserves real sources and groups', async () => {
    client.get.mockResolvedValue({ data: { user_id: 7, tier: 'premium', sources: [{ source: 'independent_grant', tier: 'premium' }], allowed_groups: null, tier_groups: null, manual_groups: [3], subscription_groups: null, default_rates: null } })
    const data = await getUserEntitlement(7)
    expect(data.sources).toHaveLength(1); expect(data.manual_groups).toEqual([3])
    expect(data.allowed_groups).toEqual([]); expect(data.subscription_groups).toEqual([]); expect(data.default_rates).toEqual({})
    client.post.mockResolvedValue({ data: { tier: 'premium', user_ids: [7], affected_user_ids: null, already_at_tier: null, granted_group_ids: null, revoked_group_ids: null } })
    const preview = await previewEntitlementChange([7], 'premium')
    expect(preview.user_ids).toEqual([7]); expect(preview.affected_user_ids).toEqual([]); expect(preview.revoked_group_ids).toEqual([])
  })
  it('sends the caller-owned idempotency key and preview token on both write routes', async () => {
    client.post.mockResolvedValue({ data: {} }); client.put.mockResolvedValue({ data: {} })
    await applyEntitlementChange([7, 8], 'premium', 'reason', 'same-key', 'signed-preview')
    expect(client.post).toHaveBeenCalledWith('/admin/entitlements/apply', { user_ids: [7, 8], tier: 'premium', reason: 'reason', request_id: 'same-key', preview_token: 'signed-preview' })
    await updateUserEntitlement(7, 'standard', 'reason', 'single-key', 'single-preview')
    expect(client.put).toHaveBeenCalledWith('/admin/users/7/entitlement', { tier: 'standard', reason: 'reason', request_id: 'single-key', preview_token: 'single-preview' })
  })
  it('does not invent a new policy request key on retry', async () => {
    client.put.mockResolvedValue({ data: { groups: null } })
    const policy = { tier: 'premium' as const, display_name: 'Premium', enabled: true, groups: [], expected_version: 3, reason: 'reason', request_id: 'policy-key' }
    await updateEntitlementPolicy(policy); await updateEntitlementPolicy(policy)
    expect(client.put.mock.calls[0]).toEqual(client.put.mock.calls[1])
  })
})
