<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <!-- Left: Search + Filters -->
          <div class="flex-1 sm:max-w-64">
            <input
              v-model="searchQuery"
              type="text"
              :placeholder="t('admin.announcements.searchAnnouncements')"
              class="input"
              @input="handleSearch"
            />
          </div>
          <Select
            v-model="filters.status"
            :options="statusFilterOptions"
            class="w-40"
            @change="handleStatusChange"
          />
          <SavedViewsControl
            storage-key="admin-announcements"
            :state="savedViewState"
            :disabled="loading"
            @apply="applySavedView"
          />

          <!-- Right: Action buttons -->
          <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
            <button
              type="button"
              @click="loadAnnouncements"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button type="button" @click="openCreateDialog" class="btn btn-primary">
              <Icon name="plus" size="md" class="mr-1" />
              {{ t('admin.announcements.createAnnouncement') }}
            </button>
          </div>
        </div>
      </template>

      <div v-if="selectedCount > 0" class="mb-4 flex flex-wrap items-center gap-3 rounded-lg bg-primary-50 p-3 dark:bg-primary-900/20" role="toolbar" :aria-label="t('common.actions')">
        <span class="mr-auto text-sm font-medium text-primary-900 dark:text-primary-100">{{ t('common.selectedCount', { count: selectedCount }) }}</span>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="bulkDeleting || bulkStatusUpdating"
          @click="bulkUpdateStatus('active')"
        >
          <Icon name="play" size="sm" />
          {{ t('admin.announcements.publish') }}
        </button>
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="bulkDeleting || bulkStatusUpdating"
          @click="bulkUpdateStatus('archived')"
        >
          <Icon name="clock" size="sm" />
          {{ t('admin.announcements.archive') }}
        </button>
        <button
          type="button"
          class="btn btn-danger btn-sm"
          :disabled="bulkDeleting || bulkStatusUpdating"
          @click="showBulkDeleteDialog = true"
        >
          <Icon name="trash" size="sm" :class="bulkDeleting ? 'animate-pulse' : ''" />
          {{ t('admin.announcements.bulkDelete') }}
        </button>
        <button type="button" class="btn btn-ghost btn-sm" :disabled="bulkDeleting || bulkStatusUpdating" @click="clearSelection">
          {{ t('common.clear') }}
        </button>
      </div>

      <template #table>
        <DataTable
          :columns="columns"
          :data="announcements"
          :loading="loading"
          :error="announcementsError"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
          @retry="loadAnnouncements"
        >
          <template #header-select>
            <input
              type="checkbox"
              :checked="allVisibleSelected"
              :indeterminate="someVisibleSelected && !allVisibleSelected"
              :aria-label="t('common.selectAll')"
              @click.stop
              @change="toggleSelectAllVisible($event)"
            />
          </template>

          <template #cell-select="{ row }">
            <input
              type="checkbox"
              :checked="selectedAnnouncementIds.has(row.id)"
              :aria-label="t('common.selectRow', { name: row.title })"
              @click.stop
              @change="toggleSelectRow(row.id, $event)"
            />
          </template>

          <template #cell-title="{ value, row }">
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <span class="truncate font-medium text-gray-900 dark:text-white">{{ value }}</span>
              </div>
              <div class="mt-1 flex items-center gap-2 text-xs text-gray-500 dark:text-dark-400">
                <span>#{{ row.id }}</span>
                <span class="text-gray-300 dark:text-dark-700">·</span>
                <span>{{ formatDateTime(row.created_at) }}</span>
              </div>
            </div>
          </template>

          <template #cell-status="{ row }">
            <span
              :class="[
                'badge',
                lifecycleStatus(row) === 'active'
                  ? 'badge-success'
                  : lifecycleStatus(row) === 'draft'
                    ? 'badge-gray'
                    : 'badge-warning'
              ]"
            >
              {{ lifecycleStatusLabel(row) }}
            </span>
          </template>

          <template #cell-notify_mode="{ row }">
            <span
              :class="[
                'badge',
                row.notify_mode === 'popup'
                  ? 'badge-warning'
                  : 'badge-gray'
              ]"
            >
              {{ row.notify_mode === 'popup' ? t('admin.announcements.notifyModeLabels.popup') : t('admin.announcements.notifyModeLabels.silent') }}
            </span>
          </template>

          <template #cell-targeting="{ row }">
            <span class="text-sm text-gray-600 dark:text-gray-300">
              {{ targetingSummary(row.targeting) }}
            </span>
          </template>

          <template #cell-timeRange="{ row }">
            <div class="text-sm text-gray-600 dark:text-gray-300">
              <div>
                <span class="font-medium">{{ t('admin.announcements.form.startsAt') }}:</span>
                <span class="ml-1">{{ row.starts_at ? formatDateTime(row.starts_at) : t('admin.announcements.timeImmediate') }}</span>
              </div>
              <div class="mt-0.5">
                <span class="font-medium">{{ t('admin.announcements.form.endsAt') }}:</span>
                <span class="ml-1">{{ row.ends_at ? formatDateTime(row.ends_at) : t('admin.announcements.timeNever') }}</span>
              </div>
            </div>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center space-x-1">
              <button
                type="button"
                @click="openReadStatus(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400"
                :title="t('admin.announcements.readStatus')"
              >
                <Icon name="eye" size="sm" />
              </button>
              <RowActionMenu
                :items="announcementActionItems(row)"
                :aria-label="t('common.more')"
                @select="(key) => handleAnnouncementAction(key, row)"
              />
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('empty.noData')"
              :description="t('admin.announcements.failedToLoad')"
              :action-text="t('admin.announcements.createAnnouncement')"
              @action="openCreateDialog"
            />
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

    <ConfirmDialog
      :show="showBulkDeleteDialog"
      :title="t('admin.announcements.bulkDelete')"
      :message="t('admin.announcements.bulkDeleteConfirm', { count: selectedCount })"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmBulkDelete"
      @cancel="showBulkDeleteDialog = false"
    />

    <!-- Create/Edit Dialog -->
    <BaseDialog
      :show="showEditDialog"
      :title="isEditing ? t('admin.announcements.editAnnouncement') : t('admin.announcements.createAnnouncement')"
      width="wide"
      @close="closeEdit"
    >
      <form id="announcement-form" @submit.prevent="handleSave" class="space-y-4">
        <div>
          <div class="flex items-center justify-between gap-3">
            <label class="input-label">{{ t('admin.announcements.form.title') }}</label>
            <span class="text-xs tabular-nums text-gray-400 dark:text-dark-500">{{ form.title.length }}/200</span>
          </div>
          <input v-model="form.title" type="text" maxlength="200" class="input" required />
        </div>

        <div>
          <div class="mb-1.5 flex items-center justify-between gap-3">
            <label class="input-label mb-0">{{ t('admin.announcements.form.content') }}</label>
            <div class="tabs p-0" role="tablist" :aria-label="t('admin.announcements.form.content')">
              <button type="button" class="tab px-2.5 py-1 text-xs" :class="editorMode === 'edit' ? 'tab-active' : ''" @click="editorMode = 'edit'">
                {{ t('admin.announcements.editor.edit') }}
              </button>
              <button type="button" class="tab px-2.5 py-1 text-xs" :class="editorMode === 'preview' ? 'tab-active' : ''" @click="editorMode = 'preview'">
                {{ t('admin.announcements.editor.preview') }}
              </button>
            </div>
          </div>
          <textarea v-if="editorMode === 'edit'" v-model="form.content" rows="8" class="input font-mono leading-6" required></textarea>
          <div v-else class="markdown-body min-h-48 border p-4 dark:bg-dark-900/30" style="border-color: var(--ui-separator)" v-html="renderMarkdown(form.content)" />
          <p class="input-hint">{{ t('admin.announcements.form.contentHint') }}</p>
        </div>

        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.announcements.form.status') }}</label>
            <Select v-model="form.status" :options="statusOptions" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.announcements.form.notifyMode') }}</label>
            <Select v-model="form.notify_mode" :options="notifyModeOptions" />
            <p class="input-hint">{{ t('admin.announcements.form.notifyModeHint') }}</p>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.announcements.form.startsAt') }}</label>
            <input v-model="form.starts_at_str" type="datetime-local" class="input" />
            <p class="input-hint">{{ t('admin.announcements.form.startsAtHint') }}</p>
          </div>
          <div>
            <label class="input-label">{{ t('admin.announcements.form.endsAt') }}</label>
            <input v-model="form.ends_at_str" type="datetime-local" class="input" />
            <p class="input-hint">{{ t('admin.announcements.form.endsAtHint') }}</p>
          </div>
        </div>

        <AnnouncementTargetingEditor
          v-model="form.targeting"
          :groups="subscriptionGroups"
        />
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" @click="closeEdit" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" form="announcement-form" :disabled="saving" class="btn btn-primary">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Delete Confirmation -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.announcements.deleteAnnouncement')"
      :message="t('admin.announcements.deleteConfirm')"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />

    <!-- Read Status Dialog -->
    <AnnouncementReadStatusDialog
      :show="showReadStatusDialog"
      :announcement-id="readStatusAnnouncementId"
      @close="showReadStatusDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useUrlQueryBindings, parseNumberQuery, parseStringQuery } from '@/composables/useUrlQueryBindings'
