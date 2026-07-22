<template>
  <div
    role="tablist"
    aria-orientation="horizontal"
    :aria-label="t('commerce.shelf.title')"
    class="relative grid min-h-11 grid-cols-2 overflow-hidden rounded-lg bg-gray-100 p-1 dark:bg-dark-700"
  >
    <span
      aria-hidden="true"
      class="pointer-events-none absolute inset-y-1 left-1 rounded-md bg-white shadow-sm transition-transform duration-200 ease-out dark:bg-dark-800"
      :class="activeSection === 'subscription' ? 'translate-x-full' : 'translate-x-0'"
      style="width: calc(50% - 0.25rem)"
    />
    <RouterLink
      id="admin-shelf-tab-currency"
      to="/admin/orders/shelves"
      role="tab"
      class="relative z-10 min-w-0 px-3 py-2 text-center text-sm font-medium transition-colors"
      :class="tabClass('currency')"
      :aria-selected="activeSection === 'currency'"
      aria-controls="admin-shelf-panel-currency"
      :tabindex="activeSection === 'currency' ? 0 : -1"
      @keydown="handleTabKeydown($event, 'currency')"
    >
      {{ t('commerce.shelf.currencyProducts') }}
    </RouterLink>
    <RouterLink
      id="admin-shelf-tab-subscription"
      to="/admin/orders/plans"
      role="tab"
      class="relative z-10 min-w-0 px-3 py-2 text-center text-sm font-medium transition-colors"
      :class="tabClass('subscription')"
      :aria-selected="activeSection === 'subscription'"
      aria-controls="admin-shelf-panel-subscription"
      :tabindex="activeSection === 'subscription' ? 0 : -1"
      @keydown="handleTabKeydown($event, 'subscription')"
    >
      {{ t('commerce.shelf.subscriptionProducts') }}
    </RouterLink>
  </div>
</template>

<script setup lang="ts">
import { nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

type ShelfSection = 'currency' | 'subscription'
const props = defineProps<{ activeSection: ShelfSection }>()
const { t } = useI18n()
const router = useRouter()

function tabClass(section: ShelfSection): string {
  return props.activeSection === section
    ? 'text-primary-700 dark:text-primary-300'
    : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'
}

async function selectSection(section: ShelfSection): Promise<void> {
  if (section !== props.activeSection) {
    await router.push(section === 'currency' ? '/admin/orders/shelves' : '/admin/orders/plans')
  }
  await nextTick()
  document.getElementById(`admin-shelf-tab-${section}`)?.focus()
}

function handleTabKeydown(event: KeyboardEvent, current: ShelfSection): void {
  let next: ShelfSection | null = null
  if (event.key === 'Home') next = 'currency'
  else if (event.key === 'End') next = 'subscription'
  else if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
    next = current === 'currency' ? 'subscription' : 'currency'
  }
  if (!next) return

  event.preventDefault()
  void selectSection(next)
}
</script>
