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
        <div v-if="authStore.isAdmin" class="flex flex-wrap gap-2 self-start">
          <RouterLink
            v-if="showAdminBankTransactions"
            to="/admin/bank/transactions"
            data-test="bank-transactions-button"
            class="btn btn-secondary inline-flex min-h-10 items-center justify-center gap-2"
          >
            <Icon name="clipboard" size="sm" />
            {{ t('finance.transactions.bankTitle') }}
          </RouterLink>
          <button
            data-test="bank-settings-button"
            type="button"
            class="btn btn-secondary inline-flex min-h-10 items-center justify-center gap-2"
            :disabled="settingsLoading"
            @click="openSettings"
          >
            <Icon name="cog" size="sm" />
            {{ t('bank.actions.settings') }}
          </button>
        </div>
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
                  : activeBankMode === 'exchange'
                    ? 'bg-indigo-50 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-300'
                    : 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300'"
              >
                <Icon :name="activeBankModeIcon" size="md" />
              </span>
              <div class="min-w-0">
                <h2 class="text-base font-semibold text-gray-900 dark:text-white">
                  {{ activeBankModeTitle }}
                </h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                  {{ activeBankModeDescription }}
                </p>
              </div>
            </div>

            <div
              data-test="bank-mode-selector"
              role="tablist"
              aria-orientation="horizontal"
              :aria-label="t('bank.operationMode')"
              class="relative grid min-h-11 grid-cols-3 overflow-hidden rounded-lg bg-gray-100 p-1 dark:bg-dark-700"
            >
              <span
                data-test="bank-mode-indicator"
                aria-hidden="true"
                class="pointer-events-none absolute inset-y-1 left-1 rounded-md bg-white shadow-sm transition-transform duration-200 ease-out dark:bg-dark-800"
                :style="bankModeIndicatorStyle"
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
              <button
                id="bank-mode-repay"
                data-test="bank-mode-repay"
                ref="repayModeTab"
                type="button"
                role="tab"
                class="relative z-10 min-w-0 px-3 py-2 text-sm font-medium transition-colors"
                :class="activeBankMode === 'repay'
                  ? 'text-primary-700 dark:text-primary-300'
                  : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'"
                :aria-selected="activeBankMode === 'repay'"
                :tabindex="activeBankMode === 'repay' ? 0 : -1"
                aria-controls="bank-panel-repay"
                @keydown="handleBankModeKeydown($event, 'repay')"
                @click="selectBankMode('repay', true)"
              >
                {{ t('bank.repay.title') }}
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
                  :disabled="advanceSubmitting || Boolean(status.active_advance) || hasInvalidPermanentBalance"
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
                    <span>{{ formatAmount(status.temporary_credit_available) }}</span><span v-if="advanceWalletAddition" data-test="advance-wallet-addition">+{{ advanceWalletAddition }}</span>
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
            <div v-if="status.exchange_progress" data-test="exchange-progress-summary" class="grid grid-cols-1 gap-3 min-[390px]:grid-cols-2 sm:grid-cols-4">
              <div class="rounded-lg border border-indigo-200 bg-indigo-50/60 p-3 dark:border-indigo-900/60 dark:bg-indigo-950/20">
                <p class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('finance.bank.currentDay') }}</p>
                <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ status.exchange_progress.date }}</p>
              </div>
              <div class="rounded-lg border border-indigo-200 bg-indigo-50/60 p-3 dark:border-indigo-900/60 dark:bg-indigo-950/20">
                <p class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('finance.bank.dailyUsed') }}</p>
                <p data-test="exchange-daily-used" class="mt-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">${{ formatAmount(status.exchange_progress.permanent_exchanged_today) }}</p>
              </div>
              <div class="relative rounded-lg border border-indigo-200 bg-indigo-50/60 p-3 pr-9 dark:border-indigo-900/60 dark:bg-indigo-950/20">
                <div class="absolute right-2 top-2">
                  <button
                    ref="exchangeTierTooltipTrigger"
                    type="button"
                    data-test="exchange-tier-tooltip-trigger"
                    class="inline-flex h-6 w-6 items-center justify-center rounded-full text-indigo-500 transition-colors hover:bg-indigo-100 hover:text-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-400 dark:text-indigo-300 dark:hover:bg-indigo-900/60 dark:hover:text-indigo-200"
                    :aria-label="t('bank.exchange.tierTooltipAriaLabel')"
                    aria-describedby="exchange-tier-tooltip"
                    :aria-expanded="exchangeTierTooltipVisible"
                    @mouseenter="setExchangeTierTooltipInteraction('hover', true)"
                    @mouseleave="setExchangeTierTooltipInteraction('hover', false)"
                    @focus="setExchangeTierTooltipInteraction('focus', true)"
                    @blur="setExchangeTierTooltipInteraction('focus', false)"
                    @keydown.esc.prevent="closeExchangeTierTooltip"
                  >
                    <Icon name="questionCircle" size="sm" />
                  </button>
                  <Teleport to="body">
                    <div
                      id="exchange-tier-tooltip"
                      ref="exchangeTierTooltip"
                      v-show="exchangeTierTooltipVisible"
                      data-test="exchange-tier-tooltip"
                      role="tooltip"
                      class="pointer-events-none fixed z-[99999] w-72 max-w-[calc(100vw-2rem)] rounded-lg bg-gray-900 p-3 text-left text-xs leading-relaxed text-white shadow-xl ring-1 ring-white/10 dark:bg-gray-800"
                      :style="exchangeTierTooltipStyle"
                    >
                      <p class="font-semibold">{{ t('bank.exchange.tierTooltipTitle') }}</p>
                      <ul class="mt-1.5 space-y-1">
                        <li
                          v-for="tier in exchangeTierTooltipRows"
                          :key="tier.index"
                          class="flex items-start justify-between gap-3"
                          :class="tier.current ? 'text-indigo-200' : 'text-gray-200'"
                        >
                          <span>{{ tier.range }}</span>
                          <span class="shrink-0 font-mono">1 : {{ tier.rate }}</span>
                        </li>
                      </ul>
                    </div>
                  </Teleport>
                </div>
                <p class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('finance.bank.tier') }}</p>
                <p data-test="exchange-current-tier" class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">#{{ status.exchange_progress.current_tier_index + 1 }}</p>
                <p class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('finance.bank.rate', { rate: formatAmount(status.exchange_progress.current_tier_rate) }) }}</p>
              </div>
              <div class="rounded-lg border border-indigo-200 bg-indigo-50/60 p-3 dark:border-indigo-900/60 dark:bg-indigo-950/20">
                <div class="flex items-center justify-between gap-2">
                  <p class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('finance.bank.progress') }}</p>
                  <span class="text-[11px] font-mono text-indigo-700 dark:text-indigo-300">{{ exchangeTierProgressPercent.toFixed(1) }}%</span>
                </div>
                <div class="mt-2 h-2 overflow-hidden rounded-full bg-indigo-100 dark:bg-indigo-900/60">
                  <div class="h-full rounded-full bg-indigo-500 transition-all" :style="{ width: `${exchangeTierProgressPercent}%` }" />
                </div>
                <p v-if="status.exchange_progress.amount_until_next_tier != null" class="mt-1 text-[11px] text-gray-500 dark:text-gray-400">{{ t('finance.bank.untilNext', { amount: formatAmount(status.exchange_progress.amount_until_next_tier) }) }}</p>
                <p v-else class="mt-1 text-[11px] text-gray-500 dark:text-gray-400">{{ t('finance.bank.unlimited') }}</p>
              </div>
            </div>
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
                  :disabled="exchangeSubmitting || hasInvalidPermanentBalance"
                  placeholder="0.00"
                  @input="exchangeTouched = true"
                />
                <p v-if="exchangeError" data-test="exchange-error" class="input-error-text mt-1.5">{{ exchangeError }}</p>
              </div>

              <div class="flex min-w-12 flex-col items-center justify-center gap-2 sm:min-w-20">
                <p data-test="exchange-rate" class="max-w-full break-words text-center text-xs font-medium text-gray-500 dark:text-gray-400 min-[390px]:max-w-20">
                  {{ t('bank.exchange.rate', { rate: formatAmount(currentExchangeRate) }) }}
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

          <form
            id="bank-panel-repay"
            data-test="repay-flow"
            role="tabpanel"
            aria-labelledby="bank-mode-repay"
            :hidden="activeBankMode !== 'repay'"
            :aria-hidden="activeBankMode !== 'repay'"
            class="mt-5 space-y-4"
            @submit.prevent="submitRepay"
          >
            <div>
              <p class="input-label">{{ t('bank.repay.source') }}</p>
              <div class="mt-2 grid grid-cols-2 rounded-lg bg-gray-100 p-1 dark:bg-dark-700" role="radiogroup" :aria-label="t('bank.repay.source')">
                <button
                  v-for="source in repaySources"
                  :key="source"
                  type="button"
                  role="radio"
                  :data-test="`repay-source-${source}`"
                  class="rounded-md px-3 py-2 text-sm font-medium transition-colors"
                  :class="repaySource === source
                    ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-800 dark:text-primary-300'
                    : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'"
                  :aria-checked="repaySource === source"
                  @click="selectRepaySource(source)"
                >
                  {{ t(`bank.repay.${source}`) }}
                </button>
              </div>
            </div>

            <div class="grid grid-cols-1 items-stretch gap-3 min-[390px]:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] min-[390px]:gap-2 sm:gap-4">
              <div class="min-w-0 rounded-lg border border-gray-200 bg-gray-50/60 p-3 dark:border-dark-600 dark:bg-dark-900/30 sm:p-4">
                <label for="bank-repay-amount" class="input-label">{{ t('bank.repay.amount') }}</label>
                <input
                  id="bank-repay-amount"
                  v-model.trim="repayAmount"
                  data-test="repay-input"
                  type="text"
                  inputmode="decimal"
                  autocomplete="off"
                  class="input mt-2 min-w-0 font-mono"
                  :class="repayError ? 'input-error ring-2 ring-red-500/20' : ''"
                  :disabled="repaySubmitting || !hasTemporaryDebt || hasInvalidRepaySourceBalance"
                  placeholder="0.00"
                  @input="repayTouched = true"
                />
                <p v-if="repayError" data-test="repay-error" class="input-error-text mt-1.5">{{ repayError }}</p>
                <p v-else class="input-hint mt-1.5">
                  {{ t('bank.repay.ratio', { rate: formatAmount(activeRepayRatio) }) }}
                </p>
              </div>

              <div class="flex min-w-12 items-center justify-center sm:min-w-16">
                <span class="flex h-9 w-9 rotate-90 items-center justify-center rounded-full bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300 min-[390px]:rotate-0">
                  <Icon name="arrowRight" size="sm" />
                </span>
              </div>

              <output
                for="bank-repay-amount"
                data-test="repay-preview"
                aria-live="polite"
                class="flex min-w-0 flex-col justify-center rounded-lg border border-emerald-200 bg-emerald-50/60 p-3 dark:border-emerald-900/60 dark:bg-emerald-950/20 sm:p-4"
              >
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('bank.repay.preview') }}</p>
                <p data-test="repay-preview-amount" class="mt-2 break-all font-mono text-lg font-semibold text-emerald-700 dark:text-emerald-300">
                  {{ formatAmount(estimatedDebtReduction) }}
                </p>
                <p data-test="repay-debt-remaining" class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                  {{ t('bank.repay.debtRemaining') }}: {{ formatAmount(estimatedDebtRemaining) }}
                </p>
              </output>
            </div>

            <button
              data-test="repay-submit"
              type="submit"
              class="btn btn-primary inline-flex min-h-10 w-full items-center justify-center gap-2"
              :disabled="!canSubmitRepay"
            >
              <Icon v-if="repaySubmitting" name="refresh" size="sm" class="animate-spin" />
              <Icon v-else name="dollar" size="sm" />
              {{ repaySubmitting ? t('bank.repay.submitting') : t('bank.repay.confirm') }}
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
              :disabled="refreshing || ledgerRefreshing"
              @click="refreshBankData"
            >
              <Icon name="refresh" size="sm" :class="refreshing || ledgerRefreshing ? 'animate-spin' : ''" />
            </button>
          </div>

          <div v-if="ledgerLoading && !ledgerItems.length" data-test="ledger-loading" class="flex min-h-40 items-center justify-center px-5 py-10">
            <Icon name="refresh" size="lg" class="animate-spin text-primary-600" />
          </div>
          <div v-else-if="ledgerLoadFailed && !ledgerItems.length" data-test="ledger-load-error" class="flex min-h-40 flex-col items-center justify-center gap-3 px-5 py-10 text-center text-sm text-gray-500 dark:text-gray-400">
            <p>{{ t('bank.ledger.loadFailed') }}</p>
            <button type="button" class="btn btn-secondary" :disabled="ledgerLoading" @click="loadLedger(ledgerPage)">{{ t('bank.actions.reload') }}</button>
          </div>
          <div v-else-if="!ledgerItems.length" data-test="empty-ledger" class="px-5 py-10 text-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('bank.ledger.empty') }}
          </div>
          <div v-else class="overflow-x-auto">
            <p v-if="ledgerLoadFailed" data-test="ledger-stale-error" class="border-b border-amber-200 bg-amber-50 px-5 py-2 text-xs text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-300">
              {{ t('bank.ledger.refreshFailed') }}
            </p>
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
                <tr v-for="item in ledgerItems" :key="item.id" data-test="ledger-row" class="text-gray-700 dark:text-gray-300">
                  <td class="whitespace-nowrap px-5 py-3">{{ formatDateTime(item.created_at) }}</td>
                  <td class="whitespace-nowrap px-5 py-3 font-medium text-gray-900 dark:text-white">{{ operationLabel(item.operation) }}</td>
                  <td class="whitespace-nowrap px-5 py-3 text-right font-mono" :class="deltaClass(item.permanent_delta)">{{ formatSignedAmount(item.permanent_delta) }}</td>
                  <td class="whitespace-nowrap px-5 py-3 text-right font-mono" :class="deltaClass(item.temporary_delta)">{{ formatSignedAmount(item.temporary_delta) }}</td>
                  <td class="whitespace-nowrap px-5 py-3 text-right font-mono" :class="deltaClass(item.debt_delta, true)">{{ formatSignedAmount(item.debt_delta) }}</td>
                  <td class="whitespace-nowrap px-5 py-3 text-right font-mono">{{ formatAmount(item.debt_after) }}</td>
                </tr>
              </tbody>
            </table>
            <Pagination
              v-if="ledgerTotal > ledgerPageSize"
              data-test="ledger-pagination"
              :total="ledgerTotal"
              :page="ledgerPage"
              :page-size="ledgerPageSize"
              :show-page-size-selector="false"
              @update:page="loadLedger"
            />
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
        <div role="tablist" aria-orientation="horizontal" :aria-label="t('bank.settings.title')" class="relative grid min-h-11 grid-cols-3 overflow-hidden rounded-lg bg-gray-100 p-1 dark:bg-dark-700">
          <span aria-hidden="true" class="pointer-events-none absolute inset-y-1 left-1 rounded-md bg-white shadow-sm transition-transform dark:bg-dark-800" :style="{ width: 'calc(33.333333% - 0.166667rem)', transform: `translateX(${activeSettingsSection === 'advance' ? '0' : activeSettingsSection === 'exchange' ? '100%' : '200%'})` }" />
          <button id="bank-settings-tab-advance" ref="advanceSettingsTab" type="button" role="tab" data-test="settings-section-advance" class="relative z-10 px-2 py-2 text-sm font-medium" :class="activeSettingsSection === 'advance' ? 'text-primary-700 dark:text-primary-300' : 'text-gray-500'" :aria-selected="activeSettingsSection === 'advance'" :tabindex="activeSettingsSection === 'advance' ? 0 : -1" aria-controls="bank-settings-panel-advance" @keydown="handleSettingsSectionKeydown($event, 'advance')" @click="selectSettingsSection('advance')">{{ t('bank.advance.title') }}</button>
          <button id="bank-settings-tab-exchange" ref="exchangeSettingsTab" type="button" role="tab" data-test="settings-section-exchange" class="relative z-10 px-2 py-2 text-sm font-medium" :class="activeSettingsSection === 'exchange' ? 'text-primary-700 dark:text-primary-300' : 'text-gray-500'" :aria-selected="activeSettingsSection === 'exchange'" :tabindex="activeSettingsSection === 'exchange' ? 0 : -1" aria-controls="bank-settings-panel-exchange" @keydown="handleSettingsSectionKeydown($event, 'exchange')" @click="selectSettingsSection('exchange')">{{ t('bank.exchange.title') }}</button>
          <button id="bank-settings-tab-repay" ref="repaySettingsTab" type="button" role="tab" data-test="settings-section-repay" class="relative z-10 px-2 py-2 text-sm font-medium" :class="activeSettingsSection === 'repay' ? 'text-primary-700 dark:text-primary-300' : 'text-gray-500'" :aria-selected="activeSettingsSection === 'repay'" :tabindex="activeSettingsSection === 'repay' ? 0 : -1" aria-controls="bank-settings-panel-repay" @keydown="handleSettingsSectionKeydown($event, 'repay')" @click="selectSettingsSection('repay')">{{ t('bank.repay.title') }}</button>
        </div>

        <div id="bank-settings-panel-advance" v-show="activeSettingsSection === 'advance'" role="tabpanel" aria-labelledby="bank-settings-tab-advance" :aria-hidden="activeSettingsSection !== 'advance'" class="space-y-5">
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label for="bank-min-advance" class="input-label">{{ t('bank.settings.advanceMin') }}</label>
              <input id="bank-min-advance" v-model.trim="settingsForm.advance_min_amount" data-test="settings-min" type="text" inputmode="decimal" class="input mt-1 font-mono" :disabled="settingsSaving" @input="markSettingsFieldDirty('advance_min_amount')" />
            </div>
            <div>
              <label for="bank-max-advance" class="input-label">{{ t('bank.settings.advanceMax') }}</label>
              <input id="bank-max-advance" v-model.trim="settingsForm.advance_max_amount" data-test="settings-max" type="text" inputmode="decimal" class="input mt-1 font-mono" :disabled="settingsSaving" @input="markSettingsFieldDirty('advance_max_amount')" />
            </div>
          </div>
          <div>
            <label for="bank-grace-days" class="input-label">{{ t('bank.settings.graceDays') }}</label>
            <input id="bank-grace-days" v-model.trim="settingsForm.debt_grace_days" data-test="settings-grace-days" type="number" min="1" max="365" step="1" class="input mt-1" :disabled="settingsSaving" />
            <p class="input-hint mt-1.5">{{ t('bank.settings.graceDaysHint') }}</p>
          </div>
          <div>
            <label for="bank-debt-ratio" class="input-label">{{ t('bank.settings.debtRatio') }}</label>
            <input id="bank-debt-ratio" v-model.trim="settingsForm.debt_conversion_ratio" data-test="settings-debt-ratio" type="text" inputmode="decimal" class="input mt-1 font-mono" :disabled="settingsSaving" @input="markSettingsFieldDirty('debt_conversion_ratio')" />
            <p class="input-hint mt-1.5">{{ t('bank.settings.debtRatioHint') }}</p>
          </div>
          <div>
            <label for="bank-unused-advance-ratio" class="input-label">{{ t('bank.settings.unusedAdvanceRatio') }}</label>
            <input id="bank-unused-advance-ratio" v-model.trim="settingsForm.unused_advance_debt_reduction_ratio" data-test="settings-unused-advance-ratio" type="text" inputmode="decimal" class="input mt-1 font-mono" :disabled="settingsSaving" @input="markSettingsFieldDirty('unused_advance_debt_reduction_ratio')" />
            <p class="input-hint mt-1.5">{{ t('bank.settings.unusedAdvanceRatioHint') }}</p>
          </div>
        </div>

        <div id="bank-settings-panel-exchange" v-show="activeSettingsSection === 'exchange'" role="tabpanel" aria-labelledby="bank-settings-tab-exchange" :aria-hidden="activeSettingsSection !== 'exchange'" class="space-y-4">
          <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
            <div class="mb-3 flex items-center justify-between gap-3">
              <div>
                <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('finance.bank.tiers') }}</h3>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('finance.bank.tierValidation') }}</p>
              </div>
              <button type="button" class="btn btn-secondary h-9" :disabled="settingsSaving" @click="addExchangeTier">{{ t('finance.bank.addTier') }}</button>
            </div>
            <div class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2 text-xs text-gray-500 dark:text-gray-400">
              <span>{{ t('finance.bank.tierTo') }}</span><span>{{ t('finance.bank.tierRate') }}</span><span />
            </div>
            <div v-for="(tier, index) in settingsForm.exchange_tiers" :key="index" class="mt-2 grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-2">
              <input v-model.trim="tier.up_to" :data-test="`settings-tier-upper-${index}`" type="text" inputmode="decimal" class="input font-mono" :placeholder="t('finance.bank.unlimited')" :disabled="settingsSaving" @input="markTierFieldDirty(tier, 'up_to')" />
              <input v-model.trim="tier.rate" :data-test="`settings-tier-rate-${index}`" type="text" inputmode="decimal" class="input font-mono" :disabled="settingsSaving" @input="markTierFieldDirty(tier, 'rate')" />
              <button type="button" class="inline-flex h-9 w-9 items-center justify-center rounded-lg text-gray-500 hover:bg-red-50 hover:text-red-600 disabled:opacity-40 dark:hover:bg-red-900/20" :disabled="settingsSaving || settingsForm.exchange_tiers.length <= 1" :title="t('finance.bank.removeTier')" @click="removeExchangeTier(index)"><Icon name="trash" size="sm" /></button>
            </div>
          </div>
        </div>

        <div id="bank-settings-panel-repay" v-show="activeSettingsSection === 'repay'" role="tabpanel" aria-labelledby="bank-settings-tab-repay" :aria-hidden="activeSettingsSection !== 'repay'" class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label for="bank-early-temporary-ratio" class="input-label">{{ t('bank.settings.earlyTemporaryRatio') }}</label>
            <input id="bank-early-temporary-ratio" v-model.trim="settingsForm.early_repay_temporary_ratio" data-test="settings-early-temporary-ratio" type="text" inputmode="decimal" class="input mt-1 font-mono" :disabled="settingsSaving" @input="markSettingsFieldDirty('early_repay_temporary_ratio')" />
            <p class="input-hint mt-1.5">{{ t('bank.settings.earlyTemporaryRatioHint') }}</p>
          </div>
          <div>
            <label for="bank-early-permanent-ratio" class="input-label">{{ t('bank.settings.earlyPermanentRatio') }}</label>
            <input id="bank-early-permanent-ratio" v-model.trim="settingsForm.early_repay_permanent_ratio" data-test="settings-early-permanent-ratio" type="text" inputmode="decimal" class="input mt-1 font-mono" :disabled="settingsSaving" @input="markSettingsFieldDirty('early_repay_permanent_ratio')" />
            <p class="input-hint mt-1.5">{{ t('bank.settings.earlyPermanentRatioHint') }}</p>
          </div>
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
  getBankLedger,
  getBankSettings,
  getBankStatus,
  repayBankDebt,
  requestBankAdvance,
  updateBankSettings,
  type BankRepaySource,
  type BankExchangeTier,
  type BankLedgerItem,
  type BankPolicy,
  type BankStatus,
} from '@/api/bank'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { formatMoneyDisplay } from '@/utils/format'

