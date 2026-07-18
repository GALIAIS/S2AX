<template>
  <AppLayout>
    <TablePageLayout>
      <template #actions>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex flex-wrap items-center gap-2">
            <span class="badge badge-primary">{{ t('admin.virtualCurrency.ledgerInvariant') }}</span>
            <span class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.virtualCurrency.unitHint') }}
            </span>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <button type="button" class="btn btn-secondary" :disabled="loading" @click="loadCurrencies">
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
              <span class="hidden sm:inline">{{ t('common.refresh') }}</span>
            </button>
            <button type="button" class="btn btn-primary" @click="openCreateDialog">
              <Icon name="plus" size="md" />
              {{ t('admin.virtualCurrency.create') }}
            </button>
          </div>
        </div>
      </template>

      <template #filters>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="text-sm text-gray-600 dark:text-dark-300">
            {{ t('admin.virtualCurrency.scopeHint') }}
          </div>
          <label class="flex items-center gap-2 text-sm text-gray-600 dark:text-dark-300">
            <input v-model="includeDisabled" type="checkbox" class="input-checkbox" @change="loadCurrencies" />
            {{ t('admin.virtualCurrency.showDisabled') }}
          </label>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="currencies"
          :loading="loading"
          :error="loadError"
          :aria-label="t('admin.virtualCurrency.title')"
          @retry="loadCurrencies"
        >
          <template #cell-code="{ row }">
            <div class="flex min-w-32 items-center gap-2">
              <span class="text-lg" aria-hidden="true">{{ row.symbol || '¤' }}</span>
              <div class="min-w-0">
                <p class="truncate font-mono text-sm font-semibold text-gray-900 dark:text-gray-100">{{ row.code }}</p>
                <p class="truncate text-xs text-gray-500 dark:text-dark-400">{{ row.name }}</p>
              </div>
            </div>
          </template>

          <template #cell-description="{ row }">
            <span class="block max-w-xs truncate text-sm text-gray-600 dark:text-dark-300" :title="row.description">
              {{ row.description || '—' }}
            </span>
          </template>

          <template #cell-scale="{ row }">
            <span class="text-sm tabular-nums text-gray-700 dark:text-dark-200">
              {{ row.scale === 0 ? t('virtualCurrency.integerUnits') : t('virtualCurrency.precision', { count: row.scale }) }}
            </span>
          </template>

          <template #cell-status="{ row }">
            <span :class="['badge', row.status === 'active' ? 'badge-success' : 'badge-gray']">
              {{ row.status === 'active' ? t('common.active') : t('common.disabled') }}
            </span>
          </template>

          <template #cell-updated_at="{ row }">
            <span class="text-xs text-gray-500 dark:text-dark-400">{{ formatDateTime(row.updated_at) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <RowActionMenu
              :items="currencyActionItems(row)"
              :aria-label="t('admin.virtualCurrency.rowActions', { code: row.code })"
              @select="(key) => handleCurrencyAction(key, row)"
            />
          </template>
        </DataTable>
      </template>
    </TablePageLayout>

    <BaseDialog
      :show="showCreateDialog"
      :title="t('admin.virtualCurrency.create')"
      width="wide"
      @close="showCreateDialog = false"
    >
      <form id="create-virtual-currency-form" class="grid gap-4 md:grid-cols-2" @submit.prevent="handleCreate">
        <div>
          <label class="input-label">{{ t('admin.virtualCurrency.code') }}</label>
          <input v-model="createForm.code" class="input font-mono" required maxlength="32" placeholder="gold" />
          <p class="input-hint">{{ t('admin.virtualCurrency.codeHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.virtualCurrency.name') }}</label>
          <input v-model="createForm.name" class="input" required maxlength="64" :placeholder="t('admin.virtualCurrency.namePlaceholder')" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.virtualCurrency.symbol') }}</label>
          <input v-model="createForm.symbol" class="input" maxlength="16" placeholder="🪙" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.virtualCurrency.scale') }}</label>
          <input v-model.number="createForm.scale" class="input" type="number" min="0" max="8" required />
          <p class="input-hint">{{ t('admin.virtualCurrency.scaleHint') }}</p>
        </div>
        <div class="md:col-span-2">
          <label class="input-label">{{ t('admin.virtualCurrency.descriptionLabel') }}</label>
          <textarea v-model="createForm.description" class="input" rows="2" maxlength="2000" />
        </div>
        <div class="md:col-span-2">
          <label class="input-label">{{ t('admin.virtualCurrency.metadata') }}</label>
          <textarea v-model="createForm.metadata" class="input font-mono text-xs" rows="3" spellcheck="false" />
          <p class="input-hint">{{ t('admin.virtualCurrency.metadataHint') }}</p>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showCreateDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="create-virtual-currency-form" class="btn btn-primary" :disabled="saving">
            {{ saving ? t('common.saving') : t('common.create') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showEditDialog"
      :title="t('admin.virtualCurrency.edit')"
      width="wide"
      @close="showEditDialog = false"
    >
      <form id="edit-virtual-currency-form" class="grid gap-4 md:grid-cols-2" @submit.prevent="handleUpdate">
        <div>
          <label class="input-label">{{ t('admin.virtualCurrency.code') }}</label>
          <input :value="editingCurrency?.code" class="input font-mono opacity-60" disabled />
          <p class="input-hint">{{ t('admin.virtualCurrency.codeImmutable') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.virtualCurrency.name') }}</label>
          <input v-model="editForm.name" class="input" required maxlength="64" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.virtualCurrency.symbol') }}</label>
          <input v-model="editForm.symbol" class="input" maxlength="16" />
        </div>
        <div class="md:col-span-2">
          <label class="input-label">{{ t('admin.virtualCurrency.descriptionLabel') }}</label>
          <textarea v-model="editForm.description" class="input" rows="2" maxlength="2000" />
        </div>
        <div class="md:col-span-2">
          <label class="input-label">{{ t('admin.virtualCurrency.metadata') }}</label>
          <textarea v-model="editForm.metadata" class="input font-mono text-xs" rows="3" spellcheck="false" />
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showEditDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="edit-virtual-currency-form" class="btn btn-primary" :disabled="saving">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showPolicyDialog"
      :title="t('admin.virtualCurrency.groupPoliciesTitle', { code: selectedCurrency?.code || '' })"
      width="extra-wide"
      @close="showPolicyDialog = false"
    >
      <div class="grid gap-6 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
        <form class="space-y-4" @submit.prevent="handleSavePolicy">
          <div>
            <label class="input-label">{{ t('admin.virtualCurrency.group') }}</label>
            <Select
              v-model="policyForm.group_id"
              :options="groupOptions"
              :placeholder="t('admin.virtualCurrency.selectGroup')"
              searchable
            />
          </div>
          <div class="grid gap-3 sm:grid-cols-3">
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="policyForm.enabled" type="checkbox" class="input-checkbox" />
              {{ t('common.enabled') }}
            </label>
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="policyForm.can_earn" type="checkbox" class="input-checkbox" />
              {{ t('admin.virtualCurrency.canEarn') }}
            </label>
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="policyForm.can_spend" type="checkbox" class="input-checkbox" />
              {{ t('admin.virtualCurrency.canSpend') }}
            </label>
          </div>
          <div>
            <label class="input-label">{{ t('admin.virtualCurrency.maxBalance') }}</label>
            <input v-model.number="policyForm.max_balance_units" class="input" type="number" min="1" :placeholder="t('admin.virtualCurrency.unlimited')" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.virtualCurrency.metadata') }}</label>
            <textarea v-model="policyMetadata" class="input font-mono text-xs" rows="2" spellcheck="false" />
          </div>
          <div class="flex justify-end gap-2">
            <button type="button" class="btn btn-secondary" @click="resetPolicyForm">{{ t('common.reset') }}</button>
            <button type="submit" class="btn btn-primary" :disabled="policySaving || !policyForm.group_id">
              {{ policySaving ? t('common.saving') : t('admin.virtualCurrency.savePolicy') }}
            </button>
          </div>
        </form>

        <div class="min-w-0">
          <div v-if="policiesLoading" class="flex items-center justify-center py-12">
            <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
          </div>
          <div v-else-if="policies.length === 0" class="empty-state py-12">
            <Icon name="shield" size="xl" class="text-gray-400 dark:text-dark-500" />
            <p class="mt-3 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.noPolicies') }}</p>
          </div>
          <div v-else class="overflow-x-auto border border-gray-200 dark:border-dark-700">
            <table class="w-full min-w-[560px] text-left text-sm">
              <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-dark-400">
                <tr>
                  <th class="px-3 py-3">{{ t('admin.virtualCurrency.group') }}</th>
                  <th class="px-3 py-3">{{ t('common.status') }}</th>
                  <th class="px-3 py-3">{{ t('admin.virtualCurrency.permissions') }}</th>
                  <th class="px-3 py-3 text-right">{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
                <tr v-for="policy in policies" :key="policy.id" class="text-gray-700 dark:text-dark-200">
                  <td class="px-3 py-3 font-medium">{{ groupName(policy.group_id) }}</td>
                  <td class="px-3 py-3">
                    <span :class="['badge', policy.enabled ? 'badge-success' : 'badge-gray']">
                      {{ policy.enabled ? t('common.enabled') : t('common.disabled') }}
                    </span>
                  </td>
                  <td class="px-3 py-3 text-xs text-gray-500 dark:text-dark-400">
                    {{ policy.can_earn ? t('admin.virtualCurrency.earnShort') : '' }}
                    {{ policy.can_earn && policy.can_spend ? ' · ' : '' }}
                    {{ policy.can_spend ? t('admin.virtualCurrency.spendShort') : '' }}
                    <span v-if="policy.max_balance_units" class="block font-mono">≤ {{ formatUnits(policy.max_balance_units, selectedCurrency?.scale || 0) }}</span>
                  </td>
                  <td class="px-3 py-3 text-right">
                    <button type="button" class="btn btn-ghost btn-sm mr-1" @click="editPolicy(policy)">{{ t('common.edit') }}</button>
                    <button type="button" class="btn btn-ghost btn-sm text-red-600 dark:text-red-400" @click="confirmDeletePolicy(policy)">{{ t('common.delete') }}</button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </BaseDialog>

    <BaseDialog
      :show="showAdjustDialog"
      :title="t('admin.virtualCurrency.adjustTitle', { code: selectedCurrency?.code || '' })"
      width="normal"
      @close="showAdjustDialog = false"
    >
      <form id="virtual-currency-adjust-form" class="space-y-4" @submit.prevent="handleAdjust">
        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.virtualCurrency.user') }}</label>
            <Select
              v-model="adjustForm.user_id"
              :options="userOptions"
              :placeholder="t('admin.virtualCurrency.selectUser')"
              :search-placeholder="t('admin.virtualCurrency.searchUser')"
              :empty-text="t('admin.virtualCurrency.noUsers')"
              :loading="userSearchLoading"
              searchable
              remote
              clearable
              @search="searchUsers"
              @change="handleAdjustUserChange"
            />
          </div>
          <div>
            <label class="input-label">{{ t('admin.virtualCurrency.group') }}</label>
            <Select
              v-model="adjustForm.group_id"
              :options="adjustGroupOptions"
              :placeholder="t('admin.virtualCurrency.selectGroup')"
              :empty-text="t('admin.virtualCurrency.noEligibleGroups')"
              :disabled="adjustPoliciesLoading"
              searchable
            />
          </div>
        </div>
        <div>
          <label class="input-label">{{ t('admin.virtualCurrency.amountUnits') }}</label>
          <input v-model.number="adjustForm.amount_units" class="input font-mono" type="number" required />
          <p class="input-hint">{{ t('admin.virtualCurrency.adjustHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.virtualCurrency.entryType') }}</label>
          <Select v-model="adjustForm.entry_type" :options="entryTypeOptions" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.virtualCurrency.sourceID') }}</label>
          <input v-model="adjustForm.source_id" class="input font-mono" maxlength="128" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.virtualCurrency.reason') }}</label>
          <textarea v-model="adjustForm.reason" class="input" rows="2" maxlength="500" required />
        </div>
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.adminSourceNotice') }}</p>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showAdjustDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="virtual-currency-adjust-form" class="btn btn-primary" :disabled="adjusting">
            {{ adjusting ? t('common.submitting') : t('admin.virtualCurrency.submitAdjustment') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showLedgerDialog"
      :title="t('admin.virtualCurrency.ledgerTitle', { code: selectedCurrency?.code || '' })"
      width="extra-wide"
      @close="showLedgerDialog = false"
    >
      <div class="space-y-4">
        <form class="flex flex-wrap items-end gap-3" @submit.prevent="loadUserLedger">
          <div class="w-full sm:w-96">
            <label class="input-label">{{ t('admin.virtualCurrency.user') }}</label>
            <Select
              v-model="ledgerUserID"
              :options="userOptions"
              :placeholder="t('admin.virtualCurrency.selectUser')"
              :search-placeholder="t('admin.virtualCurrency.searchUser')"
              :empty-text="t('admin.virtualCurrency.noUsers')"
              :loading="userSearchLoading"
              searchable
              remote
              clearable
              @search="searchUsers"
              @change="handleLedgerUserChange"
            />
          </div>
          <button type="submit" class="btn btn-secondary" :disabled="ledgerLoading || !ledgerUserID">
            <Icon name="search" size="sm" />
            {{ t('common.search') }}
          </button>
        </form>
        <div v-if="ledgerLoading" class="flex items-center justify-center py-12">
          <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
        </div>
        <div v-else-if="ledger.length === 0" class="empty-state py-10">
          <Icon name="inbox" size="xl" class="text-gray-400 dark:text-dark-500" />
          <p class="mt-3 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.noLedger') }}</p>
        </div>
        <div v-else class="overflow-x-auto border border-gray-200 dark:border-dark-700">
          <table class="w-full min-w-[900px] text-left text-sm">
            <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-dark-400">
              <tr>
                <th class="px-3 py-3">{{ t('admin.virtualCurrency.createdAt') }}</th>
                <th class="px-3 py-3">{{ t('admin.virtualCurrency.entryType') }}</th>
                <th class="px-3 py-3">{{ t('admin.virtualCurrency.amountUnits') }}</th>
                <th class="px-3 py-3">{{ t('admin.virtualCurrency.balanceAfter') }}</th>
                <th class="px-3 py-3">{{ t('admin.virtualCurrency.source') }}</th>
                <th class="px-3 py-3">{{ t('admin.virtualCurrency.reason') }}</th>
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

    <BaseDialog
      :show="showReconciliationDialog"
      :title="t('admin.virtualCurrency.reconciliationTitle', { code: selectedCurrency?.code || '' })"
      width="wide"
      @close="showReconciliationDialog = false"
    >
      <div v-if="reconciliationLoading" class="flex items-center justify-center py-12">
        <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
      </div>
      <div v-else-if="reconciliation" class="space-y-5">
        <div class="grid gap-3 sm:grid-cols-3">
          <div class="border border-gray-200 p-4 dark:border-dark-700">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.walletCount') }}</p>
            <p class="mt-1 font-mono text-xl font-semibold">{{ reconciliation.wallet_count }}</p>
          </div>
          <div class="border border-gray-200 p-4 dark:border-dark-700">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.ledgerUserCount') }}</p>
            <p class="mt-1 font-mono text-xl font-semibold">{{ reconciliation.ledger_user_count }}</p>
          </div>
          <div class="border border-gray-200 p-4 dark:border-dark-700">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.mismatchCount') }}</p>
            <p class="mt-1 flex items-center gap-2 font-mono text-xl font-semibold">
              {{ reconciliation.mismatch_count }}
              <span :class="['badge', reconciliation.mismatch_count === 0 ? 'badge-success' : 'badge-danger']">
                {{ reconciliation.mismatch_count === 0 ? t('admin.virtualCurrency.consistent') : t('admin.virtualCurrency.reviewRequired') }}
              </span>
            </p>
          </div>
        </div>
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div class="border border-gray-200 p-4 dark:border-dark-700">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.journalCount') }}</p>
            <p class="mt-1 font-mono text-lg font-semibold">{{ reconciliation.accounting.journal_count }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.postingCount', { count: reconciliation.accounting.posting_count }) }}</p>
          </div>
          <div class="border border-gray-200 p-4 dark:border-dark-700">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.circulatingUnits') }}</p>
            <p class="mt-1 font-mono text-lg font-semibold">{{ formatUnits(reconciliation.accounting.wallet_available_units, selectedCurrency?.scale || 0) }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.reservedUnits', { amount: formatUnits(reconciliation.accounting.wallet_reserved_units, selectedCurrency?.scale || 0) }) }}</p>
          </div>
          <div class="border border-gray-200 p-4 dark:border-dark-700">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.issuanceAndSink') }}</p>
            <p class="mt-1 font-mono text-lg font-semibold">{{ formatUnits(reconciliation.accounting.gross_issued_units, selectedCurrency?.scale || 0) }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.netSinkUnits', { amount: formatUnits(reconciliation.accounting.net_sink_units, selectedCurrency?.scale || 0) }) }}</p>
          </div>
          <div class="border border-gray-200 p-4 dark:border-dark-700">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.ledgerHealth') }}</p>
            <p class="mt-1 flex items-center gap-2 font-mono text-lg font-semibold">
              {{ reconciliation.accounting.invalid_journal_count }}
              <span :class="['badge', reconciliation.accounting.invalid_journal_count === 0 && reconciliation.accounting.projection_delta_units === '0' && reconciliation.accounting.conservation_delta_units === '0' ? 'badge-success' : 'badge-danger']">
                {{ reconciliation.accounting.invalid_journal_count === 0 && reconciliation.accounting.projection_delta_units === '0' && reconciliation.accounting.conservation_delta_units === '0' ? t('admin.virtualCurrency.consistent') : t('admin.virtualCurrency.reviewRequired') }}
              </span>
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.accountingDeltas', { projection: reconciliation.accounting.projection_delta_units, conservation: reconciliation.accounting.conservation_delta_units }) }}</p>
          </div>
        </div>
        <div v-if="reconciliation.mismatches.length" class="overflow-x-auto border border-red-200 dark:border-red-900/60">
          <table class="w-full min-w-[720px] text-left text-sm">
            <thead class="bg-red-50 text-xs text-red-800 dark:bg-red-950/30 dark:text-red-200">
              <tr>
                <th class="px-3 py-3">{{ t('admin.virtualCurrency.userID') }}</th>
                <th class="px-3 py-3">{{ t('admin.virtualCurrency.walletBalance') }}</th>
                <th class="px-3 py-3">{{ t('admin.virtualCurrency.ledgerSnapshot') }}</th>
                <th class="px-3 py-3">{{ t('admin.virtualCurrency.recordState') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-red-100 dark:divide-red-900/40">
              <tr v-for="item in reconciliation.mismatches" :key="item.user_id" class="text-gray-700 dark:text-dark-200">
                <td class="px-3 py-3 font-mono">{{ item.user_id }}</td>
                <td class="px-3 py-3 font-mono">{{ formatUnits(item.wallet_available_units, selectedCurrency?.scale || 0) }} / {{ formatUnits(item.wallet_reserved_units, selectedCurrency?.scale || 0) }}</td>
                <td class="px-3 py-3 font-mono">{{ formatUnits(item.ledger_available_units, selectedCurrency?.scale || 0) }} / {{ formatUnits(item.ledger_reserved_units, selectedCurrency?.scale || 0) }}</td>
                <td class="px-3 py-3 text-xs">
                  {{ item.wallet_exists ? '' : t('admin.virtualCurrency.walletMissing') }}
                  {{ !item.wallet_exists && !item.ledger_snapshot_found ? ' · ' : '' }}
                  {{ item.ledger_snapshot_found ? '' : t('admin.virtualCurrency.ledgerMissing') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-800 dark:border-emerald-900/60 dark:bg-emerald-950/20 dark:text-emerald-200">
          {{ t('admin.virtualCurrency.reconciliationPassed') }}
        </p>
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.virtualCurrency.reconciliationReadOnly') }}</p>
      </div>
    </BaseDialog>

    <ConfirmDialog
      :show="showExpireHoldsDialog"
      :title="t('admin.virtualCurrency.expireHoldsTitle')"
      :message="t('admin.virtualCurrency.expireHoldsMessage', { code: currencyToExpire?.code || '' })"
      @confirm="handleExpireHolds"
      @cancel="showExpireHoldsDialog = false"
    />

    <ConfirmDialog
      :show="showEnableForAllUsersDialog"
      :title="t('admin.virtualCurrency.enableForAllUsersTitle')"
      :message="t('admin.virtualCurrency.enableForAllUsersMessage', { code: currencyToEnableForAllUsers?.code || '' })"
      :confirm-text="t('admin.virtualCurrency.enableForAllUsersConfirm')"
      @confirm="handleEnableForAllUsers"
      @cancel="closeEnableForAllUsersDialog"
    />

    <ConfirmDialog
      :show="showDeletePolicyDialog"
      :title="t('admin.virtualCurrency.deletePolicyTitle')"
      :message="t('admin.virtualCurrency.deletePolicyMessage', { group: groupName(policyToDelete?.group_id || 0) })"
      :danger="true"
      @confirm="handleDeletePolicy"
      @cancel="showDeletePolicyDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type {
  VirtualCurrency,
  VirtualCurrencyGroupPolicy,
  VirtualCurrencyLedgerEntry,
  VirtualCurrencyReconciliationReport
} from '@/api/admin/virtualCurrencies'
import type { AdminGroup, AdminUser } from '@/types'
import { formatDateTime } from '@/utils/format'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import RowActionMenu, { type RowActionMenuItem } from '@/components/common/RowActionMenu.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'

const { t } = useI18n()
const appStore = useAppStore()

const currencies = ref<VirtualCurrency[]>([])
const loading = ref(true)
const loadError = ref<string | null>(null)
const includeDisabled = ref(true)
const saving = ref(false)
const showCreateDialog = ref(false)
const showEditDialog = ref(false)
const showPolicyDialog = ref(false)
const showAdjustDialog = ref(false)
const showLedgerDialog = ref(false)
const showReconciliationDialog = ref(false)
const showExpireHoldsDialog = ref(false)
const showEnableForAllUsersDialog = ref(false)
const editingCurrency = ref<VirtualCurrency | null>(null)
const selectedCurrency = ref<VirtualCurrency | null>(null)
const currencyToExpire = ref<VirtualCurrency | null>(null)
const currencyToEnableForAllUsers = ref<VirtualCurrency | null>(null)
const reconciliation = ref<VirtualCurrencyReconciliationReport | null>(null)
const reconciliationLoading = ref(false)

const createForm = reactive({
  code: '',
  name: '',
  symbol: '',
  description: '',
  scale: 0,
  metadata: '{}'
})

const editForm = reactive({ name: '', symbol: '', description: '', metadata: '{}' })

const groups = ref<AdminGroup[]>([])
const policies = ref<VirtualCurrencyGroupPolicy[]>([])
const policiesLoading = ref(false)
const policySaving = ref(false)
const policyMetadata = ref('{}')
const policyForm = reactive({
  group_id: null as number | null,
  enabled: true,
  can_earn: true,
  can_spend: true,
  max_balance_units: null as number | null
})
const policyToDelete = ref<VirtualCurrencyGroupPolicy | null>(null)
const showDeletePolicyDialog = ref(false)

const adjustForm = reactive({
  user_id: null as number | null,
  group_id: null as number | null,
  amount_units: 0,
  entry_type: 'grant',
  source_id: '',
  reason: ''
})
const adjusting = ref(false)
const adjustPoliciesLoading = ref(false)

const userSearchResults = ref<AdminUser[]>([])
const userSearchLoading = ref(false)
const adjustSelectedUser = ref<AdminUser | null>(null)
const ledgerSelectedUser = ref<AdminUser | null>(null)
let userSearchTimer: ReturnType<typeof setTimeout> | null = null
let userSearchController: AbortController | null = null
let userSearchRequestID = 0

const ledgerUserID = ref<number | null>(null)
const ledger = ref<VirtualCurrencyLedgerEntry[]>([])
const ledgerLoading = ref(false)
const ledgerPage = ref(1)
const ledgerPageSize = ref(20)
const ledgerTotal = ref(0)

const columns = computed<Column[]>(() => [
  { key: 'code', label: t('admin.virtualCurrency.code'), sortable: true },
  { key: 'description', label: t('admin.virtualCurrency.descriptionLabel') },
  { key: 'scale', label: t('admin.virtualCurrency.scale') },
  { key: 'status', label: t('common.status') },
  { key: 'updated_at', label: t('admin.virtualCurrency.updatedAt'), sortable: true },
  { key: 'actions', label: t('common.actions') }
])

const groupOptions = computed(() => groups.value.map((group) => ({
  value: group.id,
  label: `${group.name} (#${group.id})`
})))

const adjustGroupOptions = computed(() => policies.value
  .filter((policy) => policy.enabled)
  .map((policy) => ({
    value: policy.group_id,
    label: `${groupName(policy.group_id)} (#${policy.group_id})`,
    disabled: adjustForm.amount_units > 0 && !policy.can_earn
  })))

const userOptions = computed(() => {
  const users = new Map<number, AdminUser>()
  for (const user of [adjustSelectedUser.value, ledgerSelectedUser.value, ...userSearchResults.value]) {
    if (user) users.set(user.id, user)
  }
  return Array.from(users.values()).map((user) => ({
    value: user.id,
    label: `${user.username ? `${user.username} · ` : ''}${user.email} (#${user.id})${user.status === 'disabled' ? ` · ${t('common.disabled')}` : ''}`,
    description: `${user.username || ''} ${user.email}`.trim()
  }))
})

const entryTypeOptions = computed(() => [
  { value: 'grant', label: t('admin.virtualCurrency.entryTypes.grant'), disabled: adjustForm.amount_units < 0 },
  { value: 'adjustment', label: t('admin.virtualCurrency.entryTypes.adjustment') }
])

const currencyActionItems = (currency: VirtualCurrency): RowActionMenuItem[] => [
  { key: 'policies', label: t('admin.virtualCurrency.manageGroups'), icon: 'shield' },
  {
    key: 'enable-all',
    label: t('admin.virtualCurrency.enableForAllUsers'),
    icon: 'checkCircle',
    disabled: currency.status !== 'active',
    title: currency.status === 'active' ? t('admin.virtualCurrency.enableForAllUsersHint') : t('admin.virtualCurrency.enableFirst')
  },
  { key: 'adjust', label: t('admin.virtualCurrency.adjust'), icon: 'dollar' },
  { key: 'ledger', label: t('admin.virtualCurrency.userLedger'), icon: 'eye' },
  { key: 'reconcile', label: t('admin.virtualCurrency.reconcile'), icon: 'checkCircle' },
  { key: 'expire', label: t('admin.virtualCurrency.expireHolds'), icon: 'refresh' },
  { key: 'edit', label: t('common.edit'), icon: 'edit' },
  {
    key: 'toggle',
    label: currency.status === 'active' ? t('admin.virtualCurrency.disable') : t('admin.virtualCurrency.enable'),
    icon: currency.status === 'active' ? 'clock' : 'checkCircle',
    tone: currency.status === 'active' ? 'warning' : 'default'
  }
]

const metadataObject = (value: string): Record<string, unknown> => {
  const parsed: unknown = JSON.parse(value || '{}')
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error(t('admin.virtualCurrency.metadataInvalid'))
  return parsed as Record<string, unknown>
}

let currenciesRequestID = 0
const loadCurrencies = async () => {
	const requestID = ++currenciesRequestID
	loadError.value = null
	loading.value = true
	try {
		const result = await adminAPI.virtualCurrencies.list(includeDisabled.value)
		if (requestID === currenciesRequestID) currencies.value = result
	} catch (error: unknown) {
		if (requestID === currenciesRequestID) {
			loadError.value = error instanceof Error ? error.message : t('admin.virtualCurrency.loadFailed')
		}
	} finally {
		if (requestID === currenciesRequestID) loading.value = false
	}
}

const openCreateDialog = () => {
  Object.assign(createForm, { code: '', name: '', symbol: '', description: '', scale: 0, metadata: '{}' })
  showCreateDialog.value = true
}

const handleCreate = async () => {
  try {
    saving.value = true
    await adminAPI.virtualCurrencies.create({
      code: createForm.code.trim(),
      name: createForm.name.trim(),
      symbol: createForm.symbol.trim(),
      description: createForm.description.trim(),
      scale: Number(createForm.scale),
      metadata: metadataObject(createForm.metadata)
    })
    showCreateDialog.value = false
    appStore.showSuccess(t('admin.virtualCurrency.created'))
    await loadCurrencies()
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.saveFailed'))
  } finally {
    saving.value = false
  }
}

const openEditDialog = (currency: VirtualCurrency) => {
  editingCurrency.value = currency
  Object.assign(editForm, {
    name: currency.name,
    symbol: currency.symbol,
    description: currency.description,
    metadata: JSON.stringify(currency.metadata || {}, null, 2)
  })
  showEditDialog.value = true
}

const handleUpdate = async () => {
  if (!editingCurrency.value) return
  try {
    saving.value = true
    const updated = await adminAPI.virtualCurrencies.update(editingCurrency.value.id, {
      name: editForm.name.trim(),
      symbol: editForm.symbol.trim(),
      description: editForm.description.trim(),
      metadata: metadataObject(editForm.metadata)
    })
    const index = currencies.value.findIndex((item) => item.id === updated.id)
    if (index >= 0) currencies.value[index] = updated
    showEditDialog.value = false
    appStore.showSuccess(t('admin.virtualCurrency.updated'))
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.saveFailed'))
  } finally {
    saving.value = false
  }
}

const handleStatus = async (currency: VirtualCurrency) => {
  const nextStatus = currency.status === 'active' ? 'disabled' : 'active'
  const previousStatus = currency.status
  currency.status = nextStatus
  try {
    const updated = await adminAPI.virtualCurrencies.setStatus(currency.id, nextStatus)
    Object.assign(currency, updated)
    appStore.showSuccess(t('admin.virtualCurrency.statusUpdated'))
  } catch (error: unknown) {
    currency.status = previousStatus
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.saveFailed'))
  }
}

const handleCurrencyAction = (key: string, currency: VirtualCurrency) => {
  if (key === 'policies') void openPolicyDialog(currency)
  else if (key === 'enable-all') openEnableForAllUsersDialog(currency)
  else if (key === 'adjust') void openAdjustDialog(currency)
  else if (key === 'ledger') openLedgerDialog(currency)
  else if (key === 'reconcile') void openReconciliationDialog(currency)
  else if (key === 'expire') openExpireHoldsDialog(currency)
  else if (key === 'edit') openEditDialog(currency)
  else if (key === 'toggle') void handleStatus(currency)
}

const openEnableForAllUsersDialog = (currency: VirtualCurrency) => {
  currencyToEnableForAllUsers.value = currency
  showEnableForAllUsersDialog.value = true
}

const closeEnableForAllUsersDialog = () => {
  showEnableForAllUsersDialog.value = false
  currencyToEnableForAllUsers.value = null
}

const handleEnableForAllUsers = async () => {
  if (!currencyToEnableForAllUsers.value) return
  const currency = currencyToEnableForAllUsers.value
  try {
    const result = await adminAPI.virtualCurrencies.enableForAllUsers(currency.id)
    if (result.group_count > 0) {
      appStore.showSuccess(t('admin.virtualCurrency.enableForAllUsersSuccess', { count: result.group_count }))
      window.dispatchEvent(new Event('virtual-currency-wallets-changed'))
    } else {
      appStore.showWarning(t('admin.virtualCurrency.enableForAllUsersNoGroups'))
    }
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.enableForAllUsersFailed'))
  } finally {
    closeEnableForAllUsersDialog()
  }
}

const groupName = (groupID: number) => groups.value.find((group) => group.id === groupID)?.name || `#${groupID}`

const resetPolicyForm = () => {
  Object.assign(policyForm, { group_id: null, enabled: true, can_earn: true, can_spend: true, max_balance_units: null })
  policyMetadata.value = '{}'
}

const openPolicyDialog = async (currency: VirtualCurrency) => {
  selectedCurrency.value = currency
  resetPolicyForm()
  showPolicyDialog.value = true
  policiesLoading.value = true
  try {
    const [loadedGroups, loadedPolicies] = await Promise.all([
      adminAPI.groups.getAll(),
      adminAPI.virtualCurrencies.listGroups(currency.id)
    ])
    groups.value = loadedGroups
    policies.value = loadedPolicies
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.loadFailed'))
  } finally {
    policiesLoading.value = false
  }
}

const editPolicy = (policy: VirtualCurrencyGroupPolicy) => {
  Object.assign(policyForm, {
    group_id: policy.group_id,
    enabled: policy.enabled,
    can_earn: policy.can_earn,
    can_spend: policy.can_spend,
    max_balance_units: policy.max_balance_units ?? null
  })
  policyMetadata.value = JSON.stringify(policy.metadata || {}, null, 2)
}

const handleSavePolicy = async () => {
  if (!selectedCurrency.value || !policyForm.group_id) return
  try {
    policySaving.value = true
    const saved = await adminAPI.virtualCurrencies.upsertGroup(selectedCurrency.value.id, policyForm.group_id, {
      enabled: policyForm.enabled,
      can_earn: policyForm.can_earn,
      can_spend: policyForm.can_spend,
      max_balance_units: policyForm.max_balance_units || null,
      metadata: metadataObject(policyMetadata.value)
    })
    const index = policies.value.findIndex((item) => item.group_id === saved.group_id)
    if (index >= 0) policies.value[index] = saved
    else policies.value.push(saved)
    appStore.showSuccess(t('admin.virtualCurrency.policySaved'))
    resetPolicyForm()
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.saveFailed'))
  } finally {
    policySaving.value = false
  }
}

const confirmDeletePolicy = (policy: VirtualCurrencyGroupPolicy) => {
  policyToDelete.value = policy
  showDeletePolicyDialog.value = true
}

const handleDeletePolicy = async () => {
	if (!selectedCurrency.value || !policyToDelete.value) return
	const groupID = policyToDelete.value.group_id
	try {
		await adminAPI.virtualCurrencies.deleteGroup(selectedCurrency.value.id, groupID)
		policies.value = policies.value.filter((item) => item.group_id !== groupID)
    appStore.showSuccess(t('admin.virtualCurrency.policyDeleted'))
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.deleteFailed'))
  } finally {
    showDeletePolicyDialog.value = false
    policyToDelete.value = null
  }
}

type UserSelectValue = string | number | boolean | null

const findUserByID = (value: UserSelectValue) => {
  const userID = typeof value === 'number' ? value : Number(value)
  if (!Number.isInteger(userID) || userID <= 0) return null
  return userSearchResults.value.find((user) => user.id === userID)
    || (adjustSelectedUser.value?.id === userID ? adjustSelectedUser.value : null)
    || (ledgerSelectedUser.value?.id === userID ? ledgerSelectedUser.value : null)
}

const handleAdjustUserChange = (value: UserSelectValue) => {
  adjustSelectedUser.value = findUserByID(value)
}

const handleLedgerUserChange = (value: UserSelectValue) => {
  ledgerSelectedUser.value = findUserByID(value)
  ledger.value = []
  ledgerTotal.value = 0
  ledgerPage.value = 1
}

const resetUserSearch = () => {
  if (userSearchTimer) clearTimeout(userSearchTimer)
  userSearchTimer = null
  userSearchController?.abort()
  userSearchController = null
  userSearchRequestID += 1
  userSearchResults.value = []
  userSearchLoading.value = false
}

const searchUsers = (rawQuery: string) => {
  if (userSearchTimer) clearTimeout(userSearchTimer)
  userSearchController?.abort()
  userSearchController = null

  const requestID = ++userSearchRequestID
  const query = rawQuery.trim()
  userSearchLoading.value = true
  userSearchResults.value = []
  userSearchTimer = setTimeout(async () => {
    userSearchTimer = null
    const controller = new AbortController()
    userSearchController = controller
    try {
      const result = await adminAPI.users.list(
        1,
        10,
        { search: query || undefined, include_subscriptions: true },
        { signal: controller.signal }
      )
      if (requestID === userSearchRequestID) userSearchResults.value = result.items
    } catch {
      if (requestID === userSearchRequestID) userSearchResults.value = []
    } finally {
      if (requestID === userSearchRequestID) userSearchLoading.value = false
      if (userSearchController === controller) userSearchController = null
    }
  }, query ? 250 : 0)
}

const openAdjustDialog = async (currency: VirtualCurrency) => {
  selectedCurrency.value = currency
  resetUserSearch()
  adjustSelectedUser.value = null
  Object.assign(adjustForm, { user_id: null, group_id: null, amount_units: 0, entry_type: 'grant', source_id: '', reason: '' })
  groups.value = []
  policies.value = []
  showAdjustDialog.value = true
  adjustPoliciesLoading.value = true
  try {
    const [loadedGroups, loadedPolicies] = await Promise.all([
      adminAPI.groups.getAll(),
      adminAPI.virtualCurrencies.listGroups(currency.id)
    ])
    groups.value = loadedGroups
    policies.value = loadedPolicies
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.loadFailed'))
  } finally {
    adjustPoliciesLoading.value = false
  }
}

const handleAdjust = async () => {
  if (!selectedCurrency.value || !adjustForm.user_id || !adjustForm.group_id || !adjustForm.amount_units || !adjustForm.reason.trim()) return
  try {
    adjusting.value = true
    const entry = await adminAPI.virtualCurrencies.adjust(selectedCurrency.value.code, {
      user_id: adjustForm.user_id,
      group_id: adjustForm.group_id,
      amount_units: adjustForm.amount_units,
      entry_type: adjustForm.entry_type,
      source_id: adjustForm.source_id.trim(),
      reason: adjustForm.reason.trim()
    })
    showAdjustDialog.value = false
    appStore.showSuccess(t('admin.virtualCurrency.adjustedBalance', {
      balance: formatUnits(entry.available_after_units, entry.currency_scale)
    }))
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.adjustFailed'))
  } finally {
    adjusting.value = false
  }
}

const openLedgerDialog = (currency: VirtualCurrency) => {
  selectedCurrency.value = currency
  resetUserSearch()
  ledgerSelectedUser.value = null
  ledgerUserID.value = null
  ledger.value = []
  ledgerTotal.value = 0
  ledgerPage.value = 1
  showLedgerDialog.value = true
}

const openReconciliationDialog = async (currency: VirtualCurrency) => {
  selectedCurrency.value = currency
  reconciliation.value = null
  showReconciliationDialog.value = true
  reconciliationLoading.value = true
  try {
    reconciliation.value = await adminAPI.virtualCurrencies.reconcile(currency.id)
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.reconciliationFailed'))
    showReconciliationDialog.value = false
  } finally {
    reconciliationLoading.value = false
  }
}

const openExpireHoldsDialog = (currency: VirtualCurrency) => {
  currencyToExpire.value = currency
  showExpireHoldsDialog.value = true
}

const handleExpireHolds = async () => {
  if (!currencyToExpire.value) return
  const currency = currencyToExpire.value
  try {
    const result = await adminAPI.virtualCurrencies.expireHolds(currency.id)
    appStore.showSuccess(t('admin.virtualCurrency.expireHoldsSuccess', { count: result.expired }))
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.expireHoldsFailed'))
  } finally {
    showExpireHoldsDialog.value = false
    currencyToExpire.value = null
  }
}

const loadUserLedger = async () => {
  if (!selectedCurrency.value || !ledgerUserID.value) return
  try {
    ledgerLoading.value = true
    const result = await adminAPI.virtualCurrencies.userLedger(selectedCurrency.value.code, ledgerUserID.value, ledgerPage.value, ledgerPageSize.value)
    ledger.value = result.items
    ledgerTotal.value = result.total
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('admin.virtualCurrency.loadFailed'))
  } finally {
    ledgerLoading.value = false
  }
}

const handleLedgerPageChange = (page: number) => {
  ledgerPage.value = page
  void loadUserLedger()
}

const formatUnits = (units: number | string, scale: number) => {
  const raw = BigInt(units)
  if (!scale) return raw.toString()
  const negative = raw < 0n
  const value = negative ? -raw : raw
  const base = 10n ** BigInt(scale)
  const whole = value / base
  const fraction = (value % base).toString().padStart(scale, '0')
  return `${negative ? '-' : ''}${whole}.${fraction}`
}

watch(() => adjustForm.amount_units, (amount) => {
  if (amount < 0 && adjustForm.entry_type === 'grant') {
    adjustForm.entry_type = 'adjustment'
  }
  const selectedGroup = adjustGroupOptions.value.find((option) => option.value === adjustForm.group_id)
  if (selectedGroup?.disabled) adjustForm.group_id = null
})

onMounted(() => { void loadCurrencies() })
onUnmounted(resetUserSearch)
</script>
