<template>
  <BaseDialog :show="show" :title="t('admin.sharedQuota.title')" width="extra-wide" @close="emit('close')">
    <div v-if="group" class="space-y-4">
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.sharedQuota.description') }}</p>

      <div v-if="snapshot" class="grid gap-3 md:grid-cols-2">
        <div v-for="window in snapshot.windows" :key="window.config.key" class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600">
          <div class="flex items-center justify-between text-xs text-gray-500 dark:text-gray-400">
            <span>{{ windowLabel(window.config.key) }}</span>
            <span :class="window.hard_stop_reached ? 'text-red-600 dark:text-red-400' : window.soft_stop_reached ? 'text-amber-600 dark:text-amber-400' : 'text-emerald-600 dark:text-emerald-400'">
              {{ window.config.enabled ? `${window.utilization_percent.toFixed(2)}%` : t('admin.sharedQuota.disabled') }}
            </span>
          </div>
          <div class="mt-1 flex items-baseline justify-between gap-3 font-mono text-sm text-gray-900 dark:text-white">
            <span>{{ window.config.capacity_usd == null ? '—' : usd(window.total_used_usd) }}</span>
            <span class="text-xs text-gray-500 dark:text-gray-400">/ {{ window.config.capacity_usd == null ? '—' : usd(window.distributable_usd) }}</span>
          </div>
        </div>
      </div>

      <div v-if="loading" class="flex justify-center py-10">
        <Icon name="refresh" size="md" class="animate-spin text-primary-500" />
      </div>

      <template v-else>
        <div class="space-y-4 rounded-lg border border-gray-200 p-4 dark:border-dark-600">
          <label class="flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-300 md:col-span-2">
            <input v-model="form.enabled" type="checkbox" class="checkbox" />
            {{ t('admin.sharedQuota.enabled') }}
          </label>
          <div class="grid gap-3 md:grid-cols-2">
            <div v-for="window in windowForms" :key="window.key" class="space-y-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-700/50">
              <div class="flex items-center justify-between">
                <h4 class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ windowLabel(window.key) }}</h4>
                <label class="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
                  <input v-model="window.enabled" type="checkbox" class="checkbox" />
                  {{ window.enabled ? t('admin.sharedQuota.active') : t('admin.sharedQuota.disabled') }}
                </label>
              </div>
              <label class="input-label">
                {{ t('admin.sharedQuota.capacity') }}
                <input v-model.number="window.capacityUsd" type="number" min="0" step="0.01" class="input mt-1" />
              </label>
              <label class="input-label">
                {{ t('admin.sharedQuota.windowSeconds') }}
                <input v-model.number="window.windowSeconds" type="number" min="300" step="300" class="input mt-1" />
              </label>
              <div class="grid grid-cols-3 gap-2">
                <label class="input-label">{{ t('admin.sharedQuota.reserve') }}<input v-model.number="window.reservePercent" type="number" min="0" max="99.99" step="0.1" class="input mt-1" /></label>
                <label class="input-label">{{ t('admin.sharedQuota.softStop') }}<input v-model.number="window.softStopPercent" type="number" min="0.1" max="100" step="0.1" class="input mt-1" /></label>
                <label class="input-label">{{ t('admin.sharedQuota.hardStop') }}<input v-model.number="window.hardStopPercent" type="number" min="0.1" max="100" step="0.1" class="input mt-1" /></label>
              </div>
            </div>
          </div>
          <div class="grid gap-3 md:grid-cols-2">
            <label class="input-label">
              {{ t('admin.sharedQuota.borrowMultiplier') }}
              <input v-model.number="form.borrowMultiplier" type="number" min="1" max="10" step="0.05" class="input mt-1" />
            </label>
            <label class="flex items-center gap-2 self-end pb-2 text-sm font-medium text-gray-700 dark:text-gray-300">
              <input v-model="form.borrowEnabled" type="checkbox" class="checkbox" />
              {{ t('admin.sharedQuota.borrow') }}
            </label>
          </div>
        </div>

        <div class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
          <div class="border-b border-gray-200 px-4 py-3 text-sm font-medium text-gray-800 dark:border-dark-600 dark:text-gray-200">
            {{ t('admin.sharedQuota.members') }}
          </div>
          <div v-if="enabledWindowSnapshots.length > 0" class="flex gap-2 border-b border-gray-200 px-4 py-2 dark:border-dark-600">
            <button
              v-for="window in enabledWindowSnapshots"
              :key="window.config.key"
              type="button"
              class="btn btn-sm"
              :class="selectedWindowKey === window.config.key ? 'btn-primary' : ''"
              @click="selectedWindowKey = window.config.key"
            >
              {{ windowLabel(window.config.key) }}
            </button>
          </div>
          <div v-if="editableMembers.length === 0" class="px-4 py-8 text-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.sharedQuota.noMembers') }}
          </div>
          <div v-else class="max-h-[360px] overflow-auto">
            <table class="w-full min-w-[760px] text-sm">
              <thead class="sticky top-0 z-[1] bg-gray-50 dark:bg-dark-700">
                <tr class="border-b border-gray-200 dark:border-dark-600">
                  <th class="px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.sharedQuota.member') }}</th>
                  <th class="px-3 py-2 text-right text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.sharedQuota.weight') }}</th>
                  <th class="px-3 py-2 text-right text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.sharedQuota.share') }}</th>
                  <th class="px-3 py-2 text-right text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.sharedQuota.used') }}</th>
                  <th class="px-3 py-2 text-right text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.sharedQuota.maximum') }}</th>
                  <th class="px-3 py-2 text-right text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.sharedQuota.borrowed') }}</th>
                  <th class="px-3 py-2 text-center text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.sharedQuota.status') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-600">
                <tr v-for="member in editableMembers" :key="member.user_id" class="hover:bg-gray-50 dark:hover:bg-dark-700/50">
                  <td class="px-3 py-2 text-gray-900 dark:text-white">
                    <div>{{ member.username || member.email || `#${member.user_id}` }}</div>
                    <div v-if="member.username" class="text-xs text-gray-500 dark:text-gray-400">{{ member.email }}</div>
                  </td>
                  <td class="px-3 py-2 text-right">
                    <input v-model.number="member.weight" type="number" min="0.0001" max="100000" step="0.1" class="hide-spinner input w-24 text-right" />
                  </td>
                  <td class="px-3 py-2 text-right font-mono text-gray-700 dark:text-gray-300">{{ (windowMember(member.user_id)?.share_percent ?? 0).toFixed(2) }}%</td>
                  <td class="px-3 py-2 text-right font-mono text-gray-700 dark:text-gray-300">{{ usd(windowMember(member.user_id)?.used_usd ?? 0) }}</td>
                  <td class="px-3 py-2 text-right font-mono text-gray-700 dark:text-gray-300">{{ usd(windowMember(member.user_id)?.maximum_usd ?? 0) }}</td>
                  <td class="px-3 py-2 text-right font-mono text-gray-700 dark:text-gray-300">{{ usd(windowMember(member.user_id)?.borrowed_usd ?? 0) }}</td>
                  <td class="px-3 py-2 text-center">
                    <label class="inline-flex items-center gap-2 text-xs text-gray-600 dark:text-gray-400">
                      <input v-model="member.enabled" type="checkbox" class="checkbox" />
                      {{ member.enabled ? t('admin.sharedQuota.active') : t('admin.sharedQuota.disabled') }}
                    </label>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div class="flex items-center justify-end gap-3 border-t border-gray-200 pt-4 dark:border-dark-600">
          <button type="button" class="btn btn-sm" @click="emit('close')">{{ t('admin.sharedQuota.close') }}</button>
          <button type="button" class="btn btn-primary btn-sm" :disabled="saving || !canSave" @click="save">
            <Icon v-if="saving" name="refresh" size="sm" class="mr-1 animate-spin" />
            {{ t('admin.sharedQuota.save') }}
          </button>
        </div>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { AdminGroup, SharedQuotaPoolMember, SharedQuotaPoolSnapshot } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{ show: boolean; group: AdminGroup | null }>()
