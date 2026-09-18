import { apiClient } from '../client'

/**
 * Frontend contract for the OIDC Provider administration surface.
 *
 * This module intentionally models metadata only for keys and secrets. The
 * private signing key is never a DTO field, and client_secret is only present
 * in the create/rotate response that renders the one-time reveal UI.
 */

export const OIDC_PROVIDER_BASE_PATH = '/admin/oidc-provider'
export const OIDC_ADMIN_CSRF_COOKIE = '__Host-sub2_oidc_admin_csrf'
export const OIDC_ADMIN_CSRF_HEADER = 'X-CSRF-Token'

export const OIDC_PROVIDER_PERMISSIONS = {
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
} as const

export type OidcResourceId = string | number
export type OidcProviderStatus = 'disabled' | 'enabled' | 'degraded' | 'unknown'
export type OidcClientSecretStatus = 'active' | 'retiring' | 'revoked' | 'expired'
export type OidcSigningKeyStatus = 'pending' | 'active' | 'retiring' | 'retired' | 'revoked'
export type OidcConsentSource = 'interactive' | 'admin_pre_authorized'
export type OidcConsentStatus = 'active' | 'revoked' | 'expired'

export interface OidcProviderEndpointSet {
  discovery: string
  authorization: string
  token: string
  userinfo: string
  jwks: string
  revocation: string
}

export interface OidcSigningKeySummary {
  kid: string
  alg: 'RS256' | string
  fingerprint: string
  status: OidcSigningKeyStatus
  not_before?: string
  not_after?: string
  created_at?: string
  activated_at?: string
  retired_at?: string
  revoked_at?: string
}

export interface OidcProviderStatusResponse {
  enabled: boolean
  status: OidcProviderStatus
  issuer: string
  endpoints: OidcProviderEndpointSet
  active_key?: OidcSigningKeySummary | null
  retiring_keys?: OidcSigningKeySummary[]
  supported_scopes: string[]
  supported_claims: string[]
  security: {
    default_disabled: boolean
    secret_one_time_display: boolean
    private_key_hidden: boolean
    csrf_required: boolean
    step_up_required: boolean
    admin_session: 'http_only' | 'jwt_session_bound' | 'bearer_compatibility' | 'unknown'
  }
  warning?: string
}

export interface OidcClientSecretSummary {
  id: OidcResourceId
  fingerprint: string
  status: OidcClientSecretStatus
  not_before: string
  expires_at: string
  created_at?: string
  revoked_at?: string
}

export interface OidcClientSummary {
  id: OidcResourceId
  client_id: string
  name: string
  owner: string
  client_type: 'confidential' | string
  enabled: boolean
  trusted_skip_consent: boolean
  redirect_uris: string[]
  allowed_scopes: string[]
  secrets?: OidcClientSecretSummary[]
  last_used_at?: string | null
  created_at: string
  updated_at: string
  policy_version: number
  version: number
}

export interface OidcClientDetail extends OidcClientSummary {
  disabled_at?: string | null
  disabled_reason?: string | null
}

export interface OidcClientDraft {
  name: string
  owner: string
  redirect_uris: string[]
  allowed_scopes: string[]
  trusted_skip_consent: boolean
  reason: string
}

export interface OidcClientSecretIssueMetadata {
  id: OidcResourceId
  fingerprint: string
  not_before: string
  expires_at: string
}

export interface OidcClientCreateResponse {
  client: OidcClientDetail
  client_secret: string
  secret: OidcClientSecretIssueMetadata
}

export interface OidcClientSecretIssueResponse {
  client_secret: string
  secret: OidcClientSecretIssueMetadata
}

export interface OidcAdminActionRequest {
  reason?: string
  request_id?: string
}

export interface OidcClientUpdateRequest extends OidcClientDraft {
  version: number
}

export interface OidcConsentSummary {
  id: OidcResourceId
  user_id: number
  client_id: string
  client_name?: string
  scopes: string[]
  source: OidcConsentSource
  status: OidcConsentStatus
  policy_version: number
  approved_at?: string | null
  revoked_at?: string | null
  revoked_reason?: string | null
}