type SettingsAmountField =
  | 'advance_min_amount'
  | 'advance_max_amount'
  | 'debt_conversion_ratio'
  | 'exchange_rate'
  | 'unused_advance_debt_reduction_ratio'
  | 'early_repay_temporary_ratio'
  | 'early_repay_permanent_ratio'

interface SettingsTierForm {
  up_to: string
  rate: string
  exact_up_to: string
  exact_rate: string
  up_to_dirty: boolean
  rate_dirty: boolean
}

interface SettingsForm {
  advance_min_amount: string
  advance_max_amount: string
  debt_grace_days: string
  debt_conversion_ratio: string
  exchange_rate: string
  unused_advance_debt_reduction_ratio: string
  early_repay_temporary_ratio: string
  early_repay_permanent_ratio: string
  exchange_tiers: SettingsTierForm[]
}

const decimalAmountPattern = /^(?:0|[1-9]\d{0,11})(?:\.\d{1,8})?$/
const zeroAmount = '0.00000000'
const amountScale = 100_000_000n
const { locale, t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const showAdminBankTransactions = computed(() => authStore.isAdmin && isFeatureFlagEnabled(FeatureFlags.adminBankTransactions))
const status = ref<BankStatus | null>(null)
const loading = ref(true)
const refreshing = ref(false)
const loadFailed = ref(false)
const ledgerItems = ref<BankLedgerItem[]>([])
const ledgerPage = ref(1)
const ledgerTotal = ref(0)
const ledgerLoading = ref(true)
const ledgerRefreshing = ref(false)
const ledgerLoadFailed = ref(false)
const ledgerPageSize = 5
type BankMode = 'advance' | 'exchange' | 'repay'

const activeBankMode = ref<BankMode>('advance')
const advanceModeTab = ref<HTMLButtonElement | null>(null)
const exchangeModeTab = ref<HTMLButtonElement | null>(null)
const repayModeTab = ref<HTMLButtonElement | null>(null)
const exchangeTierTooltipTrigger = ref<HTMLButtonElement | null>(null)
const exchangeTierTooltip = ref<HTMLElement | null>(null)
const exchangeTierTooltipVisible = ref(false)
const exchangeTierTooltipStyle = ref<Record<string, string>>({ top: '0px', left: '0px' })
let exchangeTierTooltipHovered = false
let exchangeTierTooltipFocused = false
const advanceAmount = ref('')
const exchangeAmount = ref('')
const repayAmount = ref('')
const repaySource = ref<BankRepaySource>('temporary')
const repaySources: BankRepaySource[] = ['temporary', 'permanent']
const advanceTouched = ref(false)
const exchangeTouched = ref(false)
const repayTouched = ref(false)
const advanceSubmitting = ref(false)
const exchangeSubmitting = ref(false)
const repaySubmitting = ref(false)
const showSettingsDialog = ref(false)
const settingsLoading = ref(false)
const settingsLoadFailed = ref(false)
const settingsSaving = ref(false)
const settingsError = ref('')
type SettingsSection = 'advance' | 'exchange' | 'repay'
const activeSettingsSection = ref<SettingsSection>('advance')
const advanceSettingsTab = ref<HTMLButtonElement | null>(null)
const exchangeSettingsTab = ref<HTMLButtonElement | null>(null)
const repaySettingsTab = ref<HTMLButtonElement | null>(null)
const loadedPolicyHadTiers = ref(false)
const tiersDirty = ref(false)
const settingsExactValues = {} as Record<SettingsAmountField, string>
const settingsDirtyFields = new Set<SettingsAmountField>()
let latestStatusRequest = 0
let latestLedgerRequest = 0

const settingsForm = reactive<SettingsForm>({
  advance_min_amount: '',
  advance_max_amount: '',
  debt_grace_days: '3',
  debt_conversion_ratio: '',
  exchange_rate: '',
  unused_advance_debt_reduction_ratio: '',
  early_repay_temporary_ratio: '',
  early_repay_permanent_ratio: '',
  exchange_tiers: [],
})

const hasInvalidPermanentBalance = computed(() => Boolean(
  status.value && parseScaledAmount(status.value.permanent_balance) === null,
))
const hasTemporaryDebt = computed(() => {
  const debt = parseScaledAmount(status.value?.temporary_debt)
  return debt !== null && debt > 0n
})
const repaySourceBalance = computed(() => repaySource.value === 'temporary'
  ? status.value?.temporary_credit_available
  : status.value?.permanent_balance)
const hasInvalidRepaySourceBalance = computed(() => Boolean(
  status.value && parseScaledAmount(repaySourceBalance.value) === null,
))
const activeBankModeIcon = computed<'download' | 'swap' | 'dollar'>(() => activeBankMode.value === 'advance' ? 'download' : activeBankMode.value === 'exchange' ? 'swap' : 'dollar')
const activeBankModeTitle = computed(() => activeBankMode.value === 'advance' ? t('bank.advance.title') : activeBankMode.value === 'exchange' ? t('bank.exchange.title') : t('bank.repay.title'))
const activeBankModeDescription = computed(() => activeBankMode.value === 'advance'
  ? t('bank.advance.description')
  : activeBankMode.value === 'exchange'
    ? t('bank.exchange.description', { rate: formatAmount(currentExchangeRate.value) })
    : t('bank.repay.description'))
const bankModeIndicatorStyle = computed(() => ({
  width: 'calc(33.333333% - 0.166667rem)',
  transform: `translateX(${activeBankMode.value === 'advance' ? '0' : activeBankMode.value === 'exchange' ? '100%' : '200%'})`,
}))

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
  && !hasInvalidPermanentBalance.value
  && !advanceSubmitting.value
  && advanceAmount.value
  && !advanceError.value,
))