import { adminAPI } from '@/api/admin'
import { formatDateTime, formatDateTimeLocalInput, parseDateTimeLocalInput } from '@/utils/format'
import { renderMarkdown } from '@/utils/markdown'
import type { AdminGroup, Announcement, AnnouncementTargeting } from '@/types'
import type { Column } from '@/components/common/types'

import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import SavedViewsControl from '@/components/common/SavedViewsControl.vue'
import RowActionMenu, { type RowActionMenuItem } from '@/components/common/RowActionMenu.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'

import AnnouncementTargetingEditor from '@/components/admin/announcements/AnnouncementTargetingEditor.vue'
import AnnouncementReadStatusDialog from '@/components/admin/announcements/AnnouncementReadStatusDialog.vue'

const { t } = useI18n()
const appStore = useAppStore()

const announcements = ref<Announcement[]>([])
const loading = ref(true)
const announcementsError = ref<string | null>(null)
const bulkDeleting = ref(false)
const bulkStatusUpdating = ref(false)
const selectedAnnouncementIds = ref<Set<number>>(new Set())
const showBulkDeleteDialog = ref(false)

const filters = reactive({
  status: '',
})
const searchQuery = ref('')

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})

const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

const announcementActionItems = (announcement: Announcement): RowActionMenuItem[] => {
  const items: RowActionMenuItem[] = [
    { key: 'preview', label: t('admin.announcements.preview'), icon: 'eye' },
    { key: 'edit', label: t('common.edit'), icon: 'edit' },
    { key: 'duplicate', label: t('admin.announcements.duplicate'), icon: 'copy' },
  ]
  if (announcement.status !== 'active') items.push({ key: 'publish', label: t('admin.announcements.publish'), icon: 'play' })
  if (announcement.status !== 'archived') items.push({ key: 'archive', label: t('admin.announcements.archive'), icon: 'clock' })
  items.push({ key: 'delete', label: t('common.delete'), icon: 'trash', tone: 'danger' })
  return items
}

