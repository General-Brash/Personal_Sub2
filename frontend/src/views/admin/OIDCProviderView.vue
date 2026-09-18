<template>
  <AppLayout>
    <div class="mx-auto max-w-[1600px] space-y-6">
      <section class="flex flex-col gap-4 border-b border-gray-200 pb-5 dark:border-dark-700 sm:flex-row sm:items-end sm:justify-between">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-3">
            <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.oidcProvider.title') }}</h1>
            <span class="rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t('admin.oidcProvider.nav') }}</span>
          </div>
          <p class="mt-2 max-w-4xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary shrink-0" :disabled="initialLoading" @click="loadAll">
          {{ initialLoading ? t('common.loading') : t('admin.oidcProvider.refresh') }}
        </button>
      </section>

      <div class="rounded-lg border border-blue-200 bg-blue-50 px-4 py-3 text-sm text-blue-900 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-100">
        <p>{{ t('admin.oidcProvider.backendNotice') }}</p>
      </div>

      <div class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/20 dark:text-amber-100">
        <p class="font-medium">{{ t('admin.oidcProvider.disabledNotice') }}</p>
        <p class="mt-1 text-xs text-amber-800 dark:text-amber-200">{{ t('admin.oidcProvider.security.description') }}</p>
      </div>

      <div v-if="initialLoading" class="flex min-h-56 items-center justify-center">
        <LoadingSpinner size="lg" />
      </div>

      <template v-else>
        <div v-if="pageErrors.length" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-200" role="alert">
          <p class="font-medium">{{ t('admin.oidcProvider.loadFailed') }}</p>
          <ul class="mt-2 list-disc space-y-1 pl-5">
            <li v-for="error in pageErrors" :key="error">{{ error }}</li>
          </ul>
        </div>

        <section class="grid gap-6 xl:grid-cols-[minmax(0,1.35fr)_minmax(360px,0.65fr)]">
          <div class="card overflow-hidden">
            <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.oidcProvider.status.title') }}</h2>
            </div>
            <div v-if="!canProviderRead" class="p-6 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.noPermission') }}</div>
            <div v-else class="space-y-5 p-5">
              <div class="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <span class="field-label">{{ t('admin.oidcProvider.status.issuer') }}</span>
                  <code class="mt-1 block break-all rounded bg-gray-100 px-3 py-2 text-sm text-gray-800 dark:bg-dark-800 dark:text-gray-200">{{ providerStatus?.issuer || t('admin.oidcProvider.notAvailable') }}</code>
                </div>
                <span class="rounded-full px-3 py-1 text-xs font-semibold" :class="statusClass(providerStatus?.status)">{{ statusLabel(providerStatus?.status) }}</span>
              </div>

              <div>
                <span class="field-label">{{ t('admin.oidcProvider.status.endpoints') }}</span>
                <div class="mt-2 grid gap-2 sm:grid-cols-2">
                  <div v-for="endpoint in endpointEntries" :key="endpoint.key" class="rounded border border-gray-200 p-3 dark:border-dark-600">
                    <span class="block text-xs font-medium text-gray-500 dark:text-gray-400">{{ t(`admin.oidcProvider.status.${endpoint.key}`) }}</span>
                    <code class="mt-1 block break-all text-xs text-gray-800 dark:text-gray-200">{{ endpoint.value }}</code>
                  </div>
                </div>
              </div>

              <div class="grid gap-4 md:grid-cols-2">
                <div>
                  <span class="field-label">{{ t('admin.oidcProvider.status.supportedScopes') }}</span>
                  <div class="mt-2 flex flex-wrap gap-1.5">
                    <span v-for="scope in providerStatus?.supported_scopes || []" :key="scope" class="rounded bg-gray-100 px-2 py-1 font-mono text-xs dark:bg-dark-800">{{ scope }}</span>
                    <span v-if="!providerStatus?.supported_scopes?.length" class="text-sm text-gray-400">{{ t('admin.oidcProvider.notAvailable') }}</span>
                  </div>
                </div>
                <div>
                  <span class="field-label">{{ t('admin.oidcProvider.status.supportedClaims') }}</span>
                  <div class="mt-2 flex flex-wrap gap-1.5">
                    <span v-for="claim in providerStatus?.supported_claims || []" :key="claim" class="rounded bg-gray-100 px-2 py-1 font-mono text-xs dark:bg-dark-800">{{ claim }}</span>
                    <span v-if="!providerStatus?.supported_claims?.length" class="text-sm text-gray-400">{{ t('admin.oidcProvider.notAvailable') }}</span>
                  </div>
                </div>
              </div>

              <div class="grid gap-4 md:grid-cols-2">
                <div class="rounded border border-gray-200 p-3 dark:border-dark-600">
                  <span class="field-label">{{ t('admin.oidcProvider.status.activeKey') }}</span>
                  <template v-if="providerStatus?.active_key">
                    <code class="mt-1 block break-all text-xs text-gray-800 dark:text-gray-200">{{ providerStatus.active_key.kid }}</code>
                    <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ providerStatus.active_key.fingerprint }}</span>
                  </template>
                  <span v-else class="mt-1 block text-sm text-gray-400">{{ t('admin.oidcProvider.status.noActiveKey') }}</span>
                </div>
                <div class="rounded border border-gray-200 p-3 dark:border-dark-600">
                  <span class="field-label">{{ t('admin.oidcProvider.status.retiringKeys') }}</span>
                  <span class="mt-1 block text-sm text-gray-800 dark:text-gray-200">{{ providerStatus?.retiring_keys?.length || 0 }}</span>
                  <span v-if="!providerStatus?.retiring_keys?.length" class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.status.noRetiringKeys') }}</span>
                </div>
              </div>
            </div>
          </div>

          <div class="card overflow-hidden">
            <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.oidcProvider.security.title') }}</h2>
            </div>
            <div class="space-y-3 p-5 text-sm">
              <div class="flex items-start gap-3"><span class="mt-1 h-2 w-2 shrink-0 rounded-full bg-emerald-500" /><span>{{ t('admin.oidcProvider.security.defaultDisabled') }}</span></div>
              <div class="flex items-start gap-3"><span class="mt-1 h-2 w-2 shrink-0 rounded-full bg-emerald-500" /><span>{{ t('admin.oidcProvider.security.oneTimeSecret') }}</span></div>
              <div class="flex items-start gap-3"><span class="mt-1 h-2 w-2 shrink-0 rounded-full bg-emerald-500" /><span>{{ t('admin.oidcProvider.security.privateKeyHidden') }}</span></div>
              <div class="flex items-start gap-3"><span class="mt-1 h-2 w-2 shrink-0 rounded-full bg-emerald-500" /><span>{{ t('admin.oidcProvider.security.csrfRequired') }}</span></div>
              <div class="flex items-start gap-3"><span class="mt-1 h-2 w-2 shrink-0 rounded-full bg-emerald-500" /><span>{{ t('admin.oidcProvider.security.stepUpRequired') }}</span></div>
              <div class="border-t border-gray-100 pt-3 dark:border-dark-700">
                <span class="field-label">{{ t('admin.oidcProvider.security.adminSession') }}</span>
                <span class="mt-1 block text-gray-800 dark:text-gray-200">{{ securitySessionLabel }}</span>
              </div>
            </div>
          </div>
        </section>

        <div v-if="sectionErrors.clients" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-200">{{ sectionErrors.clients }}</div>
        <section class="grid gap-6 xl:grid-cols-[minmax(360px,0.8fr)_minmax(0,1.2fr)]">
          <ClientList
            :clients="clients"
            :selected-client-id="selectedClientId"
            :loading="sectionLoading.clients"
            :can-read="canClientsRead"
            :can-create="canClientsWrite"
            @select="selectClient"
            @create="openCreate"
          />
          <ClientDetail
            :client="selectedClient"
            :is-creating="isCreating"
            :can-write="canClientsWrite"
            :can-rotate-secret="canRotateSecret"
            :can-disable="canDisableClient"
            :submitting="clientSubmitting"
            :busy-action="busyAction"
            :issued-secret="issuedSecret"
            @submit="submitClient"
            @cancel="cancelCreate"
            @rotate-secret="rotateSecret"
            @revoke-secret="revokeSecret"
            @toggle="toggleClient"
            @clear-issued-secret="clearIssuedSecret"
          />
        </section>

        <div v-if="sectionErrors.keys" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-200">{{ sectionErrors.keys }}</div>
        <KeyList
          :keys="keys"
          :loading="sectionLoading.keys"
          :busy="busyAction === 'key'"
          :can-read="canKeysRead"
          :can-rotate="canRotateKey"
          :can-revoke="canRevokeKey"
          @rotate="rotateKey"
          @retire="retireKey"
          @revoke="revokeKey"
        />

        <div v-if="sectionErrors.consents" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-200">{{ sectionErrors.consents }}</div>
        <ConsentList
          :consents="consents"
          :loading="sectionLoading.consents"
          :busy-id="busyConsentId"
          :can-read="canConsentsRead"
          :can-revoke="canRevokeConsent"
          @revoke="revokeConsent"
        />

        <div v-if="sectionErrors.audit" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-200">{{ sectionErrors.audit }}</div>
        <AuditEvents :events="auditEvents" :loading="sectionLoading.audit" :can-read="canAuditRead" />
      </template>

      <TotpStepUpDialog :controller="oidcStepUp" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'