const advanceWalletAddition = computed(() => {
  const value = parseScaledAmount(advanceAmount.value)
  if (value === null || value <= 0n || advanceError.value) return ''
  return formatAmount(advanceAmount.value)
})

const canSubmitExchange = computed(() => Boolean(
  status.value
  && !hasInvalidPermanentBalance.value
  && !exchangeSubmitting.value
  && exchangeAmount.value
  && !exchangeError.value,
))

const exchangeTiers = computed<BankExchangeTier[]>(() => status.value?.policy.exchange_tiers?.length
  ? status.value.policy.exchange_tiers
  : [{ up_to: null, rate: status.value?.policy.exchange_rate ?? '0' }])

const exchangeTierTooltipRows = computed(() => {
  let previousUpper = zeroAmount
  return exchangeTiers.value.map((tier, index) => {
    const lower = formatAmount(previousUpper)
    const upper = tier.up_to == null ? null : formatAmount(tier.up_to)
    const range = upper === null
      ? t('bank.exchange.tierRangeUnlimited', { lower })
      : index === 0
        ? t('bank.exchange.tierRangeFirst', { upper })
        : t('bank.exchange.tierRange', { lower, upper })
    if (tier.up_to != null) previousUpper = tier.up_to
    return {
      index,
      range,
      rate: formatAmount(tier.rate),
      current: status.value?.exchange_progress?.current_tier_index === index,
    }
  })
})

