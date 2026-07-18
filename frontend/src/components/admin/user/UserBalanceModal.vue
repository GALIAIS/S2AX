<template>
  <BaseDialog
    :show="show"
    :title="operation === 'add' ? t('admin.users.deposit') : t('admin.users.withdraw')"
    width="narrow"
    @close="emit('close')"
  >
    <form v-if="user" id="balance-form" class="space-y-5" @submit.prevent="handleSubmit">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100">
          <span class="text-lg font-medium text-primary-700">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div class="min-w-0 flex-1">
          <p class="truncate font-medium text-gray-900 dark:text-gray-100">{{ user.email }}</p>
          <p class="text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.users.currentBalance') }}:
            <span class="font-mono">{{ currentAssetBalance }}</span>
          </p>
        </div>
      </div>

      <div>
        <label class="input-label">{{ t('admin.users.assetType') }}</label>
        <Select v-model="form.assetType" :options="assetTypeOptions" />
      </div>

      <div v-if="form.assetType === 'virtual_currency'">
        <label class="input-label">{{ t('admin.users.virtualCurrency') }}</label>
        <Select
          v-model="form.currencyCode"
          :options="currencyOptions"
          :placeholder="t('admin.users.selectVirtualCurrency')"
          :empty-text="t('admin.users.noVirtualCurrencies')"
          :loading="currenciesLoading"
          searchable
        />
      </div>

      <div>
        <label class="input-label">
          {{ operation === 'add' ? t('admin.users.depositAmount') : t('admin.users.withdrawAmount') }}
        </label>
        <div class="relative flex gap-2">
          <div class="relative flex-1">
            <div class="absolute left-3 top-1/2 -translate-y-1/2 font-medium text-gray-500">
              {{ amountPrefix }}
            </div>
            <input
              v-model="form.amount"
              type="number"
              :step="amountStep"
              min="0"
              required
              class="input pl-10"
            >
          </div>
          <button
            v-if="operation === 'subtract'"
            type="button"
            class="btn btn-secondary whitespace-nowrap"
            :disabled="virtualBalanceLoading"
            @click="fillAll"
          >
            {{ t('admin.users.withdrawAll') }}
          </button>
        </div>
        <p v-if="selectedCurrency" class="input-hint">
          {{ t('admin.users.virtualAmountHint', { scale: selectedCurrency.scale }) }}
        </p>
      </div>

      <div>
        <label class="input-label">{{ t('admin.users.notes') }}</label>
        <textarea
          v-model="form.notes"
          rows="3"
          class="input"
          :placeholder="operation === 'add' ? t('admin.users.depositNotesPlaceholder') : t('admin.users.withdrawNotesPlaceholder')"
        />
      </div>

      <div v-if="hasValidAmount" class="rounded-xl border border-blue-200 bg-blue-50 p-4 dark:border-blue-800 dark:bg-blue-950">
        <div class="flex items-center justify-between gap-4 text-sm">
          <span class="text-gray-700 dark:text-gray-300">{{ t('admin.users.newBalance') }}:</span>
          <span class="font-mono font-bold text-gray-900 dark:text-gray-100">{{ newAssetBalance }}</span>
        </div>
      </div>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button
          type="submit"
          form="balance-form"
          class="btn"
          :class="operation === 'add' ? 'bg-emerald-600 text-white' : 'btn-danger'"
          :disabled="submitting || virtualBalanceLoading || !hasValidAmount"
        >
          {{ submitting ? t('common.saving') : t('common.confirm') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { VirtualCurrency } from '@/api/admin/virtualCurrencies'
import type { AdminUser } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'

type AssetType = 'balance' | 'virtual_currency'

const props = defineProps<{
  show: boolean
  user: AdminUser | null
  operation: 'add' | 'subtract'
}>()

const emit = defineEmits<{
  close: []
  success: []
}>()

const { t } = useI18n()
const appStore = useAppStore()
const submitting = ref(false)
const currenciesLoading = ref(false)
const virtualBalanceLoading = ref(false)
const currencies = ref<VirtualCurrency[]>([])
const currentVirtualUnits = ref(0)
let virtualBalanceRequestID = 0

const form = reactive<{
  assetType: AssetType
  currencyCode: string | null
  amount: string | number
  notes: string
}>({
  assetType: 'balance',
  currencyCode: null,
  amount: '',
  notes: ''
})

const selectedCurrency = computed(() => currencies.value.find((currency) => currency.code === form.currencyCode) ?? null)
const assetTypeOptions = computed(() => [
  { value: 'balance', label: t('admin.users.accountBalance') },
  {
    value: 'virtual_currency',
    label: t('admin.users.virtualCurrency'),
    disabled: !currenciesLoading.value && currencies.value.length === 0
  }
])
const currencyOptions = computed(() => currencies.value.map((currency) => ({
  value: currency.code,
  label: `${currency.symbol || '¤'} ${currency.name} (${currency.code.toUpperCase()})`
})))
const amountPrefix = computed(() => form.assetType === 'balance' ? '$' : (selectedCurrency.value?.symbol || '¤'))
const amountStep = computed(() => {
  const scale = form.assetType === 'balance' ? 8 : (selectedCurrency.value?.scale ?? 0)
  return scale === 0 ? '1' : `0.${'0'.repeat(scale - 1)}1`
})

const formatBalance = (value: number) => {
  if (value === 0) return '0.00'
  const formatted = value.toFixed(8).replace(/\.?0+$/, '')
  const parts = formatted.split('.')
  if (parts.length === 1) return `${formatted}.00`
  return parts[1].length === 1 ? `${formatted}0` : formatted
}

const formatUnits = (units: number, scale: number) => {
  if (scale === 0) return String(units)
  const negative = units < 0
  const magnitude = Math.abs(units)
  const base = 10 ** scale
  const whole = Math.floor(magnitude / base)
  const fraction = String(magnitude % base).padStart(scale, '0')
  return `${negative ? '-' : ''}${whole}.${fraction}`
}

const parsedVirtualAmountUnits = computed(() => {
  const currency = selectedCurrency.value
  const raw = String(form.amount).trim()
  if (!currency || !raw) return null
  const pattern = currency.scale === 0
    ? /^\d+$/
    : new RegExp(`^\\d+(?:\\.\\d{0,${currency.scale}})?$`)
  if (!pattern.test(raw)) return null
  const [whole, fraction = ''] = raw.split('.')
  const units = Number(whole) * (10 ** currency.scale) + Number(fraction.padEnd(currency.scale, '0'))
  return Number.isSafeInteger(units) && units > 0 ? units : null
})

const parsedBalanceAmount = computed(() => {
  const value = Number(form.amount)
  return Number.isFinite(value) && value > 0 ? value : null
})

const hasValidAmount = computed(() => form.assetType === 'balance'
  ? parsedBalanceAmount.value !== null
  : parsedVirtualAmountUnits.value !== null && selectedCurrency.value !== null)

const currentAssetBalance = computed(() => {
  if (form.assetType === 'balance') return `$${formatBalance(props.user?.balance ?? 0)}`
  if (virtualBalanceLoading.value) return t('common.loading')
  const currency = selectedCurrency.value
  if (!currency) return '—'
  return `${currency.symbol || '¤'} ${formatUnits(currentVirtualUnits.value, currency.scale)}`
})

const newAssetBalance = computed(() => {
  if (form.assetType === 'balance') {
    const amount = parsedBalanceAmount.value ?? 0
    const current = props.user?.balance ?? 0
    const result = props.operation === 'add' ? current + amount : current - amount
    return `$${formatBalance(Math.abs(result) < 1e-10 ? 0 : result)}`
  }
  const currency = selectedCurrency.value
  const amount = parsedVirtualAmountUnits.value ?? 0
  if (!currency) return '—'
  const result = props.operation === 'add' ? currentVirtualUnits.value + amount : currentVirtualUnits.value - amount
  return `${currency.symbol || '¤'} ${formatUnits(result, currency.scale)}`
})

const resetForm = () => {
  virtualBalanceRequestID += 1
  form.assetType = 'balance'
  form.currencyCode = null
  form.amount = ''
  form.notes = ''
  currentVirtualUnits.value = 0
  virtualBalanceLoading.value = false
}

const loadCurrencies = async () => {
  currenciesLoading.value = true
  try {
    currencies.value = await adminAPI.virtualCurrencies.list(false)
    if (form.assetType === 'virtual_currency' && !form.currencyCode) {
      form.currencyCode = currencies.value[0]?.code ?? null
    }
  } catch (error: unknown) {
    currencies.value = []
    appStore.showError(extractApiErrorMessage(error, t('admin.users.virtualCurrencyLoadFailed')))
  } finally {
    currenciesLoading.value = false
  }
}

const loadVirtualBalance = async () => {
  const userID = props.user?.id
  const code = form.currencyCode
  const requestID = ++virtualBalanceRequestID
  currentVirtualUnits.value = 0
  if (!userID || !code) return
  virtualBalanceLoading.value = true
  try {
    const ledger = await adminAPI.virtualCurrencies.userLedger(code, userID, 1, 1)
    if (requestID === virtualBalanceRequestID) {
      currentVirtualUnits.value = ledger.items[0]?.available_after_units ?? 0
    }
  } catch (error: unknown) {
    if (requestID === virtualBalanceRequestID) {
      appStore.showError(extractApiErrorMessage(error, t('admin.users.virtualBalanceLoadFailed')))
    }
  } finally {
    if (requestID === virtualBalanceRequestID) virtualBalanceLoading.value = false
  }
}

const fillAll = () => {
  if (form.assetType === 'balance') {
    form.amount = String(props.user?.balance ?? 0)
    return
  }
  const currency = selectedCurrency.value
  if (currency) form.amount = formatUnits(currentVirtualUnits.value, currency.scale)
}

const handleSubmit = async () => {
  const user = props.user
  if (!user || !hasValidAmount.value) {
    appStore.showError(t('admin.users.amountRequired'))
    return
  }

  if (form.assetType === 'balance') {
    const amount = parsedBalanceAmount.value as number
    if (props.operation === 'subtract' && amount > user.balance) {
      appStore.showError(t('admin.users.insufficientBalance'))
      return
    }
    submitting.value = true
    try {
      await adminAPI.users.updateBalance(user.id, amount, props.operation, form.notes)
      appStore.showSuccess(t('common.success'))
      emit('success')
      emit('close')
    } catch (error: unknown) {
      appStore.showError(extractApiErrorMessage(error, t('common.error')))
    } finally {
      submitting.value = false
    }
    return
  }

  const currency = selectedCurrency.value
  const amountUnits = parsedVirtualAmountUnits.value
  if (!currency || amountUnits === null) return
  if (props.operation === 'subtract' && amountUnits > currentVirtualUnits.value) {
    appStore.showError(t('admin.users.insufficientBalance'))
    return
  }

  submitting.value = true
  try {
    await adminAPI.virtualCurrencies.adjust(currency.code, {
      user_id: user.id,
      amount_units: props.operation === 'add' ? amountUnits : -amountUnits,
      entry_type: props.operation === 'add' ? 'grant' : 'adjustment',
      reason: form.notes.trim() || t(props.operation === 'add'
        ? 'admin.users.virtualDepositReason'
        : 'admin.users.virtualWithdrawReason')
    })
    window.dispatchEvent(new CustomEvent('virtual-currency-wallets-changed'))
    appStore.showSuccess(t('common.success'))
    emit('success')
    emit('close')
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    submitting.value = false
  }
}

watch(() => [props.show, props.user?.id] as const, ([show]) => {
  if (!show) {
    resetForm()
    return
  }
  resetForm()
  void loadCurrencies()
})

watch(() => form.assetType, (assetType) => {
  form.amount = ''
  if (assetType === 'virtual_currency' && !form.currencyCode) {
    form.currencyCode = currencies.value[0]?.code ?? null
  }
})

watch(() => form.currencyCode, () => {
  form.amount = ''
  void loadVirtualBalance()
})
</script>
