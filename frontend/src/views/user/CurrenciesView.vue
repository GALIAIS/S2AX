<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="loadWallets">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          <span class="hidden sm:inline">{{ t('virtualCurrency.refresh') }}</span>
        </button>
      </div>

      <div v-if="loadError" class="flex items-center gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300" role="alert">
        <Icon name="xCircle" size="sm" />
        <span>{{ loadError }}</span>
        <button type="button" class="btn btn-ghost btn-sm" @click="loadWallets">{{ t('misc.retry') }}</button>
      </div>

      <div v-if="loading && wallets.length === 0" class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <div v-for="index in 3" :key="index" class="card h-48 animate-pulse bg-gray-100 dark:bg-dark-800" />
      </div>

      <div v-else-if="wallets.length === 0" class="card empty-state py-16">
        <Icon name="creditCard" size="xl" class="text-gray-400 dark:text-dark-500" />
        <p class="mt-4 text-sm text-gray-500 dark:text-dark-400">{{ t('virtualCurrency.noWallets') }}</p>
        <p class="mt-2 max-w-lg text-center text-xs text-gray-400 dark:text-dark-500">{{ t('virtualCurrency.earnHint') }}</p>
      </div>

      <div v-else class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <article
          v-for="wallet in wallets"
          :key="wallet.currency_id"
          class="card flex min-h-48 flex-col justify-between overflow-hidden border border-gray-200 dark:border-dark-700"
        >
          <div class="flex items-start justify-between gap-4 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
            <div class="flex min-w-0 items-center gap-3">
              <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary-50 text-xl text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
                {{ wallet.currency_symbol || '¤' }}
              </div>
              <div class="min-w-0">
                <h2 class="truncate text-base font-semibold text-gray-900 dark:text-white">{{ wallet.currency_name }}</h2>
                <p class="truncate font-mono text-xs text-gray-500 dark:text-dark-400">{{ wallet.currency_code }}</p>
              </div>
            </div>
            <span class="badge badge-gray tabular-nums">
              {{ wallet.currency_scale === 0 ? t('virtualCurrency.integerUnits') : t('virtualCurrency.precision', { count: wallet.currency_scale }) }}
            </span>
          </div>

          <div class="grid grid-cols-2 gap-3 px-5 py-4">
            <div>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('virtualCurrency.available') }}</p>
              <p class="mt-1 font-mono text-xl font-semibold text-gray-900 dark:text-white">
                {{ formatUnits(wallet.available_units, wallet.currency_scale) }}
              </p>
            </div>
            <div>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('virtualCurrency.reserved') }}</p>
              <p class="mt-1 font-mono text-xl text-gray-700 dark:text-dark-200">
                {{ formatUnits(wallet.reserved_units, wallet.currency_scale) }}
              </p>
            </div>
          </div>

          <div class="flex items-center justify-between gap-3 border-t border-gray-100 px-5 py-3 dark:border-dark-700">
            <div class="min-w-0 text-xs text-gray-500 dark:text-dark-400">
              <span>{{ t('virtualCurrency.groups') }}：</span>
              <span v-if="wallet.group_ids.length" class="font-mono">{{ wallet.group_ids.join(', ') }}</span>
              <span v-else>{{ t('virtualCurrency.noGroups') }}</span>
            </div>
            <button type="button" class="btn btn-ghost btn-sm shrink-0" @click="openLedger(wallet)">
              <Icon name="document" size="sm" />
              {{ t('virtualCurrency.viewLedger') }}
            </button>
          </div>
        </article>
      </div>

      <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('virtualCurrency.earnHint') }}</p>
    </div>

    <BaseDialog
      :show="showLedgerDialog"
      :title="ledgerWallet ? `${ledgerWallet.currency_name} · ${t('virtualCurrency.ledger')}` : t('virtualCurrency.ledger')"
      width="extra-wide"
      @close="showLedgerDialog = false"
    >
      <div class="space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 pb-4 dark:border-dark-700">
          <div>
            <p class="font-mono text-sm text-gray-600 dark:text-dark-300">{{ ledgerWallet?.currency_code }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('virtualCurrency.available') }}：{{ ledgerWallet ? formatUnits(ledgerWallet.available_units, ledgerWallet.currency_scale) : '—' }}
            </p>
          </div>
          <Icon name="document" size="lg" class="text-gray-400 dark:text-dark-500" />
        </div>

        <div v-if="ledgerLoading && ledger.length === 0" class="flex items-center justify-center py-12">
          <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
        </div>
        <div v-else-if="ledger.length === 0" class="empty-state py-12">
          <Icon name="inbox" size="xl" class="text-gray-400 dark:text-dark-500" />
          <p class="mt-3 text-sm text-gray-500 dark:text-dark-400">{{ t('virtualCurrency.noLedger') }}</p>
        </div>
        <div v-else class="relative overflow-x-auto border border-gray-200 dark:border-dark-700">
          <div v-if="ledgerLoading" class="pointer-events-none absolute inset-x-0 top-0 h-0.5 overflow-hidden bg-primary-100 dark:bg-primary-900/30">
            <div class="h-full w-1/3 animate-pulse bg-primary-500" />
          </div>
          <table class="w-full min-w-[820px] text-left text-sm">
            <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-dark-400">
              <tr>
                <th class="px-3 py-3">{{ t('virtualCurrency.createdAt') }}</th>
                <th class="px-3 py-3">{{ t('virtualCurrency.type') }}</th>
                <th class="px-3 py-3">{{ t('virtualCurrency.amount') }}</th>
                <th class="px-3 py-3">{{ t('virtualCurrency.balanceAfter') }}</th>
                <th class="px-3 py-3">{{ t('virtualCurrency.source') }}</th>
                <th class="px-3 py-3">{{ t('virtualCurrency.reason') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
              <tr v-for="entry in ledger" :key="entry.id" class="text-gray-700 dark:text-dark-200">
                <td class="whitespace-nowrap px-3 py-3 text-xs text-gray-500 dark:text-dark-400">{{ formatDateTime(entry.created_at) }}</td>
                <td class="px-3 py-3"><span class="badge badge-gray">{{ entry.entry_type }}</span></td>
                <td :class="['px-3 py-3 font-mono font-semibold', entry.delta_units >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400']">
                  {{ entry.delta_units >= 0 ? '+' : '' }}{{ formatUnits(entry.delta_units, entry.currency_scale) }}
                </td>
                <td class="px-3 py-3 font-mono">{{ formatUnits(entry.available_after_units, entry.currency_scale) }}</td>
                <td class="px-3 py-3 text-xs"><span class="font-mono">{{ entry.source_type }}</span><span v-if="entry.source_id" class="block text-gray-500 dark:text-dark-400">{{ entry.source_id }}</span></td>
                <td class="max-w-xs truncate px-3 py-3 text-xs text-gray-500 dark:text-dark-400" :title="entry.reason">{{ entry.reason || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <Pagination
          v-if="ledgerTotal > ledgerPageSize"
          :page="ledgerPage"
          :total="ledgerTotal"
          :page-size="ledgerPageSize"
          @update:page="handleLedgerPageChange"
        />
      </div>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import virtualCurrencyAPI, { type PaginatedVirtualCurrencyLedger, type VirtualCurrencyLedgerEntry, type VirtualCurrencyWallet } from '@/api/virtualCurrency'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()

const wallets = ref<VirtualCurrencyWallet[]>([])
const loading = ref(true)
const loadError = ref<string | null>(null)
const showLedgerDialog = ref(false)
const ledgerWallet = ref<VirtualCurrencyWallet | null>(null)
const ledger = ref<VirtualCurrencyLedgerEntry[]>([])
const ledgerLoading = ref(false)
const ledgerPage = ref(1)
const ledgerPageSize = ref(20)
const ledgerTotal = ref(0)
let walletsRequestID = 0
let ledgerRequestID = 0

const loadWallets = async () => {
  const requestID = ++walletsRequestID
  loading.value = true
  loadError.value = null
  try {
    const result = await virtualCurrencyAPI.listWallets()
    if (requestID === walletsRequestID) wallets.value = result
  } catch (error: unknown) {
    if (requestID === walletsRequestID) {
      loadError.value = error instanceof Error ? error.message : t('virtualCurrency.loadFailed')
    }
  } finally {
    if (requestID === walletsRequestID) loading.value = false
  }
}

const loadLedger = async () => {
  if (!ledgerWallet.value) return
  const requestID = ++ledgerRequestID
  ledgerLoading.value = true
  try {
    const result: PaginatedVirtualCurrencyLedger = await virtualCurrencyAPI.listLedger(
      ledgerWallet.value.currency_code,
      ledgerPage.value,
      ledgerPageSize.value
    )
    if (requestID === ledgerRequestID) {
      ledger.value = result.items
      ledgerTotal.value = result.total
    }
  } catch (error: unknown) {
    if (requestID === ledgerRequestID) appStore.showError(error instanceof Error ? error.message : t('virtualCurrency.ledgerFailed'))
  } finally {
    if (requestID === ledgerRequestID) ledgerLoading.value = false
  }
}

const openLedger = (wallet: VirtualCurrencyWallet) => {
  ledgerWallet.value = wallet
  ledger.value = []
  ledgerTotal.value = 0
  ledgerPage.value = 1
  showLedgerDialog.value = true
  void loadLedger()
}

const handleLedgerPageChange = (page: number) => {
  ledgerPage.value = page
  void loadLedger()
}

const formatUnits = (units: number, scale: number) => {
  if (!scale) return String(units)
  const negative = units < 0
  const magnitude = Math.abs(units)
  const base = 10 ** scale
  const whole = Math.floor(magnitude / base)
  const fraction = String(magnitude % base).padStart(scale, '0')
  return `${negative ? '-' : ''}${whole}.${fraction}`
}

onMounted(() => { void loadWallets() })
</script>
