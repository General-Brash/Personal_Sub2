<template>
  <section class="card overflow-hidden" aria-labelledby="oidc-client-detail-title">
    <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 id="oidc-client-detail-title" class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ isCreating ? t('admin.oidcProvider.clients.createTitle') : t('admin.oidcProvider.clients.editTitle') }}
          </h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ isCreating ? t('admin.oidcProvider.clients.description') : (client?.client_id || t('admin.oidcProvider.clients.selectHint')) }}
          </p>
        </div>
        <button v-if="isCreating" type="button" class="btn btn-secondary btn-sm" @click="$emit('cancel')">
          {{ t('admin.oidcProvider.clients.cancel') }}
        </button>
      </div>
    </div>

    <div v-if="!isCreating && !client" class="p-8 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.oidcProvider.clients.selectHint') }}
    </div>

    <form v-else class="space-y-5 p-5" novalidate @submit.prevent="submitDraft">
      <div v-if="formError" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-200" role="alert">{{ formError }}</div>

      <div class="grid gap-4 md:grid-cols-2">
        <label class="block">
          <span class="field-label">{{ t('admin.oidcProvider.clients.name') }}</span>
          <input v-model="draft.name" class="input" :placeholder="t('admin.oidcProvider.clients.namePlaceholder')" required />
        </label>
        <label class="block">
          <span class="field-label">{{ t('admin.oidcProvider.clients.owner') }}</span>
          <input v-model="draft.owner" class="input" :placeholder="t('admin.oidcProvider.clients.ownerPlaceholder')" required />
        </label>
      </div>

      <div>
        <div class="mb-2 flex items-center justify-between gap-2">
          <span class="field-label mb-0">{{ t('admin.oidcProvider.clients.redirects') }}</span>
          <button type="button" class="text-xs font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400" @click="addRedirect">
            {{ t('admin.oidcProvider.clients.addRedirect') }}
          </button>
        </div>
        <div class="space-y-2">
          <div v-for="(_, index) in draft.redirect_uris" :key="`redirect-${index}`" class="flex items-center gap-2">
            <input
              v-model="draft.redirect_uris[index]"
              class="input min-w-0 flex-1 font-mono text-sm"
              :placeholder="t('admin.oidcProvider.clients.redirectPlaceholder')"
              required
            />
            <button
              v-if="draft.redirect_uris.length > 1"
              type="button"
              class="btn btn-secondary btn-sm shrink-0"
              :aria-label="t('admin.oidcProvider.clients.removeRedirect')"
              @click="removeRedirect(index)"
            >
              ×
            </button>
          </div>
        </div>
      </div>

      <div>
        <span class="field-label">{{ t('admin.oidcProvider.clients.scopes') }}</span>
        <div class="mt-2 flex flex-wrap gap-2">
          <label
            v-for="scope in supportedScopes"
            :key="scope"
            class="inline-flex items-center gap-2 rounded border border-gray-200 px-3 py-2 text-sm dark:border-dark-600"
            :class="scope === 'openid' ? 'cursor-not-allowed opacity-70' : 'cursor-pointer'"
          >
            <input
              v-model="draft.allowed_scopes"
              type="checkbox"
              :value="scope"
              :disabled="scope === 'openid'"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
            />
            <span class="font-mono">{{ scope }}</span>
          </label>
        </div>
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.clients.scopeHint') }}</p>
      </div>

      <label class="flex items-start gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
        <input v-model="draft.trusted_skip_consent" type="checkbox" class="mt-0.5 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" />
        <span>
          <span class="block text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('admin.oidcProvider.clients.trusted') }}</span>
          <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.clients.trustedHint') }}</span>
        </span>
      </label>

      <label class="block">
        <span class="field-label">{{ t('admin.oidcProvider.clients.reason') }}</span>
        <textarea v-model="draft.reason" class="input min-h-20 resize-y" :placeholder="t('admin.oidcProvider.clients.reasonPlaceholder')" required />
      </label>

      <div class="flex flex-wrap items-center justify-end gap-2">
        <button v-if="isCreating" type="button" class="btn btn-secondary" @click="$emit('cancel')">
          {{ t('admin.oidcProvider.clients.cancel') }}
        </button>
        <button type="submit" class="btn btn-primary" :disabled="submitting || !canWrite">
          {{ submitting ? t('common.processing') : t('admin.oidcProvider.clients.save') }}
        </button>
      </div>

      <template v-if="client && !isCreating">
        <div class="border-t border-gray-100 pt-5 dark:border-dark-700">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.oidcProvider.clients.secretMetadata') }}</h3>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.security.oneTimeSecret') }}</p>
            </div>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="busyAction !== null || !canRotateSecret"
              @click="rotateSecret"
            >
              {{ t('admin.oidcProvider.clients.rotateSecret') }}
            </button>
          </div>

          <p v-if="client.enabled && !client.secrets?.some(secret => ['usable', 'expiring'].includes(secretAvailability(secret)))"
             class="mt-3 rounded-lg border border-red-300 bg-red-50 px-3 py-2 text-sm font-semibold text-red-800 dark:border-red-800 dark:bg-red-950/30 dark:text-red-200" role="alert">
            {{ expiryLabel('none') }}
          </p>
          <div v-if="client.secrets?.length" class="mt-3 overflow-x-auto">
            <table class="min-w-full text-left text-sm">
              <thead class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
                <tr>
                  <th class="px-2 py-2">{{ t('admin.oidcProvider.clients.secretFingerprint') }}</th>
                  <th class="px-2 py-2">{{ t('admin.oidcProvider.clients.secretStatus') }}</th>
                  <th class="px-2 py-2">{{ t('admin.oidcProvider.clients.secretNotBefore') }}</th>
                  <th class="px-2 py-2">{{ t('admin.oidcProvider.clients.secretExpiresAt') }}</th>
                  <th class="px-2 py-2"><span class="sr-only">{{ t('admin.oidcProvider.clients.revokeSecret') }}</span></th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                <tr v-for="secret in client.secrets" :key="secret.id">
                  <td class="px-2 py-2 font-mono text-xs text-gray-700 dark:text-gray-300">{{ secret.fingerprint }}</td>
                  <td class="px-2 py-2">
                    <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="secretStatusClass(secretAvailability(secret))">{{ secret.status }} · {{ expiryLabel(secretAvailability(secret)) }}</span>
                  </td>
                  <td class="px-2 py-2 text-xs text-gray-600 dark:text-gray-400">{{ formatDate(secret.not_before) }}</td>
                  <td class="px-2 py-2 text-xs text-gray-600 dark:text-gray-400">{{ formatDate(secret.expires_at) }}</td>
                  <td class="px-2 py-2 text-right">
                    <button
                      v-if="secret.status === 'active' || secret.status === 'retiring'"
                      type="button"
                      class="text-xs font-medium text-red-600 hover:text-red-700 dark:text-red-400"
                      :disabled="busyAction !== null || !canRotateSecret"
                      @click="revokeSecret(secret.id)"
                    >
                      {{ t('admin.oidcProvider.clients.revokeSecret') }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-else class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.clients.noSecrets') }}</p>
        </div>

        <div class="border-t border-gray-100 pt-5 dark:border-dark-700">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.oidcProvider.clients.enabled') }}</h3>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ client.enabled ? t('admin.oidcProvider.clients.trustedEnabled') : t('admin.oidcProvider.clients.disabled') }}</p>
            </div>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="busyAction !== null || (!client.enabled ? !canDisable : !canDisable)"
              @click="toggleClient"
            >
              {{ client.enabled ? t('admin.oidcProvider.clients.disable') : t('admin.oidcProvider.clients.enable') }}
            </button>
          </div>
        </div>
      </template>
    </form>

    <div v-if="issuedSecret" class="border-t border-amber-200 bg-amber-50 p-5 dark:border-amber-900/60 dark:bg-amber-950/20">
      <div class="flex items-start gap-3">
        <div class="min-w-0 flex-1">
          <h3 class="font-semibold text-amber-900 dark:text-amber-100">{{ t('admin.oidcProvider.clients.oneTimeTitle') }}</h3>
          <p class="mt-1 text-sm text-amber-800 dark:text-amber-200">{{ t('admin.oidcProvider.clients.oneTimeWarning') }}</p>
          <div class="mt-3 flex flex-col gap-2 sm:flex-row">
            <code class="min-w-0 flex-1 break-all rounded border border-amber-300 bg-white px-3 py-2 text-sm text-gray-900 dark:border-amber-800 dark:bg-dark-900 dark:text-gray-100">{{ issuedSecret.client_secret }}</code>
            <button type="button" class="btn btn-secondary btn-sm shrink-0" @click="copySecret">
              {{ copied ? t('admin.oidcProvider.clients.copied') : t('admin.oidcProvider.clients.copySecret') }}
            </button>
          </div>
          <p class="mt-2 text-xs text-amber-800 dark:text-amber-200">{{ issuedSecret.secret.fingerprint }} · {{ formatDate(issuedSecret.secret.expires_at) }}</p>
        </div>
        <button type="button" class="text-amber-800 hover:text-amber-950 dark:text-amber-200 dark:hover:text-white" :aria-label="t('admin.oidcProvider.clients.dismissSecret')" @click="$emit('clear-issued-secret')">×</button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OidcClientDetail, OidcClientDraft, OidcClientSecretSummary, OidcClientSecretIssueResponse, OidcResourceId } from '@/api/admin'