import { adminAPI, OIDC_PROVIDER_PERMISSIONS, type OidcAuditEvent, type OidcClientCreateResponse, type OidcClientDetail, type OidcClientDraft, type OidcClientSecretIssueResponse, type OidcClientSummary, type OidcConsentSummary, type OidcProviderStatusResponse, type OidcResourceId, type OidcSigningKeySummary } from '@/api/admin'
import { useAppStore, useAuthStore } from '@/stores'
import ClientList from './oidc-provider/ClientList.vue'
import ClientDetail from './oidc-provider/ClientDetail.vue'
import KeyList from './oidc-provider/KeyList.vue'
import ConsentList from './oidc-provider/ConsentList.vue'
import AuditEvents from './oidc-provider/AuditEvents.vue'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const oidcStepUp = useStepUp()

const initialLoading = ref(true)
const providerStatus = ref<OidcProviderStatusResponse | null>(null)
const clients = ref<OidcClientSummary[]>([])
const selectedClientId = ref<OidcResourceId | null>(null)
const selectedClient = ref<OidcClientDetail | null>(null)
const isCreating = ref(false)
const keys = ref<OidcSigningKeySummary[]>([])
const consents = ref<OidcConsentSummary[]>([])
const auditEvents = ref<OidcAuditEvent[]>([])
const issuedSecret = ref<OidcClientSecretIssueResponse | null>(null)
const busyAction = ref<string | null>(null)
const busyConsentId = ref<OidcResourceId | null>(null)
const clientSubmitting = ref(false)
const sectionLoading = reactive({ clients: false, keys: false, consents: false, audit: false })
const sectionErrors = reactive<Record<string, string>>({})