const emit = defineEmits<{ close: []; success: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const saving = ref(false)
const snapshot = ref<SharedQuotaPoolSnapshot | null>(null)
const editableMembers = ref<SharedQuotaPoolMember[]>([])
const selectedWindowKey = ref('long')
const form = reactive({
  enabled: false,
  borrowEnabled: true,
  borrowMultiplier: 1.5
})
const windowForms = ref<Array<{
  key: string
  enabled: boolean
  capacityUsd: number | null
  windowSeconds: number
  reservePercent: number
  softStopPercent: number
  hardStopPercent: number
}>>([])

const canSave = computed(() => {
  if (!form.enabled) return true
  const enabledWindows = windowForms.value.filter(window => window.enabled)
  return enabledWindows.length > 0 && enabledWindows.every(window => window.capacityUsd != null && window.capacityUsd > 0)
})
const enabledWindowSnapshots = computed(() => snapshot.value?.windows.filter(window => window.config.enabled) ?? [])
const selectedWindow = computed(() => {
  const windows = snapshot.value?.windows ?? []
  return windows.find(window => window.config.key === selectedWindowKey.value) ?? windows.find(window => window.config.enabled)
})
const visibleMembers = computed(() => {
  const members = selectedWindow.value?.members ?? []
  return members.length > 0 ? members : snapshot.value?.members ?? []
})
const usd = (value: number) => new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value)
const windowLabel = (key: string) => key === 'short' ? '5h / 5 小时窗口' : key === 'long' ? '7d / 7 天窗口' : key

