import { apiClient } from './client'

export interface AdminPermissionDefinition {
  permission: string
  resource: string
  action: string
  sensitive: boolean
  description: string
}

export interface AdminPermissionGrant {
  permission: string
  effect: 'allow' | 'deny'
  scope?: Record<string, unknown>
  reason?: string
}

export interface AdminCapabilities {
  writes_enabled: boolean
  can_write: boolean
  mode: 'disabled' | 'shadow' | 'enforce'
  deny_reason?: string
}

export interface AdminUserPermissionState {
  user_id: number
  role: string
  version: number
  grants: AdminPermissionGrant[]
  capabilities?: AdminCapabilities
}

export async function listAdminPermissionCatalog(): Promise<AdminPermissionDefinition[]> {
  const { data } = await apiClient.get<AdminPermissionDefinition[]>('/admin/permissions')
  return data
}

export async function getUserAdminPermissions(userId: number): Promise<AdminUserPermissionState> {
  const { data } = await apiClient.get<AdminUserPermissionState>(`/admin/users/${userId}/permissions`)
  return data
}

export async function grantUserAdminPermission(userId: number, body: AdminPermissionGrant & { request_id?: string }): Promise<void> {
  await apiClient.put(`/admin/users/${userId}/permissions/${encodeURIComponent(body.permission)}`, {
    ...body,
    request_id: body.request_id || crypto.randomUUID(),
  })
}

export async function revokeUserAdminPermission(userId: number, permission: string, reason = ''): Promise<void> {
  await apiClient.delete(`/admin/users/${userId}/permissions/${encodeURIComponent(permission)}`, { data: { reason } })
}