const canProviderRead = computed(() => authStore.canAdmin(OIDC_PROVIDER_PERMISSIONS.providerRead))
const canClientsRead = computed(() => authStore.canAdmin(OIDC_PROVIDER_PERMISSIONS.clientsRead))
const canClientsWrite = computed(() => authStore.canAdmin(OIDC_PROVIDER_PERMISSIONS.clientsWrite))
const canRotateSecret = computed(() => authStore.canAdmin(OIDC_PROVIDER_PERMISSIONS.clientsSecretRotate))
const canDisableClient = computed(() => authStore.canAdmin(OIDC_PROVIDER_PERMISSIONS.clientsDisable))
const canConsentsRead = computed(() => authStore.canAdmin(OIDC_PROVIDER_PERMISSIONS.consentsRead))
const canRevokeConsent = computed(() => authStore.canAdmin(OIDC_PROVIDER_PERMISSIONS.consentsRevoke))
const canKeysRead = computed(() => authStore.canAdmin(OIDC_PROVIDER_PERMISSIONS.keysRead))
const canRotateKey = computed(() => authStore.canAdmin(OIDC_PROVIDER_PERMISSIONS.keysRotate))
const canRevokeKey = computed(() => authStore.canAdmin(OIDC_PROVIDER_PERMISSIONS.keysRevoke))
const canAuditRead = computed(() => authStore.canAdmin(OIDC_PROVIDER_PERMISSIONS.auditRead))