export interface OidcAuditEvent {
  id: OidcResourceId
  created_at: string
  action: string
  result: 'success' | 'failure' | string
  actor_user_id?: number | null
  client_id?: string | null
  secret_id?: string | null
  secret_fingerprint?: string | null
  kid?: string | null
  user_id?: number | null
  family_id?: string | null
  scope?: string[] | null
  old_status?: string | null
  new_status?: string | null
  reason?: string | null
  request_id?: string | null
}

export interface OidcCollectionQuery {
  page?: number
  page_size?: number
  client_id?: string
  status?: string
  q?: string
}

export interface OidcCollectionResponse<T> {
  items: T[]
}

function requestId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `oidc-admin-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function withRequestId<T extends Record<string, unknown>>(body: T): T & { request_id: string } {
  const existingRequestId = typeof body.request_id === 'string' ? body.request_id : undefined
  return {
    ...body,
    request_id: existingRequestId || requestId(),
  }
}

function pathSegment(value: string | number): string {
  return encodeURIComponent(String(value))
}

function readCookie(name: string): string {
  if (typeof document === 'undefined') return ''

  const prefix = `${name}=`
  for (const part of document.cookie.split(';')) {
    const cookie = part.trim()
    if (!cookie.startsWith(prefix)) continue

    const value = cookie.slice(prefix.length)
    try {
      return decodeURIComponent(value)
    } catch {
      return value
    }
  }

  return ''
}

/**
 * The CSRF cookie is deliberately non-HttpOnly so the browser can echo it in
 * the synchronizer-token header. It must never be persisted in localStorage
 * or sessionStorage.
 */
export function getOidcAdminCsrfToken(): string {
  return readCookie(OIDC_ADMIN_CSRF_COOKIE)
}

function writeConfig(headers: Record<string, string> = {}) {
  return {
    headers: {
      [OIDC_ADMIN_CSRF_HEADER]: getOidcAdminCsrfToken(),
      ...headers,
    },
  }
}

function collection<T>(value: unknown, label: string): T[] {
  if (typeof value === 'object' && value !== null) {
    const candidate = value as Partial<OidcCollectionResponse<unknown>>
    if (Array.isArray(candidate.items)) return candidate.items as T[]
  }
  throw new Error(`Invalid ${label} response from the OIDC Provider admin API.`)
}

async function getCollection<T>(path: string, query: OidcCollectionQuery | undefined, label: string): Promise<T[]> {
  const response = query && Object.keys(query).length > 0
    ? await apiClient.get<unknown>(path, { params: query })
    : await apiClient.get<unknown>(path)
  return collection<T>(response.data, label)
}

export async function getProviderStatus(): Promise<OidcProviderStatusResponse> {
  const { data } = await apiClient.get<OidcProviderStatusResponse>(`${OIDC_PROVIDER_BASE_PATH}/status`)
  return data
}

export async function listClients(query?: OidcCollectionQuery): Promise<OidcClientSummary[]> {
  return getCollection<OidcClientSummary>(`${OIDC_PROVIDER_BASE_PATH}/clients`, query, 'clients')
}

export async function getClient(id: string | number): Promise<OidcClientDetail> {
  const { data } = await apiClient.get<OidcClientDetail>(`${OIDC_PROVIDER_BASE_PATH}/clients/${pathSegment(id)}`)
  return data
}

export async function createClient(payload: OidcClientDraft): Promise<OidcClientCreateResponse> {
  const { data } = await apiClient.post<OidcClientCreateResponse>(
    `${OIDC_PROVIDER_BASE_PATH}/clients`,
    withRequestId(payload as unknown as Record<string, unknown>),
    writeConfig()
  )
  return data
}

export async function updateClient(id: string | number, payload: OidcClientUpdateRequest): Promise<OidcClientDetail> {
  const { version, ...draft } = payload
  const { data } = await apiClient.put<OidcClientDetail>(
    `${OIDC_PROVIDER_BASE_PATH}/clients/${pathSegment(id)}`,
    withRequestId(draft as unknown as Record<string, unknown>),
    writeConfig({ 'If-Match': String(version) })
  )
  return data
}

export async function rotateClientSecret(
  id: string | number,
  payload: OidcAdminActionRequest = {}
): Promise<OidcClientSecretIssueResponse> {
  const { data } = await apiClient.post<OidcClientSecretIssueResponse>(
    `${OIDC_PROVIDER_BASE_PATH}/clients/${pathSegment(id)}/secrets`,
    withRequestId(payload as Record<string, unknown>),
    writeConfig()
  )
  return data
}

export async function revokeClientSecret(
  clientId: string | number,
  secretId: string | number,
  payload: OidcAdminActionRequest = {}
): Promise<void> {
  await apiClient.post<void>(
    `${OIDC_PROVIDER_BASE_PATH}/clients/${pathSegment(clientId)}/secrets/${pathSegment(secretId)}/revoke`,
    withRequestId(payload as Record<string, unknown>),
    writeConfig()
  )
}

export async function disableClient(
  id: string | number,
  payload: OidcAdminActionRequest = {}
): Promise<void> {
  await apiClient.post<void>(
    `${OIDC_PROVIDER_BASE_PATH}/clients/${pathSegment(id)}/disable`,
    withRequestId(payload as Record<string, unknown>),
    writeConfig()
  )
}

export async function enableClient(
  id: string | number,
  payload: OidcAdminActionRequest = {}
): Promise<void> {
  await apiClient.post<void>(
    `${OIDC_PROVIDER_BASE_PATH}/clients/${pathSegment(id)}/enable`,
    withRequestId(payload as Record<string, unknown>),
    writeConfig()
  )
}

export async function listConsents(query?: OidcCollectionQuery): Promise<OidcConsentSummary[]> {
  return getCollection<OidcConsentSummary>(`${OIDC_PROVIDER_BASE_PATH}/consents`, query, 'consents')
}

export async function revokeConsent(
  id: string | number,
  payload: OidcAdminActionRequest = {}
): Promise<void> {
  await apiClient.post<void>(
    `${OIDC_PROVIDER_BASE_PATH}/consents/${pathSegment(id)}/revoke`,
    withRequestId(payload as Record<string, unknown>),
    writeConfig()
  )
}

export async function listKeys(query?: OidcCollectionQuery): Promise<OidcSigningKeySummary[]> {
  return getCollection<OidcSigningKeySummary>(`${OIDC_PROVIDER_BASE_PATH}/keys`, query, 'keys')
}

export async function rotateKey(payload: OidcAdminActionRequest = {}): Promise<OidcSigningKeySummary> {
  const { data } = await apiClient.post<OidcSigningKeySummary>(
    `${OIDC_PROVIDER_BASE_PATH}/keys/rotate`,
    withRequestId(payload as Record<string, unknown>),
    writeConfig()
  )
  return data
}

export async function retireKey(
  kid: string,
  payload: OidcAdminActionRequest = {}
): Promise<void> {
  await apiClient.post<void>(
    `${OIDC_PROVIDER_BASE_PATH}/keys/${pathSegment(kid)}/retire`,
    withRequestId(payload as Record<string, unknown>),
    writeConfig()
  )
}

export async function revokeKey(
  kid: string,
  payload: OidcAdminActionRequest = {}
): Promise<void> {
  await apiClient.post<void>(
    `${OIDC_PROVIDER_BASE_PATH}/keys/${pathSegment(kid)}/revoke`,
    withRequestId(payload as Record<string, unknown>),
    writeConfig()
  )
}

export async function listAuditEvents(query?: OidcCollectionQuery): Promise<OidcAuditEvent[]> {
  return getCollection<OidcAuditEvent>(`${OIDC_PROVIDER_BASE_PATH}/audit-events`, query, 'audit events')
}

export const oidcProviderAPI = {
  getProviderStatus,
  listClients,
  getClient,
  createClient,
  updateClient,
  rotateClientSecret,
  revokeClientSecret,
  disableClient,
  enableClient,
  listConsents,
  revokeConsent,
  listKeys,
  rotateKey,
  retireKey,
  revokeKey,
  listAuditEvents,
}

export default oidcProviderAPI