const handleAnnouncementAction = (key: string, announcement: Announcement) => {
  if (key === 'preview') openPreviewDialog(announcement)
  else if (key === 'edit') openEditDialog(announcement)
  else if (key === 'duplicate') void duplicateAnnouncement(announcement)
  else if (key === 'publish') void updateAnnouncementStatus(announcement, 'active')
  else if (key === 'archive') void updateAnnouncementStatus(announcement, 'archived')
  else if (key === 'delete') handleDelete(announcement)
}

const savedViewState = computed<Record<string, unknown>>(() => ({
  search: searchQuery.value,
  status: filters.status,
  page: pagination.page,
  page_size: pagination.page_size,
  sort_by: sortState.sort_by,
  sort_order: sortState.sort_order
}))

const applySavedView = (state: Record<string, unknown>) => {
  searchQuery.value = typeof state.search === 'string' ? state.search : ''
  filters.status = typeof state.status === 'string' ? state.status : ''
  pagination.page = Number.isFinite(Number(state.page)) ? Math.max(1, Number(state.page)) : 1
  pagination.page_size = Number.isFinite(Number(state.page_size)) ? Math.max(1, Number(state.page_size)) : pagination.page_size
  sortState.sort_by = typeof state.sort_by === 'string' ? state.sort_by : 'created_at'
  sortState.sort_order = state.sort_order === 'asc' ? 'asc' : 'desc'
  void loadAnnouncements()
}