function updateExchangeTierTooltipPosition(): void {
  const trigger = exchangeTierTooltipTrigger.value
  const tooltip = exchangeTierTooltip.value
  if (!trigger || !tooltip) return

  const rect = trigger.getBoundingClientRect()
  const viewportWidth = window.innerWidth || document.documentElement.clientWidth || 1280
  const viewportHeight = window.innerHeight || document.documentElement.clientHeight || 720
  const viewportMargin = 16
  const triggerGap = 8
  const availableWidth = Math.max(0, viewportWidth - viewportMargin * 2)
  const tooltipWidth = Math.min(tooltip.offsetWidth || 288, availableWidth)
  const tooltipHeight = tooltip.offsetHeight || 160

  let left = rect.left + rect.width / 2 - tooltipWidth / 2
  left = Math.max(viewportMargin, Math.min(left, viewportWidth - viewportMargin - tooltipWidth))

  let top = rect.bottom + triggerGap
  if (top + tooltipHeight > viewportHeight - viewportMargin) {
    top = Math.max(viewportMargin, rect.top - tooltipHeight - triggerGap)
  }

  exchangeTierTooltipStyle.value = {
    top: `${Math.round(top)}px`,
    left: `${Math.round(left)}px`,
  }
}

function removeExchangeTierTooltipListeners(): void {
  window.removeEventListener('scroll', updateExchangeTierTooltipPosition, true)
  window.removeEventListener('resize', updateExchangeTierTooltipPosition)
}

