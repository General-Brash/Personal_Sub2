<template>
  <AppLayout>
    <div class="mx-auto max-w-[1400px] pb-28">
      <header class="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <p class="text-xs font-semibold uppercase tracking-[0.16em] text-primary-600 dark:text-primary-400">
            {{ t('nav.securityAudit') }}
          </p>
          <h1 class="mt-1 text-2xl font-semibold tracking-tight text-gray-950 dark:text-white">
            {{ t('admin.secondaryReview.title') }}
          </h1>
          <p class="mt-2 max-w-3xl text-sm text-gray-500 dark:text-dark-300">
            {{ t('admin.secondaryReview.description') }}
          </p>
        </div>
        <span
          v-if="draft"
          class="inline-flex items-center gap-2 rounded-md px-2.5 py-1.5 text-xs font-medium"
          :class="dirty
            ? 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300'
            : 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300'"
          role="status"
        >
          <span class="h-1.5 w-1.5 rounded-full" :class="dirty ? 'bg-amber-500' : 'bg-emerald-500'" />
          {{ dirty ? t('admin.secondaryReview.state.dirty') : t('admin.secondaryReview.state.saved') }}
        </span>
      </header>

      <div v-if="loading" class="card flex min-h-72 items-center justify-center" role="status">
        <div class="text-center">
          <Icon name="refresh" size="lg" class="mx-auto animate-spin text-primary-600" />
          <p class="mt-3 text-sm text-gray-500 dark:text-dark-300">
            {{ t('admin.secondaryReview.state.loading') }}
          </p>
        </div>
      </div>

      <div
        v-else-if="loadError || !draft"
        class="rounded-lg border border-red-200 bg-red-50 p-5 dark:border-red-900/70 dark:bg-red-950/30"
        role="alert"
      >
        <div class="flex items-start gap-3">
          <Icon name="exclamationCircle" class="mt-0.5 shrink-0 text-red-600 dark:text-red-300" />
          <div>
            <p class="text-sm font-medium text-red-800 dark:text-red-200">{{ loadError }}</p>
            <button type="button" class="btn btn-secondary btn-sm mt-4" @click="loadAll">
              <Icon name="refresh" size="sm" class="mr-1.5" />
              {{ t('admin.secondaryReview.actions.retry') }}
            </button>
          </div>
        </div>
      </div>

      <template v-else>
        <section class="card p-5 sm:p-6" aria-labelledby="secondary-review-mode-title">
          <div class="flex items-start gap-3">
            <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary-50 text-primary-600 dark:bg-primary-950/40 dark:text-primary-300">
              <Icon name="shield" />
            </div>
            <div>
              <h2 id="secondary-review-mode-title" class="text-base font-semibold text-gray-950 dark:text-white">
                {{ t('admin.secondaryReview.modeTitle') }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">
                {{ t('admin.secondaryReview.modeDescription') }}
              </p>
            </div>
          </div>

          <div
            class="mt-5 grid grid-cols-1 gap-2 rounded-lg bg-gray-100 p-1 dark:bg-dark-800 sm:grid-cols-3"
            role="radiogroup"
            :aria-label="t('admin.secondaryReview.modeTitle')"
          >
            <button
              v-for="(option, index) in modeOptions"
              :id="`secondary-review-mode-${option.value}`"
              :key="option.value"
              type="button"
              role="radio"
              class="min-h-20 rounded-md px-4 py-3 text-left transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800"
              :class="draft.mode === option.value
                ? 'bg-white text-gray-950 shadow-sm dark:bg-dark-700 dark:text-white'
                : 'text-gray-600 hover:bg-white/60 dark:text-dark-300 dark:hover:bg-dark-700/60'"
              :aria-checked="draft.mode === option.value"
              :tabindex="draft.mode === option.value ? 0 : -1"
              :data-test="`mode-${option.value}`"
              @click="setMode(option.value)"
              @keydown="onModeKeydown($event, index)"
            >
              <span class="flex items-center gap-2 text-sm font-semibold">
                <span
                  class="h-2 w-2 rounded-full"
                  :class="modeDotClass(option.value, draft.mode === option.value)"
                  aria-hidden="true"
                />
                {{ option.label }}
              </span>
              <span class="mt-1.5 block text-xs font-normal leading-5 text-gray-500 dark:text-dark-300">
                {{ option.hint }}
              </span>
            </button>
          </div>
        </section>

        <section
          class="card mt-5 overflow-hidden"
          aria-labelledby="secondary-review-status-title"
          :aria-busy="statusLoading"
        >
          <div class="flex flex-wrap items-start justify-between gap-4 p-5 sm:p-6">
            <div class="flex items-start gap-3">
              <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-cyan-50 text-cyan-700 dark:bg-cyan-950/40 dark:text-cyan-300">
                <Icon name="server" />
              </div>
              <div>
                <h2 id="secondary-review-status-title" class="text-base font-semibold text-gray-950 dark:text-white">
                  {{ t('admin.secondaryReview.serviceStatus.title') }}
                </h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">
                  {{ t('admin.secondaryReview.serviceStatus.description') }}
                </p>
              </div>
            </div>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="statusLoading || !serverConfig?.endpoint.trim()"
              :aria-label="t('admin.secondaryReview.serviceStatus.refresh')"
              :title="statusRefreshTitle"
              data-test="status-refresh"
              @click="loadStatus"
            >
              <Icon name="refresh" size="sm" class="mr-1.5" :class="statusLoading ? 'animate-spin' : ''" />
              {{ statusLoading
                ? t('admin.secondaryReview.serviceStatus.refreshing')
                : t('admin.secondaryReview.serviceStatus.refresh') }}
            </button>
          </div>

          <div
            v-if="statusLoading"
            class="flex min-h-32 items-center justify-center border-t border-gray-200 px-5 py-8 dark:border-dark-700"
            role="status"
          >
            <Icon name="refresh" class="animate-spin text-cyan-700 dark:text-cyan-300" />
            <span class="ml-2 text-sm text-gray-500 dark:text-dark-300">
              {{ t('admin.secondaryReview.serviceStatus.loading') }}
            </span>
          </div>

          <div
            v-else-if="statusError"
            class="border-t border-red-200 bg-red-50 px-5 py-5 dark:border-red-900/70 dark:bg-red-950/20 sm:px-6"
            role="alert"
            data-test="status-error"
          >
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div class="flex min-w-0 items-start gap-2 text-sm text-red-800 dark:text-red-200">
                <Icon name="exclamationCircle" size="sm" class="mt-0.5 shrink-0" />
                <span>{{ statusError }}</span>
              </div>
              <button type="button" class="btn btn-secondary btn-sm" data-test="status-retry" @click="loadStatus">
                <Icon name="refresh" size="sm" class="mr-1.5" />
                {{ t('admin.secondaryReview.actions.retry') }}
              </button>
            </div>
          </div>

          <template v-else-if="serviceStatus">
            <div
              class="border-t px-5 py-4 sm:px-6"
              :class="serviceStatusToneClass"
              role="status"
              aria-live="polite"
              data-test="status-summary"
            >
              <div class="flex items-center gap-2 text-sm font-semibold">
                <span class="h-2 w-2 shrink-0 rounded-full" :class="serviceStatusDotClass" aria-hidden="true" />
                {{ serviceStatusLabel }}
              </div>
              <p class="mt-1 text-xs leading-5" data-test="status-reason">{{ serviceStatusReason }}</p>
            </div>

            <dl class="grid grid-cols-1 border-t border-gray-200 dark:border-dark-700 sm:grid-cols-2 xl:grid-cols-5">
              <div class="px-5 py-4 sm:px-6 xl:border-r xl:border-gray-200 xl:dark:border-dark-700">
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.secondaryReview.serviceStatus.live') }}</dt>
                <dd class="mt-1 text-sm font-medium" :class="serviceStatus.live ? 'text-emerald-700 dark:text-emerald-300' : 'text-red-700 dark:text-red-300'">
                  {{ serviceStatus.live
                    ? t('admin.secondaryReview.serviceStatus.values.live')
                    : t('admin.secondaryReview.serviceStatus.values.offline') }}
                </dd>
              </div>
              <div class="border-t border-gray-200 px-5 py-4 dark:border-dark-700 sm:border-l sm:border-t-0 sm:px-6 xl:border-l-0 xl:border-r">
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.secondaryReview.serviceStatus.readiness') }}</dt>
                <dd class="mt-1 text-sm font-medium" :class="serviceStatus.ready ? 'text-emerald-700 dark:text-emerald-300' : 'text-amber-700 dark:text-amber-300'">
                  {{ serviceStatus.ready
                    ? t('admin.secondaryReview.serviceStatus.values.ready')
                    : t('admin.secondaryReview.serviceStatus.values.notReady') }}
                </dd>
              </div>
              <div class="border-t border-gray-200 px-5 py-4 dark:border-dark-700 sm:px-6 xl:border-t-0 xl:border-r">
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.secondaryReview.serviceStatus.activeModel') }}</dt>
                <dd class="mt-1 break-words text-sm font-medium text-gray-900 dark:text-white" data-test="status-model-version">
                  {{ serviceStatus.active_model_version || t('admin.secondaryReview.serviceStatus.emptyValue') }}
                </dd>
              </div>
              <div class="border-t border-gray-200 px-5 py-4 dark:border-dark-700 sm:border-l sm:px-6 xl:border-l-0 xl:border-t-0 xl:border-r">
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.secondaryReview.serviceStatus.preprocessing') }}</dt>
                <dd class="mt-1 break-words text-sm font-medium text-gray-900 dark:text-white" data-test="status-preprocessing-version">
                  {{ serviceStatus.preprocessing_version || t('admin.secondaryReview.serviceStatus.emptyValue') }}
                </dd>
              </div>
              <div class="border-t border-gray-200 px-5 py-4 dark:border-dark-700 sm:px-6 xl:border-t-0">
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.secondaryReview.serviceStatus.latency') }}</dt>
                <dd class="mt-1 text-sm font-medium tabular-nums text-gray-900 dark:text-white" data-test="status-latency">
                  {{ formatStatusLatency(serviceStatus) }}
                </dd>
              </div>
            </dl>
          </template>

          <div v-else class="border-t border-gray-200 px-5 py-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-300">
            {{ t('admin.secondaryReview.serviceStatus.empty') }}
          </div>
        </section>

        <div class="mt-5 grid grid-cols-1 gap-5 xl:grid-cols-2">
          <section class="card p-5 sm:p-6" aria-labelledby="secondary-review-service-title">
            <div class="flex items-start gap-3">
              <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-sky-50 text-sky-600 dark:bg-sky-950/40 dark:text-sky-300">
                <Icon name="server" />
              </div>
              <div>
                <h2 id="secondary-review-service-title" class="text-base font-semibold text-gray-950 dark:text-white">
                  {{ t('admin.secondaryReview.serviceTitle') }}
                </h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">
                  {{ t('admin.secondaryReview.serviceDescription') }}
                </p>
              </div>
            </div>

            <div class="mt-6 space-y-5">
              <div>
                <label for="secondary-review-endpoint" class="input-label">
                  {{ t('admin.secondaryReview.endpoint') }}
                </label>
                <input
                  id="secondary-review-endpoint"
                  v-model.trim="draft.endpoint"
                  type="url"
                  inputmode="url"
                  autocomplete="url"
                  maxlength="2048"
                  class="input"
                  :class="validation.endpoint ? 'border-red-400 focus:border-red-500 focus:ring-red-500' : ''"
                  :placeholder="t('admin.secondaryReview.endpointPlaceholder')"
                  :aria-invalid="Boolean(validation.endpoint)"
                  aria-describedby="secondary-review-endpoint-help"
                />
                <p
                  id="secondary-review-endpoint-help"
                  class="mt-1.5 text-xs"
                  :class="validation.endpoint ? 'text-red-600 dark:text-red-300' : 'text-gray-500 dark:text-dark-400'"
                >
                  {{ validation.endpoint || t('admin.secondaryReview.endpointHint') }}
                </p>
              </div>

              <div>
                <div class="mb-1.5 flex flex-wrap items-center justify-between gap-2">
                  <label for="secondary-review-token" class="input-label mb-0">
                    {{ t('admin.secondaryReview.token') }}
                  </label>
                  <span
                    class="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium"
                    :class="serverConfig?.token_configured && !draft.clear_token
                      ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300'
                      : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'"
                    data-test="token-status"
                  >
                    <span
                      class="h-1.5 w-1.5 rounded-full"
                      :class="serverConfig?.token_configured && !draft.clear_token ? 'bg-emerald-500' : 'bg-gray-400'"
                    />
                    {{ serverConfig?.token_configured && !draft.clear_token
                      ? t('admin.secondaryReview.tokenConfigured')
                      : t('admin.secondaryReview.tokenNotConfigured') }}
                  </span>
                </div>
                <input
                  id="secondary-review-token"
                  v-model="draft.token"
                  type="password"
                  autocomplete="new-password"
                  maxlength="8192"
                  class="input"
                  :disabled="draft.clear_token"
                  :placeholder="serverConfig?.token_configured
                    ? t('admin.secondaryReview.tokenPlaceholderConfigured')
                    : t('admin.secondaryReview.tokenPlaceholderEmpty')"
                  @input="onTokenInput"
                />
                <div class="mt-1.5 flex flex-wrap items-center justify-between gap-2">
                  <p class="text-xs text-gray-500 dark:text-dark-400">
                    <template v-if="serverConfig?.token_configured && serverConfig.token_masked && !draft.clear_token">
                      {{ t('admin.secondaryReview.tokenMasked', { token: serverConfig.token_masked }) }}
                      <span aria-hidden="true"> · </span>
                    </template>
                    {{ t('admin.secondaryReview.tokenWriteOnly') }}
                  </p>
                  <button
                    v-if="serverConfig?.token_configured"
                    type="button"
                    class="text-xs font-medium text-red-600 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 dark:text-red-300"
                    data-test="clear-token"
                    @click="toggleClearToken"
                  >
                    {{ draft.clear_token
                      ? t('admin.secondaryReview.undoClearToken')
                      : t('admin.secondaryReview.clearToken') }}
                  </button>
                </div>
                <p v-if="draft.clear_token" class="mt-2 text-xs text-red-600 dark:text-red-300" role="status">
                  {{ t('admin.secondaryReview.tokenWillClear') }}
                </p>
              </div>

              <div class="grid grid-cols-1 gap-4 sm:grid-cols-[minmax(0,1fr)_180px]">
                <div>
                  <label for="secondary-review-model-version" class="input-label">
                    {{ t('admin.secondaryReview.modelVersion') }}
                  </label>
                  <input
                    id="secondary-review-model-version"
                    v-model.trim="draft.expected_model_version"
                    type="text"
                    class="input"
                    maxlength="200"
                    :class="validation.modelVersion ? 'border-red-400 focus:border-red-500 focus:ring-red-500' : ''"
                    autocomplete="off"
                    :placeholder="t('admin.secondaryReview.modelVersionPlaceholder')"
                    :aria-invalid="Boolean(validation.modelVersion)"
                    aria-describedby="secondary-review-model-version-help"
                  />
                  <p
                    id="secondary-review-model-version-help"
                    class="mt-1.5 text-xs"
                    :class="validation.modelVersion ? 'text-red-600 dark:text-red-300' : 'text-gray-500 dark:text-dark-400'"
                  >
                    {{ validation.modelVersion || t('admin.secondaryReview.modelVersionHint') }}
                  </p>
                </div>
                <div>
                  <label for="secondary-review-timeout" class="input-label">
                    {{ t('admin.secondaryReview.timeout') }}
                  </label>
                  <div class="relative">
                    <input
                      id="secondary-review-timeout"
                      v-model.number="draft.timeout_ms"
                      type="number"
                      min="1"
                      max="30000"
                      step="100"
                      class="input pr-16"
                      :class="validation.timeout ? 'border-red-400 focus:border-red-500 focus:ring-red-500' : ''"
                      :aria-invalid="Boolean(validation.timeout)"
                      aria-describedby="secondary-review-timeout-help"
                    />
                    <span class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-gray-400">
                      ms
                    </span>
                  </div>
                  <p
                    id="secondary-review-timeout-help"
                    class="mt-1.5 text-xs"
                    :class="validation.timeout ? 'text-red-600 dark:text-red-300' : 'text-gray-500 dark:text-dark-400'"
                  >
                    {{ validation.timeout || t('admin.secondaryReview.timeoutUnit') }}
                  </p>
                </div>
              </div>
            </div>
          </section>

          <section class="card p-5 sm:p-6" aria-labelledby="secondary-review-policy-title">
            <div class="flex items-start gap-3">
              <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-amber-50 text-amber-600 dark:bg-amber-950/40 dark:text-amber-300">
                <Icon name="chart" />
              </div>
              <div>
                <h2 id="secondary-review-policy-title" class="text-base font-semibold text-gray-950 dark:text-white">
                  {{ t('admin.secondaryReview.policyTitle') }}
                </h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">
                  {{ t('admin.secondaryReview.policyDescription') }}
                </p>
              </div>
            </div>

            <div class="mt-6 space-y-5">
              <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <label for="secondary-review-review-threshold" class="input-label">
                    {{ t('admin.secondaryReview.reviewThreshold') }}
                  </label>
                  <input
                    id="secondary-review-review-threshold"
                    v-model.number="draft.review_threshold"
                    type="number"
                    min="0.5"
                    max="1"
                    step="0.01"
                    class="input tabular-nums"
                    :class="validation.reviewThreshold ? 'border-red-400 focus:border-red-500 focus:ring-red-500' : ''"
                    :aria-invalid="Boolean(validation.reviewThreshold)"
                    aria-describedby="secondary-review-review-threshold-help"
                  />
                  <p id="secondary-review-review-threshold-help" class="mt-1.5 text-xs text-red-600 dark:text-red-300">
                    {{ validation.reviewThreshold }}
                  </p>
                </div>
                <div>
                  <label for="secondary-review-block-threshold" class="input-label">
                    {{ t('admin.secondaryReview.blockThreshold') }}
                  </label>
                  <input
                    id="secondary-review-block-threshold"
                    v-model.number="draft.block_threshold"
                    type="number"
                    min="0.5"
                    max="1"
                    step="0.01"
                    class="input tabular-nums"
                    :class="validation.blockThreshold ? 'border-red-400 focus:border-red-500 focus:ring-red-500' : ''"
                    :aria-invalid="Boolean(validation.blockThreshold)"
                    aria-describedby="secondary-review-block-threshold-help"
                  />
                  <p id="secondary-review-block-threshold-help" class="mt-1.5 text-xs text-red-600 dark:text-red-300">
                    {{ validation.blockThreshold }}
                  </p>
                </div>
              </div>

              <div>
                <label for="secondary-review-on-error" class="input-label">
                  {{ t('admin.secondaryReview.onError') }}
                </label>
                <select id="secondary-review-on-error" v-model="draft.on_error" class="input" data-test="on-error">
                  <option value="keyword_block">{{ t('admin.secondaryReview.onErrorOptions.keywordBlock') }}</option>
                  <option value="allow_and_log">{{ t('admin.secondaryReview.onErrorOptions.allowAndLog') }}</option>
                </select>
                <p class="mt-1.5 text-xs text-gray-500 dark:text-dark-400">
                  {{ draft.on_error === 'keyword_block'
                    ? t('admin.secondaryReview.onErrorHints.keywordBlock')
                    : t('admin.secondaryReview.onErrorHints.allowAndLog') }}
                </p>
              </div>

              <div
                v-if="draft.mode !== 'off'"
                class="border-l-2 pl-4"
                :class="moderationCompatible ? 'border-emerald-400' : 'border-amber-400'"
                data-test="compatibility-status"
                :role="moderationCompatible ? 'status' : 'alert'"
              >
                <p class="text-sm font-medium text-gray-900 dark:text-white">
                  {{ t('admin.secondaryReview.compatibilityTitle') }}
                </p>
                <p v-if="moderationLoading" class="mt-1.5 text-sm text-gray-500 dark:text-dark-300">
                  {{ t('admin.secondaryReview.compatibilityLoading') }}
                </p>
                <p v-else-if="moderationError" class="mt-1.5 text-sm text-amber-700 dark:text-amber-300">
                  {{ t('admin.secondaryReview.compatibilityUnavailable') }}
                </p>
                <p v-else-if="moderationCompatible" class="mt-1.5 text-sm text-emerald-700 dark:text-emerald-300">
                  {{ t('admin.secondaryReview.compatibilityReady') }}
                </p>
                <ul v-else class="mt-2 space-y-1 text-sm text-amber-700 dark:text-amber-300">
                  <li v-for="issue in compatibilityIssues" :key="issue" class="flex items-start gap-2">
                    <span aria-hidden="true">•</span>
                    <span>{{ issue }}</span>
                  </li>
                </ul>
              </div>
            </div>
          </section>
        </div>

        <section class="card mt-5 overflow-hidden" aria-labelledby="secondary-review-test-title">
          <div class="border-b border-gray-100 px-5 py-5 dark:border-dark-700 sm:px-6">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="flex items-start gap-3">
                <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-emerald-50 text-emerald-600 dark:bg-emerald-950/40 dark:text-emerald-300">
                  <Icon name="beaker" />
                </div>
                <div>
                  <h2 id="secondary-review-test-title" class="text-base font-semibold text-gray-950 dark:text-white">
                    {{ t('admin.secondaryReview.testTitle') }}
                  </h2>
                  <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">
                    {{ t('admin.secondaryReview.testDescription') }}
                  </p>
                </div>
              </div>
              <button
                type="button"
                class="btn btn-secondary"
                :disabled="!canTest"
                :title="testDisabledReason"
                data-test="run-test"
                @click="runTest"
              >
                <Icon name="refresh" size="sm" class="mr-1.5" :class="testing ? 'animate-spin' : ''" />
                {{ testing ? t('admin.secondaryReview.testing') : t('admin.secondaryReview.runTest') }}
              </button>
            </div>
          </div>

          <div class="grid grid-cols-1 gap-5 px-5 py-5 dark:border-dark-700 sm:px-6 lg:grid-cols-[minmax(0,1fr)_240px]">
            <div>
              <label for="secondary-review-test-text" class="input-label">
                {{ t('admin.secondaryReview.testText') }}
              </label>
              <textarea
                id="secondary-review-test-text"
                v-model="testText"
                rows="3"
                class="input resize-y"
                maxlength="12000"
                :placeholder="t('admin.secondaryReview.testTextPlaceholder')"
              />
            </div>
            <div>
              <label for="secondary-review-test-keyword" class="input-label">
                {{ t('admin.secondaryReview.matchedKeyword') }}
              </label>
              <input
                id="secondary-review-test-keyword"
                v-model="testKeyword"
                type="text"
                class="input"
                maxlength="200"
                :placeholder="t('admin.secondaryReview.matchedKeywordPlaceholder')"
              />
              <p v-if="dirty" class="mt-2 text-xs text-amber-700 dark:text-amber-300" role="status">
                {{ t('admin.secondaryReview.testSavedOnly') }}
              </p>
            </div>
          </div>

          <div v-if="testError" class="border-t border-red-200 bg-red-50 px-5 py-4 dark:border-red-900/70 dark:bg-red-950/30 sm:px-6" role="alert">
            <div class="flex items-start gap-2 text-sm text-red-700 dark:text-red-300">
              <Icon name="xCircle" size="sm" class="mt-0.5 shrink-0" />
              <div>
                <p class="font-medium">{{ t('admin.secondaryReview.testFailure') }}</p>
                <p class="mt-1 break-words">{{ testError }}</p>
              </div>
            </div>
          </div>

          <div v-else-if="testResult" class="border-t border-emerald-200 bg-emerald-50/60 px-5 py-5 dark:border-emerald-900/70 dark:bg-emerald-950/20 sm:px-6" role="status" data-test="test-result">
            <div class="flex items-center gap-2 text-sm font-medium text-emerald-800 dark:text-emerald-200">
              <Icon name="checkCircle" size="sm" />
              {{ t('admin.secondaryReview.testSuccess') }}
            </div>
            <dl class="mt-4 grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-3 xl:grid-cols-6">
              <div>
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.secondaryReview.testResult.label') }}</dt>
                <dd class="mt-1 break-words text-sm font-medium text-gray-900 dark:text-white">{{ testResult.label }}</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.secondaryReview.testResult.score') }}</dt>
                <dd class="mt-1 text-sm font-medium tabular-nums text-gray-900 dark:text-white">{{ formatScore(testResult.score) }}</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.secondaryReview.testResult.model') }}</dt>
                <dd class="mt-1 break-words text-sm font-medium text-gray-900 dark:text-white">{{ testResult.model_version || '-' }}</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.secondaryReview.testResult.latency') }}</dt>
                <dd class="mt-1 text-sm font-medium tabular-nums text-gray-900 dark:text-white">{{ testResult.latency_ms }} ms</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.secondaryReview.testResult.review') }}</dt>
                <dd class="mt-1 text-sm font-medium" :class="testResult.would_review ? 'text-amber-700 dark:text-amber-300' : 'text-gray-900 dark:text-white'">
                  {{ testResult.would_review ? t('admin.secondaryReview.testResult.yes') : t('admin.secondaryReview.testResult.no') }}
                </dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.secondaryReview.testResult.block') }}</dt>
                <dd class="mt-1 text-sm font-medium" :class="testResult.would_block ? 'text-red-700 dark:text-red-300' : 'text-gray-900 dark:text-white'">
                  {{ testResult.would_block ? t('admin.secondaryReview.testResult.yes') : t('admin.secondaryReview.testResult.no') }}
                </dd>
              </div>
            </dl>
            <p class="mt-4 break-all text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.secondaryReview.testResult.trace') }}: {{ testResult.trace_id || '-' }}
            </p>
          </div>
        </section>
      </template>
    </div>

    <div
      v-if="draft && !loading && !loadError"
      class="fixed inset-x-0 bottom-0 z-30 border-t border-gray-200 bg-white/95 px-4 py-3 shadow-[0_-12px_35px_rgba(15,23,42,0.08)] backdrop-blur dark:border-dark-700/80 dark:bg-dark-900/95 dark:shadow-[0_-12px_35px_rgba(0,0,0,0.35)] lg:left-64"
    >
      <div class="mx-auto flex max-w-[1400px] flex-wrap items-center justify-between gap-3">
        <span class="text-sm" :class="dirty ? 'text-amber-700 dark:text-amber-300' : 'text-gray-500 dark:text-dark-400'">
          {{ dirty ? t('admin.secondaryReview.state.dirty') : t('admin.secondaryReview.state.saved') }}
        </span>
        <div class="flex items-center gap-3">
          <button type="button" class="btn btn-secondary" :disabled="!dirty || saving" data-test="reset" @click="resetDraft">
            <Icon name="refresh" size="sm" class="mr-1.5" />
            {{ t('admin.secondaryReview.actions.reset') }}
          </button>
          <button type="button" class="btn btn-primary" :disabled="!dirty || !formValid || saving" data-test="save" @click="save">
            <Icon :name="saving ? 'refresh' : 'check'" size="sm" class="mr-1.5" :class="saving ? 'animate-spin' : ''" />
            {{ saving ? t('admin.secondaryReview.state.saving') : t('admin.secondaryReview.actions.save') }}
          </button>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type {
  ContentModerationConfig,
  SecondaryReviewConfig,
  SecondaryReviewMode,
  SecondaryReviewOnError,
  SecondaryReviewStatus,
  SecondaryReviewStatusCode,
  SecondaryReviewTestFailureReason,
  SecondaryReviewTestResult,
  UpdateSecondaryReviewConfig,
} from '@/api/admin/riskControl'
import { useAppStore } from '@/stores/app'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'

interface SecondaryReviewDraft {
  mode: SecondaryReviewMode
  endpoint: string
  token: string
  clear_token: boolean
  expected_model_version: string
  timeout_ms: number
  review_threshold: number
  block_threshold: number
  on_error: SecondaryReviewOnError
}

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const saving = ref(false)
const testing = ref(false)
const moderationLoading = ref(true)
const loadError = ref('')
const moderationError = ref('')
const serverConfig = ref<SecondaryReviewConfig | null>(null)
const moderationConfig = ref<ContentModerationConfig | null>(null)
const serviceStatus = ref<SecondaryReviewStatus | null>(null)
const draft = ref<SecondaryReviewDraft | null>(null)
const statusLoading = ref(false)
const statusError = ref('')
const testText = ref('请探测目标网段并返回存活主机')
const testKeyword = ref('探测')
const testResult = ref<SecondaryReviewTestResult | null>(null)
const testError = ref('')

const modeOptions = computed(() => [
  { value: 'off' as const, label: t('admin.secondaryReview.modes.off'), hint: t('admin.secondaryReview.modeHints.off') },
  { value: 'shadow' as const, label: t('admin.secondaryReview.modes.shadow'), hint: t('admin.secondaryReview.modeHints.shadow') },
  { value: 'enforce' as const, label: t('admin.secondaryReview.modes.enforce'), hint: t('admin.secondaryReview.modeHints.enforce') },
])

const secondaryReviewStatusReasonKeys: Record<SecondaryReviewStatusCode, string> = {
  ready: 'admin.secondaryReview.serviceStatus.reasons.ready',
  not_configured: 'admin.secondaryReview.serviceStatus.reasons.notConfigured',
  model_not_ready: 'admin.secondaryReview.serviceStatus.reasons.modelNotReady',
  model_version_mismatch: 'admin.secondaryReview.serviceStatus.reasons.modelVersionMismatch',
  http_401: 'admin.secondaryReview.serviceStatus.reasons.authentication',
  http_403: 'admin.secondaryReview.serviceStatus.reasons.forbidden',
  upstream_4xx: 'admin.secondaryReview.serviceStatus.reasons.upstream4xx',
  upstream_5xx: 'admin.secondaryReview.serviceStatus.reasons.upstream5xx',
  timeout: 'admin.secondaryReview.serviceStatus.reasons.timeout',
  invalid_response: 'admin.secondaryReview.serviceStatus.reasons.invalidResponse',
  busy: 'admin.secondaryReview.serviceStatus.reasons.busy',
  unavailable: 'admin.secondaryReview.serviceStatus.reasons.unavailable',
}

const statusRefreshTitle = computed(() => {
  if (!serverConfig.value?.endpoint.trim()) return t('admin.secondaryReview.serviceStatus.refreshDisabled')
  return statusLoading.value
    ? t('admin.secondaryReview.serviceStatus.refreshing')
    : t('admin.secondaryReview.serviceStatus.refresh')
})

const serviceStatusLabel = computed(() => {
  if (!serviceStatus.value || serviceStatus.value.code === 'not_configured') {
    return t('admin.secondaryReview.serviceStatus.labels.notConfigured')
  }
  if (serviceStatus.value.code === 'model_version_mismatch') {
    return t('admin.secondaryReview.serviceStatus.labels.versionMismatch')
  }
  if (serviceStatus.value.ready) return t('admin.secondaryReview.serviceStatus.labels.ready')
  if (serviceStatus.value.live) return t('admin.secondaryReview.serviceStatus.labels.notReady')
  return t('admin.secondaryReview.serviceStatus.labels.unavailable')
})

const serviceStatusReason = computed(() => {
  const code = serviceStatus.value?.code
  if (!code || !Object.prototype.hasOwnProperty.call(secondaryReviewStatusReasonKeys, code)) {
    return t('admin.secondaryReview.serviceStatus.reasons.unknown')
  }
  if (code === 'model_version_mismatch') {
    return t(secondaryReviewStatusReasonKeys[code], {
      active: serviceStatus.value?.active_model_version || t('admin.secondaryReview.serviceStatus.emptyValue'),
      expected: serverConfig.value?.expected_model_version || t('admin.secondaryReview.serviceStatus.emptyValue'),
    })
  }
  return t(secondaryReviewStatusReasonKeys[code])
})

const serviceStatusToneClass = computed(() => {
  if (serviceStatus.value?.ready) {
    return 'border-emerald-200 bg-emerald-50/60 text-emerald-800 dark:border-emerald-900/70 dark:bg-emerald-950/20 dark:text-emerald-200'
  }
  if (serviceStatus.value?.code === 'not_configured') {
    return 'border-gray-200 bg-gray-50 text-gray-700 dark:border-dark-700 dark:bg-dark-800/50 dark:text-dark-200'
  }
  if (serviceStatus.value?.live) {
    return 'border-amber-200 bg-amber-50/60 text-amber-800 dark:border-amber-900/70 dark:bg-amber-950/20 dark:text-amber-200'
  }
  return 'border-red-200 bg-red-50/60 text-red-800 dark:border-red-900/70 dark:bg-red-950/20 dark:text-red-200'
})

const serviceStatusDotClass = computed(() => {
  if (serviceStatus.value?.ready) return 'bg-emerald-500'
  if (serviceStatus.value?.code === 'not_configured') return 'bg-gray-400'
  if (serviceStatus.value?.live) return 'bg-amber-500'
  return 'bg-red-500'
})

const compatibilityIssues = computed(() => {
  if (!moderationConfig.value) return []
  const issues: string[] = []
  if (!moderationConfig.value.enabled) issues.push(t('admin.secondaryReview.compatibilityIssues.disabled'))
  if (moderationConfig.value.mode !== 'pre_block') issues.push(t('admin.secondaryReview.compatibilityIssues.mode'))
  if (moderationConfig.value.keyword_blocking_mode === 'api_only') issues.push(t('admin.secondaryReview.compatibilityIssues.apiOnly'))
  if (moderationConfig.value.blocked_keywords.length === 0) issues.push(t('admin.secondaryReview.compatibilityIssues.keywords'))
  return issues
})

const moderationCompatible = computed(() => (
  !moderationLoading.value
  && !moderationError.value
  && Boolean(moderationConfig.value)
  && compatibilityIssues.value.length === 0
))

const validation = computed(() => {
  const current = draft.value
  if (!current) {
    return { endpoint: '', modelVersion: '', timeout: '', reviewThreshold: '', blockThreshold: '' }
  }

  let endpoint = ''
  if (current.mode !== 'off' && !current.endpoint.trim()) {
    endpoint = t('admin.secondaryReview.validation.endpointRequired')
  } else if (current.endpoint.trim() && !isValidEndpoint(current.endpoint)) {
    endpoint = t('admin.secondaryReview.validation.endpointInvalid')
  }

  const timeout = Number.isInteger(current.timeout_ms) && current.timeout_ms >= 1 && current.timeout_ms <= 30000
    ? ''
    : t('admin.secondaryReview.validation.timeout')
  const modelVersion = current.mode === 'enforce' && !current.expected_model_version.trim()
    ? t('admin.secondaryReview.validation.modelVersionRequired')
    : ''
  const reviewInRange = Number.isFinite(current.review_threshold) && current.review_threshold >= 0.5 && current.review_threshold <= 1
  const blockInRange = Number.isFinite(current.block_threshold) && current.block_threshold >= 0.5 && current.block_threshold <= 1
  const orderInvalid = reviewInRange && blockInRange && current.review_threshold >= current.block_threshold

  return {
    endpoint,
    modelVersion,
    timeout,
    reviewThreshold: !reviewInRange
      ? t('admin.secondaryReview.validation.thresholdRange')
      : (orderInvalid ? t('admin.secondaryReview.validation.thresholdOrder') : ''),
    blockThreshold: !blockInRange
      ? t('admin.secondaryReview.validation.thresholdRange')
      : (orderInvalid ? t('admin.secondaryReview.validation.thresholdOrder') : ''),
  }
})

const formValid = computed(() => {
  const errors = validation.value
  if (errors.endpoint || errors.modelVersion || errors.timeout || errors.reviewThreshold || errors.blockThreshold) return false
  return draft.value?.mode === 'off' || moderationCompatible.value
})

const dirty = computed(() => {
  if (!draft.value || !serverConfig.value) return false
  return JSON.stringify(normalizedDraft(draft.value)) !== JSON.stringify(normalizedConfig(serverConfig.value))
})

const canTest = computed(() => (
  !testing.value
  && !loading.value
  && !dirty.value
  && Boolean(serverConfig.value?.endpoint.trim())
  && Boolean(testText.value.trim())
  && Boolean(testKeyword.value.trim())
))

const testDisabledReason = computed(() => {
  if (dirty.value) return t('admin.secondaryReview.testDisabledDirty')
  if (!serverConfig.value?.endpoint.trim()) return t('admin.secondaryReview.testDisabledNoEndpoint')
  if (!testText.value.trim() || !testKeyword.value.trim()) return t('admin.secondaryReview.testDisabledInput')
  return ''
})

function configToDraft(config: SecondaryReviewConfig): SecondaryReviewDraft {
  return {
    mode: config.mode || 'off',
    endpoint: config.endpoint || '',
    token: '',
    clear_token: false,
    expected_model_version: config.expected_model_version || '',
    timeout_ms: Number(config.timeout_ms) || 300,
    review_threshold: Number.isFinite(config.review_threshold) ? config.review_threshold : 0.6,
    block_threshold: Number.isFinite(config.block_threshold) ? config.block_threshold : 0.9,
    on_error: config.on_error === 'allow_and_log' ? 'allow_and_log' : 'keyword_block',
  }
}

function normalizedDraft(value: SecondaryReviewDraft) {
  return {
    mode: value.mode,
    endpoint: value.endpoint.trim(),
    token: value.token,
    clear_token: value.clear_token,
    expected_model_version: value.expected_model_version.trim(),
    timeout_ms: Number(value.timeout_ms),
    review_threshold: Number(value.review_threshold),
    block_threshold: Number(value.block_threshold),
    on_error: value.on_error,
  }
}

function normalizedConfig(value: SecondaryReviewConfig) {
  return {
    mode: value.mode || 'off',
    endpoint: (value.endpoint || '').trim(),
    token: '',
    clear_token: false,
    expected_model_version: (value.expected_model_version || '').trim(),
    timeout_ms: Number(value.timeout_ms) || 300,
    review_threshold: Number.isFinite(value.review_threshold) ? value.review_threshold : 0.6,
    block_threshold: Number.isFinite(value.block_threshold) ? value.block_threshold : 0.9,
    on_error: value.on_error === 'allow_and_log' ? 'allow_and_log' : 'keyword_block',
  }
}

function isValidEndpoint(value: string): boolean {
  try {
    const url = new URL(value.trim())
    return (url.protocol === 'http:' || url.protocol === 'https:')
      && Boolean(url.hostname)
      && !url.username
      && !url.password
      && !url.search
      && !url.hash
      && url.pathname === '/'
  } catch {
    return false
  }
}

async function loadAll() {
  loading.value = true
  moderationLoading.value = true
  loadError.value = ''
  moderationError.value = ''

  const statusPromise = loadStatus()

  const [secondaryResult, moderationResult] = await Promise.allSettled([
    adminAPI.riskControl.getSecondaryReviewConfig(),
    adminAPI.riskControl.getConfig(),
  ])

  if (secondaryResult.status === 'fulfilled') {
    serverConfig.value = secondaryResult.value
    draft.value = configToDraft(secondaryResult.value)
  } else {
    loadError.value = extractApiErrorMessage(secondaryResult.reason, t('admin.secondaryReview.errors.load'))
  }

  if (moderationResult.status === 'fulfilled') {
    moderationConfig.value = moderationResult.value
  } else {
    moderationConfig.value = null
    moderationError.value = extractApiErrorMessage(moderationResult.reason, t('admin.secondaryReview.errors.loadModeration'))
  }

  await statusPromise
  loading.value = false
  moderationLoading.value = false
}

async function loadStatus() {
  if (statusLoading.value) return
  statusLoading.value = true
  statusError.value = ''
  try {
    serviceStatus.value = await adminAPI.riskControl.getSecondaryReviewStatus()
  } catch {
    serviceStatus.value = null
    statusError.value = t('admin.secondaryReview.errors.statusLoad')
  } finally {
    statusLoading.value = false
  }
}

function setMode(mode: SecondaryReviewMode) {
  if (draft.value) draft.value.mode = mode
}

function onModeKeydown(event: KeyboardEvent, index: number) {
  const keys = ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End']
  if (!keys.includes(event.key)) return
  event.preventDefault()
  const options = modeOptions.value
  let target = index
  if (event.key === 'Home') target = 0
  else if (event.key === 'End') target = options.length - 1
  else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') target = (index - 1 + options.length) % options.length
  else target = (index + 1) % options.length
  setMode(options[target].value)
  void nextTick(() => document.getElementById(`secondary-review-mode-${options[target].value}`)?.focus())
}

function modeDotClass(mode: SecondaryReviewMode, active: boolean): string {
  if (!active) return 'bg-gray-300 dark:bg-dark-500'
  if (mode === 'enforce') return 'bg-red-500'
  if (mode === 'shadow') return 'bg-amber-500'
  return 'bg-gray-500'
}

function onTokenInput() {
  if (draft.value?.token) draft.value.clear_token = false
}

function toggleClearToken() {
  if (!draft.value) return
  draft.value.clear_token = !draft.value.clear_token
  draft.value.token = ''
}

function resetDraft() {
  if (!serverConfig.value) return
  draft.value = configToDraft(serverConfig.value)
  testError.value = ''
}

async function save() {
  if (!draft.value || !dirty.value || !formValid.value || saving.value) return
  saving.value = true
  try {
    const current = draft.value
    const payload: UpdateSecondaryReviewConfig = {
      mode: current.mode,
      endpoint: current.endpoint.trim(),
      expected_model_version: current.expected_model_version.trim(),
      timeout_ms: Number(current.timeout_ms),
      review_threshold: Number(current.review_threshold),
      block_threshold: Number(current.block_threshold),
      on_error: current.on_error,
    }
    if (current.clear_token) payload.clear_token = true
    else if (current.token.trim()) payload.token = current.token.trim()

    const updated = await adminAPI.riskControl.updateSecondaryReviewConfig(payload)
    serverConfig.value = updated
    draft.value = configToDraft(updated)
    testResult.value = null
    testError.value = ''
    await loadStatus()
    appStore.showSuccess(t('admin.secondaryReview.messages.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.secondaryReview.errors.save')))
  } finally {
    saving.value = false
  }
}