useUrlQueryBindings([
  {
    key: 'search',
    get: () => searchQuery.value,
    set: (value: string) => { searchQuery.value = value },
    parse: parseStringQuery,
    omit: (value: string) => !value
  },
  {
    key: 'status',
    get: () => filters.status,
    set: (value: string) => { filters.status = value },
    parse: parseStringQuery,
    omit: (value: string) => !value
  },
  {
    key: 'page',
    get: () => pagination.page,
    set: (value: number) => { pagination.page = Math.max(1, Math.floor(value)) },
    parse: parseNumberQuery,
    omit: (value: number) => value <= 1
  },
  {
    key: 'page_size',
    get: () => pagination.page_size,
    set: (value: number) => { pagination.page_size = Math.max(1, Math.floor(value)) },
    parse: parseNumberQuery,
    omit: (value: number) => value === getPersistedPageSize()
  },
  {
    key: 'sort_by',
    get: () => sortState.sort_by,
    set: (value: string) => { sortState.sort_by = value },
    parse: parseStringQuery,
    omit: (value: string) => !value || value === 'created_at'
  },
  {
    key: 'sort_order',
    get: () => sortState.sort_order,
    set: (value: string) => { sortState.sort_order = value === 'asc' ? 'asc' : 'desc' },
    parse: parseStringQuery,
    omit: (value: string) => !value || value === 'desc'
  }
])

const statusFilterOptions = computed(() => [
  { value: '', label: t('admin.announcements.allStatus') },
  { value: 'draft', label: t('admin.announcements.statusLabels.draft') },
  { value: 'active', label: t('admin.announcements.statusLabels.active') },
  { value: 'archived', label: t('admin.announcements.statusLabels.archived') }
])

const statusOptions = computed(() => [
  { value: 'draft', label: t('admin.announcements.statusLabels.draft') },
  { value: 'active', label: t('admin.announcements.statusLabels.active') },
  { value: 'archived', label: t('admin.announcements.statusLabels.archived') }
])

const notifyModeOptions = computed(() => [
  { value: 'silent', label: t('admin.announcements.notifyModeLabels.silent') },
  { value: 'popup', label: t('admin.announcements.notifyModeLabels.popup') }
])

const columns = computed<Column[]>(() => [
  { key: 'select', label: '', sortable: false, class: 'w-10 text-center' },
  { key: 'title', label: t('admin.announcements.columns.title'), sortable: true },
  { key: 'status', label: t('admin.announcements.columns.status'), sortable: true },
  { key: 'notify_mode', label: t('admin.announcements.columns.notifyMode'), sortable: true },
  { key: 'targeting', label: t('admin.announcements.columns.targeting') },
  { key: 'timeRange', label: t('admin.announcements.columns.timeRange') },
  { key: 'created_at', label: t('admin.announcements.columns.createdAt'), sortable: true },
  { key: 'actions', label: t('admin.announcements.columns.actions') }
])

const statusLabel = (status: string) => {
  if (status === 'draft') return t('admin.announcements.statusLabels.draft')
  if (status === 'active') return t('admin.announcements.statusLabels.active')
  if (status === 'archived') return t('admin.announcements.statusLabels.archived')
  return status
}

const lifecycleStatus = (announcement: Announcement): 'draft' | 'active' | 'archived' | 'scheduled' | 'expired' => {
  if (announcement.status !== 'active') return announcement.status
  const now = Date.now()
  if (announcement.starts_at && new Date(announcement.starts_at).getTime() > now) return 'scheduled'
  if (announcement.ends_at && new Date(announcement.ends_at).getTime() <= now) return 'expired'
  return 'active'
}

const lifecycleStatusLabel = (announcement: Announcement) => {
  const status = lifecycleStatus(announcement)
  if (status === 'scheduled') return t('admin.announcements.statusLabels.scheduled')
  if (status === 'expired') return t('admin.announcements.statusLabels.expired')
  return statusLabel(status)
}

const targetingSummary = (targeting: AnnouncementTargeting) => {
  const anyOf = targeting?.any_of ?? []
  if (!anyOf || anyOf.length === 0) return t('admin.announcements.targetingSummaryAll')
  return t('admin.announcements.targetingSummaryCustom', { groups: anyOf.length })
}

// ===== CRUD / list =====
let currentController: AbortController | null = null