const props = defineProps<{
  client: OidcClientDetail | null
  isCreating: boolean
  canWrite: boolean
  canRotateSecret: boolean
  canDisable: boolean
  submitting: boolean
  busyAction: string | null
  issuedSecret: OidcClientSecretIssueResponse | null
}>()

const emit = defineEmits<{
  submit: [draft: OidcClientDraft]
  cancel: []
  rotateSecret: [id: OidcResourceId, reason: string]
  revokeSecret: [id: OidcResourceId, reason: string]
  toggle: [id: OidcResourceId, enabled: boolean, reason: string]
  'clear-issued-secret': []
}>()

const { t, locale } = useI18n()
const supportedScopes = ['openid', 'profile', 'email', 'roles', 'offline_access']
const copied = ref(false)
const formError = ref('')
const currentTime = ref(Date.now())
let clockTimer: ReturnType<typeof setTimeout> | undefined

function scheduleClock(): void {
  clearTimeout(clockTimer)
  clockTimer = undefined
  if (document.hidden || props.isCreating || !props.client?.secrets?.length) return
  const now = Date.now()
  const boundaries = props.client.secrets
    .filter(secret => secret.status === 'active' || secret.status === 'retiring')
    .flatMap(secret => {
      const start = Date.parse(secret.not_before)
      const end = Date.parse(secret.expires_at)
      return [start, end - 7 * 86400 * 1000, end]
    })
    .filter(boundary => Number.isFinite(boundary) && boundary > now)
  const next = Math.min(60_000, ...boundaries.map(boundary => boundary - now))
  clockTimer = setTimeout(refreshClock, Math.max(1, next))
}