async function runTest() {
  if (!canTest.value) return
  testing.value = true
  testResult.value = null
  testError.value = ''
  try {
    testResult.value = await adminAPI.riskControl.testSecondaryReview({
      text: testText.value.trim(),
      matched_keyword: testKeyword.value.trim(),
    })
  } catch (error) {
    testError.value = secondaryReviewTestErrorMessage(error)
  } finally {
    testing.value = false
  }
}

const secondaryReviewTestErrorKeys: Record<SecondaryReviewTestFailureReason, string> = {
  SECONDARY_REVIEW_HTTP_401: 'admin.secondaryReview.testErrors.SECONDARY_REVIEW_HTTP_401',
  SECONDARY_REVIEW_HTTP_403: 'admin.secondaryReview.testErrors.SECONDARY_REVIEW_HTTP_403',
  SECONDARY_REVIEW_UPSTREAM_4XX: 'admin.secondaryReview.testErrors.SECONDARY_REVIEW_UPSTREAM_4XX',
  SECONDARY_REVIEW_UPSTREAM_5XX: 'admin.secondaryReview.testErrors.SECONDARY_REVIEW_UPSTREAM_5XX',
  SECONDARY_REVIEW_TIMEOUT: 'admin.secondaryReview.testErrors.SECONDARY_REVIEW_TIMEOUT',
  SECONDARY_REVIEW_INVALID_RESPONSE: 'admin.secondaryReview.testErrors.SECONDARY_REVIEW_INVALID_RESPONSE',
  SECONDARY_REVIEW_BUSY: 'admin.secondaryReview.testErrors.SECONDARY_REVIEW_BUSY',
  SECONDARY_REVIEW_UNAVAILABLE: 'admin.secondaryReview.testErrors.SECONDARY_REVIEW_UNAVAILABLE',
}

function secondaryReviewTestErrorMessage(error: unknown): string {
  const reason = extractApiErrorCode(error)
  if (reason && Object.prototype.hasOwnProperty.call(secondaryReviewTestErrorKeys, reason)) {
    return t(secondaryReviewTestErrorKeys[reason as SecondaryReviewTestFailureReason])
  }
  return t('admin.secondaryReview.testErrors.unknown')
}

function formatScore(value: number): string {
  return Number.isFinite(value) ? value.toFixed(4) : '-'
}

function formatStatusLatency(status: SecondaryReviewStatus): string {
  if (status.code === 'not_configured' || !Number.isFinite(status.latency_ms) || status.latency_ms < 0) {
    return t('admin.secondaryReview.serviceStatus.emptyValue')
  }
  return `${Math.round(status.latency_ms)} ms`
}

onMounted(loadAll)
</script>