async function loadAnnouncements() {
  currentController?.abort()
  const requestController = new AbortController()
  currentController = requestController
  const { signal } = requestController

  try {
    loading.value = true
    announcementsError.value = null
    const res = await adminAPI.announcements.list(pagination.page, pagination.page_size, {
      status: filters.status || undefined,
      search: searchQuery.value || undefined,
      sort_by: sortState.sort_by,
      sort_order: sortState.sort_order
    }, { signal })

    if (signal.aborted || currentController !== requestController) return

    announcements.value = res.items
    pagination.total = res.total
    pagination.pages = res.pages
    pagination.page = res.page
    pagination.page_size = res.page_size
    const visibleIds = new Set(res.items.map((item) => item.id))
    selectedAnnouncementIds.value = new Set([...selectedAnnouncementIds.value].filter((id) => visibleIds.has(id)))
  } catch (error: any) {
    if (
      signal.aborted ||
      currentController !== requestController ||
      error?.name === 'AbortError' ||
      error?.code === 'ERR_CANCELED'
    ) {
      return
    }
    console.error('Error loading announcements:', error)
    const message = error.response?.data?.detail || t('admin.announcements.failedToLoad')
    announcementsError.value = message
    appStore.showError(message)
  } finally {
    if (currentController === requestController) {
      loading.value = false
      currentController = null
    }
  }
}

const selectedCount = computed(() => selectedAnnouncementIds.value.size)
const allVisibleSelected = computed(() => (
  announcements.value.length > 0 && announcements.value.every((announcement) => selectedAnnouncementIds.value.has(announcement.id))
))
const someVisibleSelected = computed(() => announcements.value.some((announcement) => selectedAnnouncementIds.value.has(announcement.id)))

const toggleSelectRow = (id: number, event: Event) => {
  const checked = (event.target as HTMLInputElement).checked
  const next = new Set(selectedAnnouncementIds.value)
  if (checked) next.add(id)
  else next.delete(id)
  selectedAnnouncementIds.value = next
}

const toggleSelectAllVisible = (event: Event) => {
  const checked = (event.target as HTMLInputElement).checked
  const next = new Set(selectedAnnouncementIds.value)
  announcements.value.forEach((announcement) => {
    if (checked) next.add(announcement.id)
    else next.delete(announcement.id)
  })
  selectedAnnouncementIds.value = next
}

const clearSelection = () => {
  selectedAnnouncementIds.value = new Set()
}

function handlePageChange(page: number) {
  pagination.page = page
  loadAnnouncements()
}

function handlePageSizeChange(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  loadAnnouncements()
}

function handleStatusChange() {
  pagination.page = 1
  loadAnnouncements()
}

function handleSort(key: string, order: 'asc' | 'desc') {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  loadAnnouncements()
}

let searchDebounceTimer: number | null = null
function handleSearch() {
  if (searchDebounceTimer) window.clearTimeout(searchDebounceTimer)
  searchDebounceTimer = window.setTimeout(() => {
    pagination.page = 1
    loadAnnouncements()
  }, 300)
}

// ===== Create/Edit dialog =====
const showEditDialog = ref(false)
const saving = ref(false)
const editingAnnouncement = ref<Announcement | null>(null)
const editorMode = ref<'edit' | 'preview'>('edit')

const isEditing = computed(() => !!editingAnnouncement.value)

const form = reactive({
  title: '',
  content: '',
  status: 'draft',
  notify_mode: 'silent',
  starts_at_str: '',
  ends_at_str: '',
  targeting: { any_of: [] } as AnnouncementTargeting
})

const subscriptionGroups = ref<AdminGroup[]>([])

async function loadSubscriptionGroups() {
  try {
    const all = await adminAPI.groups.getAll()
    subscriptionGroups.value = (all || []).filter((g) => g.subscription_type === 'subscription')
  } catch (error: any) {
    console.error('Error loading groups:', error)
    // not fatal
  }
}

function resetForm() {
  form.title = ''
  form.content = ''
  form.status = 'draft'
  form.notify_mode = 'silent'
  form.starts_at_str = ''
  form.ends_at_str = ''
  form.targeting = { any_of: [] }
}