const pageErrors = computed(() => Object.values(sectionErrors).filter(Boolean))
const endpointEntries = computed(() => {
  const endpoints = providerStatus.value?.endpoints
  if (!endpoints) return []
  return [
    { key: 'discovery', value: endpoints.discovery },
    { key: 'authorization', value: endpoints.authorization },
    { key: 'token', value: endpoints.token },
    { key: 'userinfo', value: endpoints.userinfo },
    { key: 'jwks', value: endpoints.jwks },
    { key: 'revocation', value: endpoints.revocation },
  ]
})
const securitySessionLabel = computed(() => {
  const mode = providerStatus.value?.security.admin_session
  if (mode === 'http_only') return t('admin.oidcProvider.security.httpOnly')
  if (mode === 'jwt_session_bound') return t('admin.oidcProvider.security.jwtSessionBound')
  if (mode === 'bearer_compatibility') return t('admin.oidcProvider.security.bearerCompat')
  return t('admin.oidcProvider.security.unknown')
})

function errorMessage(error: unknown, fallback: string): string {
  if (typeof error === 'object' && error !== null) {
    const value = error as { code?: unknown; error?: unknown; message?: unknown; status?: unknown }
    const code = typeof value.code === 'string'
      ? value.code
      : typeof value.error === 'string'
        ? value.error
        : ''
    const message = typeof value.message === 'string' ? value.message : ''
    const status = typeof value.status === 'number' ? ` (${value.status})` : ''
    if (code && message) return `${fallback} [${code}]${status}: ${message}`
    if (code) return `${fallback} [${code}]${status}`
    if (message) return `${fallback}${status}: ${message}`
  }
  return fallback
}

function clearErrors(): void {
  for (const key of Object.keys(sectionErrors)) delete sectionErrors[key]
}

async function loadAll(): Promise<void> {
  initialLoading.value = true
  clearErrors()
  providerStatus.value = null
  try {
    await Promise.all([loadStatus(), loadClients(), loadKeys(), loadConsents(), loadAudit()])
    if (canClientsRead.value && clients.value.length > 0) {
      const selectedStillExists = selectedClientId.value && clients.value.some((client) => client.id === selectedClientId.value)
      if (!selectedStillExists && !isCreating.value) selectedClientId.value = clients.value[0].id
      if (selectedClientId.value && !isCreating.value) await selectClient(selectedClientId.value)
    } else if (!isCreating.value) {
      selectedClientId.value = null
      selectedClient.value = null
    }
  } finally {
    initialLoading.value = false
  }
}

async function loadStatus(): Promise<void> {
  if (!canProviderRead.value) return
  try {
    providerStatus.value = await adminAPI.oidcProvider.getProviderStatus()
  } catch (error: unknown) {
    sectionErrors.status = errorMessage(error, t('admin.oidcProvider.errors.status'))
  }
}

async function loadClients(): Promise<void> {
  if (!canClientsRead.value) return
  sectionLoading.clients = true
  try {
    clients.value = await adminAPI.oidcProvider.listClients()
  } catch (error: unknown) {
    sectionErrors.clients = errorMessage(error, t('admin.oidcProvider.errors.clients'))
  } finally {
    sectionLoading.clients = false
  }
}

async function loadKeys(): Promise<void> {
  if (!canKeysRead.value) return
  sectionLoading.keys = true
  try {
    keys.value = await adminAPI.oidcProvider.listKeys()
  } catch (error: unknown) {
    sectionErrors.keys = errorMessage(error, t('admin.oidcProvider.errors.keys'))
  } finally {
    sectionLoading.keys = false
  }
}

