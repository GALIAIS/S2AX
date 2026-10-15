<template>
  <div class="min-w-[160px] space-y-1 text-xs">
    <span v-if="loading" role="status" class="text-gray-400">{{ t('common.loading') }}</span>
    <p v-else-if="error" role="alert" class="max-w-[240px] text-red-500">{{ error }}</p>
    <template v-else-if="usage">
      <div class="flex items-center justify-between gap-2"><span>{{ usage.plan }}</span><span>{{ t('admin.accounts.workbuddy.credits') }}</span></div>
      <div class="h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-gray-700" role="progressbar" :aria-label="t('admin.accounts.workbuddy.used')" :aria-valuenow="percent" aria-valuemin="0" aria-valuemax="100"><div class="h-full rounded-full bg-cyan-500" :style="{ width: `${percent}%` }"></div></div>
      <div>{{ t('admin.accounts.workbuddy.used') }} {{ number(usage.used) }} / {{ number(usage.total) }}</div>
      <div class="text-gray-500">{{ t('admin.accounts.workbuddy.remaining') }} {{ number(usage.remaining) }}</div>
      <div v-if="usage.resets_at" class="text-gray-500">{{ t('admin.accounts.workbuddy.resets') }} {{ date(usage.resets_at) }}</div>
      <div v-if="usage.expires_at" class="text-amber-600">{{ number(usage.expiring_credits || 0) }} credit · {{ date(usage.expires_at) }}</div>
      <div v-if="usage.expiry_warning" class="text-amber-600">{{ t('admin.accounts.workbuddy.expiryUnavailable') }}</div>
    </template>
    <button type="button" class="text-cyan-700 underline disabled:opacity-50 dark:text-cyan-300" :disabled="loading" @click="load">{{ t('common.refresh') }}</button>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { fetchWorkBuddyUsage } from '@/api/admin/workbuddy'
import type { WorkBuddyUsage } from '@/api/admin/workbuddy'
import type { Account } from '@/types'
import { enqueueUsageRequest } from '@/utils/usageLoadQueue'

const props = defineProps<{ account: Account }>()
const { t, locale } = useI18n()
const usage = ref<WorkBuddyUsage | null>(null)
const loading = ref(false)
const error = ref('')
let controller: AbortController | null = null
const percent = computed(() => usage.value && usage.value.total > 0 ? Math.min(100, Math.max(0, usage.value.used / usage.value.total * 100)) : 0)
const number = (value: number) => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }).format(value)
const date = (value: string) => new Date(value).toLocaleString(locale.value)
async function load() {
  controller?.abort()
  const attempt = new AbortController()
  controller = attempt
  loading.value = true
  error.value = ''
  const account = props.account
  try {
    const result = await enqueueUsageRequest(account, () => fetchWorkBuddyUsage(account.id, attempt.signal), { shouldRun: () => !attempt.signal.aborted })
    if (!attempt.signal.aborted) usage.value = result
  } catch (err) {
    if (!attempt.signal.aborted) {
      const apiError = err as { response?: { data?: { message?: string } } }
      error.value = apiError.response?.data?.message || t('admin.accounts.workbuddy.usageError')
    }
  } finally { if (controller === attempt) loading.value = false }
}
watch(() => props.account.id, () => { usage.value = null; void load() }, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>
