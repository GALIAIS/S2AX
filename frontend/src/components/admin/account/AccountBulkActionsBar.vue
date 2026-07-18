<template>
  <div class="mb-4 flex min-h-12 flex-wrap items-center justify-between gap-3 rounded-lg bg-primary-50 p-3 dark:bg-primary-900/20">
    <div class="flex min-w-0 flex-wrap items-center gap-2">
      <span class="text-sm font-medium text-primary-900 dark:text-primary-100">
        {{ selectedIds.length > 0
          ? t('admin.accounts.bulkActions.selected', { count: selectedIds.length })
          : t('admin.accounts.bulkEdit.title') }}
      </span>
      <template v-if="selectedIds.length > 0">
        <button type="button" class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200" @click="emit('select-page')">
          {{ t('admin.accounts.bulkActions.selectCurrentPage') }}
        </button>
        <span class="text-primary-300 dark:text-primary-700" aria-hidden="true">·</span>
        <button type="button" class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200" @click="emit('clear')">
          {{ t('admin.accounts.bulkActions.clear') }}
        </button>
      </template>
    </div>

    <div class="relative flex shrink-0 items-center gap-2" ref="menuRef">
      <template v-if="selectedIds.length > 0">
        <button type="button" class="btn btn-danger btn-sm" @click="emit('delete')">
          {{ t('admin.accounts.bulkActions.delete') }}
        </button>
        <button type="button" class="btn btn-primary btn-sm" @click="emit('edit-selected')">
          {{ t('admin.accounts.bulkActions.edit') }}
        </button>
        <button
          type="button"
          class="btn btn-secondary btn-icon btn-sm"
          :aria-expanded="moreOpen"
          :aria-label="t('common.more')"
          :title="t('common.more')"
          @click="moreOpen = !moreOpen"
        >
          <Icon name="more" size="sm" />
        </button>
        <div v-if="moreOpen" class="absolute right-0 top-full z-50 mt-2 w-60 overflow-hidden rounded-xl bg-white py-1 shadow-lg ring-1 ring-black/5 dark:bg-dark-800 dark:ring-white/10" role="menu">
          <button type="button" role="menuitem" class="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 focus:bg-gray-100 focus:outline-none dark:text-gray-200 dark:hover:bg-dark-700 dark:focus:bg-dark-700" @click="run('reset-status')">
            <Icon name="refresh" size="sm" />
            {{ t('admin.accounts.bulkActions.resetStatus') }}
          </button>
          <button type="button" role="menuitem" class="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 focus:bg-gray-100 focus:outline-none dark:text-gray-200 dark:hover:bg-dark-700 dark:focus:bg-dark-700" @click="run('refresh-token')">
            <Icon name="key" size="sm" />
            {{ t('admin.accounts.bulkActions.refreshToken') }}
          </button>
          <button type="button" role="menuitem" class="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 focus:bg-gray-100 focus:outline-none dark:text-gray-200 dark:hover:bg-dark-700 dark:focus:bg-dark-700" @click="run('probe-upstream-billing')">
            <Icon name="chart" size="sm" />
            {{ t('admin.accounts.bulkActions.probeUpstreamBilling') }}
          </button>
          <div class="my-1 border-t border-gray-100 dark:border-dark-700" role="separator"></div>
          <button type="button" role="menuitem" class="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 focus:bg-gray-100 focus:outline-none dark:text-gray-200 dark:hover:bg-dark-700 dark:focus:bg-dark-700" @click="runToggle(true)">
            <Icon name="checkCircle" size="sm" />
            {{ t('admin.accounts.bulkActions.enableScheduling') }}
          </button>
          <button type="button" role="menuitem" class="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 focus:bg-gray-100 focus:outline-none dark:text-gray-200 dark:hover:bg-dark-700 dark:focus:bg-dark-700" @click="runToggle(false)">
            <Icon name="xCircle" size="sm" />
            {{ t('admin.accounts.bulkActions.disableScheduling') }}
          </button>
        </div>
      </template>
      <button type="button" class="btn btn-primary btn-sm" @click="emit('edit-filtered')">
        {{ t('admin.accounts.bulkEdit.submit') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

defineProps<{ selectedIds: number[] }>()
const emit = defineEmits<{
  (e: 'delete'): void
  (e: 'edit-selected'): void
  (e: 'edit-filtered'): void
  (e: 'clear'): void
  (e: 'select-page'): void
  (e: 'toggle-schedulable', value: boolean): void
  (e: 'reset-status'): void
  (e: 'refresh-token'): void
  (e: 'probe-upstream-billing'): void
}>()

const { t } = useI18n()
const moreOpen = ref(false)
const menuRef = ref<HTMLElement | null>(null)

const run = (event: 'reset-status' | 'refresh-token' | 'probe-upstream-billing') => {
  if (event === 'reset-status') emit('reset-status')
  else if (event === 'refresh-token') emit('refresh-token')
  else emit('probe-upstream-billing')
  moreOpen.value = false
}

const runToggle = (value: boolean) => {
  emit('toggle-schedulable', value)
  moreOpen.value = false
}

const closeOnOutsideClick = (event: MouseEvent) => {
  if (menuRef.value && !menuRef.value.contains(event.target as Node)) {
    moreOpen.value = false
  }
}

onMounted(() => document.addEventListener('click', closeOnOutsideClick))
onUnmounted(() => document.removeEventListener('click', closeOnOutsideClick))

</script>
