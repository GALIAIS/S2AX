<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-col justify-between gap-4 lg:flex-row lg:items-end">
          <div class="grid flex-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
            <div>
              <label class="input-label">{{ t('admin.accountAllocations.filters.status') }}</label>
              <Select v-model="statusFilter" :options="statusOptions" clearable @change="applyFilters" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.accountAllocations.filters.user') }}</label>
              <Select
                v-model="userFilter"
                :options="userOptions"
                :placeholder="t('admin.accountAllocations.filters.allUsers')"
                :empty-text="t('admin.accountAllocations.noUsers')"
                searchable
                clearable
                @change="applyFilters"
              />
            </div>
            <div>
              <label class="input-label">{{ t('admin.accountAllocations.filters.group') }}</label>
              <Select
                v-model="groupFilter"
                :options="groupOptions"
                :placeholder="t('admin.accountAllocations.filters.allGroups')"
                :empty-text="t('common.noGroupsAvailable')"
                searchable
                clearable
                @change="applyFilters"
              />
            </div>
          </div>
          <div class="flex shrink-0 items-center justify-end gap-3">
            <button type="button" class="btn btn-secondary" :disabled="loading" :title="t('common.refresh')" @click="loadPolicies">
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button type="button" class="btn btn-primary" @click="openCreateDialog">
              <Icon name="plus" size="md" />
              {{ t('admin.accountAllocations.create') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="policies" :loading="loading" :error="loadError" @retry="loadPolicies">
          <template #cell-user_email="{ row }">
            <div class="min-w-48">
              <p class="truncate font-medium text-gray-900 dark:text-white">{{ row.user_email }}</p>
              <p v-if="row.username" class="truncate text-xs text-gray-500 dark:text-dark-400">{{ row.username }} · #{{ row.user_id }}</p>
            </div>
          </template>

          <template #cell-group_name="{ row }">
            <div class="min-w-40">
              <p class="font-medium text-gray-900 dark:text-white">{{ row.group_name }}</p>
              <p class="font-mono text-xs text-gray-500 dark:text-dark-400">{{ row.group_platform }} · #{{ row.group_id }}</p>
            </div>
          </template>

          <template #cell-capacity="{ row }">
            <div class="min-w-28">
              <p class="font-mono font-semibold text-gray-900 dark:text-white">{{ row.active_assignment_count }} / {{ row.desired_count }}</p>
              <p v-if="row.shortage > 0" class="mt-1 text-xs text-amber-700 dark:text-amber-300">{{ t('admin.accountAllocations.shortageCount', { count: row.shortage }) }}</p>
              <p v-else class="mt-1 text-xs text-emerald-700 dark:text-emerald-300">{{ t('admin.accountAllocations.capacityMet') }}</p>
            </div>
          </template>

          <template #cell-automation="{ row }">
            <div class="space-y-1 text-xs text-gray-600 dark:text-dark-300">
              <p><span :class="['badge', row.auto_replenish ? 'badge-success' : 'badge-gray']">{{ row.auto_replenish ? t('admin.accountAllocations.autoReplenishOn') : t('admin.accountAllocations.autoReplenishOff') }}</span></p>
              <p>{{ t('admin.accountAllocations.replace401') }}: {{ row.replace_on_401 ? t('common.enabled') : t('common.disabled') }}</p>
              <p>{{ t('admin.accountAllocations.replace429') }}: {{ row.replace_on_429 ? t('common.enabled') : t('common.disabled') }}</p>
            </div>
          </template>

          <template #cell-last_reconciled_at="{ value }">
            <span class="whitespace-nowrap text-xs text-gray-500 dark:text-dark-400">{{ value ? formatDateTime(value) : t('common.time.never') }}</span>
          </template>

          <template #cell-status="{ row }">
            <span :class="['badge', row.status === 'active' ? 'badge-success' : 'badge-gray']">
              {{ row.status === 'active' ? t('common.active') : t('common.disabled') }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <RowActionMenu :items="policyActionItems(row)" :aria-label="t('admin.accountAllocations.rowActions', { email: row.user_email })" @select="(key) => handlePolicyAction(key, row)" />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <BaseDialog :show="showCreateDialog" :title="t('admin.accountAllocations.create')" width="wide" @close="showCreateDialog = false">
      <form id="create-account-allocation-policy" class="space-y-5" @submit.prevent="createPolicy">
        <div class="grid gap-4 md:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.accountAllocations.user') }}</label>
            <Select
              v-model="createForm.user_id"
              :options="userOptions"
              :placeholder="t('admin.accountAllocations.selectUser')"
              :empty-text="t('admin.accountAllocations.noUsers')"
              searchable
            />
            <p class="input-hint">{{ t('admin.accountAllocations.userHint') }}</p>
          </div>
          <div>
            <label class="input-label">{{ t('admin.accountAllocations.group') }}</label>
            <Select
              v-model="createForm.group_id"
              :options="groupOptions"
              :placeholder="t('admin.accountAllocations.selectGroup')"
              :empty-text="t('common.noGroupsAvailable')"
              searchable
            />
            <p class="input-hint">{{ t('admin.accountAllocations.groupHint') }}</p>
          </div>
        </div>

        <div>
          <label class="input-label">{{ t('admin.accountAllocations.desiredCount') }}</label>
          <input v-model.number="createForm.desired_count" type="number" min="0" :max="maxDesiredCount" class="input font-mono" required />
          <p class="input-hint">{{ t('admin.accountAllocations.desiredCountHint', { max: maxDesiredCount }) }}</p>
        </div>

        <div class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-700">
          <label class="flex cursor-pointer items-start gap-3">
            <input v-model="createForm.auto_replenish" type="checkbox" class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" />
            <span>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accountAllocations.autoReplenish') }}</span>
              <span class="mt-1 block text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.accountAllocations.autoReplenishHint') }}</span>
            </span>
          </label>
          <label class="flex cursor-pointer items-start gap-3">
            <input v-model="createForm.replace_on_401" type="checkbox" class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" />
            <span>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accountAllocations.replace401') }}</span>
              <span class="mt-1 block text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.accountAllocations.replace401Hint') }}</span>
            </span>
          </label>
          <label class="flex cursor-pointer items-start gap-3">
            <input v-model="createForm.replace_on_429" type="checkbox" class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" />
            <span>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accountAllocations.replace429') }}</span>
              <span class="mt-1 block text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.accountAllocations.replace429Hint') }}</span>
            </span>
          </label>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showCreateDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="create-account-allocation-policy" class="btn btn-primary" :disabled="creating">
            {{ creating ? t('common.submitting') : t('common.create') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog :show="showEditDialog" :title="t('admin.accountAllocations.edit')" width="normal" @close="showEditDialog = false">
      <form id="update-account-allocation-policy" class="space-y-5" @submit.prevent="updatePolicy">
        <div v-if="editingPolicy" class="rounded-lg bg-gray-50 px-4 py-3 text-sm text-gray-600 dark:bg-dark-800 dark:text-dark-300">
          {{ editingPolicy.user_email }} · {{ editingPolicy.group_name }}
        </div>
        <div>
          <label class="input-label">{{ t('admin.accountAllocations.desiredCount') }}</label>
          <input v-model.number="editForm.desired_count" type="number" min="0" :max="maxDesiredCount" class="input font-mono" required />
        </div>
        <div class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-700">
          <label class="flex cursor-pointer items-center gap-3 text-sm text-gray-800 dark:text-dark-100"><input v-model="editForm.auto_replenish" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" />{{ t('admin.accountAllocations.autoReplenish') }}</label>
          <label class="flex cursor-pointer items-center gap-3 text-sm text-gray-800 dark:text-dark-100"><input v-model="editForm.replace_on_401" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" />{{ t('admin.accountAllocations.replace401') }}</label>
          <label class="flex cursor-pointer items-center gap-3 text-sm text-gray-800 dark:text-dark-100"><input v-model="editForm.replace_on_429" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" />{{ t('admin.accountAllocations.replace429') }}</label>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showEditDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="update-account-allocation-policy" class="btn btn-primary" :disabled="updating">
            {{ updating ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog :show="showDetailsDialog" :title="selectedPolicy ? t('admin.accountAllocations.detailsTitle', { user: selectedPolicy.user_email, group: selectedPolicy.group_name }) : t('admin.accountAllocations.details')" width="extra-wide" @close="showDetailsDialog = false">
      <div v-if="selectedPolicy" class="space-y-6">
        <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accountAllocations.desiredCount') }}</p><p class="mt-1 font-mono text-lg font-semibold text-gray-900 dark:text-white">{{ selectedPolicy.desired_count }}</p></div>
          <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accountAllocations.activeAssignments') }}</p><p class="mt-1 font-mono text-lg font-semibold text-gray-900 dark:text-white">{{ selectedPolicy.active_assignment_count }}</p></div>
          <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accountAllocations.shortage') }}</p><p :class="['mt-1 font-mono text-lg font-semibold', selectedPolicy.shortage > 0 ? 'text-amber-600 dark:text-amber-400' : 'text-emerald-600 dark:text-emerald-400']">{{ selectedPolicy.shortage }}</p></div>
          <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accountAllocations.lastReconciled') }}</p><p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ selectedPolicy.last_reconciled_at ? formatDateTime(selectedPolicy.last_reconciled_at) : t('common.time.never') }}</p></div>
        </div>

        <section class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
          <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
            <div>
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.accountAllocations.manualAssignment') }}</h3>
              <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.accountAllocations.manualAssignmentHint') }}</p>
            </div>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="detailsLoading || reconcilingPolicyID === selectedPolicy.id" @click="reconcilePolicy(selectedPolicy)">
              <Icon name="refresh" size="sm" :class="reconcilingPolicyID === selectedPolicy.id ? 'animate-spin' : ''" />
              {{ t('admin.accountAllocations.reconcileNow') }}
            </button>
          </div>
          <div class="mt-4 flex flex-col gap-3 sm:flex-row">
            <Select
              v-model="selectedCandidateID"
              :options="candidateOptions"
              :placeholder="selectedPolicy.status === 'active' ? t('admin.accountAllocations.selectCandidate') : t('admin.accountAllocations.policyDisabled')"
              :empty-text="t('admin.accountAllocations.noCandidates')"
              :disabled="selectedPolicy.status !== 'active' || detailsLoading"
              searchable
            />
            <button type="button" class="btn btn-primary shrink-0" :disabled="!selectedCandidateID || assigning || selectedPolicy.status !== 'active'" @click="assignCandidate">
              <Icon name="plus" size="sm" />
              {{ assigning ? t('common.submitting') : t('admin.accountAllocations.assignManually') }}
            </button>
          </div>
        </section>

        <section>
          <div class="mb-3 flex items-center justify-between gap-3"><h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.accountAllocations.assignments') }}</h3><span class="text-xs text-gray-500 dark:text-dark-400">{{ assignments.length }}</span></div>
          <div v-if="detailsLoading" class="flex items-center justify-center py-10"><Icon name="refresh" size="lg" class="animate-spin text-gray-400" /></div>
          <div v-else-if="assignments.length === 0" class="empty-state rounded-lg border border-dashed border-gray-200 py-10 dark:border-dark-700"><Icon name="inbox" size="lg" class="text-gray-400 dark:text-dark-500" /><p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.accountAllocations.noAssignments') }}</p></div>
          <div v-else class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
            <table class="w-full min-w-[900px] text-left text-sm">
              <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-dark-400"><tr><th class="px-3 py-3">{{ t('admin.accountAllocations.account') }}</th><th class="px-3 py-3">{{ t('admin.accountAllocations.capacity') }}</th><th class="px-3 py-3">{{ t('common.status') }}</th><th class="px-3 py-3">{{ t('admin.accountAllocations.assignedAt') }}</th><th class="px-3 py-3">{{ t('admin.accountAllocations.releaseReason') }}</th><th class="px-3 py-3 text-right">{{ t('common.actions') }}</th></tr></thead>
              <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
                <tr v-for="assignment in assignments" :key="assignment.id" class="text-gray-700 dark:text-dark-200">
                  <td class="px-3 py-3">
                    <p class="font-medium">{{ assignment.account_id > 0 ? assignment.account_name : t('admin.accountAllocations.removedAccount') }}</p>
                    <p v-if="assignment.account_id > 0" class="font-mono text-xs text-gray-500 dark:text-dark-400">{{ assignment.platform }} · {{ assignment.account_type }} · #{{ assignment.account_id }}</p>
                    <p v-else class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.accountAllocations.removedAccountHint') }}</p>
                  </td>
                  <td class="px-3 py-3 font-mono">{{ assignment.concurrency }}</td>
                  <td class="px-3 py-3"><span :class="['badge', assignment.status === 'active' && assignment.schedulable ? 'badge-success' : 'badge-gray']">{{ assignment.status === 'active' && assignment.schedulable ? t('admin.accountAllocations.assignmentActive') : t('admin.accountAllocations.assignmentReleased') }}</span></td>
                  <td class="px-3 py-3 text-xs text-gray-500 dark:text-dark-400">{{ formatDateTime(assignment.assigned_at) }}</td>
                  <td class="px-3 py-3 text-xs text-gray-500 dark:text-dark-400">{{ assignment.release_reason || '—' }}</td>
                  <td class="px-3 py-3 text-right"><button v-if="assignment.status === 'active'" type="button" class="btn btn-ghost btn-sm text-red-600 dark:text-red-400" @click="confirmRelease(assignment)">{{ t('admin.accountAllocations.release') }}</button></td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section>
          <div class="mb-3 flex items-center justify-between gap-3"><h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.accountAllocations.eventHistory') }}</h3><span class="text-xs text-gray-500 dark:text-dark-400">{{ eventsTotal }}</span></div>
          <div v-if="detailsLoading" class="h-24 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800" />
          <div v-else-if="events.length === 0" class="rounded-lg border border-dashed border-gray-200 px-4 py-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-400">{{ t('admin.accountAllocations.noEvents') }}</div>
          <ol v-else class="divide-y divide-gray-200 rounded-lg border border-gray-200 dark:divide-dark-700 dark:border-dark-700">
            <li v-for="event in events" :key="event.id" class="flex flex-wrap items-center justify-between gap-2 px-4 py-3"><div><p class="font-mono text-sm font-medium text-gray-900 dark:text-white">{{ event.event_type }}</p><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ event.assignment_id ? `#${event.assignment_id}` : '—' }} · {{ event.actor_user_id ? `#${event.actor_user_id}` : t('admin.accountAllocations.systemActor') }}</p></div><time class="text-xs text-gray-500 dark:text-dark-400">{{ formatDateTime(event.created_at) }}</time></li>
          </ol>
        </section>
      </div>
    </BaseDialog>

    <ConfirmDialog
      :show="Boolean(statusTarget)"
      :title="statusTarget?.status === 'active' ? t('admin.accountAllocations.disable') : t('admin.accountAllocations.enable')"
      :message="statusTarget?.status === 'active' ? t('admin.accountAllocations.disableConfirm') : t('admin.accountAllocations.enableConfirm')"
      :confirm-text="statusTarget?.status === 'active' ? t('common.disable') : t('common.enable')"
      :danger="statusTarget?.status === 'active'"
      @confirm="togglePolicyStatus"
      @cancel="statusTarget = null"
    />

    <ConfirmDialog
      :show="Boolean(releaseTarget)"
      :title="t('admin.accountAllocations.release')"
      :message="t('admin.accountAllocations.releaseConfirm')"
      :confirm-text="t('admin.accountAllocations.release')"
      danger
      @confirm="releaseAssignment"
      @cancel="releaseTarget = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  AccountAllocationAssignment,
  AccountAllocationCandidate,
  AccountAllocationEvent,
  AccountAllocationPolicy,
  AccountAllocationPolicyStatus
} from '@/api/admin/accountAllocations'
import type { AdminGroup, AdminUser } from '@/types'
import type { Column } from '@/components/common/types'
import type { RowActionMenuItem } from '@/components/common/RowActionMenu.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import RowActionMenu from '@/components/common/RowActionMenu.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()

const policies = ref<AccountAllocationPolicy[]>([])
const loading = ref(true)
const loadError = ref<string | null>(null)
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const statusFilter = ref<AccountAllocationPolicyStatus | null>(null)
const userFilter = ref<number | null>(null)
const groupFilter = ref<number | null>(null)
const users = ref<AdminUser[]>([])
const groups = ref<AdminGroup[]>([])
const referencesLoading = ref(false)
let listController: AbortController | null = null
let listRequestID = 0

const showCreateDialog = ref(false)
const showEditDialog = ref(false)
const showDetailsDialog = ref(false)
const creating = ref(false)
const updating = ref(false)
const assigning = ref(false)
const detailsLoading = ref(false)
const reconcilingPolicyID = ref<number | null>(null)
const editingPolicy = ref<AccountAllocationPolicy | null>(null)
const selectedPolicy = ref<AccountAllocationPolicy | null>(null)
const statusTarget = ref<AccountAllocationPolicy | null>(null)
const releaseTarget = ref<AccountAllocationAssignment | null>(null)
const assignments = ref<AccountAllocationAssignment[]>([])
const candidates = ref<AccountAllocationCandidate[]>([])
const events = ref<AccountAllocationEvent[]>([])
const eventsTotal = ref(0)
const selectedCandidateID = ref<number | null>(null)
const maxDesiredCount = ref(50)

const createForm = reactive({
  user_id: null as number | null,
  group_id: null as number | null,
  desired_count: 1,
  auto_replenish: true,
  replace_on_401: true,
  replace_on_429: true
})

const editForm = reactive({
  desired_count: 0,
  auto_replenish: false,
  replace_on_401: true,
  replace_on_429: true
})

const columns = computed<Column[]>(() => [
  { key: 'user_email', label: t('admin.accountAllocations.columns.user') },
  { key: 'group_name', label: t('admin.accountAllocations.columns.group') },
  { key: 'capacity', label: t('admin.accountAllocations.columns.capacity') },
  { key: 'automation', label: t('admin.accountAllocations.columns.automation') },
  { key: 'last_reconciled_at', label: t('admin.accountAllocations.columns.lastReconciled') },
  { key: 'status', label: t('common.status') },
  { key: 'actions', label: t('common.actions') }
])

const statusOptions = computed(() => [
  { value: 'active', label: t('common.active') },
  { value: 'disabled', label: t('common.disabled') }
])

const userOptions = computed(() => users.value.map((user) => ({
  value: user.id,
  label: `${user.username ? `${user.username} · ` : ''}${user.email} (#${user.id})`,
  description: `${user.username || ''} ${user.email}`.trim()
})))

const groupOptions = computed(() => groups.value.map((group) => ({
  value: group.id,
  label: `${group.name} · ${group.platform} (#${group.id})`,
  description: `${group.platform} #${group.id}`
})))

const candidateOptions = computed(() => candidates.value.map((candidate) => ({
  value: candidate.account_id,
  label: `${candidate.account_name} · ${candidate.platform} / ${candidate.account_type}`,
  description: `${t('admin.accountAllocations.capacity')}: ${candidate.concurrency} · P${candidate.priority}`
})))

const policyActionItems = (policy: AccountAllocationPolicy): RowActionMenuItem[] => [
  { key: 'details', label: t('admin.accountAllocations.details'), icon: 'eye' },
  { key: 'reconcile', label: t('admin.accountAllocations.reconcileNow'), icon: 'refresh', disabled: policy.status !== 'active' },
  { key: 'edit', label: t('common.edit'), icon: 'edit' },
  {
    key: 'toggle',
    label: policy.status === 'active' ? t('admin.accountAllocations.disable') : t('admin.accountAllocations.enable'),
    icon: 'checkCircle',
    tone: policy.status === 'active' ? 'warning' : 'default'
  }
]

const handlePolicyAction = (key: string, policy: AccountAllocationPolicy) => {
  if (key === 'details') void openDetails(policy)
  else if (key === 'reconcile') void reconcilePolicy(policy)
  else if (key === 'edit') openEditDialog(policy)
  else if (key === 'toggle') statusTarget.value = policy
}

const loadReferences = async () => {
  if (referencesLoading.value || (users.value.length > 0 && groups.value.length > 0)) return
  referencesLoading.value = true
  try {
    const [userResponse, groupList] = await Promise.all([
      adminAPI.users.list(1, 200, { status: 'active', sort_by: 'email', sort_order: 'asc' }),
      adminAPI.groups.getAll()
    ])
    users.value = userResponse.items
    groups.value = groupList
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountAllocations.referencesLoadFailed')))
  } finally {
    referencesLoading.value = false
  }
}

const loadCapabilities = async () => {
  try {
    const capabilities = await adminAPI.accountAllocations.getCapabilities()
    if (Number.isInteger(capabilities.max_desired_count) && capabilities.max_desired_count > 0) {
      maxDesiredCount.value = capabilities.max_desired_count
    }
  } catch {
    // Keep the server default as a safe UI fallback. Create/update remain
    // validated by the backend when a deployment has a custom limit.
  }
}

const loadPolicies = async () => {
  listController?.abort()
  const controller = new AbortController()
  listController = controller
  const currentRequestID = ++listRequestID
  loading.value = true
  loadError.value = null
  try {
    const result = await adminAPI.accountAllocations.list(
      pagination.page,
      pagination.page_size,
      {
        status: statusFilter.value ?? undefined,
        user_id: userFilter.value ?? undefined,
        group_id: groupFilter.value ?? undefined
      },
      { signal: controller.signal }
    )
    if (currentRequestID === listRequestID) {
      policies.value = result.items
      pagination.total = result.total
    }
  } catch (error: unknown) {
    if (currentRequestID === listRequestID && !controller.signal.aborted) {
      loadError.value = extractApiErrorMessage(error, t('admin.accountAllocations.loadFailed'))
    }
  } finally {
    if (currentRequestID === listRequestID) loading.value = false
    if (listController === controller) listController = null
  }
}

const applyFilters = () => {
  pagination.page = 1
  void loadPolicies()
}

const handlePageChange = (page: number) => {
  pagination.page = page
  void loadPolicies()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize
  pagination.page = 1
  void loadPolicies()
}

const resetCreateForm = () => {
  Object.assign(createForm, {
    user_id: null,
    group_id: null,
    desired_count: 1,
    auto_replenish: true,
    replace_on_401: true,
    replace_on_429: true
  })
}

const openCreateDialog = () => {
  resetCreateForm()
  showCreateDialog.value = true
  void loadReferences()
}

const createPolicy = async () => {
  if (!createForm.user_id || !createForm.group_id) {
    appStore.showError(t('admin.accountAllocations.formRequired'))
    return
  }
  creating.value = true
  try {
    const policy = await adminAPI.accountAllocations.create({ ...createForm, user_id: createForm.user_id, group_id: createForm.group_id })
    showCreateDialog.value = false
    appStore.showSuccess(t('admin.accountAllocations.created'))
    pagination.page = 1
    await loadPolicies()
    await openDetails(policy)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountAllocations.createFailed')))
  } finally {
    creating.value = false
  }
}

const openEditDialog = (policy: AccountAllocationPolicy) => {
  editingPolicy.value = policy
  Object.assign(editForm, {
    desired_count: policy.desired_count,
    auto_replenish: policy.auto_replenish,
    replace_on_401: policy.replace_on_401,
    replace_on_429: policy.replace_on_429
  })
  showEditDialog.value = true
}

const replacePolicyInView = (updated: AccountAllocationPolicy) => {
  policies.value = policies.value.map((policy) => policy.id === updated.id ? updated : policy)
  if (selectedPolicy.value?.id === updated.id) selectedPolicy.value = updated
  if (editingPolicy.value?.id === updated.id) editingPolicy.value = updated
}

const updatePolicy = async () => {
  if (!editingPolicy.value) return
  updating.value = true
  try {
    const updated = await adminAPI.accountAllocations.update(editingPolicy.value.id, { ...editForm })
    replacePolicyInView(updated)
    showEditDialog.value = false
    appStore.showSuccess(t('admin.accountAllocations.updated'))
    await loadPolicies()
    if (selectedPolicy.value?.id === updated.id) await loadDetails(updated)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountAllocations.updateFailed')))
  } finally {
    updating.value = false
  }
}

const openDetails = async (policy: AccountAllocationPolicy) => {
  selectedPolicy.value = policy
  selectedCandidateID.value = null
  showDetailsDialog.value = true
  await loadDetails(policy)
}

const loadDetails = async (policy = selectedPolicy.value) => {
  if (!policy) return
  const selectedID = policy.id
  detailsLoading.value = true
  try {
    const [policyDetails, assignmentRows, eventResponse, candidateRows] = await Promise.all([
      adminAPI.accountAllocations.getById(selectedID),
      adminAPI.accountAllocations.listAssignments(selectedID),
      adminAPI.accountAllocations.listEvents(selectedID),
      policy.status === 'active' ? adminAPI.accountAllocations.listCandidates(selectedID) : Promise.resolve([])
    ])
    if (selectedPolicy.value?.id === selectedID) {
      selectedPolicy.value = policyDetails
      replacePolicyInView(policyDetails)
      assignments.value = assignmentRows
      events.value = eventResponse.items
      eventsTotal.value = eventResponse.total
      candidates.value = candidateRows
      selectedCandidateID.value = null
    }
  } catch (error: unknown) {
    if (selectedPolicy.value?.id === selectedID) {
      appStore.showError(extractApiErrorMessage(error, t('admin.accountAllocations.detailsLoadFailed')))
    }
  } finally {
    if (selectedPolicy.value?.id === selectedID) detailsLoading.value = false
  }
}

const reconcilePolicy = async (policy: AccountAllocationPolicy) => {
  reconcilingPolicyID.value = policy.id
  try {
    const result = await adminAPI.accountAllocations.reconcile(policy.id)
    appStore.showSuccess(t('admin.accountAllocations.reconciled', { assigned: result.assigned_count, released: result.released_count, shortage: result.shortage }))
    await loadPolicies()
    if (selectedPolicy.value?.id === policy.id) {
      const updated = await adminAPI.accountAllocations.getById(policy.id)
      await loadDetails(updated)
    }
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountAllocations.reconcileFailed')))
  } finally {
    if (reconcilingPolicyID.value === policy.id) reconcilingPolicyID.value = null
  }
}

const assignCandidate = async () => {
  if (!selectedPolicy.value || !selectedCandidateID.value) return
  assigning.value = true
  try {
    await adminAPI.accountAllocations.assign(selectedPolicy.value.id, selectedCandidateID.value)
    appStore.showSuccess(t('admin.accountAllocations.assigned'))
    await loadPolicies()
    await loadDetails(selectedPolicy.value)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountAllocations.assignFailed')))
  } finally {
    assigning.value = false
  }
}

const confirmRelease = (assignment: AccountAllocationAssignment) => {
  releaseTarget.value = assignment
}

const releaseAssignment = async () => {
  if (!selectedPolicy.value || !releaseTarget.value) return
  const assignment = releaseTarget.value
  releaseTarget.value = null
  try {
    await adminAPI.accountAllocations.release(selectedPolicy.value.id, assignment.id)
    appStore.showSuccess(t('admin.accountAllocations.released'))
    await loadPolicies()
    await loadDetails(selectedPolicy.value)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountAllocations.releaseFailed')))
  }
}

const togglePolicyStatus = async () => {
  if (!statusTarget.value) return
  const target = statusTarget.value
  statusTarget.value = null
  const enabled = target.status !== 'active'
  try {
    const updated = await adminAPI.accountAllocations.setStatus(target.id, enabled)
    replacePolicyInView(updated)
    appStore.showSuccess(enabled ? t('admin.accountAllocations.enabled') : t('admin.accountAllocations.disabled'))
    await loadPolicies()
    if (selectedPolicy.value?.id === updated.id) await loadDetails(updated)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountAllocations.statusFailed')))
  }
}

onMounted(() => {
  void loadCapabilities()
  void loadReferences()
  void loadPolicies()
})

onUnmounted(() => {
  listRequestID += 1
  listController?.abort()
})
</script>
