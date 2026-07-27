<template>
  <section class="py-6" data-test="prompt-job-workspace">
    <div class="mb-5 flex flex-col gap-3 border-b border-gray-200 pb-4 dark:border-dark-700 lg:flex-row lg:items-end lg:justify-between">
      <div>
        <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('admin.promptAudit.jobs.title') }}</h2>
        <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.jobs.description') }}</p>
      </div>
      <button type="button" class="btn btn-secondary btn-sm self-start" :disabled="loading" @click="loadJobs(page.page)">
        <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
        {{ t('admin.promptAudit.actions.refresh') }}
      </button>
    </div>

    <div v-if="page.failure_reasons.length" class="mb-5 grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
      <button
        v-for="item in page.failure_reasons.slice(0, 8)"
        :key="item.error_code"
        type="button"
        class="flex items-center justify-between border border-gray-200 bg-white px-3 py-2 text-left text-sm hover:border-primary-400 dark:border-dark-700 dark:bg-dark-850 dark:hover:border-primary-500"
        :class="{ 'border-primary-500 dark:border-primary-500': filters.error_code === item.error_code }"
        @click="toggleErrorCode(item.error_code)"
      >
        <span class="min-w-0 truncate font-mono text-xs text-gray-700 dark:text-dark-200">{{ item.error_code }}</span>
        <span class="ml-3 tabular-nums text-gray-500 dark:text-dark-400">{{ item.count }}</span>
      </button>
    </div>

    <form class="mb-5 grid gap-3 border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-850 lg:grid-cols-[minmax(16rem,1fr)_13rem_13rem_auto]" @submit.prevent="applyFilters">
      <label class="relative block">
        <span class="sr-only">{{ t('admin.promptAudit.jobs.search') }}</span>
        <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
        <input v-model="filters.keyword" type="search" class="input w-full pl-10" :placeholder="t('admin.promptAudit.jobs.search')" />
      </label>
      <Select v-model="filters.status" :options="statusOptions" :aria-label="t('admin.promptAudit.jobs.status')" />
      <Select v-model="filters.error_code" :options="errorOptions" :aria-label="t('admin.promptAudit.jobs.errorCode')" />
      <div class="flex gap-2">
        <button type="submit" class="btn btn-primary flex-1 lg:flex-none">{{ t('common.search') }}</button>
        <button type="button" class="btn btn-secondary" @click="resetFilters">{{ t('common.reset') }}</button>
      </div>
    </form>

    <div v-if="error" role="alert" class="mb-5 border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
      {{ error }}
    </div>

    <div class="hidden overflow-x-auto border border-gray-200 dark:border-dark-700 md:block">
      <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
        <thead class="bg-gray-50 dark:bg-dark-800">
          <tr>
            <th class="table-th">{{ t('admin.promptAudit.jobs.request') }}</th>
            <th class="table-th">{{ t('admin.promptAudit.jobs.subject') }}</th>
            <th class="table-th">{{ t('admin.promptAudit.jobs.runtime') }}</th>
            <th class="table-th">{{ t('admin.promptAudit.jobs.failure') }}</th>
            <th class="table-th">{{ t('admin.promptAudit.jobs.payload') }}</th>
            <th class="table-th">{{ t('admin.promptAudit.common.actions') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-850">
          <tr v-for="item in page.items" :key="item.job.id">
            <td class="table-td">
              <p class="font-mono text-xs font-medium text-gray-900 dark:text-white">#{{ item.job.id }} · {{ item.job.snapshot.request_id || '—' }}</p>
              <p class="mt-1 max-w-xs truncate text-xs text-gray-500 dark:text-dark-400">{{ item.job.snapshot.redacted_preview || '—' }}</p>
              <p class="mt-1 text-xs text-gray-400">{{ formatDate(item.job.created_at) }}</p>
            </td>
            <td class="table-td">
              <p class="max-w-56 truncate text-sm text-gray-900 dark:text-white">{{ subjectLabel(item) }}</p>
              <p class="mt-1 max-w-56 truncate text-xs text-gray-500 dark:text-dark-400">{{ item.job.snapshot.group_name || '—' }}</p>
            </td>
            <td class="table-td">
              <span :class="statusBadge(item.job.status)">{{ statusLabel(item.job.status) }}</span>
              <p class="mt-2 text-xs tabular-nums text-gray-500 dark:text-dark-400">
                {{ item.job.attempts }} / {{ item.job.max_attempts }} · v{{ item.job.config_version }}
              </p>
              <p class="mt-1 max-w-48 truncate text-xs text-gray-400">{{ item.job.snapshot.model || item.job.snapshot.endpoint || '—' }}</p>
            </td>
            <td class="table-td">
              <p class="max-w-64 break-all font-mono text-xs text-red-600 dark:text-red-300">{{ item.job.last_error_code || '—' }}</p>
              <p v-if="item.job.last_error_message" class="mt-1 max-w-64 text-xs text-gray-500 dark:text-dark-400">{{ item.job.last_error_message }}</p>
            </td>
            <td class="table-td">
              <span :class="payloadBadge(item.payload_state)">{{ payloadLabel(item) }}</span>
            </td>
            <td class="table-td">
              <div class="flex flex-wrap gap-2">
                <button
                  v-if="canRetry(item)"
                  type="button"
                  class="btn btn-secondary btn-xs"
                  @click="openOperation(item, 'retry')"
                >{{ t('admin.promptAudit.jobs.retry') }}</button>
                <button
                  v-if="item.job.status === 'failed'"
                  type="button"
                  class="btn btn-secondary btn-xs"
                  @click="openOperation(item, 'quarantine')"
                >{{ t('admin.promptAudit.jobs.quarantine') }}</button>
                <button
                  v-if="['failed', 'quarantined'].includes(item.job.status)"
                  type="button"
                  class="btn btn-ghost btn-xs text-red-600 dark:text-red-300"
                  @click="openOperation(item, 'discard')"
                >{{ t('admin.promptAudit.jobs.discard') }}</button>
              </div>
              <p v-if="item.operations?.length" class="mt-2 text-xs text-gray-400">
                {{ t('admin.promptAudit.jobs.lastOperation', {
                  operation: operationLabel(item.operations[0].operation),
                  time: formatDate(item.operations[0].created_at),
                }) }}
              </p>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="grid gap-3 md:hidden">
      <article v-for="item in page.items" :key="item.job.id" class="border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-850">
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <p class="truncate font-mono text-xs text-gray-500">#{{ item.job.id }} · {{ item.job.snapshot.request_id || '—' }}</p>
            <p class="mt-1 truncate font-medium text-gray-950 dark:text-white">{{ subjectLabel(item) }}</p>
          </div>
          <span :class="statusBadge(item.job.status)">{{ statusLabel(item.job.status) }}</span>
        </div>
        <p class="mt-3 break-all font-mono text-xs text-red-600 dark:text-red-300">{{ item.job.last_error_code || '—' }}</p>
        <div class="mt-3 flex items-center justify-between gap-3 text-xs text-gray-500 dark:text-dark-400">
          <span>{{ item.job.attempts }} / {{ item.job.max_attempts }}</span>
          <span :class="payloadBadge(item.payload_state)">{{ payloadLabel(item) }}</span>
        </div>
        <div class="mt-4 flex flex-wrap gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
          <button v-if="canRetry(item)" type="button" class="btn btn-secondary btn-xs" @click="openOperation(item, 'retry')">{{ t('admin.promptAudit.jobs.retry') }}</button>
          <button v-if="item.job.status === 'failed'" type="button" class="btn btn-secondary btn-xs" @click="openOperation(item, 'quarantine')">{{ t('admin.promptAudit.jobs.quarantine') }}</button>
          <button v-if="['failed', 'quarantined'].includes(item.job.status)" type="button" class="btn btn-ghost btn-xs text-red-600" @click="openOperation(item, 'discard')">{{ t('admin.promptAudit.jobs.discard') }}</button>
        </div>
      </article>
    </div>

    <div v-if="loading && !page.items.length" class="flex min-h-56 items-center justify-center">
      <span class="loading-spinner" />
    </div>
    <EmptyState v-else-if="!page.items.length" :text="t('admin.promptAudit.jobs.empty')" />
    <Pagination
      v-if="page.total > 0"
      class="mt-4 border border-gray-200 dark:border-dark-700"
      :total="page.total"
      :page="page.page"
      :page-size="page.page_size"
      @update:page="loadJobs"
      @update:page-size="changePageSize"
    />

    <BaseDialog
      :show="operationDialog.show"
      :title="operationDialogTitle"
      width="normal"
      @close="closeOperation"
    >
      <div class="space-y-4">
        <div v-if="operationDialog.item" class="border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-700 dark:bg-dark-800">
          <p class="font-mono text-xs">#{{ operationDialog.item.job.id }} · {{ operationDialog.item.job.last_error_code || '—' }}</p>
          <p class="mt-2 text-gray-600 dark:text-dark-300">{{ operationWarning }}</p>
        </div>
        <label class="block">
          <span class="mb-2 block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.promptAudit.jobs.reason') }}</span>
          <textarea v-model.trim="operationDialog.reason" class="input min-h-28 w-full" maxlength="256" :placeholder="t('admin.promptAudit.jobs.reasonPlaceholder')" />
          <span class="mt-1 block text-right text-xs tabular-nums text-gray-400">{{ operationDialog.reason.length }} / 256</span>
        </label>
        <div class="flex justify-end gap-2">
          <button type="button" class="btn btn-secondary" :disabled="mutating" @click="closeOperation">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-primary" :disabled="operationDialog.reason.length < 3 || mutating" @click="submitOperation">
            <span v-if="mutating" class="loading-spinner mr-2 h-4 w-4" />
            {{ operationDialogConfirm }}
          </button>
        </div>
      </div>
    </BaseDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'
import { extractApiErrorMessage } from '@/utils/apiError'
import promptAuditAPI from '../api'
import type {
  PromptAuditAdminJob,
  PromptAuditJobFilters,
  PromptAuditJobPage,
  PromptAuditJobStatus,
} from '../types'

type JobOperation = 'retry' | 'quarantine' | 'discard'

const { t, locale } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()
const loading = ref(false)
const mutating = ref(false)
const error = ref('')
const page = reactive<PromptAuditJobPage>({
  items: [], failure_reasons: [], total: 0, page: 1, page_size: 20, pages: 0,
})
const filters = reactive<PromptAuditJobFilters>({
  status: '', error_code: '', keyword: '', start_at: '', end_at: '',
})
const appliedFilters = reactive<PromptAuditJobFilters>({ ...filters })
const operationDialog = reactive<{
  show: boolean
  operation: JobOperation
  item: PromptAuditAdminJob | null
  reason: string
}>({ show: false, operation: 'retry', item: null, reason: '' })

const statusOptions = computed(() => [
  { value: '', label: t('admin.promptAudit.jobs.allStatuses') },
  ...(['failed', 'quarantined', 'retry', 'queued', 'processing', 'done', 'discarded', 'staging'] as PromptAuditJobStatus[])
    .map((value) => ({ value, label: statusLabel(value) })),
])
const errorOptions = computed(() => [
  { value: '', label: t('admin.promptAudit.jobs.allErrors') },
  ...page.failure_reasons.map((item) => ({
    value: item.error_code,
    label: `${item.error_code} (${item.count})`,
  })),
])
const operationDialogTitle = computed(() => t(`admin.promptAudit.jobs.operationTitle.${operationDialog.operation}`))
const operationDialogConfirm = computed(() => t(`admin.promptAudit.jobs.${operationDialog.operation}`))
const operationWarning = computed(() => t(`admin.promptAudit.jobs.operationWarning.${operationDialog.operation}`))

function statusLabel(status: PromptAuditJobStatus): string {
  return t(`admin.promptAudit.jobs.statuses.${status}`)
}

function operationLabel(operation: JobOperation): string {
  return t(`admin.promptAudit.jobs.${operation}`)
}

function statusBadge(status: PromptAuditJobStatus): string {
  if (status === 'done') return 'badge badge-success'
  if (status === 'failed') return 'badge badge-danger'
  if (status === 'quarantined') return 'badge badge-warning'
  if (status === 'discarded') return 'badge badge-gray'
  if (status === 'processing') return 'badge badge-primary'
  return 'badge badge-gray'
}

function payloadBadge(state: PromptAuditAdminJob['payload_state']): string {
  if (state === 'available') return 'badge badge-success'
  if (state === 'unknown') return 'badge badge-warning'
  return 'badge badge-gray'
}

function payloadLabel(item: PromptAuditAdminJob): string {
  if (item.payload_state === 'available') {
    return t('admin.promptAudit.jobs.payloadAvailable', { seconds: item.payload_ttl_seconds })
  }
  return t(`admin.promptAudit.jobs.payloadStates.${item.payload_state}`)
}

function subjectLabel(item: PromptAuditAdminJob): string {
  return item.job.snapshot.user_email || item.job.snapshot.username || t('admin.promptAudit.jobs.unknownSubject')
}

function canRetry(item: PromptAuditAdminJob): boolean {
  return ['failed', 'quarantined'].includes(item.job.status) && item.payload_state === 'available' && item.payload_ttl_seconds >= 10
}

function formatDate(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat(locale.value.startsWith('zh') ? 'zh-CN' : 'en-US', {
    dateStyle: 'short', timeStyle: 'medium',
  }).format(date)
}

async function loadJobs(targetPage = 1) {
  loading.value = true
  error.value = ''
  try {
    const result = await promptAuditAPI.listJobs(appliedFilters, targetPage, page.page_size)
    Object.assign(page, result)
  } catch (loadError) {
    error.value = extractApiErrorMessage(loadError, t('admin.promptAudit.jobs.loadFailed'))
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  Object.assign(appliedFilters, filters)
  void loadJobs(1)
}

function resetFilters() {
  Object.assign(filters, { status: '', error_code: '', keyword: '', start_at: '', end_at: '' })
  applyFilters()
}

function toggleErrorCode(errorCode: string) {
  filters.error_code = filters.error_code === errorCode ? '' : errorCode
  applyFilters()
}

function changePageSize(pageSize: number) {
  page.page_size = pageSize
  void loadJobs(1)
}

function openOperation(item: PromptAuditAdminJob, operation: JobOperation) {
  operationDialog.show = true
  operationDialog.operation = operation
  operationDialog.item = item
  operationDialog.reason = ''
}

function closeOperation() {
  if (mutating.value) return
  operationDialog.show = false
  operationDialog.item = null
  operationDialog.reason = ''
}

async function submitOperation() {
  const item = operationDialog.item
  const reason = operationDialog.reason.trim()
  if (!item || reason.length < 3) return
  mutating.value = true
  try {
    const execute = () => promptAuditAPI.transitionJob(item.job.id, operationDialog.operation, reason)
    await stepUp.run(execute)
    appStore.showSuccess(t('admin.promptAudit.jobs.operationSucceeded'))
    operationDialog.show = false
    operationDialog.item = null
    operationDialog.reason = ''
    await loadJobs(page.page)
  } catch (operationError) {
    if (isStepUpCancelled(operationError)) return
    if (isStepUpBlocked(operationError)) {
      appStore.showError(
        stepUpBlockReason(operationError) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
          ? t('stepUp.adminApiKeyForbidden')
          : t('stepUp.notEnabled'),
      )
      return
    }
    appStore.showError(extractApiErrorMessage(operationError, t('admin.promptAudit.jobs.operationFailed')))
  } finally {
    mutating.value = false
  }
}

onMounted(() => loadJobs(1))
</script>