function fillFormFromAnnouncement(a: Announcement) {
  form.title = a.title
  form.content = a.content
  form.status = a.status
  form.notify_mode = a.notify_mode || 'silent'

  // Backend returns RFC3339 strings
  form.starts_at_str = a.starts_at ? formatDateTimeLocalInput(Math.floor(new Date(a.starts_at).getTime() / 1000)) : ''
  form.ends_at_str = a.ends_at ? formatDateTimeLocalInput(Math.floor(new Date(a.ends_at).getTime() / 1000)) : ''

  form.targeting = a.targeting ?? { any_of: [] }
}

function openCreateDialog() {
  editingAnnouncement.value = null
  resetForm()
  editorMode.value = 'edit'
  showEditDialog.value = true
}

function openEditDialog(row: Announcement) {
  editingAnnouncement.value = row
  fillFormFromAnnouncement(row)
  editorMode.value = 'edit'
  showEditDialog.value = true
}

function openPreviewDialog(row: Announcement) {
  editingAnnouncement.value = row
  fillFormFromAnnouncement(row)
  editorMode.value = 'preview'
  showEditDialog.value = true
}

function closeEdit() {
  showEditDialog.value = false
  editingAnnouncement.value = null
  editorMode.value = 'edit'
}

function buildCreatePayload() {
  const startsAt = parseDateTimeLocalInput(form.starts_at_str)
  const endsAt = parseDateTimeLocalInput(form.ends_at_str)

  return {
    title: form.title,
    content: form.content,
    status: form.status as any,
    notify_mode: form.notify_mode as any,
    targeting: form.targeting,
    starts_at: startsAt ?? undefined,
    ends_at: endsAt ?? undefined
  }
}

function buildUpdatePayload(original: Announcement) {
  const payload: any = {}

  if (form.title !== original.title) payload.title = form.title
  if (form.content !== original.content) payload.content = form.content
  if (form.status !== original.status) payload.status = form.status
  if (form.notify_mode !== (original.notify_mode || 'silent')) payload.notify_mode = form.notify_mode

  // starts_at / ends_at: distinguish unchanged vs clear(0) vs set
  const originalStarts = original.starts_at ? Math.floor(new Date(original.starts_at).getTime() / 1000) : null
  const originalEnds = original.ends_at ? Math.floor(new Date(original.ends_at).getTime() / 1000) : null

  const newStarts = parseDateTimeLocalInput(form.starts_at_str)
  const newEnds = parseDateTimeLocalInput(form.ends_at_str)

  if (newStarts !== originalStarts) {
    payload.starts_at = newStarts === null ? 0 : newStarts
  }
  if (newEnds !== originalEnds) {
    payload.ends_at = newEnds === null ? 0 : newEnds
  }

  // targeting: do shallow compare by JSON
  if (JSON.stringify(form.targeting ?? {}) !== JSON.stringify(original.targeting ?? {})) {
    payload.targeting = form.targeting
  }

  return payload
}

async function handleSave() {
  // Frontend validation for targeting (to avoid ANNOUNCEMENT_INVALID_TARGET)
  const anyOf = form.targeting?.any_of ?? []
  if (anyOf.length > 50) {
    appStore.showError(t('admin.announcements.failedToCreate'))
    return
  }
  for (const g of anyOf) {
    const allOf = g?.all_of ?? []
    if (allOf.length > 50) {
      appStore.showError(t('admin.announcements.failedToCreate'))
      return
    }
  }

  const startsAt = parseDateTimeLocalInput(form.starts_at_str)
  const endsAt = parseDateTimeLocalInput(form.ends_at_str)
  if (startsAt !== null && endsAt !== null && startsAt >= endsAt) {
    appStore.showError(t('admin.announcements.invalidSchedule'))
    return
  }

  saving.value = true
  try {
    if (!editingAnnouncement.value) {
      const payload = buildCreatePayload()
      await adminAPI.announcements.create(payload)
      appStore.showSuccess(t('common.success'))
      showEditDialog.value = false
      await loadAnnouncements()
      return
    }

    const original = editingAnnouncement.value
    const payload = buildUpdatePayload(original)
    await adminAPI.announcements.update(original.id, payload)
    appStore.showSuccess(t('common.success'))
    showEditDialog.value = false
    editingAnnouncement.value = null
    await loadAnnouncements()
  } catch (error: any) {
    console.error('Failed to save announcement:', error)
    appStore.showError(error.response?.data?.detail || (editingAnnouncement.value ? t('admin.announcements.failedToUpdate') : t('admin.announcements.failedToCreate')))
  } finally {
    saving.value = false
  }
}

