<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="card border border-primary-100 p-5 dark:border-primary-900/50">
        <div class="flex items-start gap-3">
          <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
            <Icon name="shield" size="lg" />
          </div>
          <div class="min-w-0">
            <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountAllocations.readOnlyTitle') }}</p>
            <p class="mt-1 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('accountAllocations.readOnlyDescription') }}</p>
          </div>
        </div>
      </section>

      <div v-if="loadError" class="flex items-center gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300" role="alert">
        <Icon name="xCircle" size="sm" />
        <span>{{ loadError }}</span>
        <button type="button" class="btn btn-ghost btn-sm" @click="loadAssignments">{{ t('common.retry') }}</button>
      </div>

      <div v-if="loading && assignments.length === 0" class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <div v-for="index in 3" :key="index" class="card h-64 animate-pulse bg-gray-100 dark:bg-dark-800" />
      </div>

      <div v-else-if="assignments.length === 0" class="card empty-state py-16">
        <Icon name="inbox" size="xl" class="text-gray-400 dark:text-dark-500" />
        <p class="mt-4 text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('accountAllocations.emptyTitle') }}</p>
        <p class="mt-2 max-w-lg text-center text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('accountAllocations.emptyDescription') }}</p>
      </div>

      <div v-else class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <article
          v-for="assignment in assignments"
          :key="assignment.assignment_id"
          class="card overflow-hidden border border-gray-200 dark:border-dark-700"
        >
          <div class="flex items-start justify-between gap-4 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
            <div class="min-w-0">
              <p class="truncate text-base font-semibold text-gray-900 dark:text-white">{{ assignment.group_name }}</p>
              <p class="mt-1 truncate font-mono text-xs text-gray-500 dark:text-dark-400">{{ assignment.platform }} · {{ assignment.account_type }}</p>
            </div>
            <span :class="statusClass(assignment.status)" class="badge shrink-0">{{ statusLabel(assignment.status) }}</span>
          </div>

          <dl class="grid grid-cols-2 gap-x-4 gap-y-5 px-5 py-4">
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.capacity') }}</dt>
              <dd class="mt-1 font-mono text-lg font-semibold text-gray-900 dark:text-white">{{ assignment.capacity.concurrency }}</dd>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.concurrentRequests') }}</p>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.requestUsage') }}</dt>
              <dd class="mt-1 font-mono text-lg font-semibold text-gray-900 dark:text-white">{{ formatNumber(assignment.usage.request_count) }}</dd>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.requests') }}</p>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.tokenUsage') }}</dt>
              <dd class="mt-1 font-mono text-lg font-semibold text-gray-900 dark:text-white">{{ formatNumber(assignment.usage.total_tokens) }}</dd>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.tokens') }}</p>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.assignedAt') }}</dt>
              <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ formatDateTime(assignment.assigned_at) }}</dd>
            </div>
          </dl>

          <div class="border-t border-gray-100 px-5 py-3 text-xs dark:border-dark-700">
            <template v-if="assignment.status === 'cooling' && assignment.rate_limit_reset_at">
              <span class="text-amber-700 dark:text-amber-300">{{ t('accountAllocations.coolingUntil', { time: formatDateTime(assignment.rate_limit_reset_at) }) }}</span>
            </template>
            <template v-else-if="assignment.status === 'unavailable'">
              <span class="text-gray-500 dark:text-dark-400">{{ t('accountAllocations.unavailableHint') }}</span>
            </template>
            <template v-else>
              <span class="text-emerald-700 dark:text-emerald-300">{{ t('accountAllocations.readyHint') }}</span>
            </template>
          </div>
        </article>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import accountAllocationsAPI, { type AccountAllocationUserStatus, type UserAccountAllocation } from '@/api/accountAllocations'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime, formatNumber } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()

const assignments = ref<UserAccountAllocation[]>([])
const loading = ref(true)
const loadError = ref<string | null>(null)
let requestID = 0

const statusLabel = (status: AccountAllocationUserStatus): string => {
  if (status === 'ready') return t('accountAllocations.status.ready')
  if (status === 'cooling') return t('accountAllocations.status.cooling')
  return t('accountAllocations.status.unavailable')
}

const statusClass = (status: AccountAllocationUserStatus): string => {
  if (status === 'ready') return 'badge-success'
  if (status === 'cooling') return 'badge-warning'
  return 'badge-gray'
}

const loadAssignments = async () => {
  const currentRequestID = ++requestID
  loading.value = true
  loadError.value = null
  try {
    const result = await accountAllocationsAPI.listMine()
    if (currentRequestID === requestID) assignments.value = result
  } catch (error: unknown) {
    if (currentRequestID === requestID) {
      loadError.value = extractApiErrorMessage(error, t('accountAllocations.loadFailed'))
    }
  } finally {
    if (currentRequestID === requestID) loading.value = false
  }
}

onMounted(() => { void loadAssignments() })
onUnmounted(() => { requestID += 1 })
</script>
