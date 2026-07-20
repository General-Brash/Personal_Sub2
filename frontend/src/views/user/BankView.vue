<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <header class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('bank.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('bank.description') }}
          </p>
        </div>
        <button
          v-if="authStore.isAdmin"
          data-test="bank-settings-button"
          type="button"
          class="btn btn-secondary inline-flex min-h-10 items-center justify-center gap-2 self-start"
          :disabled="settingsLoading"
          @click="openSettings"
        >
          <Icon name="cog" size="sm" />
          {{ t('bank.actions.settings') }}
        </button>
      </header>

      <section v-if="loading" data-test="bank-loading" class="card flex min-h-48 items-center justify-center">
        <Icon name="refresh" size="lg" class="animate-spin text-primary-600" />
      </section>

      <section
        v-else-if="loadFailed && !status"
        data-test="bank-load-error"
        class="card flex min-h-48 flex-col items-center justify-center gap-3 p-6 text-center"
      >
        <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('bank.errors.loadStatus') }}</p>
        <button
          type="button"
          class="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-600 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-700"
          :aria-label="t('bank.actions.reloadStatus')"
          :title="t('bank.actions.reload')"
          @click="retryStatus"
        >
          <Icon name="refresh" size="sm" />
        </button>
      </section>

      <template v-else-if="status">
        <section class="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <article class="card min-w-0 p-5">
            <div class="flex items-center gap-3">
              <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-indigo-50 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-300">
                <Icon name="creditCard" size="md" />
              </span>
              <div class="min-w-0">
                <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('bank.balances.permanent') }}</p>
                <p
                  data-test="permanent-balance"
                  class="mt-1 break-all font-mono text-xl font-semibold text-gray-900 dark:text-white"
                  :class="isNegative(status.permanent_balance) ? 'text-red-600 dark:text-red-400' : ''"
                >
                  {{ formatAmount(status.permanent_balance) }}
                </p>
              </div>
            </div>
            <p v-if="isNegative(status.permanent_balance)" class="mt-3 text-xs text-red-600 dark:text-red-400">
              {{ t('bank.balances.negativeBlocked') }}
            </p>
          </article>

          <article class="card min-w-0 p-5">
            <div class="flex items-center gap-3">
              <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300">
                <Icon name="bolt" size="md" />
              </span>
              <div class="min-w-0">
                <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('bank.balances.temporaryAvailable') }}</p>
                <p data-test="temporary-balance" class="mt-1 break-all font-mono text-xl font-semibold text-emerald-600 dark:text-emerald-400">
                  {{ formatAmount(status.temporary_credit_available) }}
                </p>
              </div>
            </div>
            <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
              {{ t('bank.balances.earliestExpiry', { date: formatDateTime(status.temporary_credit_earliest_expires_at) }) }}
            </p>
          </article>

          <article class="card min-w-0 p-5">
            <div class="flex items-center gap-3">
              <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-red-50 text-red-600 dark:bg-red-900/30 dark:text-red-300">
                <Icon name="exclamationTriangle" size="md" />
              </span>
              <div class="min-w-0">
                <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('bank.balances.temporaryDebt') }}</p>
                <p data-test="temporary-debt" class="mt-1 break-all font-mono text-xl font-semibold text-red-600 dark:text-red-400">
                  {{ formatAmount(status.temporary_debt) }}
                </p>
              </div>
            </div>
            <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
              {{ t('bank.balances.debtDueAt', { date: formatDateTime(status.temporary_debt_due_at) }) }}
            </p>
          </article>
        </section>

        <section v-if="status.active_advance" data-test="active-advance" class="card border-l-4 border-l-amber-500 p-5">
          <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('bank.advance.activeTitle') }}</p>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('bank.advance.activeDescription') }}
              </p>
            </div>
            <span class="self-start rounded-md bg-amber-50 px-2.5 py-1 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300">
              {{ advanceStatusLabel(status.active_advance.status) }}
            </span>
          </div>
          <dl class="mt-4 grid grid-cols-1 gap-4 text-sm sm:grid-cols-3">
            <div>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('bank.advance.principal') }}</dt>
              <dd class="mt-1 break-all font-mono font-semibold text-gray-900 dark:text-white">{{ formatAmount(status.active_advance.principal) }}</dd>
            </div>
            <div>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('bank.advance.debtRemaining') }}</dt>
              <dd class="mt-1 break-all font-mono font-semibold text-red-600 dark:text-red-400">{{ formatAmount(status.active_advance.debt_remaining) }}</dd>
            </div>
            <div>
              <dt class="text-gray-500 dark:text-gray-400">{{ t('bank.advance.settlementAt') }}</dt>
              <dd class="mt-1 font-medium text-gray-900 dark:text-white">{{ formatDateTime(status.active_advance.settlement_due_at) }}</dd>
            </div>
          </dl>
        </section>

        <section data-test="bank-operation-card" class="card p-5 sm:p-6">
          <div class="flex flex-col gap-5">
            <div class="flex items-start gap-3">
              <span
                class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg"
                :class="activeBankMode === 'advance'
                  ? 'bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300'
                  : 'bg-indigo-50 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-300'"
              >
                <Icon :name="activeBankMode === 'advance' ? 'download' : 'swap'" size="md" />
              </span>
              <div class="min-w-0">
                <h2 class="text-base font-semibold text-gray-900 dark:text-white">
                  {{ activeBankMode === 'advance' ? t('bank.advance.title') : t('bank.exchange.title') }}
                </h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                  {{ activeBankMode === 'advance'
                    ? t('bank.advance.description')
                    : t('bank.exchange.description', { rate: formatAmount(status.policy.exchange_rate) }) }}
                </p>
                <p
                  v-if="activeBankMode === 'exchange' && exchangeMaintenanceActive"
                  data-test="exchange-maintenance"
                  class="mt-2 text-xs font-medium text-amber-600 dark:text-amber-400"
                >
                  {{ t('bank.exchangeMaintenance') }}
                </p>
              </div>
            </div>

            <div
              data-test="bank-mode-selector"
              role="tablist"
              aria-orientation="horizontal"
              :aria-label="t('bank.operationMode')"
              class="relative grid min-h-11 grid-cols-2 overflow-hidden rounded-lg bg-gray-100 p-1 dark:bg-dark-700"
            >
              <span
                data-test="bank-mode-indicator"
                aria-hidden="true"
                class="pointer-events-none absolute inset-y-1 left-1 rounded-md bg-white shadow-sm transition-transform duration-200 ease-out dark:bg-dark-800"
                :class="activeBankMode === 'exchange' ? 'translate-x-full' : 'translate-x-0'"
                style="width: calc(50% - 0.25rem)"
              />
              <button
                id="bank-mode-advance"
                data-test="bank-mode-advance"
                ref="advanceModeTab"
                type="button"
                role="tab"
                class="relative z-10 min-w-0 px-3 py-2 text-sm font-medium transition-colors"
                :class="activeBankMode === 'advance'
                  ? 'text-primary-700 dark:text-primary-300'
                  : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'"
                :aria-selected="activeBankMode === 'advance'"
                :tabindex="activeBankMode === 'advance' ? 0 : -1"
                aria-controls="bank-panel-advance"
                @keydown="handleBankModeKeydown($event, 'advance')"
                @click="selectBankMode('advance', true)"
              >
                {{ t('bank.advance.title') }}
              </button>
              <button
                id="bank-mode-exchange"
                data-test="bank-mode-exchange"
                ref="exchangeModeTab"
                type="button"
                role="tab"
                class="relative z-10 min-w-0 px-3 py-2 text-sm font-medium transition-colors"
                :class="activeBankMode === 'exchange'
                  ? 'text-primary-700 dark:text-primary-300'
                  : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'"
                :aria-selected="activeBankMode === 'exchange'"
                :tabindex="activeBankMode === 'exchange' ? 0 : -1"
                aria-controls="bank-panel-exchange"
                @keydown="handleBankModeKeydown($event, 'exchange')"
                @click="selectBankMode('exchange', true)"
              >
                {{ t('bank.exchange.title') }}
              </button>
            </div>
          </div>

          <form
            id="bank-panel-advance"
            data-test="advance-flow"
            role="tabpanel"
            aria-labelledby="bank-mode-advance"
            :hidden="activeBankMode !== 'advance'"
            :aria-hidden="activeBankMode !== 'advance'"
            class="mt-5 space-y-4"
            @submit.prevent="submitAdvance"
          >
            <div class="grid grid-cols-1 items-stretch gap-3 min-[390px]:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] min-[390px]:gap-2 sm:gap-4">
              <div class="min-w-0 rounded-lg border border-gray-200 bg-gray-50/60 p-3 dark:border-dark-600 dark:bg-dark-900/30 sm:p-4">
                <label for="bank-advance-amount" class="input-label">{{ t('bank.advance.amount') }}</label>
                <input
                  id="bank-advance-amount"
                  v-model.trim="advanceAmount"
                  data-test="advance-input"
                  type="text"
                  inputmode="decimal"
                  autocomplete="off"
                  class="input mt-2 min-w-0 font-mono"
                  :class="advanceError ? 'input-error ring-2 ring-red-500/20' : ''"
                  :disabled="advanceSubmitting || Boolean(status.active_advance) || hasNegativePermanentBalance"
                  :placeholder="`${formatAmount(status.policy.advance_min_amount)} - ${formatAmount(status.policy.advance_max_amount)}`"
                  @input="advanceTouched = true"
                />
                <p v-if="advanceError" data-test="advance-error" class="input-error-text mt-1.5">{{ advanceError }}</p>
                <p v-else class="input-hint mt-1.5">
                  {{ t('bank.advance.range', {
                    min: formatAmount(status.policy.advance_min_amount),
                    max: formatAmount(status.policy.advance_max_amount),
                  }) }}
                </p>
              </div>

              <div class="flex min-w-8 items-center justify-center sm:min-w-12">
                <span class="flex h-9 w-9 rotate-90 items-center justify-center rounded-full bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300 min-[390px]:rotate-0">
                  <Icon name="arrowRight" size="sm" />
                </span>
              </div>

              <div
                data-test="advance-wallet"
                class="flex min-w-0 flex-col justify-between rounded-lg border border-emerald-200 bg-emerald-50/60 p-3 dark:border-emerald-900/60 dark:bg-emerald-950/20 sm:p-4"
              >
                <div class="flex items-center gap-2 text-emerald-700 dark:text-emerald-300">
                  <Icon name="creditCard" size="sm" />
                  <p class="text-sm font-semibold">{{ t('bank.wallet.title') }}</p>
                </div>
                <div class="mt-4 min-w-0">
                  <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('bank.wallet.available') }}</p>
                  <p data-test="advance-wallet-balance" class="mt-1 break-all font-mono text-lg font-semibold text-emerald-700 dark:text-emerald-300">
                    {{ formatAmount(status.temporary_credit_available) }}
                  </p>
                </div>
              </div>
            </div>

            <button
              data-test="advance-submit"
              type="submit"
              class="btn btn-primary inline-flex min-h-10 w-full items-center justify-center gap-2"
              :disabled="!canSubmitAdvance"
            >
              <Icon v-if="advanceSubmitting" name="refresh" size="sm" class="animate-spin" />
              <Icon v-else name="download" size="sm" />
              {{ advanceSubmitting
                ? t('bank.advance.submitting')
                : status.active_advance
                  ? t('bank.advance.activeButton')
                  : t('bank.advance.confirm') }}
            </button>
          </form>

          <form
            id="bank-panel-exchange"
            data-test="exchange-flow"
            role="tabpanel"
            aria-labelledby="bank-mode-exchange"
            :hidden="activeBankMode !== 'exchange'"
            :aria-hidden="activeBankMode !== 'exchange'"
            class="mt-5 space-y-4"
            @submit.prevent="submitExchange"
          >
            <div
              data-test="exchange-flow-grid"
              class="grid grid-cols-1 items-stretch gap-3 min-[390px]:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] min-[390px]:gap-2 sm:gap-4"
            >
              <div data-test="exchange-input-card" class="min-w-0 rounded-lg border border-gray-200 bg-gray-50/60 p-3 dark:border-dark-600 dark:bg-dark-900/30 sm:p-4">
                <label for="bank-exchange-amount" data-test="exchange-input-label" class="input-label min-w-0 break-words">
                  {{ t('bank.exchange.amount') }}
                </label>
                <input
                  id="bank-exchange-amount"
                  v-model.trim="exchangeAmount"
                  data-test="exchange-input"
                  type="text"
                  inputmode="decimal"
                  autocomplete="off"
                  class="input mt-2 min-w-0 font-mono"
                  :class="exchangeError ? 'input-error ring-2 ring-red-500/20' : ''"
                  :disabled="exchangeSubmitting || hasNegativePermanentBalance || exchangeMaintenanceActive"
                  placeholder="0.00"
                  @input="exchangeTouched = true"
                />
                <p v-if="exchangeError" data-test="exchange-error" class="input-error-text mt-1.5">{{ exchangeError }}</p>
              </div>

              <div class="flex min-w-12 flex-col items-center justify-center gap-2 sm:min-w-20">
                <p data-test="exchange-rate" class="max-w-full break-words text-center text-xs font-medium text-gray-500 dark:text-gray-400 min-[390px]:max-w-20">
                  {{ t('bank.exchange.rate', { rate: formatAmount(status.policy.exchange_rate) }) }}
                </p>
                <span class="flex h-9 w-9 rotate-90 items-center justify-center rounded-full bg-indigo-50 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-300 min-[390px]:rotate-0">
                  <Icon name="arrowRight" size="sm" />
                </span>
              </div>

              <output
                for="bank-exchange-amount"
                data-test="exchange-preview"
                aria-live="polite"
                class="flex min-w-0 flex-col justify-center rounded-lg border border-indigo-200 bg-indigo-50/60 p-3 dark:border-indigo-900/60 dark:bg-indigo-950/20 sm:p-4"
              >
                <p data-test="exchange-preview-label" class="min-w-0 break-words text-xs text-gray-500 dark:text-gray-400">
                  {{ t('bank.exchange.preview') }}
                </p>
                <p data-test="exchange-preview-amount" class="mt-2 break-all font-mono text-lg font-semibold text-indigo-700 dark:text-indigo-300">
                  {{ formatAmount(estimatedTemporaryAmount) }}
                </p>
              </output>
            </div>

            <button
              data-test="exchange-submit"
              type="submit"
              class="btn btn-primary inline-flex min-h-10 w-full items-center justify-center gap-2"
              :disabled="!canSubmitExchange"
            >
              <Icon v-if="exchangeSubmitting" name="refresh" size="sm" class="animate-spin" />
              <Icon v-else name="swap" size="sm" />
              {{ exchangeSubmitting ? t('bank.exchange.submitting') : t('bank.exchange.confirm') }}
            </button>
          </form>
        </section>

        <section class="card overflow-hidden">
          <div class="flex flex-col gap-1 border-b border-gray-100 px-5 py-4 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('bank.ledger.title') }}</h2>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('bank.ledger.description') }}</p>
            </div>
            <button
              type="button"
              class="inline-flex h-9 w-9 items-center justify-center self-end rounded-lg text-gray-500 transition-colors hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-dark-700 sm:self-auto"
              :aria-label="t('bank.ledger.refresh')"
              :title="t('bank.actions.refresh')"
              :disabled="refreshing"
              @click="loadStatus(true)"
            >
              <Icon name="refresh" size="sm" :class="refreshing ? 'animate-spin' : ''" />
            </button>
          </div>

          <div v-if="!status.ledger.length" data-test="empty-ledger" class="px-5 py-10 text-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('bank.ledger.empty') }}
          </div>
          <div v-else class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700">
              <thead class="bg-gray-50 dark:bg-dark-900/40">
                <tr class="text-left text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
                  <th class="whitespace-nowrap px-5 py-3">{{ t('bank.ledger.columns.time') }}</th>
                  <th class="whitespace-nowrap px-5 py-3">{{ t('bank.ledger.columns.operation') }}</th>
                  <th class="whitespace-nowrap px-5 py-3 text-right">{{ t('bank.ledger.columns.permanentDelta') }}</th>
                  <th class="whitespace-nowrap px-5 py-3 text-right">{{ t('bank.ledger.columns.temporaryDelta') }}</th>
                  <th class="whitespace-nowrap px-5 py-3 text-right">{{ t('bank.ledger.columns.debtDelta') }}</th>
                  <th class="whitespace-nowrap px-5 py-3 text-right">{{ t('bank.ledger.columns.debtAfter') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-800">
                <tr v-for="item in status.ledger" :key="item.id" data-test="ledger-row" class="text-gray-700 dark:text-gray-300">
                  <td class="whitespace-nowrap px-5 py-3">{{ formatDateTime(item.created_at) }}</td>
                  <td class="whitespace-nowrap px-5 py-3 font-medium text-gray-900 dark:text-white">{{ operationLabel(item.operation) }}</td>
                  <td class="whitespace-nowrap px-5 py-3 text-right font-mono" :class="deltaClass(item.permanent_delta)">{{ formatSignedAmount(item.permanent_delta) }}</td>
                  <td class="whitespace-nowrap px-5 py-3 text-right font-mono" :class="deltaClass(item.temporary_delta)">{{ formatSignedAmount(item.temporary_delta) }}</td>
                  <td class="whitespace-nowrap px-5 py-3 text-right font-mono" :class="deltaClass(item.debt_delta, true)">{{ formatSignedAmount(item.debt_delta) }}</td>
                  <td class="whitespace-nowrap px-5 py-3 text-right font-mono">{{ formatAmount(item.debt_after) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </template>
    </div>

    <BaseDialog
      :show="showSettingsDialog"
      :title="t('bank.settings.title')"
      width="normal"
      :close-on-click-outside="false"
      @close="closeSettings"
    >
      <div v-if="settingsLoading" data-test="settings-loading" class="flex min-h-40 items-center justify-center">
        <Icon name="refresh" size="lg" class="animate-spin text-primary-600" />
      </div>
      <div v-else-if="settingsLoadFailed" class="flex min-h-40 flex-col items-center justify-center gap-3 text-center">
        <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('bank.settings.loadFailed') }}</p>
        <button type="button" class="btn btn-secondary" @click="loadSettings">{{ t('bank.actions.reload') }}</button>
      </div>
      <form v-else class="space-y-5" @submit.prevent="saveSettings">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label for="bank-min-advance" class="input-label">{{ t('bank.settings.advanceMin') }}</label>
            <input id="bank-min-advance" v-model.trim="settingsForm.advance_min_amount" data-test="settings-min" type="text" inputmode="decimal" class="input mt-1 font-mono" :disabled="settingsSaving" />
          </div>
          <div>
            <label for="bank-max-advance" class="input-label">{{ t('bank.settings.advanceMax') }}</label>
            <input id="bank-max-advance" v-model.trim="settingsForm.advance_max_amount" data-test="settings-max" type="text" inputmode="decimal" class="input mt-1 font-mono" :disabled="settingsSaving" />
          </div>
        </div>
        <div>
          <label for="bank-grace-days" class="input-label">{{ t('bank.settings.graceDays') }}</label>
          <input id="bank-grace-days" v-model.trim="settingsForm.debt_grace_days" data-test="settings-grace-days" type="number" min="1" max="365" step="1" class="input mt-1" :disabled="settingsSaving" />
          <p class="input-hint mt-1.5">{{ t('bank.settings.graceDaysHint') }}</p>
        </div>
        <div>
          <label for="bank-debt-ratio" class="input-label">{{ t('bank.settings.debtRatio') }}</label>
          <input id="bank-debt-ratio" v-model.trim="settingsForm.debt_conversion_ratio" data-test="settings-debt-ratio" type="text" inputmode="decimal" class="input mt-1 font-mono" :disabled="settingsSaving" />
          <p class="input-hint mt-1.5">{{ t('bank.settings.debtRatioHint') }}</p>
        </div>
        <div>
          <label for="bank-exchange-rate" class="input-label">{{ t('bank.settings.exchangeRate') }}</label>
          <input id="bank-exchange-rate" v-model.trim="settingsForm.exchange_rate" data-test="settings-exchange-rate" type="text" inputmode="decimal" class="input mt-1 font-mono" :disabled="settingsSaving" />
          <p class="input-hint mt-1.5">{{ t('bank.settings.exchangeRateHint') }}</p>
        </div>
        <p v-if="settingsError" data-test="settings-error" class="input-error-text">{{ settingsError }}</p>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" :disabled="settingsSaving" @click="closeSettings">{{ t('bank.actions.cancel') }}</button>
          <button
            data-test="settings-save"
            type="button"
            class="btn btn-primary inline-flex min-w-20 items-center justify-center gap-2"
            :disabled="settingsLoading || settingsLoadFailed || settingsSaving"
            @click="saveSettings"
          >
            <Icon v-if="settingsSaving" name="refresh" size="sm" class="animate-spin" />
            {{ settingsSaving ? t('bank.actions.saving') : t('bank.actions.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  exchangePermanentForTemporary,
  getBankSettings,
  getBankStatus,
  requestBankAdvance,
  updateBankSettings,
  type BankPolicy,
  type BankStatus,
} from '@/api/bank'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { formatMoneyDisplay } from '@/utils/format'

interface SettingsForm {
  advance_min_amount: string
  advance_max_amount: string
  debt_grace_days: string
  debt_conversion_ratio: string
  exchange_rate: string
}

const decimalAmountPattern = /^(?:0|[1-9]\d{0,11})(?:\.\d{1,8})?$/
const zeroAmount = '0.00000000'
const amountScale = 100_000_000n
const exchangeMaintenanceErrorCode = 'BANK_EXCHANGE_MAINTENANCE_WINDOW'
const shanghaiTimeFormatter = new Intl.DateTimeFormat('en-GB', {
  timeZone: 'Asia/Shanghai',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
})

const { locale, t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const status = ref<BankStatus | null>(null)
const loading = ref(true)
const refreshing = ref(false)
const loadFailed = ref(false)
type BankMode = 'advance' | 'exchange'

const activeBankMode = ref<BankMode>('advance')
const advanceModeTab = ref<HTMLButtonElement | null>(null)
const exchangeModeTab = ref<HTMLButtonElement | null>(null)
const advanceAmount = ref('')
const exchangeAmount = ref('')
const advanceTouched = ref(false)
const exchangeTouched = ref(false)
const advanceSubmitting = ref(false)
const exchangeSubmitting = ref(false)
const exchangeMaintenanceActive = ref(isExchangeMaintenanceWindow())
const showSettingsDialog = ref(false)
const settingsLoading = ref(false)
const settingsLoadFailed = ref(false)
const settingsSaving = ref(false)
const settingsError = ref('')
let latestStatusRequest = 0
let exchangeMaintenanceTimer: number | undefined

const settingsForm = reactive<SettingsForm>({
  advance_min_amount: '',
  advance_max_amount: '',
  debt_grace_days: '3',
  debt_conversion_ratio: '',
  exchange_rate: '',
})

const hasNegativePermanentBalance = computed(() => isNegative(status.value?.permanent_balance))

const advanceError = computed(() => {
  if (!advanceTouched.value && !advanceAmount.value) return ''
  const value = parseScaledAmount(advanceAmount.value)
  if (value === null || value <= 0n) return t('bank.validation.positiveAmount')
  if (!status.value) return ''
  const minimum = parseScaledAmount(status.value.policy.advance_min_amount)
  const maximum = parseScaledAmount(status.value.policy.advance_max_amount)
  if (minimum !== null && value < minimum) {
    return t('bank.validation.advanceBelowMin', { amount: formatAmount(status.value.policy.advance_min_amount) })
  }
  if (maximum !== null && value > maximum) {
    return t('bank.validation.advanceAboveMax', { amount: formatAmount(status.value.policy.advance_max_amount) })
  }
  return ''
})

const exchangeError = computed(() => {
  if (!exchangeTouched.value && !exchangeAmount.value) return ''
  const value = parseScaledAmount(exchangeAmount.value)
  if (value === null || value <= 0n) return t('bank.validation.positiveAmount')
  const balance = parseScaledAmount(status.value?.permanent_balance)
  if (balance !== null && value > balance) return t('bank.validation.insufficientPermanent')
  return ''
})

const canSubmitAdvance = computed(() => Boolean(
  status.value
  && !status.value.active_advance
  && !hasNegativePermanentBalance.value
  && !advanceSubmitting.value
  && advanceAmount.value
  && !advanceError.value,
))

const canSubmitExchange = computed(() => Boolean(
  status.value
  && !hasNegativePermanentBalance.value
  && !exchangeMaintenanceActive.value
  && !exchangeSubmitting.value
  && exchangeAmount.value
  && !exchangeError.value,
))

const estimatedTemporaryAmount = computed(() => multiplyAmounts(
  exchangeAmount.value,
  status.value?.policy.exchange_rate,
))

async function loadStatus(background = false, refreshUserAfter = true): Promise<void> {
  const request = ++latestStatusRequest
  if (background) refreshing.value = true
  else if (!status.value) loading.value = true
  loadFailed.value = false
  try {
    const nextStatus = await getBankStatus()
    if (request !== latestStatusRequest) return
    status.value = nextStatus
    if (refreshUserAfter) await refreshUserSilently()
  } catch (error) {
    if (request !== latestStatusRequest) return
    console.error('Failed to load bank status:', error)
    loadFailed.value = true
    if (status.value) appStore.showError(errorMessage(error, t('bank.errors.refreshStatus')))
  } finally {
    if (request === latestStatusRequest) {
      loading.value = false
      refreshing.value = false
    }
  }
}

async function submitAdvance(): Promise<void> {
  advanceTouched.value = true
  if (!canSubmitAdvance.value) return
  advanceSubmitting.value = true
  try {
    await requestBankAdvance(
      advanceAmount.value,
      getOrCreateMutationIdempotencyKey('advance', advanceAmount.value),
    )
    clearMutationIdempotencyKey('advance')
    advanceAmount.value = ''
    advanceTouched.value = false
    appStore.showSuccess(t('bank.messages.advanceSucceeded'))
    await Promise.all([loadStatus(true, false), refreshUserSilently()])
  } catch (error) {
    console.error('Failed to request bank advance:', error)
    appStore.showError(errorMessage(error, t('bank.errors.advanceFailed')))
  } finally {
    advanceSubmitting.value = false
  }
}

async function submitExchange(): Promise<void> {
  exchangeTouched.value = true
  updateExchangeMaintenanceState()
  if (exchangeMaintenanceActive.value) {
    appStore.showError(t('bank.exchangeMaintenance'))
    return
  }
  if (!canSubmitExchange.value) return
  exchangeSubmitting.value = true
  try {
    await exchangePermanentForTemporary(
      exchangeAmount.value,
      getOrCreateMutationIdempotencyKey('exchange', exchangeAmount.value),
    )
    clearMutationIdempotencyKey('exchange')
    exchangeAmount.value = ''
    exchangeTouched.value = false
    appStore.showSuccess(t('bank.messages.exchangeSucceeded'))
    await Promise.all([loadStatus(true, false), refreshUserSilently()])
  } catch (error) {
    console.error('Failed to exchange permanent credit:', error)
    appStore.showError(isExchangeMaintenanceError(error)
      ? t('bank.exchangeMaintenance')
      : errorMessage(error, t('bank.errors.exchangeFailed')))
  } finally {
    exchangeSubmitting.value = false
  }
}

async function openSettings(): Promise<void> {
  if (!authStore.isAdmin) return
  showSettingsDialog.value = true
  await loadSettings()
}

async function loadSettings(): Promise<void> {
  if (!authStore.isAdmin) return
  settingsLoading.value = true
  settingsLoadFailed.value = false
  settingsError.value = ''
  try {
    applyPolicyToForm(await getBankSettings())
  } catch (error) {
    console.error('Failed to load bank settings:', error)
    settingsLoadFailed.value = true
  } finally {
    settingsLoading.value = false
  }
}

function closeSettings(): void {
  if (settingsSaving.value) return
  showSettingsDialog.value = false
  settingsError.value = ''
}

async function saveSettings(): Promise<void> {
  if (!authStore.isAdmin || settingsSaving.value) return
  const policy = validatedSettingsPolicy()
  if (!policy) return
  settingsSaving.value = true
  try {
    const updated = await updateBankSettings(policy, createIdempotencyKey('bank-settings'))
    applyPolicyToForm(updated)
    if (status.value) status.value = { ...status.value, policy: updated }
    appStore.showSuccess(t('bank.messages.settingsSaved'))
    showSettingsDialog.value = false
  } catch (error) {
    console.error('Failed to save bank settings:', error)
    settingsError.value = errorMessage(error, t('bank.errors.saveSettings'))
    appStore.showError(settingsError.value)
  } finally {
    settingsSaving.value = false
  }
}

function validatedSettingsPolicy(): BankPolicy | null {
  settingsError.value = ''
  const fields = [
    settingsForm.advance_min_amount,
    settingsForm.advance_max_amount,
    settingsForm.debt_conversion_ratio,
    settingsForm.exchange_rate,
  ]
  const values = fields.map(parseScaledAmount)
  if (values.some((value) => value === null || value <= 0n)) {
    settingsError.value = t('bank.validation.allPositive')
    return null
  }
  if ((values[0] ?? 0n) > (values[1] ?? 0n)) {
    settingsError.value = t('bank.validation.minAboveMax')
    return null
  }
  const graceDays = Number(settingsForm.debt_grace_days)
  if (!Number.isInteger(graceDays) || graceDays < 1 || graceDays > 365) {
    settingsError.value = t('bank.validation.invalidGraceDays')
    return null
  }
  return {
    advance_min_amount: settingsForm.advance_min_amount,
    advance_max_amount: settingsForm.advance_max_amount,
    debt_grace_days: graceDays,
    debt_conversion_ratio: settingsForm.debt_conversion_ratio,
    exchange_rate: settingsForm.exchange_rate,
  }
}

function applyPolicyToForm(policy: BankPolicy): void {
  settingsForm.advance_min_amount = policy.advance_min_amount
  settingsForm.advance_max_amount = policy.advance_max_amount
  settingsForm.debt_grace_days = String(policy.debt_grace_days)
  settingsForm.debt_conversion_ratio = policy.debt_conversion_ratio
  settingsForm.exchange_rate = policy.exchange_rate
}

function parseScaledAmount(value: string | null | undefined): bigint | null {
  if (!value || !decimalAmountPattern.test(value)) return null
  const [integer, fraction = ''] = value.split('.')
  return BigInt(integer) * amountScale + BigInt(fraction.padEnd(8, '0'))
}

function formatAmount(value: string | number | null | undefined): string {
  return formatMoneyDisplay(value, 2)
}

function formatSignedAmount(value: string | number | null | undefined): string {
  const formatted = formatAmount(value)
  return !formatted.startsWith('-') && formatted !== '0.00' ? `+${formatted}` : formatted
}

function isNegative(value: string | number | null | undefined): boolean {
  const raw = String(value ?? '').trim()
  return /^-\d+(?:\.\d*)?$/.test(raw) && /[1-9]/.test(raw)
}

function deltaClass(value: string, debt = false): string {
  const raw = value.trim()
  if (!/^[+-]?\d+(?:\.\d*)?$/.test(raw) || !/[1-9]/.test(raw)) {
    return 'text-gray-500 dark:text-gray-400'
  }
  const positive = !raw.startsWith('-')
  if (debt) return positive ? 'text-red-600 dark:text-red-400' : 'text-emerald-600 dark:text-emerald-400'
  return positive ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'
}

function multiplyAmounts(left: string, right: string | undefined): string {
  const leftAmount = parseScaledAmount(left)
  const rightAmount = parseScaledAmount(right)
  if (leftAmount === null || rightAmount === null) return zeroAmount
  const product = (leftAmount * rightAmount + amountScale / 2n) / amountScale
  const integer = product / amountScale
  const fraction = String(product % amountScale).padStart(8, '0')
  return `${integer}.${fraction}`
}

function formatDateTime(value: string | null | undefined): string {
  if (!value) return t('bank.common.unavailable')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return t('bank.common.unavailable')
  return new Intl.DateTimeFormat(locale.value.startsWith('zh') ? 'zh-CN' : 'en-US', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: 'Asia/Shanghai',
  }).format(date)
}

function operationLabel(operation: string): string {
  const key = ({
    advance: 'bank.operations.advance',
    exchange: 'bank.operations.exchange',
    debt_offset: 'bank.operations.debtOffset',
    permanent_settlement: 'bank.operations.permanentSettlement',
  } as Record<string, string>)[operation]
  return key ? t(key) : operation
}

function advanceStatusLabel(statusValue: string): string {
  const key = ({
    active: 'bank.advance.status.active',
    repaid: 'bank.advance.status.repaid',
    settled: 'bank.advance.status.settled',
  } as Record<string, string>)[statusValue]
  return key ? t(key) : statusValue
}

function mutationStorageKey(scope: 'advance' | 'exchange', field: 'payload' | 'key'): string {
  return `bank-${scope}-idempotency-${field}`
}

function getOrCreateMutationIdempotencyKey(scope: 'advance' | 'exchange', payload: string): string {
  const normalizedPayload = payload.trim()
  const payloadKey = mutationStorageKey(scope, 'payload')
  const idempotencyKey = mutationStorageKey(scope, 'key')
  const storedKey = localStorage.getItem(idempotencyKey)

  if (storedKey && localStorage.getItem(payloadKey) === normalizedPayload) return storedKey

  const nextKey = createIdempotencyKey(`bank-${scope}`)
  localStorage.setItem(payloadKey, normalizedPayload)
  localStorage.setItem(idempotencyKey, nextKey)
  return nextKey
}

function clearMutationIdempotencyKey(scope: 'advance' | 'exchange'): void {
  localStorage.removeItem(mutationStorageKey(scope, 'payload'))
  localStorage.removeItem(mutationStorageKey(scope, 'key'))
}

function createIdempotencyKey(scope: string): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return `${scope}-${crypto.randomUUID()}`
  }
  return `${scope}-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function isExchangeMaintenanceWindow(now = new Date()): boolean {
  const parts = Object.fromEntries(
    shanghaiTimeFormatter
      .formatToParts(now)
      .filter((part) => part.type === 'hour' || part.type === 'minute')
      .map((part) => [part.type, Number(part.value)]),
  ) as { hour?: number, minute?: number }
  const hour = parts.hour ?? -1
  const minute = parts.minute ?? -1
  return (hour === 23 && minute >= 55) || (hour === 0 && minute < 5)
}

function updateExchangeMaintenanceState(now = new Date()): void {
  exchangeMaintenanceActive.value = isExchangeMaintenanceWindow(now)
}

function selectBankMode(mode: BankMode, focusTab = false): void {
  activeBankMode.value = mode
  if (!focusTab) return

  void nextTick(() => {
    const tab = mode === 'advance' ? advanceModeTab.value : exchangeModeTab.value
    tab?.focus()
  })
}

function handleBankModeKeydown(event: KeyboardEvent, currentMode: BankMode): void {
  let nextMode: BankMode | null = null
  if (event.key === 'Home') nextMode = 'advance'
  else if (event.key === 'End') nextMode = 'exchange'
  else if (event.key === 'ArrowRight') nextMode = currentMode === 'advance' ? 'exchange' : 'advance'
  else if (event.key === 'ArrowLeft') nextMode = currentMode === 'advance' ? 'exchange' : 'advance'
  if (!nextMode) return

  event.preventDefault()
  selectBankMode(nextMode, true)
}

function scheduleExchangeMaintenanceRefresh(): void {
  if (exchangeMaintenanceTimer !== undefined) window.clearTimeout(exchangeMaintenanceTimer)
  const now = new Date()
  updateExchangeMaintenanceState(now)
  const nextMinuteDelay = 60_000 - (now.getSeconds() * 1_000 + now.getMilliseconds())
  exchangeMaintenanceTimer = window.setTimeout(scheduleExchangeMaintenanceRefresh, nextMinuteDelay)
}

function isExchangeMaintenanceError(error: unknown): boolean {
  if (typeof error !== 'object' || !error) return false
  const apiError = error as {
    status?: unknown
    code?: unknown
    response?: { status?: unknown, data?: { code?: unknown } }
  }
  const status = Number(apiError.status ?? apiError.response?.status)
  const code = String(apiError.code ?? apiError.response?.data?.code ?? '').toUpperCase()
  return status >= 400 && status < 500 && code === exchangeMaintenanceErrorCode
}

function errorMessage(error: unknown, fallback: string): string {
  if (typeof error === 'object' && error && 'message' in error) {
    const message = String((error as { message?: unknown }).message || '').trim()
    if (message) return message
  }
  return fallback
}

function retryStatus(): void {
  void loadStatus()
}

async function refreshUserSilently(): Promise<void> {
  try {
    await authStore.refreshUser()
  } catch (error) {
    console.error('Failed to refresh user after bank balance change:', error)
  }
}

onMounted(() => {
  scheduleExchangeMaintenanceRefresh()
  void loadStatus()
})

onBeforeUnmount(() => {
  if (exchangeMaintenanceTimer !== undefined) window.clearTimeout(exchangeMaintenanceTimer)
})
</script>