function setExchangeTierTooltipInteraction(source: 'hover' | 'focus', active: boolean): void {
  if (source === 'hover') exchangeTierTooltipHovered = active
  else exchangeTierTooltipFocused = active

  const shouldShow = exchangeTierTooltipHovered || exchangeTierTooltipFocused
  exchangeTierTooltipVisible.value = shouldShow
  if (!shouldShow) {
    removeExchangeTierTooltipListeners()
    return
  }

  void nextTick(() => {
    updateExchangeTierTooltipPosition()
    window.addEventListener('scroll', updateExchangeTierTooltipPosition, true)
    window.addEventListener('resize', updateExchangeTierTooltipPosition)
  })
}

function closeExchangeTierTooltip(): void {
  exchangeTierTooltipHovered = false
  exchangeTierTooltipFocused = false
  exchangeTierTooltipVisible.value = false
  removeExchangeTierTooltipListeners()
}

const currentExchangeRate = computed(() => status.value?.exchange_progress?.current_tier_rate
  ?? exchangeTiers.value[0]?.rate
  ?? status.value?.policy.exchange_rate
  ?? zeroAmount)

const exchangeTierProgressPercent = computed(() => {
  const progress = status.value?.exchange_progress
  if (!progress || progress.current_tier_up_to == null) return 100
  const upper = parseScaledAmount(progress.current_tier_up_to)
  const used = parseScaledAmount(progress.permanent_exchanged_today)
  const previousUpper = progress.current_tier_index > 0
    ? parseScaledAmount(exchangeTiers.value[progress.current_tier_index - 1]?.up_to)
    : 0n
  if (upper === null || used === null || previousUpper === null || upper <= previousUpper) return 0
  const consumed = used > previousUpper ? used - previousUpper : 0n
  return Number((consumed * 10000n) / (upper - previousUpper)) / 100
})