async function loadConsents(): Promise<void> {
  if (!canConsentsRead.value) return
  sectionLoading.consents = true
  try {
    consents.value = await adminAPI.oidcProvider.listConsents()
  } catch (error: unknown) {
    sectionErrors.consents = errorMessage(error, t('admin.oidcProvider.errors.consents'))
  } finally {
    sectionLoading.consents = false
  }
}

async function loadAudit(): Promise<void> {
  if (!canAuditRead.value) return
  sectionLoading.audit = true
  try {
    auditEvents.value = await adminAPI.oidcProvider.listAuditEvents()
  } catch (error: unknown) {
    sectionErrors.audit = errorMessage(error, t('admin.oidcProvider.errors.audit'))
  } finally {
    sectionLoading.audit = false
  }
}

async function selectClient(id: OidcResourceId): Promise<void> {
  if (!canClientsRead.value) return
  selectedClientId.value = id
  isCreating.value = false
  issuedSecret.value = null
  try {
    selectedClient.value = await adminAPI.oidcProvider.getClient(id)
  } catch (error: unknown) {
    selectedClient.value = null
    sectionErrors.client = errorMessage(error, t('admin.oidcProvider.errors.client'))
  }
}

function openCreate(): void {
  issuedSecret.value = null
  selectedClientId.value = null
  selectedClient.value = null
  isCreating.value = true
}

function cancelCreate(): void {
  isCreating.value = false
  if (clients.value.length > 0) void selectClient(clients.value[0].id)
}

async function submitClient(draft: OidcClientDraft): Promise<void> {
  if (!canClientsWrite.value) return
  clientSubmitting.value = true
  sectionErrors.client = ''
  try {
    if (isCreating.value) {
      const result = await oidcStepUp.run(() => adminAPI.oidcProvider.createClient(draft))
      const oneTimeSecret = issuedSecretFromCreate(result)
      appStore.showSuccess(t('admin.oidcProvider.actions.created'))
      isCreating.value = false
      await loadClients()
      await selectClient(result.client.id)
      issuedSecret.value = oneTimeSecret
    } else if (selectedClient.value) {
      const result = await oidcStepUp.run(() => adminAPI.oidcProvider.updateClient(selectedClient.value!.id, { ...draft, version: selectedClient.value!.version }))
      selectedClient.value = result
      appStore.showSuccess(t('admin.oidcProvider.actions.saved'))
      await loadClients()
    }
  } catch (error: unknown) {
    reportActionError(error)
  } finally {
    clientSubmitting.value = false
  }
}

function issuedSecretFromCreate(result: OidcClientCreateResponse): OidcClientSecretIssueResponse {
  return {
    client_secret: result.client_secret,
    secret: { ...result.secret },
  }
}

async function rotateSecret(id: OidcResourceId, reason: string): Promise<void> {
  if (!canRotateSecret.value) return
  busyAction.value = 'secret'
  try {
    const oneTimeSecret = await oidcStepUp.run(() => adminAPI.oidcProvider.rotateClientSecret(id, { reason }))
    appStore.showSuccess(t('admin.oidcProvider.actions.secretRotated'))
    await selectClient(id)
    issuedSecret.value = oneTimeSecret
  } catch (error: unknown) {
    reportActionError(error)
  } finally {
    busyAction.value = null
  }
}

async function revokeSecret(secretId: OidcResourceId, reason: string): Promise<void> {
  if (!canRotateSecret.value || !selectedClient.value) return
  const clientId = selectedClient.value.id
  busyAction.value = 'secret'
  try {
    await oidcStepUp.run(() => adminAPI.oidcProvider.revokeClientSecret(clientId, secretId, { reason }))
    appStore.showSuccess(t('admin.oidcProvider.actions.secretRevoked'))
    await selectClient(clientId)
    await loadClients()
  } catch (error: unknown) {
    reportActionError(error)
  } finally {
    busyAction.value = null
  }
}