const load = async () => {
  if (!props.group) return
  loading.value = true
  try {
    const result = await adminAPI.groups.getSharedQuota(props.group.id)
    snapshot.value = result
    form.enabled = result.config.enabled
    form.borrowEnabled = result.config.borrow_enabled
    form.borrowMultiplier = result.config.borrow_multiplier
    windowForms.value = result.windows.map(window => ({
      key: window.config.key,
      enabled: window.config.enabled,
      capacityUsd: window.config.capacity_usd,
      windowSeconds: window.config.window_seconds,
      reservePercent: window.config.reserve_ratio * 100,
      softStopPercent: window.config.soft_stop_ratio * 100,
      hardStopPercent: window.config.hard_stop_ratio * 100
    }))
    if (!result.windows.some(window => window.config.key === selectedWindowKey.value && window.config.enabled)) {
      selectedWindowKey.value = result.windows.find(window => window.config.key === 'long')?.config.key
        ?? result.windows.find(window => window.config.enabled)?.config.key
        ?? 'long'
    }
    editableMembers.value = result.members.map(member => ({ ...member }))
  } catch (error) {
    appStore.showError(t('admin.sharedQuota.loadFailed'))
    console.error('Error loading shared quota pool:', error)
  } finally {
    loading.value = false
  }
}

const windowMember = (userId: number): SharedQuotaPoolMember | undefined =>
  visibleMembers.value.find(member => member.user_id === userId)

const save = async () => {
  if (!props.group || !canSave.value) return
  saving.value = true
  try {
    await adminAPI.groups.updateSharedQuota(props.group.id, {
      enabled: form.enabled,
      borrow_enabled: form.borrowEnabled,
      borrow_multiplier: form.borrowMultiplier,
      windows: windowForms.value.map(window => ({
        key: window.key,
        enabled: window.enabled,
        capacity_usd: window.capacityUsd,
        window_seconds: window.windowSeconds,
        reserve_ratio: window.reservePercent / 100,
        soft_stop_ratio: window.softStopPercent / 100,
        hard_stop_ratio: window.hardStopPercent / 100
      })),
      members: editableMembers.value.map(member => ({
        user_id: member.user_id,
        weight: member.weight,
        enabled: member.enabled
      }))
    })
    appStore.showSuccess(t('admin.sharedQuota.saveSuccess'))
    emit('success')
    await load()
  } catch (error) {
    appStore.showError(t('admin.sharedQuota.saveFailed'))
    console.error('Error saving shared quota pool:', error)
  } finally {
    saving.value = false
  }
}

watch(() => [props.show, props.group?.id], ([show]) => {
  if (show) void load()
})
</script>