const estimatedTemporaryAmount = computed(() => calculateTieredExchange(
  exchangeAmount.value,
  status.value?.exchange_progress?.permanent_exchanged_today ?? zeroAmount,
  exchangeTiers.value,
))

const activeRepayRatio = computed(() => status.value?.policy[
  repaySource.value === 'temporary' ? 'early_repay_temporary_ratio' : 'early_repay_permanent_ratio'
] ?? zeroAmount)

const repayError = computed(() => {
  if (!repayTouched.value && !repayAmount.value) return ''
  if (!hasTemporaryDebt.value) return t('bank.validation.noDebt')
  const value = parseScaledAmount(repayAmount.value)
  if (value === null || value <= 0n) return t('bank.validation.positiveAmount')
  const balance = parseScaledAmount(repaySourceBalance.value)
  if (balance !== null && value > balance) {
    return t(repaySource.value === 'temporary' ? 'bank.validation.insufficientTemporary' : 'bank.validation.insufficientPermanent')
  }
  return ''
})

const estimatedDebtReduction = computed(() => {
  const reduced = multiplyAmounts(repayAmount.value, activeRepayRatio.value)
  const reducedScaled = parseScaledAmount(reduced)
  const debtScaled = parseScaledAmount(status.value?.temporary_debt)
  if (reducedScaled === null || debtScaled === null) return zeroAmount
  return formatScaledAmount(reducedScaled > debtScaled ? debtScaled : reducedScaled)
})

const estimatedDebtRemaining = computed(() => {
  const debtScaled = parseScaledAmount(status.value?.temporary_debt)
  const reducedScaled = parseScaledAmount(estimatedDebtReduction.value)
  if (debtScaled === null) return zeroAmount
  if (reducedScaled === null) return formatScaledAmount(debtScaled)
  return formatScaledAmount(reducedScaled >= debtScaled ? 0n : debtScaled - reducedScaled)
})

const canSubmitRepay = computed(() => Boolean(
  status.value
  && hasTemporaryDebt.value
  && !hasInvalidRepaySourceBalance.value
  && !repaySubmitting.value
  && repayAmount.value
  && !repayError.value,
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

async function loadLedger(page = ledgerPage.value, background = false): Promise<void> {
  const requestedPage = Math.max(1, Math.trunc(page) || 1)
  const request = ++latestLedgerRequest
  if (background) ledgerRefreshing.value = true
  else ledgerLoading.value = true
  ledgerLoadFailed.value = false

  try {
    const response = await getBankLedger(requestedPage)
    if (request !== latestLedgerRequest) return

    const lastPage = Math.max(1, response.pages || Math.ceil(response.total / ledgerPageSize))
    if (response.total > 0 && requestedPage > lastPage) {
      ledgerPage.value = lastPage
      await loadLedger(lastPage, background)
      return
    }

    ledgerItems.value = response.items.slice(0, ledgerPageSize)
    ledgerTotal.value = Math.max(0, response.total)
    ledgerPage.value = response.total === 0
      ? 1
      : Math.min(Math.max(1, response.page || requestedPage), lastPage)
  } catch (error) {
    if (request !== latestLedgerRequest) return
    console.error('Failed to load bank ledger:', error)
    ledgerLoadFailed.value = true
    if (ledgerItems.value.length) appStore.showError(errorMessage(error, t('bank.ledger.refreshFailed')))
  } finally {
    if (request === latestLedgerRequest) {
      ledgerLoading.value = false
      ledgerRefreshing.value = false
    }
  }
}

async function refreshBankData(): Promise<void> {
  await Promise.all([
    loadStatus(true),
    loadLedger(ledgerPage.value, true),
  ])
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
    await Promise.all([loadStatus(true, false), loadLedger(1, true), refreshUserSilently()])
  } catch (error) {
    console.error('Failed to request bank advance:', error)
    appStore.showError(errorMessage(error, t('bank.errors.advanceFailed')))
  } finally {
    advanceSubmitting.value = false
  }
}

async function submitExchange(): Promise<void> {
  exchangeTouched.value = true
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
    await Promise.all([loadStatus(true, false), loadLedger(1, true), refreshUserSilently()])
  } catch (error) {
    console.error('Failed to exchange permanent credit:', error)
    appStore.showError(errorMessage(error, t('bank.errors.exchangeFailed')))
  } finally {
    exchangeSubmitting.value = false
  }
}