async function toggleClient(id: OidcResourceId, enabled: boolean, reason: string): Promise<void> {
  if (!canDisableClient.value) return
  busyAction.value = 'client'
  try {
    await oidcStepUp.run(() => enabled
      ? adminAPI.oidcProvider.enableClient(id, { reason })
      : adminAPI.oidcProvider.disableClient(id, { reason }))
    appStore.showSuccess(t(enabled ? 'admin.oidcProvider.actions.enabled' : 'admin.oidcProvider.actions.disabled'))
    await selectClient(id)
    await loadClients()
  } catch (error: unknown) {
    reportActionError(error)
  } finally {
    busyAction.value = null
  }
}

function askReason(): string | null {
  const value = window.prompt(t('admin.oidcProvider.clients.reason'), '')
  const reason = value?.trim() || ''
  return reason || null
}

async function rotateKey(): Promise<void> {
  if (!canRotateKey.value) return
  const reason = askReason()
  if (!reason) return
  busyAction.value = 'key'
  try {
    await oidcStepUp.run(() => adminAPI.oidcProvider.rotateKey({ reason }))
    appStore.showSuccess(t('admin.oidcProvider.actions.keyRotated'))
    await Promise.all([loadKeys(), loadStatus()])
  } catch (error: unknown) {
    reportActionError(error)
  } finally {
    busyAction.value = null
  }
}

async function retireKey(kid: string): Promise<void> {
  if (!canRevokeKey.value) return
  const reason = askReason()
  if (!reason) return
  busyAction.value = 'key'
  try {
    await oidcStepUp.run(() => adminAPI.oidcProvider.retireKey(kid, { reason }))
    appStore.showSuccess(t('admin.oidcProvider.actions.keyRetired'))
    await Promise.all([loadKeys(), loadStatus()])
  } catch (error: unknown) {
    reportActionError(error)
  } finally {
    busyAction.value = null
  }
}

async function revokeKey(kid: string): Promise<void> {
  if (!canRevokeKey.value) return
  const reason = askReason()
  if (!reason) return
  busyAction.value = 'key'
  try {
    await oidcStepUp.run(() => adminAPI.oidcProvider.revokeKey(kid, { reason }))
    appStore.showSuccess(t('admin.oidcProvider.actions.keyRevoked'))
    await Promise.all([loadKeys(), loadStatus()])
  } catch (error: unknown) {
    reportActionError(error)
  } finally {
    busyAction.value = null
  }
}

async function revokeConsent(id: OidcResourceId): Promise<void> {
  if (!canRevokeConsent.value) return
  const reason = askReason()
  if (!reason) return
  busyConsentId.value = id
  try {
    await oidcStepUp.run(() => adminAPI.oidcProvider.revokeConsent(id, { reason }))
    appStore.showSuccess(t('admin.oidcProvider.actions.consentRevoked'))
    await loadConsents()
  } catch (error: unknown) {
    reportActionError(error)
  } finally {
    busyConsentId.value = null
  }
}

function clearIssuedSecret(): void {
  issuedSecret.value = null
}

function reportActionError(error: unknown): void {
  if (isStepUpCancelled(error)) return
  if (isStepUpBlocked(error)) {
    appStore.showError(stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? t('stepUp.adminApiKeyForbidden') : t('stepUp.notEnabled'))
    return
  }
  appStore.showError(errorMessage(error, t('admin.oidcProvider.errors.action')))
}

function statusLabel(status?: string): string {
  if (status === 'enabled') return t('admin.oidcProvider.status.enabled')
  if (status === 'disabled') return t('admin.oidcProvider.status.disabled')
  if (status === 'degraded') return t('admin.oidcProvider.status.degraded')
  return t('admin.oidcProvider.status.unknown')
}

function statusClass(status?: string): string {
  if (status === 'enabled') return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  if (status === 'degraded') return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
}

onMounted(() => {
  void loadAll()
})

onBeforeUnmount(() => {
  // The one-time plaintext secret must not survive the page lifecycle.
  issuedSecret.value = null
})
</script>

