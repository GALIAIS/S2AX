<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-800">
        <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
          <div class="max-w-3xl">
            <div class="flex flex-wrap items-center gap-2">
              <span class="inline-flex h-8 w-8 items-center justify-center border border-primary-200 bg-primary-50 text-primary-700 dark:border-primary-900/60 dark:bg-primary-950/30 dark:text-primary-300">
                <Icon name="shield" size="md" />
              </span>
              <span class="badge badge-primary">{{ t('admin.cityAgentRuntime.safeBoundary') }}</span>
            </div>
            <h1 class="mt-3 text-xl font-semibold tracking-tight text-gray-900 dark:text-white">{{ t('admin.cityAgentRuntime.title') }}</h1>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('admin.cityAgentRuntime.description') }}</p>
          </div>
          <button type="button" class="btn btn-secondary shrink-0" :disabled="healthLoading" @click="refreshRuntime(true)">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': healthLoading }" />
            {{ t('common.refresh') }}
          </button>
        </div>
      </section>

      <section v-if="healthError" class="border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300" role="alert">
        <div class="flex items-center justify-between gap-3">
          <span>{{ healthError }}</span>
          <button type="button" class="btn btn-ghost btn-sm shrink-0" @click="refreshRuntime()">{{ t('common.retry') }}</button>
        </div>
      </section>

      <section class="border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
        <div class="flex flex-col gap-4 border-b border-gray-200 px-5 py-4 dark:border-dark-700 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.cityAgentRuntime.workerHealth') }}</h2>
            <p class="mt-1 text-sm text-gray-600 dark:text-dark-300">{{ t('admin.cityAgentRuntime.queueDescription') }}</p>
          </div>
          <div class="w-full lg:w-80">
            <label class="input-label" for="city-agent-runtime-world">{{ t('admin.cityAgentRuntime.world') }}</label>
            <Select
              id="city-agent-runtime-world"
              v-model="selectedWorldID"
              :options="worldOptions"
              :placeholder="t('admin.cityAgentRuntime.selectWorld')"
              :disabled="healthLoading || worldOptions.length === 0"
              :searchable="worldOptions.length > 8"
            />
          </div>
        </div>

        <div v-if="healthLoading && !selectedWorld" class="flex min-h-48 items-center justify-center">
          <Icon name="refresh" size="lg" class="animate-spin text-primary-500" />
        </div>
        <div v-else-if="!selectedWorld" class="empty-state min-h-48 px-6 py-10">
          <Icon name="inbox" size="xl" class="text-gray-400 dark:text-dark-500" />
          <p class="mt-3 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.cityAgentRuntime.noRealtimeWorlds') }}</p>
        </div>
        <template v-else>
          <div class="grid divide-y divide-gray-200 border-b border-gray-200 dark:divide-dark-700 dark:border-dark-700 sm:grid-cols-2 sm:divide-x sm:divide-y-0 xl:grid-cols-4">
            <div class="p-4">
              <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ t('admin.cityAgentRuntime.worldState') }}</p>
              <p class="mt-2 font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ selectedWorld.world_name }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ selectedWorld.lifecycle_status }} · {{ selectedWorld.clock_state }}</p>
            </div>
            <div class="p-4">
              <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ t('admin.cityAgentRuntime.queued') }}</p>
              <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ workerHealth.queued_requests }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.cityAgentRuntime.retryScheduled') }} · {{ workerHealth.retry_scheduled }}</p>
            </div>
            <div class="p-4">
              <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ t('admin.cityAgentRuntime.quarantined') }}</p>
              <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ workerHealth.quarantined_requests }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.cityAgentRuntime.leased') }} · {{ workerHealth.leased_requests }}</p>
            </div>
            <div class="p-4">
              <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ t('admin.cityAgentRuntime.openBreakers') }}</p>
              <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ workerHealth.open_circuit_breakers }}</p>
              <p class="mt-1 truncate font-mono text-xs text-gray-500 dark:text-dark-400">{{ workerHealth.last_failure_code || '—' }}</p>
            </div>
          </div>

          <div v-if="workerHealth.stale_quarantined_requests > 0" class="border-b border-amber-200 bg-amber-50 px-5 py-4 dark:border-amber-900/60 dark:bg-amber-950/30">
            <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div class="flex items-start gap-3">
                <Icon name="clock" size="md" class="mt-0.5 shrink-0 text-amber-700 dark:text-amber-300" />
                <div>
                  <p class="font-medium text-amber-900 dark:text-amber-100">{{ t('admin.cityAgentRuntime.staleQuarantined') }} · {{ workerHealth.stale_quarantined_requests }}</p>
                  <p class="mt-1 text-sm text-amber-800 dark:text-amber-200">
                    {{ t('admin.cityAgentRuntime.staleAfter', { hours: staleAfterHours }) }}
                    <span v-if="workerHealth.oldest_quarantined_at">· {{ t('admin.cityAgentRuntime.oldestQuarantine') }} {{ formatDateTime(workerHealth.oldest_quarantined_at) }}</span>
                  </p>
                </div>
              </div>
              <button type="button" class="btn btn-secondary btn-sm" @click="queueScope = 'queued'">{{ t('admin.cityAgentRuntime.queuedFilter') }}</button>
            </div>
          </div>
        </template>
      </section>

      <section class="border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
        <div class="flex flex-col gap-4 border-b border-gray-200 px-5 py-4 dark:border-dark-700 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.cityAgentRuntime.queueTitle') }}</h2>
            <p class="mt-1 max-w-3xl text-sm text-gray-600 dark:text-dark-300">{{ t('admin.cityAgentRuntime.queueDescription') }}</p>
          </div>
          <div class="flex w-full flex-col gap-3 sm:flex-row lg:w-auto">
            <div class="min-w-48">
              <label class="input-label" for="city-agent-runtime-scope">{{ t('admin.cityAgentRuntime.queueFilter') }}</label>
              <Select id="city-agent-runtime-scope" v-model="queueScope" :options="queueScopeOptions" :disabled="!selectedWorldID" :searchable="false" />
            </div>
            <button type="button" class="btn btn-secondary self-end" :disabled="queueLoading || !selectedWorldID" @click="loadQueue()">
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': queueLoading }" />
              {{ t('common.refresh') }}
            </button>
          </div>
        </div>

        <DataTable
          :columns="queueColumns"
          :data="queueItems"
          :loading="queueLoading"
          :error="queueError"
          row-key="request_code"
          :aria-label="t('admin.cityAgentRuntime.queueTitle')"
          @retry="loadQueue()"
        >
          <template #cell-request_code="{ row }">
            <div class="min-w-44">
              <p class="font-mono text-xs font-semibold text-gray-900 dark:text-gray-100">{{ row.request_code }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ formatDateTime(row.created_at) }}</p>
            </div>
          </template>

          <template #cell-agent="{ row }">
            <div>
              <p class="font-mono text-xs text-gray-800 dark:text-dark-200">{{ row.agent_definition_code }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ modelProfileLabel(row) }}</p>
            </div>
          </template>

          <template #cell-state="{ row }">
            <div class="flex min-w-36 flex-wrap gap-1">
              <span :class="['badge', requestStatusBadgeClass(row.request_status)]">{{ requestStatusLabel(row.request_status) }}</span>
              <span :class="['badge', outboxStatusBadgeClass(row.outbox_status)]">{{ outboxStatusLabel(row.outbox_status) }}</span>
            </div>
          </template>

          <template #cell-attempts="{ row }">
            <div class="min-w-28">
              <p class="font-mono text-sm font-semibold text-gray-800 dark:text-dark-100">{{ row.attempt_count }}</p>
              <p class="mt-1 max-w-36 truncate font-mono text-xs text-gray-500 dark:text-dark-400" :title="row.last_error_code || ''">{{ row.last_error_code || '—' }}</p>
            </div>
          </template>

          <template #cell-retry_at="{ row }">
            <span class="whitespace-nowrap text-xs text-gray-600 dark:text-dark-300">{{ row.retry_not_before ? formatDateTime(row.retry_not_before) : '—' }}</span>
          </template>

          <template #cell-quarantine="{ row }">
            <div class="min-w-36">
              <span :class="['badge', quarantineBadgeClass(row)]">{{ quarantineLabel(row) }}</span>
              <p v-if="row.dead_letter_reason_code" class="mt-1 font-mono text-xs text-gray-500 dark:text-dark-400">{{ deadLetterReasonLabel(row.dead_letter_reason_code) }}</p>
              <p v-if="row.dead_letter_quarantined_at" class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ formatDateTime(row.dead_letter_quarantined_at) }}</p>
            </div>
          </template>

          <template #cell-actions="{ row }">
            <RowActionMenu
              :items="queueActionItems(row)"
              :aria-label="row.request_code"
              @select="(key) => handleQueueAction(key, row)"
            />
          </template>

          <template #empty>
            <div class="flex flex-col items-center">
              <Icon name="inbox" size="xl" class="mb-4 h-12 w-12 text-gray-400 dark:text-dark-500" />
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('admin.cityAgentRuntime.queueEmpty') }}</p>
            </div>
          </template>
        </DataTable>

        <div v-if="nextQueueCursor" class="flex justify-center border-t border-gray-200 px-5 py-4 dark:border-dark-700">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="queueLoading" @click="loadQueue(true)">
            <Icon v-if="queueLoading" name="refresh" size="sm" class="animate-spin" />
            {{ t('admin.cityAgentRuntime.moreQueue') }}
          </button>
        </div>
      </section>
    </div>

    <BaseDialog
      :show="showQuarantineDialog"
      :title="t('admin.cityAgentRuntime.quarantineTitle')"
      width="normal"
      @close="closeQuarantineDialog"
    >
      <div class="space-y-4">
        <p class="text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('admin.cityAgentRuntime.quarantineDescription') }}</p>
        <div v-if="pendingQuarantine" class="border border-gray-200 bg-gray-50 px-3 py-2 font-mono text-xs text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200">
          {{ pendingQuarantine.request_code }}
        </div>
        <div>
          <label class="input-label" for="city-agent-runtime-quarantine-reason">{{ t('admin.cityAgentRuntime.quarantineReason') }}</label>
          <Select id="city-agent-runtime-quarantine-reason" v-model="quarantineReason" :options="quarantineReasonOptions" :searchable="false" />
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" :disabled="actionSaving" @click="closeQuarantineDialog">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-primary" :disabled="actionSaving || !pendingQuarantine" @click="submitQuarantine">
            <Icon v-if="actionSaving" name="refresh" size="sm" class="animate-spin" />
            {{ t('admin.cityAgentRuntime.quarantineConfirm') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showEventsDialog"
      :title="t('admin.cityAgentRuntime.auditEvents')"
      width="wide"
      @close="closeEventsDialog"
    >
      <div class="space-y-4">
        <p class="text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('admin.cityAgentRuntime.auditEventsDescription') }}</p>
        <p v-if="eventTarget" class="font-mono text-xs text-gray-500 dark:text-dark-400">{{ eventTarget.request_code }}</p>
        <div v-if="eventsLoading && deadLetterEvents.length === 0" class="flex min-h-40 items-center justify-center">
          <Icon name="refresh" size="lg" class="animate-spin text-primary-500" />
        </div>
        <div v-else-if="eventsError" class="border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
          <div class="flex items-center justify-between gap-3">
            <span>{{ eventsError }}</span>
            <button type="button" class="btn btn-ghost btn-sm" @click="loadDeadLetterEvents()">{{ t('common.retry') }}</button>
          </div>
        </div>
        <div v-else-if="deadLetterEvents.length === 0" class="empty-state min-h-40 px-6 py-10">
          <Icon name="inbox" size="xl" class="text-gray-400 dark:text-dark-500" />
          <p class="mt-3 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.cityAgentRuntime.auditEventsEmpty') }}</p>
        </div>
        <div v-else class="overflow-x-auto border border-gray-200 dark:border-dark-700">
          <table class="w-full min-w-[620px] text-left text-sm">
            <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-900 dark:text-dark-400">
              <tr>
                <th class="px-4 py-3">{{ t('admin.cityAgentRuntime.event') }}</th>
                <th class="px-4 py-3">{{ t('admin.cityAgentRuntime.reason') }}</th>
                <th class="px-4 py-3">{{ t('admin.cityAgentRuntime.actor') }}</th>
                <th class="px-4 py-3">{{ t('admin.cityAgentRuntime.eventTime') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
              <tr v-for="event in deadLetterEvents" :key="event.event_id">
                <td class="px-4 py-3"><span class="badge badge-gray">{{ deadLetterEventLabel(event.event_type) }}</span></td>
                <td class="px-4 py-3 font-mono text-xs text-gray-700 dark:text-dark-200">{{ deadLetterReasonLabel(event.reason_code) }}</td>
                <td class="px-4 py-3 font-mono text-xs text-gray-700 dark:text-dark-200">#{{ event.actor_user_id }}</td>
                <td class="whitespace-nowrap px-4 py-3 text-xs text-gray-500 dark:text-dark-400">{{ formatDateTime(event.created_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="nextEventCursor" class="flex justify-center">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="eventsLoading" @click="loadDeadLetterEvents(true)">
            <Icon v-if="eventsLoading" name="refresh" size="sm" class="animate-spin" />
            {{ t('admin.cityAgentRuntime.moreEvents') }}
          </button>
        </div>
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="closeEventsDialog">{{ t('common.close') }}</button>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="confirmAction !== null"
      :title="confirmAction?.kind === 'release' ? t('admin.cityAgentRuntime.releaseConfirmTitle') : t('admin.cityAgentRuntime.retryConfirmTitle')"
      :message="confirmAction?.kind === 'release' ? t('admin.cityAgentRuntime.releaseConfirm') : t('admin.cityAgentRuntime.retryConfirm')"
      :confirm-text="confirmAction?.kind === 'release' ? t('admin.cityAgentRuntime.releaseAction') : t('admin.cityAgentRuntime.retryNow')"
      @confirm="submitConfirmAction"
      @cancel="confirmAction = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  CityRealtimeAgentDecisionDeadLetterEvent,
  CityRealtimeAgentDecisionWorkerHealth,
  CityRealtimeAgentDecisionQueueItem,
  CityRealtimeAgentDecisionQueueStatus,
  CityRealtimeAgentDeadLetterReason,
  CityRealtimeOperationalHealth,
  CityRealtimeOperationalWorldHealth
} from '@/api/admin/cityAgentRuntime'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import RowActionMenu, { type RowActionMenuItem } from '@/components/common/RowActionMenu.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'

type ConfirmAction = {
  kind: 'release' | 'retry'
  item: CityRealtimeAgentDecisionQueueItem
}

const { t } = useI18n()
const appStore = useAppStore()

const health = ref<CityRealtimeOperationalHealth>({ worlds: [], nodes: [] })
const healthLoading = ref(true)
const healthError = ref<string | null>(null)
const selectedWorldID = ref<number | null>(null)
const queueScope = ref<CityRealtimeAgentDecisionQueueStatus>('active')
const queueItems = ref<CityRealtimeAgentDecisionQueueItem[]>([])
const queueLoading = ref(false)
const queueError = ref<string | null>(null)
const nextQueueCursor = ref<string | null>(null)
const showQuarantineDialog = ref(false)
const pendingQuarantine = ref<CityRealtimeAgentDecisionQueueItem | null>(null)
const quarantineReason = ref<CityRealtimeAgentDeadLetterReason>('operator_review')
const actionSaving = ref(false)
const confirmAction = ref<ConfirmAction | null>(null)
const showEventsDialog = ref(false)
const eventTarget = ref<CityRealtimeAgentDecisionQueueItem | null>(null)
const deadLetterEvents = ref<CityRealtimeAgentDecisionDeadLetterEvent[]>([])
const eventsLoading = ref(false)
const eventsError = ref<string | null>(null)
const nextEventCursor = ref<number | null>(null)
let suppressQueueWatch = false

const worldOptions = computed<SelectOption[]>(() => health.value.worlds.map((world) => ({
  value: world.world_id,
  label: `${world.world_name} · #${world.world_id}`
})))

const selectedWorld = computed<CityRealtimeOperationalWorldHealth | null>(() => (
  health.value.worlds.find((world) => world.world_id === selectedWorldID.value) ?? null
))

const workerHealth = computed<CityRealtimeAgentDecisionWorkerHealth>(() => selectedWorld.value?.agent_decision_worker ?? {
  queued_requests: 0,
  leased_requests: 0,
  retry_scheduled: 0,
  quarantined_requests: 0,
  stale_quarantined_requests: 0,
  quarantine_stale_after_seconds: 0,
  open_circuit_breakers: 0
})

const staleAfterHours = computed(() => Math.max(1, Math.round(workerHealth.value.quarantine_stale_after_seconds / 3600)))

const queueScopeOptions = computed<SelectOption[]>(() => [
  { value: 'active', label: t('admin.cityAgentRuntime.active') },
  { value: 'queued', label: t('admin.cityAgentRuntime.queuedFilter') },
  { value: 'leased', label: t('admin.cityAgentRuntime.leasedFilter') },
  { value: 'terminal', label: t('admin.cityAgentRuntime.terminal') },
  { value: 'all', label: t('admin.cityAgentRuntime.all') }
])

const quarantineReasonOptions = computed<SelectOption[]>(() => [
  'operator_review',
  'provider_configuration',
  'provider_incident',
  'budget_review',
  'world_maintenance'
].map((value) => ({
  value,
  label: deadLetterReasonLabel(value as CityRealtimeAgentDeadLetterReason)
})))

const queueColumns = computed<Column[]>(() => [
  { key: 'request_code', label: t('admin.cityAgentRuntime.requestCode') },
  { key: 'agent', label: t('admin.cityAgentRuntime.agent') },
  { key: 'state', label: t('admin.cityAgentRuntime.state') },
  { key: 'attempts', label: t('admin.cityAgentRuntime.attempts') },
  { key: 'retry_at', label: t('admin.cityAgentRuntime.retryAt') },
  { key: 'quarantine', label: t('admin.cityAgentRuntime.quarantine') },
  { key: 'actions', label: t('common.actions'), class: 'w-16 text-right' }
])

function errorMessage(error: unknown, fallback: string): string {
  if (typeof error === 'object' && error !== null) {
    const response = (error as { response?: { data?: { detail?: unknown; message?: unknown } } }).response
    if (typeof response?.data?.detail === 'string' && response.data.detail.trim()) return response.data.detail
    if (typeof response?.data?.message === 'string' && response.data.message.trim()) return response.data.message
  }
  return error instanceof Error && error.message ? error.message : fallback
}

function requestStatusLabel(status: CityRealtimeAgentDecisionQueueItem['request_status']): string {
  const labels: Record<CityRealtimeAgentDecisionQueueItem['request_status'], string> = {
    queued: 'statusQueued',
    leased: 'statusLeased',
    accepted: 'statusAccepted',
    rejected: 'statusRejected',
    stale: 'statusStale',
    failed_terminal: 'statusFailedTerminal',
    cancelled: 'statusCancelled'
  }
  return t(`admin.cityAgentRuntime.${labels[status]}`)
}

function outboxStatusLabel(status: CityRealtimeAgentDecisionQueueItem['outbox_status']): string {
  const labels: Record<CityRealtimeAgentDecisionQueueItem['outbox_status'], string> = {
    queued: 'outboxQueued',
    leased: 'outboxLeased',
    succeeded: 'outboxSucceeded',
    failed: 'outboxFailed',
    cancelled: 'outboxCancelled'
  }
  return t(`admin.cityAgentRuntime.${labels[status]}`)
}

function requestStatusBadgeClass(status: CityRealtimeAgentDecisionQueueItem['request_status']): string {
  if (status === 'accepted') return 'badge-success'
  if (status === 'queued' || status === 'leased') return 'badge-warning'
  if (status === 'rejected' || status === 'failed_terminal' || status === 'stale' || status === 'cancelled') return 'badge-gray'
  return 'badge-gray'
}

function outboxStatusBadgeClass(status: CityRealtimeAgentDecisionQueueItem['outbox_status']): string {
  if (status === 'succeeded') return 'badge-success'
  if (status === 'queued' || status === 'leased') return 'badge-warning'
  return 'badge-gray'
}

function modelProfileLabel(item: CityRealtimeAgentDecisionQueueItem): string {
  if (!item.model_profile_code || !item.model_profile_version) return '—'
  return `${item.model_profile_code}@v${item.model_profile_version}`
}

function deadLetterReasonLabel(reason: CityRealtimeAgentDeadLetterReason | 'operator_release'): string {
  return t(`admin.cityAgentRuntime.reasonLabels.${reason}`)
}

function deadLetterEventLabel(event: CityRealtimeAgentDecisionDeadLetterEvent['event_type']): string {
  return t(`admin.cityAgentRuntime.eventLabels.${event}`)
}

function quarantineLabel(item: CityRealtimeAgentDecisionQueueItem): string {
  if (item.dead_letter_status === 'quarantined') return t('admin.cityAgentRuntime.quarantineActive')
  if (item.dead_letter_status === 'released') return t('admin.cityAgentRuntime.quarantineReleased')
  return t('admin.cityAgentRuntime.quarantineNone')
}

function quarantineBadgeClass(item: CityRealtimeAgentDecisionQueueItem): string {
  if (item.dead_letter_status === 'quarantined') return 'badge-warning'
  if (item.dead_letter_status === 'released') return 'badge-gray'
  return 'badge-gray'
}

function canQuarantine(item: CityRealtimeAgentDecisionQueueItem): boolean {
  return item.request_status === 'queued' && item.outbox_status === 'queued' && item.dead_letter_status !== 'quarantined'
}

function canRelease(item: CityRealtimeAgentDecisionQueueItem): boolean {
  return item.request_status === 'queued' && item.outbox_status === 'queued' && item.dead_letter_status === 'quarantined'
}

function canWakeRetry(item: CityRealtimeAgentDecisionQueueItem): boolean {
  return item.request_status === 'queued' && item.outbox_status === 'queued' && item.dead_letter_status !== 'quarantined' && Boolean(item.retry_not_before)
}

function queueActionItems(item: CityRealtimeAgentDecisionQueueItem): RowActionMenuItem[] {
  const items: RowActionMenuItem[] = [
    { key: 'events', label: t('admin.cityAgentRuntime.auditEvents'), icon: 'eye' }
  ]
  if (canQuarantine(item)) items.push({ key: 'quarantine', label: t('admin.cityAgentRuntime.quarantineAction'), icon: 'shield', tone: 'warning' })
  if (canRelease(item)) items.push({ key: 'release', label: t('admin.cityAgentRuntime.releaseAction'), icon: 'checkCircle' })
  if (canWakeRetry(item)) items.push({ key: 'retry', label: t('admin.cityAgentRuntime.retryNow'), icon: 'refresh' })
  return items
}

async function refreshRuntime(notify = false): Promise<void> {
  healthLoading.value = true
  healthError.value = null
  try {
    const loaded = await adminAPI.cityAgentRuntime.getCityRealtimeOperationalHealth()
    health.value = loaded
    suppressQueueWatch = true
    if (!loaded.worlds.some((world) => world.world_id === selectedWorldID.value)) {
      selectedWorldID.value = loaded.worlds[0]?.world_id ?? null
    }
    await nextTick()
    suppressQueueWatch = false
    if (selectedWorldID.value) await loadQueue()
    else {
      queueItems.value = []
      nextQueueCursor.value = null
    }
    if (notify) appStore.showSuccess(t('admin.cityAgentRuntime.refreshed'))
  } catch (error: unknown) {
    healthError.value = errorMessage(error, t('admin.cityAgentRuntime.refreshFailed'))
    appStore.showError(healthError.value)
  } finally {
    suppressQueueWatch = false
    healthLoading.value = false
  }
}

async function loadQueue(append = false): Promise<void> {
  const worldID = selectedWorldID.value
  const scope = queueScope.value
  if (!worldID) {
    queueItems.value = []
    nextQueueCursor.value = null
    return
  }
  if (append && !nextQueueCursor.value) return
  queueLoading.value = true
  queueError.value = null
  try {
    const page = await adminAPI.cityAgentRuntime.listCityRealtimeAgentDecisionQueue({
      worldID,
      status: scope,
      beforeCursor: append ? nextQueueCursor.value ?? undefined : undefined,
      limit: 50
    })
    if (worldID !== selectedWorldID.value || scope !== queueScope.value) return
    if (append) {
      const known = new Set(queueItems.value.map((item) => item.request_code))
      queueItems.value = [...queueItems.value, ...page.items.filter((item) => !known.has(item.request_code))]
    } else {
      queueItems.value = page.items
    }
    nextQueueCursor.value = page.next_cursor ?? null
  } catch (error: unknown) {
    queueError.value = errorMessage(error, t('admin.cityAgentRuntime.queueLoadFailed'))
  } finally {
    queueLoading.value = false
  }
}

function openQuarantineDialog(item: CityRealtimeAgentDecisionQueueItem): void {
  pendingQuarantine.value = item
  quarantineReason.value = 'operator_review'
  showQuarantineDialog.value = true
}

function closeQuarantineDialog(): void {
  if (actionSaving.value) return
  showQuarantineDialog.value = false
  pendingQuarantine.value = null
}

async function submitQuarantine(): Promise<void> {
  const item = pendingQuarantine.value
  if (!item || actionSaving.value) return
  actionSaving.value = true
  try {
    await adminAPI.cityAgentRuntime.quarantineCityRealtimeAgentDecision(item.world_id, item.request_code, quarantineReason.value)
    appStore.showSuccess(t('admin.cityAgentRuntime.quarantineSucceeded'))
    showQuarantineDialog.value = false
    pendingQuarantine.value = null
    await refreshRuntime()
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('admin.cityAgentRuntime.actionFailed')))
  } finally {
    actionSaving.value = false
  }
}

async function submitConfirmAction(): Promise<void> {
  const action = confirmAction.value
  if (!action || actionSaving.value) return
  actionSaving.value = true
  try {
    if (action.kind === 'release') {
      await adminAPI.cityAgentRuntime.releaseCityRealtimeAgentDecisionDeadLetter(action.item.world_id, action.item.request_code)
      appStore.showSuccess(t('admin.cityAgentRuntime.releaseSucceeded'))
    } else {
      await adminAPI.cityAgentRuntime.retryCityRealtimeAgentDecision(action.item.world_id, action.item.request_code)
      appStore.showSuccess(t('admin.cityAgentRuntime.retrySucceeded'))
    }
    confirmAction.value = null
    await refreshRuntime()
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('admin.cityAgentRuntime.actionFailed')))
  } finally {
    actionSaving.value = false
  }
}

function openEventsDialog(item: CityRealtimeAgentDecisionQueueItem): void {
  eventTarget.value = item
  deadLetterEvents.value = []
  nextEventCursor.value = null
  eventsError.value = null
  showEventsDialog.value = true
  void loadDeadLetterEvents()
}

function closeEventsDialog(): void {
  showEventsDialog.value = false
  eventTarget.value = null
  deadLetterEvents.value = []
  nextEventCursor.value = null
  eventsError.value = null
}

async function loadDeadLetterEvents(append = false): Promise<void> {
  const item = eventTarget.value
  if (!item || (append && !nextEventCursor.value)) return
  eventsLoading.value = true
  eventsError.value = null
  try {
    const page = await adminAPI.cityAgentRuntime.listCityRealtimeAgentDecisionDeadLetterEvents({
      worldID: item.world_id,
      requestCode: item.request_code,
      beforeEventID: append ? nextEventCursor.value ?? undefined : undefined,
      limit: 50
    })
    if (eventTarget.value?.request_code !== item.request_code) return
    if (append) {
      const known = new Set(deadLetterEvents.value.map((event) => event.event_id))
      deadLetterEvents.value = [...deadLetterEvents.value, ...page.items.filter((event) => !known.has(event.event_id))]
    } else {
      deadLetterEvents.value = page.items
    }
    nextEventCursor.value = page.next_before_event_id ?? null
  } catch (error: unknown) {
    eventsError.value = errorMessage(error, t('admin.cityAgentRuntime.auditEventsFailed'))
  } finally {
    eventsLoading.value = false
  }
}

function handleQueueAction(key: string, item: CityRealtimeAgentDecisionQueueItem): void {
  if (key === 'events') {
    openEventsDialog(item)
    return
  }
  if (key === 'quarantine') {
    openQuarantineDialog(item)
    return
  }
  if (key === 'release') {
    confirmAction.value = { kind: 'release', item }
    return
  }
  if (key === 'retry') confirmAction.value = { kind: 'retry', item }
}

watch([selectedWorldID, queueScope], () => {
  if (!suppressQueueWatch) void loadQueue()
})

onMounted(() => {
  void refreshRuntime()
})
</script>