function selectRepaySource(source: BankRepaySource): void {
  if (repaySource.value === source) return
  repaySource.value = source
  repayAmount.value = ''
  repayTouched.value = false
}

async function submitRepay(): Promise<void> {
  repayTouched.value = true
  if (!canSubmitRepay.value) return
  repaySubmitting.value = true
  const payloadKey = `${repaySource.value}:${repayAmount.value}`
  try {
    await repayBankDebt(
      repaySource.value,
      repayAmount.value,
      getOrCreateMutationIdempotencyKey('repay', payloadKey),
    )
    clearMutationIdempotencyKey('repay')
    repayAmount.value = ''
    repayTouched.value = false
    appStore.showSuccess(t('bank.messages.repaySucceeded'))
    await Promise.all([loadStatus(true, false), loadLedger(1, true), refreshUserSilently()])
  } catch (error) {
    console.error('Failed to repay bank debt:', error)
    appStore.showError(errorMessage(error, t('bank.errors.repayFailed')))
  } finally {
    repaySubmitting.value = false
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
  const exchangeRate = settingsForm.exchange_tiers[0]
    ? tierFieldValue(settingsForm.exchange_tiers[0], 'rate')
    : settingsAmountValue('exchange_rate')
  const fields = [
    settingsAmountValue('advance_min_amount'),
    settingsAmountValue('advance_max_amount'),
    settingsAmountValue('debt_conversion_ratio'),
    exchangeRate,
    settingsAmountValue('unused_advance_debt_reduction_ratio'),
    settingsAmountValue('early_repay_temporary_ratio'),
    settingsAmountValue('early_repay_permanent_ratio'),
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
  const tiers = validatedExchangeTiers()
  if (!tiers) return null
  return {
    advance_min_amount: fields[0],
    advance_max_amount: fields[1],
    debt_grace_days: graceDays,
    debt_conversion_ratio: fields[2],
    exchange_rate: exchangeRate,
    unused_advance_debt_reduction_ratio: fields[4],
    early_repay_temporary_ratio: fields[5],
    early_repay_permanent_ratio: fields[6],
    ...((loadedPolicyHadTiers.value || tiersDirty.value) ? { exchange_tiers: tiers } : {}),
  }
}

function applyPolicyToForm(policy: BankPolicy): void {
  applySettingsAmount('advance_min_amount', policy.advance_min_amount)
  applySettingsAmount('advance_max_amount', policy.advance_max_amount)
  settingsForm.debt_grace_days = String(policy.debt_grace_days)
  applySettingsAmount('debt_conversion_ratio', policy.debt_conversion_ratio)
  applySettingsAmount('exchange_rate', policy.exchange_rate)
  applySettingsAmount('unused_advance_debt_reduction_ratio', policy.unused_advance_debt_reduction_ratio)
  applySettingsAmount('early_repay_temporary_ratio', policy.early_repay_temporary_ratio)
  applySettingsAmount('early_repay_permanent_ratio', policy.early_repay_permanent_ratio)
  settingsDirtyFields.clear()
  loadedPolicyHadTiers.value = Boolean(policy.exchange_tiers?.length)
  tiersDirty.value = false
  settingsForm.exchange_tiers.splice(0, settingsForm.exchange_tiers.length, ...(
    policy.exchange_tiers?.length
      ? policy.exchange_tiers.map((tier) => createSettingsTier(tier.up_to, tier.rate))
      : [createSettingsTier(null, policy.exchange_rate)]
  ))
}

function applySettingsAmount(field: SettingsAmountField, value: string): void {
  settingsExactValues[field] = value
  settingsForm[field] = formatAmount(value)
}

function markSettingsFieldDirty(field: SettingsAmountField): void {
  settingsDirtyFields.add(field)
}

function settingsAmountValue(field: SettingsAmountField): string {
  return (settingsDirtyFields.has(field)
    ? settingsForm[field]
    : settingsExactValues[field] ?? settingsForm[field]).trim()
}

function createSettingsTier(upTo: string | null, rate: string): SettingsTierForm {
  return {
    up_to: upTo == null ? '' : formatAmount(upTo),
    rate: formatAmount(rate),
    exact_up_to: upTo ?? '',
    exact_rate: rate,
    up_to_dirty: false,
    rate_dirty: false,
  }
}

function markTierFieldDirty(tier: SettingsTierForm, field: 'up_to' | 'rate'): void {
  tier[`${field}_dirty`] = true
  tiersDirty.value = true
}

function tierFieldValue(tier: SettingsTierForm, field: 'up_to' | 'rate'): string {
  return (tier[`${field}_dirty`] ? tier[field] : tier[`exact_${field}`]).trim()
}

function validatedExchangeTiers(): BankExchangeTier[] | null {
  if (!settingsForm.exchange_tiers.length) {
    settingsError.value = t('finance.bank.tierValidation')
    return null
  }
  const tiers: BankExchangeTier[] = []
  let previousUpper = 0n
  for (const [index, tier] of settingsForm.exchange_tiers.entries()) {
    const rawRate = tierFieldValue(tier, 'rate')
    const rate = parseScaledAmount(rawRate)
    if (rate === null || rate <= 0n) {
      settingsError.value = t('finance.bank.tierValidation')
      return null
    }
    const isLast = index === settingsForm.exchange_tiers.length - 1
    const rawUpper = tierFieldValue(tier, 'up_to')
    if (!rawUpper) {
      if (!isLast) {
        settingsError.value = t('finance.bank.tierValidation')
        return null
      }
      tiers.push({ up_to: null, rate: rawRate })
      continue
    }
    const upper = parseScaledAmount(rawUpper)
    if (upper === null || upper <= previousUpper) {
      settingsError.value = t('finance.bank.tierValidation')
      return null
    }
    tiers.push({ up_to: rawUpper, rate: rawRate })
    previousUpper = upper
  }
  if (tiers[tiers.length - 1]?.up_to !== null) {
    settingsError.value = t('finance.bank.tierValidation')
    return null
  }
  return tiers
}

function addExchangeTier(): void {
  const last = settingsForm.exchange_tiers[settingsForm.exchange_tiers.length - 1]
  if (last && !last.up_to) {
    last.up_to = '100.00'
    last.up_to_dirty = true
  }
  const nextRate = last?.rate || settingsForm.exchange_rate || '1.00'
  settingsForm.exchange_tiers.push({
    up_to: '',
    rate: nextRate,
    exact_up_to: '',
    exact_rate: nextRate,
    up_to_dirty: true,
    rate_dirty: true,
  })
  tiersDirty.value = true
}

function removeExchangeTier(index: number): void {
  if (settingsForm.exchange_tiers.length <= 1) return
  settingsForm.exchange_tiers.splice(index, 1)
  const last = settingsForm.exchange_tiers[settingsForm.exchange_tiers.length - 1]
  if (last) {
    last.up_to = ''
    last.up_to_dirty = true
  }
  tiersDirty.value = true
}

function parseScaledAmount(value: string | null | undefined): bigint | null {
  if (!value || !decimalAmountPattern.test(value)) return null
  const [integer, fraction = ''] = value.split('.')
  return BigInt(integer) * amountScale + BigInt(fraction.padEnd(8, '0'))
}

function formatScaledAmount(value: bigint): string {
  const integer = value / amountScale
  const fraction = String(value % amountScale).padStart(8, '0')
  return `${integer}.${fraction}`
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

function calculateTieredExchange(amount: string, alreadyUsed: string, tiers: BankExchangeTier[]): string {
  let remaining = parseScaledAmount(amount)
  let cursor = parseScaledAmount(alreadyUsed)
  if (remaining === null || remaining <= 0n || cursor === null) return zeroAmount
  let temporaryTotal = 0n
  let lowerBound = 0n

  for (const tier of tiers) {
    if (remaining <= 0n) break
    const rate = parseScaledAmount(tier.rate)
    if (rate === null || rate <= 0n) return zeroAmount
    const upperBound = tier.up_to === null ? null : parseScaledAmount(tier.up_to)
    if (upperBound !== null && upperBound <= lowerBound) return zeroAmount
    if (upperBound !== null && cursor >= upperBound) {
      lowerBound = upperBound
      continue
    }
    const capacity = upperBound === null ? remaining : upperBound - (cursor > lowerBound ? cursor : lowerBound)
    const allocated = capacity < remaining ? capacity : remaining
    if (allocated > 0n) {
      temporaryTotal += (allocated * rate + amountScale / 2n) / amountScale
      remaining -= allocated
      cursor += allocated
    }
    if (upperBound !== null) lowerBound = upperBound
  }

  return formatScaledAmount(temporaryTotal)
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
    unused_advance_repayment: 'bank.operations.unusedAdvanceRepayment',
    early_repay_temporary: 'bank.operations.earlyRepayTemporary',
    early_repay_permanent: 'bank.operations.earlyRepayPermanent',
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

type BankMutationScope = 'advance' | 'exchange' | 'repay'

function mutationStorageKey(scope: BankMutationScope, field: 'payload' | 'key'): string {
  return `bank-${scope}-idempotency-${field}`
}

function getOrCreateMutationIdempotencyKey(scope: BankMutationScope, payload: string): string {
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

function clearMutationIdempotencyKey(scope: BankMutationScope): void {
  localStorage.removeItem(mutationStorageKey(scope, 'payload'))
  localStorage.removeItem(mutationStorageKey(scope, 'key'))
}

function createIdempotencyKey(scope: string): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return `${scope}-${crypto.randomUUID()}`
  }
  return `${scope}-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function selectBankMode(mode: BankMode, focusTab = false): void {
  activeBankMode.value = mode
  if (!focusTab) return

  void nextTick(() => {
    const tab = mode === 'advance' ? advanceModeTab.value : mode === 'exchange' ? exchangeModeTab.value : repayModeTab.value
    tab?.focus()
  })
}

function handleBankModeKeydown(event: KeyboardEvent, currentMode: BankMode): void {
  let nextMode: BankMode | null = null
  const modes: BankMode[] = ['advance', 'exchange', 'repay']
  const currentIndex = modes.indexOf(currentMode)
  if (event.key === 'Home') nextMode = 'advance'
  else if (event.key === 'End') nextMode = 'repay'
  else if (event.key === 'ArrowRight') nextMode = modes[(currentIndex + 1) % modes.length]
  else if (event.key === 'ArrowLeft') nextMode = modes[(currentIndex - 1 + modes.length) % modes.length]
  if (!nextMode) return

  event.preventDefault()
  selectBankMode(nextMode, true)
}

function selectSettingsSection(section: SettingsSection, focusTab = false): void {
  activeSettingsSection.value = section
  if (!focusTab) return

  void nextTick(() => {
    const tab = section === 'advance'
      ? advanceSettingsTab.value
      : section === 'exchange'
        ? exchangeSettingsTab.value
        : repaySettingsTab.value
    tab?.focus()
  })
}

function handleSettingsSectionKeydown(event: KeyboardEvent, currentSection: SettingsSection): void {
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    selectSettingsSection(currentSection, true)
    return
  }

  let nextSection: SettingsSection | null = null
  const sections: SettingsSection[] = ['advance', 'exchange', 'repay']
  const currentIndex = sections.indexOf(currentSection)
  if (event.key === 'Home') nextSection = 'advance'
  else if (event.key === 'End') nextSection = 'repay'
  else if (event.key === 'ArrowRight') nextSection = sections[(currentIndex + 1) % sections.length]
  else if (event.key === 'ArrowLeft') nextSection = sections[(currentIndex - 1 + sections.length) % sections.length]
  if (!nextSection) return

  event.preventDefault()
  selectSettingsSection(nextSection, true)
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
  void loadStatus()
  void loadLedger(1)
})

onBeforeUnmount(() => {
  removeExchangeTierTooltipListeners()
})
</script>