function refreshClock(): void {
  currentTime.value = Date.now()
  scheduleClock()
}

function onVisibilityChange(): void {
  if (document.hidden) {
    clearTimeout(clockTimer)
    clockTimer = undefined
  } else {
    refreshClock()
  }
}

onMounted(() => {
  document.addEventListener('visibilitychange', onVisibilityChange)
  refreshClock()
})
onUnmounted(() => {
  clearTimeout(clockTimer)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
watch(() => [props.client, props.isCreating], refreshClock)

const draft = reactive<OidcClientDraft>({
  name: '',
  owner: '',
  redirect_uris: [''],
  allowed_scopes: ['openid', 'profile'],
  trusted_skip_consent: false,
  reason: '',
})

function resetDraft(): void {
  draft.name = props.client?.name || ''
  draft.owner = props.client?.owner || ''
  draft.redirect_uris = props.client?.redirect_uris?.length ? [...props.client.redirect_uris] : ['']
  draft.allowed_scopes = props.client?.allowed_scopes?.length ? [...props.client.allowed_scopes] : ['openid', 'profile']
  if (!draft.allowed_scopes.includes('openid')) draft.allowed_scopes.unshift('openid')
  draft.trusted_skip_consent = props.client?.trusted_skip_consent || false
  draft.reason = ''
  copied.value = false
  formError.value = ''
}

watch(() => [props.client?.id, props.isCreating], resetDraft, { immediate: true })

function addRedirect(): void {
  draft.redirect_uris.push('')
}

function removeRedirect(index: number): void {
  draft.redirect_uris.splice(index, 1)
}

function submitDraft(): void {
  const redirectUris = draft.redirect_uris.map((value) => value.trim()).filter(Boolean)
  if (!draft.name.trim() || !draft.owner.trim() || redirectUris.length === 0) {
    formError.value = t('admin.oidcProvider.errors.invalidForm')
    return
  }
  if (!draft.reason.trim()) {
    formError.value = t('admin.oidcProvider.clients.reasonRequired')
    return
  }
  formError.value = ''
  emit('submit', {
    name: draft.name.trim(),
    owner: draft.owner.trim(),
    redirect_uris: redirectUris,
    allowed_scopes: [...new Set(['openid', ...draft.allowed_scopes])],
    trusted_skip_consent: draft.trusted_skip_consent,
    reason: draft.reason.trim(),
  })
}

function rotateSecret(): void {
  if (!props.client) return
  if (!draft.reason.trim()) {
    formError.value = t('admin.oidcProvider.clients.reasonRequired')
    return
  }
  if (window.confirm(t('admin.oidcProvider.clients.confirmRotate'))) {
    emit('rotateSecret', props.client.id, draft.reason.trim())
  }
}

function revokeSecret(id: OidcResourceId): void {
  if (!draft.reason.trim()) {
    formError.value = t('admin.oidcProvider.clients.reasonRequired')
    return
  }
  if (window.confirm(t('admin.oidcProvider.clients.confirmRevoke'))) {
    emit('revokeSecret', id, draft.reason.trim())
  }
}

function toggleClient(): void {
  if (!props.client) return
  if (!draft.reason.trim()) {
    formError.value = t('admin.oidcProvider.clients.reasonRequired')
    return
  }
  const message = props.client.enabled
    ? t('admin.oidcProvider.clients.confirmDisable')
    : t('admin.oidcProvider.clients.confirmEnable')
  if (window.confirm(message)) {
    emit('toggle', props.client.id, !props.client.enabled, draft.reason.trim())
  }
}

async function copySecret(): Promise<void> {
  if (!props.issuedSecret) return
  try {
    await navigator.clipboard.writeText(props.issuedSecret.client_secret)
    copied.value = true
  } catch {
    copied.value = false
  }
}

function formatDate(value?: string | null): string {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

type SecretAvailability = 'usable' | 'expiring' | 'expired' | 'notYet' | 'inactive' | 'unknown'

function secretAvailability(secret: OidcClientSecretSummary): SecretAvailability {
  if (secret.status !== 'active' && secret.status !== 'retiring') return 'inactive'
  const start = Date.parse(secret.not_before)
  const end = Date.parse(secret.expires_at)
  if (!Number.isFinite(start) || !Number.isFinite(end)) return 'unknown'
  const now = currentTime.value
  if (end <= now) return 'expired'
  if (start > now) return 'notYet'
  return end - now <= 7 * 86400 * 1000 ? 'expiring' : 'usable'
}

function expiryLabel(state: SecretAvailability | 'none'): string {
  const zh = locale.value.startsWith('zh')
  const labels = zh
    ? { usable: '有效', expiring: '7 天内到期，请准备轮换', expired: '已过期，不可用', notYet: '尚未生效', inactive: '不可用', unknown: '有效期未知，不可判定可用', none: '此客户端没有当前可用的密钥，请安全轮换并更新依赖端' }
    : { usable: 'Usable', expiring: 'Expires within 7 days — plan rotation', expired: 'Expired — unusable', notYet: 'Not yet valid', inactive: 'Unavailable', unknown: 'Validity unknown — not verified usable', none: 'No currently usable secret. Rotate securely and update the relying party.' }
  return labels[state]
}

function secretStatusClass(state: SecretAvailability): string {
  if (state === 'usable') return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  if (state === 'expired' || state === 'inactive') return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
  return 'bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-200'
}
</script>