async function duplicateAnnouncement(announcement: Announcement) {
  try {
    await adminAPI.announcements.create({
      title: `${announcement.title} ${t('admin.announcements.copySuffix')}`,
      content: announcement.content,
      status: 'draft',
      notify_mode: announcement.notify_mode,
      targeting: announcement.targeting,
    })
    appStore.showSuccess(t('admin.announcements.duplicated'))
    await loadAnnouncements()
  } catch (error: any) {
    console.error('Failed to duplicate announcement:', error)
    appStore.showError(error.response?.data?.detail || t('admin.announcements.failedToCreate'))
  }
}

async function updateAnnouncementStatus(announcement: Announcement, status: 'active' | 'archived') {
  try {
    await adminAPI.announcements.update(announcement.id, { status })
    appStore.showSuccess(t('admin.announcements.statusUpdated'))
    await loadAnnouncements()
  } catch (error: any) {
    console.error('Failed to update announcement status:', error)
    appStore.showError(error.response?.data?.detail || t('admin.announcements.failedToUpdate'))
  }
}

// ===== Delete =====
const showDeleteDialog = ref(false)
const deletingAnnouncement = ref<Announcement | null>(null)

function handleDelete(row: Announcement) {
  deletingAnnouncement.value = row
  showDeleteDialog.value = true
}

async function confirmDelete() {
  if (!deletingAnnouncement.value) return

  const deletedId = deletingAnnouncement.value.id
  try {
    await adminAPI.announcements.delete(deletedId)
    appStore.showSuccess(t('common.success'))
    showDeleteDialog.value = false
    deletingAnnouncement.value = null
    const next = new Set(selectedAnnouncementIds.value)
    next.delete(deletedId)
    selectedAnnouncementIds.value = next
    await loadAnnouncements()
  } catch (error: any) {
    console.error('Failed to delete announcement:', error)
    appStore.showError(error.response?.data?.detail || t('admin.announcements.failedToDelete'))
  }
}

async function confirmBulkDelete() {
  const ids = Array.from(selectedAnnouncementIds.value)
  if (ids.length === 0 || bulkDeleting.value) return

  bulkDeleting.value = true
  try {
    const results = await Promise.allSettled(ids.map((id) => adminAPI.announcements.delete(id)))
    const success = results.filter((result) => result.status === 'fulfilled').length
    const failed = results.length - success
    if (failed === 0) {
      appStore.showSuccess(t('admin.announcements.bulkDeleteSuccess', { count: success }))
    } else {
      appStore.showWarning(t('admin.announcements.bulkDeletePartial', { success, failed }))
    }
    clearSelection()
    showBulkDeleteDialog.value = false
    await loadAnnouncements()
  } catch (error) {
    appStore.showError(t('admin.announcements.failedToDelete'))
    console.error('Error bulk deleting announcements:', error)
  } finally {
    bulkDeleting.value = false
  }
}

async function bulkUpdateStatus(status: 'active' | 'archived') {
  const ids = Array.from(selectedAnnouncementIds.value)
  if (ids.length === 0 || bulkStatusUpdating.value) return

  bulkStatusUpdating.value = true
  try {
    const results = await Promise.allSettled(ids.map((id) => adminAPI.announcements.update(id, { status })))
    const success = results.filter((result) => result.status === 'fulfilled').length
    const failed = results.length - success
    if (failed === 0) appStore.showSuccess(t('admin.announcements.bulkStatusSuccess', { count: success }))
    else appStore.showWarning(t('admin.announcements.bulkStatusPartial', { success, failed }))
    clearSelection()
    await loadAnnouncements()
  } catch (error) {
    console.error('Error bulk updating announcement status:', error)
    appStore.showError(t('admin.announcements.failedToUpdate'))
  } finally {
    bulkStatusUpdating.value = false
  }
}

// ===== Read status =====
const showReadStatusDialog = ref(false)
const readStatusAnnouncementId = ref<number | null>(null)

function openReadStatus(row: Announcement) {
  readStatusAnnouncementId.value = row.id
  showReadStatusDialog.value = true
}

onMounted(async () => {
  await loadSubscriptionGroups()
  await loadAnnouncements()
})

onUnmounted(() => {
  if (searchDebounceTimer) window.clearTimeout(searchDebounceTimer)
  currentController?.abort()
})
</script>
