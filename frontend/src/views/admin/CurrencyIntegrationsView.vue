<template>
  <AppLayout>
    <TablePageLayout>
      <template #actions>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex flex-wrap items-center gap-2">
            <span class="badge badge-primary">{{ t('admin.currencyIntegration.hmacLabel') }}</span>
            <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.currencyIntegration.secretNotice') }}</span>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <button type="button" class="btn btn-secondary" :disabled="loading" @click="loadIntegrations">
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
              <span class="hidden sm:inline">{{ t('common.refresh') }}</span>
            </button>
            <button type="button" class="btn btn-primary" @click="openCreateDialog">
              <Icon name="plus" size="md" />
              {{ t('admin.currencyIntegration.create') }}
            </button>
          </div>
        </div>
      </template>

      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="min-w-56 flex-1 sm:max-w-sm">
            <input
              v-model="searchQuery"
              type="search"
              class="input"
              :placeholder="t('admin.currencyIntegration.searchPlaceholder')"
            />
          </div>
          <Select v-model="statusFilter" class="w-44" :options="statusOptions" />
          <label class="flex items-center gap-2 text-sm text-gray-600 dark:text-dark-300">
            <input v-model="includeDisabled" type="checkbox" class="input-checkbox" @change="loadIntegrations" />
            {{ t('admin.currencyIntegration.showDisabled') }}
          </label>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="filteredIntegrations"
          :loading="loading"
          :error="loadError"
          :aria-label="t('admin.currencyIntegration.title')"
          @retry="loadIntegrations"
        >
          <template #empty>
            <div class="flex flex-col items-center gap-3 py-8">
              <Icon name="link" size="xl" class="text-gray-400 dark:text-dark-500" />
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('admin.currencyIntegration.empty') }}</p>
              <button type="button" class="btn btn-secondary btn-sm" @click="openCreateDialog">
                <Icon name="plus" size="sm" />
                {{ t('admin.currencyIntegration.create') }}
              </button>
            </div>
          </template>

          <template #cell-code="{ row }">
            <div class="min-w-44">
              <div class="flex items-center gap-2">
                <code class="font-mono text-sm font-semibold text-gray-900 dark:text-gray-100">{{ row.code }}</code>
                <span :class="['badge', row.status === 'active' ? 'badge-success' : 'badge-gray']">
                  {{ statusLabel(row.status) }}
                </span>
              </div>
              <p class="mt-1 max-w-xs truncate text-xs text-gray-500 dark:text-dark-400" :title="row.name">{{ row.name }}</p>
            </div>
          </template>

          <template #cell-secret_hint="{ row }">
            <code class="font-mono text-xs text-gray-600 dark:text-dark-300">••••{{ row.secret_hint }}</code>
          </template>

          <template #cell-scopes="{ row }">
            <button type="button" class="link-button" @click="openScopesDialog(row)">
              {{ scopeCounts[row.id] ?? '—' }}
            </button>
          </template>

          <template #cell-metadata="{ row }">
            <span class="block max-w-xs truncate font-mono text-xs text-gray-500 dark:text-dark-400" :title="metadataPreview(row.metadata)">
              {{ metadataPreview(row.metadata) }}
            </span>
          </template>

          <template #cell-updated_at="{ row }">
            <span class="whitespace-nowrap text-xs text-gray-500 dark:text-dark-400">{{ formatDateTime(row.updated_at) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <RowActionMenu
              :items="integrationActionItems(row)"
              :aria-label="t('admin.currencyIntegration.rowActions', { code: row.code })"
              @select="(key) => handleIntegrationAction(key, row)"
            />
          </template>
        </DataTable>
      </template>
    </TablePageLayout>

    <BaseDialog
      :show="showCreateDialog"
      :title="t('admin.currencyIntegration.create')"
      width="wide"
      @close="showCreateDialog = false"
    >
      <form id="create-currency-integration-form" class="grid gap-4 md:grid-cols-2" @submit.prevent="handleCreate">
        <div>
          <label class="input-label">{{ t('admin.currencyIntegration.code') }}</label>
          <input v-model="createForm.code" class="input font-mono" maxlength="64" required placeholder="game-rewards" />
          <p class="input-hint">{{ t('admin.currencyIntegration.codeHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.currencyIntegration.name') }}</label>
          <input v-model="createForm.name" class="input" maxlength="128" required :placeholder="t('admin.currencyIntegration.namePlaceholder')" />
        </div>
        <div class="md:col-span-2">
          <label class="input-label">{{ t('admin.currencyIntegration.metadata') }}</label>
          <textarea v-model="createForm.metadata" class="input min-h-24 font-mono text-xs" rows="4" spellcheck="false" />
          <p class="input-hint">{{ t('admin.currencyIntegration.metadataHint') }}</p>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showCreateDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="create-currency-integration-form" class="btn btn-primary" :disabled="saving">
            {{ saving ? t('common.saving') : t('common.create') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showEditDialog"
      :title="t('admin.currencyIntegration.edit')"
      width="wide"
      @close="showEditDialog = false"
    >
      <form id="edit-currency-integration-form" class="grid gap-4 md:grid-cols-2" @submit.prevent="handleUpdate">
        <div>
          <label class="input-label">{{ t('admin.currencyIntegration.code') }}</label>
          <input :value="editingIntegration?.code" class="input font-mono opacity-60" disabled />
          <p class="input-hint">{{ t('admin.currencyIntegration.codeImmutable') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.currencyIntegration.name') }}</label>
          <input v-model="editForm.name" class="input" maxlength="128" required />
        </div>
        <div class="md:col-span-2">
          <label class="input-label">{{ t('admin.currencyIntegration.metadata') }}</label>
          <textarea v-model="editForm.metadata" class="input min-h-24 font-mono text-xs" rows="4" spellcheck="false" />
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showEditDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="edit-currency-integration-form" class="btn btn-primary" :disabled="saving">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showSecretDialog"
      :title="t('admin.currencyIntegration.secretTitle')"
      width="wide"
      @close="closeSecretDialog"
    >
      <div v-if="secretResult" class="space-y-4">
        <div class="flex items-start gap-3 border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
          <Icon name="exclamationTriangle" size="md" class="mt-0.5 shrink-0" />
          <p>{{ t('admin.currencyIntegration.secretOneTimeNotice') }}</p>
        </div>
        <div>
          <label class="input-label">{{ secretResult.integration.code }}</label>
          <div class="flex gap-2">
            <input :value="secretResult.secret" readonly class="input flex-1 font-mono text-xs" @focus="selectInput" />
            <button type="button" class="btn btn-secondary shrink-0" @click="copySecret">
              <Icon name="copy" size="sm" />
              {{ t('admin.currencyIntegration.copySecret') }}
            </button>
          </div>
        </div>
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.currencyIntegration.secretFormatHint') }}</p>
      </div>
      <template #footer>
        <div class="flex justify-end">
          <button type="button" class="btn btn-primary" @click="closeSecretDialog">{{ t('common.close') }}</button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showScopesDialog"
      :title="t('admin.currencyIntegration.scopesTitle', { code: selectedIntegration?.code || '' })"
      width="extra-wide"
      @close="showScopesDialog = false"
    >
      <div class="grid gap-6 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
        <form class="space-y-4" @submit.prevent="handleSaveScope">
          <div>
            <label class="input-label">{{ t('admin.currencyIntegration.currency') }}</label>
            <Select v-model="scopeForm.currency_id" :options="currencyOptions" :placeholder="t('admin.currencyIntegration.selectCurrency')" searchable />
          </div>
          <div>
            <label class="input-label">{{ t('admin.currencyIntegration.group') }}</label>
            <Select v-model="scopeForm.group_id" :options="groupOptions" :placeholder="t('admin.currencyIntegration.selectGroup')" searchable />
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="scopeForm.enabled" type="checkbox" class="input-checkbox" />
              {{ t('common.enabled') }}
            </label>
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="scopeForm.can_earn" type="checkbox" class="input-checkbox" />
              {{ t('admin.currencyIntegration.canEarn') }}
            </label>
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="scopeForm.can_spend" type="checkbox" class="input-checkbox" />
              {{ t('admin.currencyIntegration.canSpend') }}
            </label>
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="scopeForm.can_settle" type="checkbox" class="input-checkbox" />
              {{ t('admin.currencyIntegration.canSettle') }}
            </label>
          </div>
          <div>
            <label class="input-label">{{ t('admin.currencyIntegration.metadata') }}</label>
            <textarea v-model="scopeForm.metadata" class="input font-mono text-xs" rows="3" spellcheck="false" />
          </div>
          <div class="flex justify-end gap-2">
            <button type="button" class="btn btn-secondary" @click="resetScopeForm">{{ t('common.reset') }}</button>
            <button type="submit" class="btn btn-primary" :disabled="scopeSaving || !scopeForm.currency_id || !scopeForm.group_id">
              {{ scopeSaving ? t('common.saving') : t('admin.currencyIntegration.saveScope') }}
            </button>
          </div>
        </form>

        <div class="min-w-0">
          <div v-if="scopesLoading" class="flex items-center justify-center py-12">
            <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
          </div>
          <div v-else-if="scopes.length === 0" class="empty-state py-12">
            <Icon name="shield" size="xl" class="text-gray-400 dark:text-dark-500" />
            <p class="mt-3 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.currencyIntegration.noScopes') }}</p>
          </div>
          <div v-else class="overflow-x-auto border border-gray-200 dark:border-dark-700">
            <table class="w-full min-w-[680px] text-left text-sm">
              <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-dark-400">
                <tr>
                  <th class="px-3 py-3">{{ t('admin.currencyIntegration.currency') }}</th>
                  <th class="px-3 py-3">{{ t('admin.currencyIntegration.group') }}</th>
                  <th class="px-3 py-3">{{ t('admin.currencyIntegration.permissions') }}</th>
                  <th class="px-3 py-3">{{ t('common.status') }}</th>
                  <th class="px-3 py-3 text-right">{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
                <tr v-for="scope in scopes" :key="scope.id" class="text-gray-700 dark:text-dark-200">
                  <td class="px-3 py-3">
                    <p class="font-medium">{{ currencyLabel(scope.currency_id) }}</p>
                    <p class="font-mono text-xs text-gray-500 dark:text-dark-400">#{{ scope.currency_id }}</p>
                  </td>
                  <td class="px-3 py-3">
                    <p class="font-medium">{{ groupLabel(scope.group_id) }}</p>
                    <p class="font-mono text-xs text-gray-500 dark:text-dark-400">#{{ scope.group_id }}</p>
                  </td>
                  <td class="px-3 py-3 text-xs text-gray-500 dark:text-dark-400">
                    <span v-if="scope.can_earn">{{ t('admin.currencyIntegration.earnShort') }}</span>
                    <span v-if="scope.can_earn && scope.can_spend"> · </span>
                    <span v-if="scope.can_spend">{{ t('admin.currencyIntegration.spendShort') }}</span>
                    <span v-if="scope.can_settle"> · {{ t('admin.currencyIntegration.settleShort') }}</span>
                  </td>
                  <td class="px-3 py-3">
                    <span :class="['badge', scope.enabled ? 'badge-success' : 'badge-gray']">
                      {{ scope.enabled ? t('common.enabled') : t('common.disabled') }}
                    </span>
                  </td>
                  <td class="px-3 py-3 text-right">
                    <div class="flex justify-end gap-2">
                      <button type="button" class="btn btn-ghost btn-sm" @click="editScope(scope)">{{ t('common.edit') }}</button>
                      <button type="button" class="btn btn-ghost btn-sm text-red-600 dark:text-red-400" @click="confirmDeleteScope(scope)">{{ t('common.delete') }}</button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </BaseDialog>

    <ConfirmDialog
      :show="showRotateDialog"
      :title="t('admin.currencyIntegration.rotateTitle')"
      :message="t('admin.currencyIntegration.rotateMessage', { code: rotateTarget?.code || '' })"
      :danger="true"
      :confirm-text="t('admin.currencyIntegration.rotateConfirm')"
      @confirm="handleRotateSecret"
      @cancel="showRotateDialog = false"
    />

    <ConfirmDialog
      :show="showStatusDialog"
      :title="t('admin.currencyIntegration.statusTitle')"
      :message="t('admin.currencyIntegration.statusMessage', { action: statusTarget?.status === 'active' ? t('admin.currencyIntegration.disable') : t('admin.currencyIntegration.enable'), code: statusTarget?.code || '' })"
      :danger="statusTarget?.status === 'active'"
      @confirm="handleSetStatus"
      @cancel="showStatusDialog = false"
    />

    <ConfirmDialog
      :show="showDeleteScopeDialog"
      :title="t('admin.currencyIntegration.deleteScopeTitle')"
      :message="t('admin.currencyIntegration.deleteScopeMessage', { currency: currencyLabel(scopeToDelete?.currency_id || 0), group: groupLabel(scopeToDelete?.group_id || 0) })"
      :danger="true"
      @confirm="handleDeleteScope"
      @cancel="showDeleteScopeDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  VirtualCurrencyIntegration,
  VirtualCurrencyIntegrationScope,
  VirtualCurrencyIntegrationSecretResult
} from '@/api/admin/virtualCurrencyIntegrations'
import type { VirtualCurrency } from '@/api/admin/virtualCurrencies'
import type { AdminGroup } from '@/types'
import type { Column } from '@/components/common/types'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { formatDateTime } from '@/utils/format'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import RowActionMenu, { type RowActionMenuItem } from '@/components/common/RowActionMenu.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const integrations = ref<VirtualCurrencyIntegration[]>([])
const currencies = ref<VirtualCurrency[]>([])
const groups = ref<AdminGroup[]>([])
const scopes = ref<VirtualCurrencyIntegrationScope[]>([])
const scopeCounts = ref<Record<number, number>>({})
const loading = ref(true)
const loadError = ref<string | null>(null)
const scopesLoading = ref(false)
const scopeSaving = ref(false)
const saving = ref(false)
const includeDisabled = ref(true)
const searchQuery = ref('')
const statusFilter = ref('')

const showCreateDialog = ref(false)
const showEditDialog = ref(false)
const showSecretDialog = ref(false)
const showScopesDialog = ref(false)
const showRotateDialog = ref(false)
const showStatusDialog = ref(false)
const showDeleteScopeDialog = ref(false)

const editingIntegration = ref<VirtualCurrencyIntegration | null>(null)
const selectedIntegration = ref<VirtualCurrencyIntegration | null>(null)
const rotateTarget = ref<VirtualCurrencyIntegration | null>(null)
const statusTarget = ref<VirtualCurrencyIntegration | null>(null)
const secretResult = ref<VirtualCurrencyIntegrationSecretResult | null>(null)
const scopeToDelete = ref<VirtualCurrencyIntegrationScope | null>(null)

const createForm = reactive({ code: '', name: '', metadata: '{}' })
const editForm = reactive({ name: '', metadata: '{}' })
const scopeForm = reactive({
  currency_id: null as number | null,
  group_id: null as number | null,
  enabled: true,
  can_earn: true,
  can_spend: true,
  can_settle: true,
  metadata: '{}'
})

const columns = computed<Column[]>(() => [
  { key: 'code', label: t('admin.currencyIntegration.columns.integration'), sortable: true },
  { key: 'secret_hint', label: t('admin.currencyIntegration.columns.secret'), sortable: false },
  { key: 'scopes', label: t('admin.currencyIntegration.columns.scopes'), sortable: false },
  { key: 'metadata', label: t('admin.currencyIntegration.columns.metadata'), sortable: false },
  { key: 'updated_at', label: t('admin.currencyIntegration.columns.updatedAt'), sortable: true },
  { key: 'actions', label: t('admin.currencyIntegration.columns.actions'), sortable: false, class: 'w-16' }
])

const statusOptions = computed(() => [
  { value: '', label: t('admin.currencyIntegration.allStatuses') },
  { value: 'active', label: t('common.active') },
  { value: 'disabled', label: t('common.disabled') }
])

const filteredIntegrations = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return integrations.value.filter((item) => {
    const matchesStatus = !statusFilter.value || item.status === statusFilter.value
    const matchesSearch = !query || `${item.code} ${item.name}`.toLowerCase().includes(query)
    return matchesStatus && matchesSearch
  })
})

const currencyOptions = computed(() => currencies.value
  .filter((currency) => currency.status === 'active')
  .map((currency) => ({ value: currency.id, label: `${currency.symbol || '¤'} ${currency.name} (${currency.code})` })))

const groupOptions = computed(() => groups.value
  .filter((group) => group.status === 'active')
  .map((group) => ({ value: group.id, label: `${group.name} · ${group.platform}` })))

const statusLabel = (status: string) => status === 'active' ? t('common.active') : t('common.disabled')

const currencyLabel = (id: number) => {
  const currency = currencies.value.find((item) => item.id === id)
  return currency ? `${currency.symbol || '¤'} ${currency.name} (${currency.code})` : `#${id}`
}

const groupLabel = (id: number) => groups.value.find((group) => group.id === id)?.name || `#${id}`

const metadataPreview = (metadata: Record<string, unknown>) => {
  const keys = Object.keys(metadata || {})
  return keys.length === 0 ? '—' : `{ ${keys.slice(0, 3).join(', ')}${keys.length > 3 ? ', …' : ''} }`
}

const integrationActionItems = (item: VirtualCurrencyIntegration): RowActionMenuItem[] => [
  { key: 'scopes', label: t('admin.currencyIntegration.manageScopes'), icon: 'shield' },
  { key: 'edit', label: t('common.edit'), icon: 'edit' },
  { key: 'rotate', label: t('admin.currencyIntegration.rotateSecret'), icon: 'refresh', tone: 'warning' },
  item.status === 'active'
    ? { key: 'disable', label: t('admin.currencyIntegration.disable'), icon: 'shield', tone: 'danger' }
    : { key: 'enable', label: t('admin.currencyIntegration.enable'), icon: 'checkCircle' }
]

const handleIntegrationAction = (key: string, item: VirtualCurrencyIntegration) => {
  if (key === 'scopes') void openScopesDialog(item)
  else if (key === 'edit') openEditDialog(item)
  else if (key === 'rotate') {
    rotateTarget.value = item
    showRotateDialog.value = true
  } else if (key === 'disable' || key === 'enable') {
    statusTarget.value = item
    showStatusDialog.value = true
  }
}

function parseMetadata(value: string): Record<string, unknown> | null {
  try {
    const parsed: unknown = JSON.parse(value || '{}')
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null
    return parsed as Record<string, unknown>
  } catch {
    return null
  }
}

async function loadIntegrations() {
  loading.value = true
  loadError.value = null
  try {
    const [items, currencyItems, groupItems] = await Promise.all([
      adminAPI.virtualCurrencyIntegrations.list(includeDisabled.value),
      adminAPI.virtualCurrencies.list(false),
      adminAPI.groups.getAll()
    ])
    integrations.value = items
    currencies.value = currencyItems
    groups.value = groupItems

    const countEntries = await Promise.all(items.map(async (item) => {
      try {
        const itemScopes = await adminAPI.virtualCurrencyIntegrations.listScopes(item.id)
        return [item.id, itemScopes.length] as const
      } catch {
        return [item.id, 0] as const
      }
    }))
    scopeCounts.value = Object.fromEntries(countEntries)
  } catch (error: any) {
    const message = error?.response?.data?.detail || t('admin.currencyIntegration.loadFailed')
    loadError.value = message
    appStore.showError(message)
  } finally {
    loading.value = false
  }
}

function resetCreateForm() {
  createForm.code = ''
  createForm.name = ''
  createForm.metadata = '{}'
}

function openCreateDialog() {
  resetCreateForm()
  showCreateDialog.value = true
}

function openEditDialog(item: VirtualCurrencyIntegration) {
  editingIntegration.value = item
  editForm.name = item.name
  editForm.metadata = JSON.stringify(item.metadata || {}, null, 2)
  showEditDialog.value = true
}

async function handleCreate() {
  const metadata = parseMetadata(createForm.metadata)
  if (!metadata) {
    appStore.showError(t('admin.currencyIntegration.metadataInvalid'))
    return
  }
  saving.value = true
  try {
    const result = await adminAPI.virtualCurrencyIntegrations.create({
      code: createForm.code.trim(),
      name: createForm.name.trim(),
      metadata
    })
    showCreateDialog.value = false
    secretResult.value = result
    showSecretDialog.value = true
    appStore.showSuccess(t('admin.currencyIntegration.created'))
    await loadIntegrations()
  } catch (error: any) {
    appStore.showError(error?.response?.data?.detail || t('admin.currencyIntegration.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function handleUpdate() {
  if (!editingIntegration.value) return
  const metadata = parseMetadata(editForm.metadata)
  if (!metadata) {
    appStore.showError(t('admin.currencyIntegration.metadataInvalid'))
    return
  }
  saving.value = true
  try {
    await adminAPI.virtualCurrencyIntegrations.update(editingIntegration.value.id, {
      name: editForm.name.trim(),
      metadata
    })
    showEditDialog.value = false
    appStore.showSuccess(t('admin.currencyIntegration.updated'))
    await loadIntegrations()
  } catch (error: any) {
    appStore.showError(error?.response?.data?.detail || t('admin.currencyIntegration.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function handleRotateSecret() {
  if (!rotateTarget.value) return
  saving.value = true
  try {
    secretResult.value = await adminAPI.virtualCurrencyIntegrations.rotateSecret(rotateTarget.value.id)
    showRotateDialog.value = false
    showSecretDialog.value = true
    appStore.showSuccess(t('admin.currencyIntegration.secretRotated'))
    await loadIntegrations()
  } catch (error: any) {
    appStore.showError(error?.response?.data?.detail || t('admin.currencyIntegration.rotateFailed'))
  } finally {
    saving.value = false
  }
}

async function handleSetStatus() {
  if (!statusTarget.value) return
  const nextStatus = statusTarget.value.status === 'active' ? 'disabled' : 'active'
  saving.value = true
  try {
    await adminAPI.virtualCurrencyIntegrations.setStatus(statusTarget.value.id, nextStatus)
    showStatusDialog.value = false
    appStore.showSuccess(t('admin.currencyIntegration.statusUpdated'))
    await loadIntegrations()
  } catch (error: any) {
    appStore.showError(error?.response?.data?.detail || t('admin.currencyIntegration.statusFailed'))
  } finally {
    saving.value = false
  }
}

async function openScopesDialog(item: VirtualCurrencyIntegration) {
  selectedIntegration.value = item
  resetScopeForm()
  showScopesDialog.value = true
  await loadScopes(item.id)
}

async function loadScopes(integrationID: number) {
  scopesLoading.value = true
  try {
    scopes.value = await adminAPI.virtualCurrencyIntegrations.listScopes(integrationID)
    scopeCounts.value = { ...scopeCounts.value, [integrationID]: scopes.value.length }
  } catch (error: any) {
    appStore.showError(error?.response?.data?.detail || t('admin.currencyIntegration.scopesLoadFailed'))
  } finally {
    scopesLoading.value = false
  }
}

function resetScopeForm() {
  scopeForm.currency_id = null
  scopeForm.group_id = null
  scopeForm.enabled = true
  scopeForm.can_earn = true
  scopeForm.can_spend = true
  scopeForm.can_settle = true
  scopeForm.metadata = '{}'
}

function editScope(scope: VirtualCurrencyIntegrationScope) {
  scopeForm.currency_id = scope.currency_id
  scopeForm.group_id = scope.group_id
  scopeForm.enabled = scope.enabled
  scopeForm.can_earn = scope.can_earn
  scopeForm.can_spend = scope.can_spend
  scopeForm.can_settle = scope.can_settle
  scopeForm.metadata = JSON.stringify(scope.metadata || {}, null, 2)
}

async function handleSaveScope() {
  if (!selectedIntegration.value || !scopeForm.currency_id || !scopeForm.group_id) return
  const metadata = parseMetadata(scopeForm.metadata)
  if (!metadata) {
    appStore.showError(t('admin.currencyIntegration.metadataInvalid'))
    return
  }
  scopeSaving.value = true
  try {
    await adminAPI.virtualCurrencyIntegrations.upsertScope(
      selectedIntegration.value.id,
      scopeForm.currency_id,
      scopeForm.group_id,
      {
        enabled: scopeForm.enabled,
        can_earn: scopeForm.can_earn,
        can_spend: scopeForm.can_spend,
        can_settle: scopeForm.can_settle,
        metadata
      }
    )
    appStore.showSuccess(t('admin.currencyIntegration.scopeSaved'))
    resetScopeForm()
    await loadScopes(selectedIntegration.value.id)
    scopeCounts.value = { ...scopeCounts.value, [selectedIntegration.value.id]: scopes.value.length }
  } catch (error: any) {
    appStore.showError(error?.response?.data?.detail || t('admin.currencyIntegration.scopeSaveFailed'))
  } finally {
    scopeSaving.value = false
  }
}

function confirmDeleteScope(scope: VirtualCurrencyIntegrationScope) {
  scopeToDelete.value = scope
  showDeleteScopeDialog.value = true
}

async function handleDeleteScope() {
  if (!selectedIntegration.value || !scopeToDelete.value) return
  scopeSaving.value = true
  try {
    await adminAPI.virtualCurrencyIntegrations.deleteScope(
      selectedIntegration.value.id,
      scopeToDelete.value.currency_id,
      scopeToDelete.value.group_id
    )
    showDeleteScopeDialog.value = false
    appStore.showSuccess(t('admin.currencyIntegration.scopeDeleted'))
    await loadScopes(selectedIntegration.value.id)
    scopeCounts.value = { ...scopeCounts.value, [selectedIntegration.value.id]: scopes.value.length }
  } catch (error: any) {
    appStore.showError(error?.response?.data?.detail || t('admin.currencyIntegration.scopeDeleteFailed'))
  } finally {
    scopeSaving.value = false
  }
}

function closeSecretDialog() {
  showSecretDialog.value = false
  secretResult.value = null
}

async function copySecret() {
  if (secretResult.value) await copyToClipboard(secretResult.value.secret, t('admin.currencyIntegration.secretCopied'))
}

function selectInput(event: Event) {
  const input = event.target as HTMLInputElement
  input.select()
}

onMounted(() => {
  void loadIntegrations()
})
</script>

<style scoped>
.link-button {
  color: rgb(37 99 235);
  font-weight: 700;
  text-decoration: underline;
  text-underline-offset: 3px;
}

.link-button:hover,
.link-button:focus-visible {
  color: rgb(29 78 216);
  outline: none;
}

.dark .link-button {
  color: rgb(96 165 250);
}

.dark .link-button:hover,
.dark .link-button:focus-visible {
  color: rgb(147 197 253);
}
</style>
