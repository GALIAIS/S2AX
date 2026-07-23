<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="card border border-primary-100 p-5 dark:border-primary-900/50">
        <div class="flex items-start gap-3">
          <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
            <Icon name="shield" size="lg" />
          </div>
          <div class="min-w-0">
            <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('accountAllocations.readOnlyTitle') }}</p>
            <p class="mt-1 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('accountAllocations.readOnlyDescription') }}</p>
            <p class="mt-2 flex items-center gap-1.5 text-xs text-gray-500 dark:text-dark-400">
              <Icon name="lock" size="xs" />
              {{ t('accountAllocations.privacyNotice') }}
            </p>
          </div>
        </div>
      </section>

      <div
        v-if="loadError"
        class="flex items-center gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
        role="alert"
      >
        <Icon name="xCircle" size="sm" />
        <span class="min-w-0 flex-1">{{ loadError }}</span>
        <button type="button" class="btn btn-ghost btn-sm shrink-0" @click="loadAccounts">{{ t('common.retry') }}</button>
      </div>

      <section v-if="!loading || accounts.length" class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <div class="card border border-gray-200 px-4 py-3 dark:border-dark-700">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.summary.publicGroups') }}</p>
          <p class="mt-1 font-mono text-2xl font-semibold text-gray-900 dark:text-white">{{ summary.public_group_count }}</p>
        </div>
        <div class="card border border-gray-200 px-4 py-3 dark:border-dark-700">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.summary.dedicatedGroups') }}</p>
          <p class="mt-1 font-mono text-2xl font-semibold text-gray-900 dark:text-white">{{ summary.dedicated_group_count }}</p>
        </div>
        <div class="card border border-gray-200 px-4 py-3 dark:border-dark-700">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.summary.visibleAccounts') }}</p>
          <p class="mt-1 font-mono text-2xl font-semibold text-gray-900 dark:text-white">{{ visibleAccountCount }}</p>
        </div>
        <div class="card border border-gray-200 px-4 py-3 dark:border-dark-700">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.summary.readyAccounts') }}</p>
          <p class="mt-1 font-mono text-2xl font-semibold text-emerald-600 dark:text-emerald-400">{{ summary.ready_account_count }}</p>
        </div>
      </section>

      <section class="card border border-gray-200 p-4 dark:border-dark-700">
        <div class="flex flex-col gap-3 xl:flex-row xl:items-end">
          <label class="relative block min-w-0 flex-1">
            <span class="sr-only">{{ t('accountAllocations.searchLabel') }}</span>
            <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-400" />
            <input
              v-model="searchQuery"
              type="search"
              class="input w-full pl-9"
              :placeholder="t('accountAllocations.searchPlaceholder')"
            />
          </label>

          <div class="grid grid-cols-2 gap-3 sm:grid-cols-4 xl:w-[44rem]">
            <Select v-model="sourceFilter" :options="sourceOptions" />
            <Select v-model="groupFilter" :options="groupOptions" :searchable="'auto'" />
            <Select v-model="platformFilter" :options="platformOptions" :searchable="'auto'" />
            <Select v-model="statusFilter" :options="statusOptions" />
          </div>

          <div class="flex items-center justify-between gap-2 xl:justify-end">
            <div class="inline-flex border border-gray-200 bg-white p-1 dark:border-dark-700 dark:bg-dark-900" role="group" :aria-label="t('accountAllocations.viewMode')">
              <button
                type="button"
                class="btn btn-ghost btn-sm px-2"
                :class="viewMode === 'list' && 'bg-gray-100 text-gray-900 dark:bg-dark-700 dark:text-white'"
                :aria-pressed="viewMode === 'list'"
                :title="t('accountAllocations.viewModes.list')"
                @click="viewMode = 'list'"
              >
                <Icon name="menu" size="sm" />
                <span class="sr-only">{{ t('accountAllocations.viewModes.list') }}</span>
              </button>
              <button
                type="button"
                class="btn btn-ghost btn-sm px-2"
                :class="viewMode === 'grid' && 'bg-gray-100 text-gray-900 dark:bg-dark-700 dark:text-white'"
                :aria-pressed="viewMode === 'grid'"
                :title="t('accountAllocations.viewModes.grid')"
                @click="viewMode = 'grid'"
              >
                <Icon name="grid" size="sm" />
                <span class="sr-only">{{ t('accountAllocations.viewModes.grid') }}</span>
              </button>
            </div>
            <button type="button" class="btn btn-secondary btn-sm shrink-0" :disabled="loading" @click="loadAccounts">
              <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
              <span class="hidden sm:inline">{{ t('common.refresh') }}</span>
            </button>
          </div>
        </div>

        <div class="mt-3 flex flex-wrap items-center justify-between gap-2 border-t border-gray-100 pt-3 text-xs text-gray-500 dark:border-dark-700 dark:text-dark-400">
          <span>{{ t('accountAllocations.showing', { count: filteredAccounts.length, total: visibleAccountCount }) }}</span>
          <button v-if="filtersActive" type="button" class="btn btn-ghost btn-sm" @click="resetFilters">
            {{ t('accountAllocations.resetFilters') }}
          </button>
        </div>
      </section>

      <div v-if="loading && accounts.length === 0" class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <div v-for="index in 6" :key="index" class="card h-56 animate-pulse bg-gray-100 dark:bg-dark-800" />
      </div>

      <section v-else-if="accounts.length === 0" class="card empty-state py-16">
        <Icon name="inbox" size="xl" class="text-gray-400 dark:text-dark-500" />
        <p class="mt-4 text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('accountAllocations.emptyTitle') }}</p>
        <p class="mt-2 max-w-lg text-center text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('accountAllocations.emptyDescription') }}</p>
      </section>

      <section v-else-if="filteredAccounts.length === 0" class="card empty-state py-16">
        <Icon name="search" size="xl" class="text-gray-400 dark:text-dark-500" />
        <p class="mt-4 text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('accountAllocations.noFilterResultsTitle') }}</p>
        <p class="mt-2 max-w-lg text-center text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('accountAllocations.noFilterResultsDescription') }}</p>
        <button type="button" class="btn btn-secondary btn-sm mt-4" @click="resetFilters">{{ t('accountAllocations.resetFilters') }}</button>
      </section>

      <section v-else-if="viewMode === 'list'" class="card overflow-hidden border border-gray-200 dark:border-dark-700">
        <DataTable
          :columns="tableColumns"
          :data="filteredAccounts"
          :loading="loading"
          row-key="view_key"
          :clickable-rows="true"
          :sticky-first-column="false"
          :sticky-actions-column="false"
          :expandable-actions="false"
          :estimate-row-height="84"
          :virtualize-threshold="80"
          :aria-label="t('accountAllocations.tableAriaLabel')"
          @row-click="openDetails"
          @retry="loadAccounts"
        >
          <template #cell-account_name="{ row }">
            <div class="flex min-w-[12rem] flex-col gap-1">
              <div class="flex min-w-0 items-center gap-2">
                <span class="truncate font-medium text-gray-900 dark:text-white">{{ row.account_name }}</span>
                <span v-if="row.account_name_masked" class="badge badge-gray shrink-0 text-[10px]">{{ t('accountAllocations.masked') }}</span>
              </div>
              <span class="text-xs text-gray-500 dark:text-dark-400">{{ sourceLabel(row.source) }}</span>
            </div>
          </template>

          <template #cell-group_name="{ row }">
            <div class="min-w-[11rem]">
              <p class="truncate font-medium text-gray-900 dark:text-white">{{ row.group_name }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ subscriptionLabel(row.subscription_type) }}</p>
            </div>
          </template>

          <template #cell-platform_type="{ row }">
            <div class="min-w-[9rem]">
              <PlatformTypeBadge :platform="row.platform" :type="row.account_type" />
            </div>
          </template>

          <template #cell-capacity="{ row }">
            <div class="min-w-[7rem]">
              <p class="font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ row.capacity.concurrency }}</p>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.concurrentRequests') }}</p>
            </div>
          </template>

          <template #cell-usage="{ row }">
            <div class="min-w-[15rem] space-y-1.5">
              <div v-if="row.upstream_quota" class="space-y-1 border-b border-gray-100 pb-1.5 dark:border-dark-700">
                <UsageProgressBar
                  v-if="row.upstream_quota.five_hour"
                  label="5h"
                  :utilization="row.upstream_quota.five_hour.utilization"
                  :resets-at="row.upstream_quota.five_hour.resets_at"
                  color="indigo"
                  :show-now-when-idle="true"
                />
                <UsageProgressBar
                  v-if="row.upstream_quota.seven_day"
                  label="7d"
                  :utilization="row.upstream_quota.seven_day.utilization"
                  :resets-at="row.upstream_quota.seven_day.resets_at"
                  color="emerald"
                  :show-now-when-idle="true"
                />
                <p class="text-[10px] text-gray-400 dark:text-dark-500" :title="formatDateTime(row.upstream_quota.updated_at)">
                  {{ t('accountAllocations.cachedQuotaSnapshot') }} · {{ formatRelativeTime(row.upstream_quota.updated_at) }}
                </p>
              </div>
              <div class="flex flex-wrap items-center gap-1 text-[11px] text-gray-600 dark:text-dark-300">
                <span class="badge badge-gray font-medium">{{ usageWindowLabel(row.usage.scope) }}</span>
                <span class="rounded bg-gray-100 px-1.5 py-0.5 font-mono dark:bg-dark-800">{{ formatCompactNumber(row.usage.request_count, { allowBillions: false }) }} {{ t('accountAllocations.requests') }}</span>
                <span class="rounded bg-gray-100 px-1.5 py-0.5 font-mono dark:bg-dark-800">{{ formatCompactNumber(row.usage.total_tokens) }} {{ t('accountAllocations.tokens') }}</span>
                <span v-if="hasLeaseUsage(row) && row.usage.account_cost != null" class="rounded bg-gray-100 px-1.5 py-0.5 font-mono dark:bg-dark-800" :title="t('accountAllocations.leaseAccountCost')">A ${{ formatCostFixed(row.usage.account_cost ?? 0, 2) }}</span>
                <span v-if="hasLeaseUsage(row) && row.usage.user_cost != null" class="rounded bg-gray-100 px-1.5 py-0.5 font-mono dark:bg-dark-800" :title="t('accountAllocations.leaseUserCost')">U ${{ formatCostFixed(row.usage.user_cost ?? 0, 2) }}</span>
              </div>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ usageScopeLabel(row.usage.scope) }}</p>
            </div>
          </template>

          <template #cell-last_activity_at="{ value }">
            <span class="min-w-[6rem] text-sm text-gray-500 dark:text-dark-400" :title="formatDateTime(value)">
              {{ formatRelativeTime(value) }}
            </span>
          </template>

          <template #cell-status="{ row }">
            <div class="flex min-w-[8rem] flex-col gap-1">
              <span :class="statusClass(row.status)" class="badge w-fit">{{ statusLabel(row.status) }}</span>
              <span v-if="row.status === 'cooling' && row.rate_limit_reset_at" class="text-xs text-amber-700 dark:text-amber-300">{{ formatDateTime(row.rate_limit_reset_at) }}</span>
            </div>
          </template>
        </DataTable>
      </section>

      <section v-else class="space-y-4">
        <div v-if="loading" class="flex items-center gap-2 text-xs text-gray-500 dark:text-dark-400" role="status">
          <Icon name="refresh" size="xs" class="animate-spin" />
          {{ t('common.refreshing') }}
        </div>
        <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <button
            v-for="account in gridAccounts"
            :key="account.view_key"
            type="button"
            class="card group min-w-0 overflow-hidden border border-gray-200 p-0 text-left transition-colors hover:border-primary-300 hover:bg-primary-50/30 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:border-dark-700 dark:hover:border-primary-700 dark:hover:bg-primary-900/10"
            @click="openDetails(account)"
          >
            <div class="flex items-start justify-between gap-3 border-b border-gray-100 px-4 py-4 dark:border-dark-700">
              <div class="min-w-0">
                <div class="flex min-w-0 items-center gap-2">
                  <p class="truncate font-semibold text-gray-900 dark:text-white">{{ account.account_name }}</p>
                  <span v-if="account.account_name_masked" class="badge badge-gray shrink-0 text-[10px]">{{ t('accountAllocations.masked') }}</span>
                </div>
                <p class="mt-1 truncate text-xs text-gray-500 dark:text-dark-400">{{ account.group_name }}</p>
              </div>
              <span :class="statusClass(account.status)" class="badge shrink-0">{{ statusLabel(account.status) }}</span>
            </div>

            <div class="grid grid-cols-2 gap-x-4 gap-y-4 px-4 py-4">
              <div>
                <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.source') }}</p>
                <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ sourceLabel(account.source) }}</p>
              </div>
              <div>
                <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.platformType') }}</p>
                <div class="mt-1"><PlatformTypeBadge :platform="account.platform" :type="account.account_type" /></div>
              </div>
              <div>
                <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.capacity') }}</p>
                <p class="mt-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ account.capacity.concurrency }}</p>
              </div>
              <div>
                <p class="text-xs text-gray-500 dark:text-dark-400">{{ usageScopeLabel(account.usage.scope) }}</p>
                <div class="mt-1 flex flex-wrap gap-1 text-[11px] text-gray-600 dark:text-dark-300">
                  <span class="rounded bg-gray-100 px-1.5 py-0.5 font-mono dark:bg-dark-800">{{ formatCompactNumber(account.usage.request_count, { allowBillions: false }) }} {{ t('accountAllocations.requests') }}</span>
                  <span class="rounded bg-gray-100 px-1.5 py-0.5 font-mono dark:bg-dark-800">{{ formatCompactNumber(account.usage.total_tokens) }} {{ t('accountAllocations.tokens') }}</span>
                  <span v-if="hasLeaseUsage(account) && account.usage.account_cost != null" class="rounded bg-gray-100 px-1.5 py-0.5 font-mono dark:bg-dark-800" :title="t('accountAllocations.leaseAccountCost')">A ${{ formatCostFixed(account.usage.account_cost ?? 0, 2) }}</span>
                  <span v-if="hasLeaseUsage(account) && account.usage.user_cost != null" class="rounded bg-gray-100 px-1.5 py-0.5 font-mono dark:bg-dark-800" :title="t('accountAllocations.leaseUserCost')">U ${{ formatCostFixed(account.usage.user_cost ?? 0, 2) }}</span>
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400" :title="formatDateTime(account.last_activity_at)">
                  {{ t('accountAllocations.lastActivity') }} · {{ formatRelativeTime(account.last_activity_at) }}
                </p>
              </div>
              <div v-if="account.upstream_quota" class="col-span-2 space-y-1 border-t border-gray-100 pt-3 dark:border-dark-700">
                <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.upstreamQuota') }}</p>
                <UsageProgressBar
                  v-if="account.upstream_quota.five_hour"
                  label="5h"
                  :utilization="account.upstream_quota.five_hour.utilization"
                  :resets-at="account.upstream_quota.five_hour.resets_at"
                  color="indigo"
                  :show-now-when-idle="true"
                />
                <UsageProgressBar
                  v-if="account.upstream_quota.seven_day"
                  label="7d"
                  :utilization="account.upstream_quota.seven_day.utilization"
                  :resets-at="account.upstream_quota.seven_day.resets_at"
                  color="emerald"
                  :show-now-when-idle="true"
                />
                <p class="text-[10px] text-gray-400 dark:text-dark-500" :title="formatDateTime(account.upstream_quota.updated_at)">
                  {{ t('accountAllocations.cachedQuotaSnapshot') }} · {{ formatRelativeTime(account.upstream_quota.updated_at) }}
                </p>
              </div>
            </div>

            <div class="flex items-center justify-between border-t border-gray-100 px-4 py-3 text-xs dark:border-dark-700">
              <span :class="account.status === 'ready' ? 'text-emerald-700 dark:text-emerald-300' : 'text-gray-500 dark:text-dark-400'">{{ availabilityHint(account) }}</span>
              <span class="font-medium text-primary-600 dark:text-primary-400">{{ t('accountAllocations.viewDetails') }}</span>
            </div>
          </button>
        </div>

        <div v-if="hasMoreGridAccounts" class="flex justify-center">
          <button type="button" class="btn btn-secondary" @click="showMoreGridAccounts">{{ t('accountAllocations.showMore', { count: remainingGridAccountCount }) }}</button>
        </div>
      </section>
    </div>

    <BaseDialog
      :show="!!selectedAccount"
      :title="selectedAccount ? `${selectedAccount.account_name} · ${t('accountAllocations.detailTitle')}` : t('accountAllocations.detailTitle')"
      width="wide"
      @close="selectedAccount = null"
    >
      <div v-if="selectedAccount" class="space-y-5">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 pb-4 dark:border-dark-700">
          <div class="min-w-0">
            <div class="flex min-w-0 items-center gap-2">
              <p class="truncate text-base font-semibold text-gray-900 dark:text-white">{{ selectedAccount.account_name }}</p>
              <span v-if="selectedAccount.account_name_masked" class="badge badge-gray shrink-0">{{ t('accountAllocations.masked') }}</span>
            </div>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ selectedAccount.group_name }}</p>
          </div>
          <span :class="statusClass(selectedAccount.status)" class="badge shrink-0">{{ statusLabel(selectedAccount.status) }}</span>
        </div>

        <dl class="grid gap-x-6 gap-y-5 sm:grid-cols-2">
          <div>
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.source') }}</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ sourceLabel(selectedAccount.source) }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.groupType') }}</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ subscriptionLabel(selectedAccount.subscription_type) }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.platformType') }}</dt>
            <dd class="mt-1"><PlatformTypeBadge :platform="selectedAccount.platform" :type="selectedAccount.account_type" /></dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.capacity') }}</dt>
            <dd class="mt-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ selectedAccount.capacity.concurrency }} {{ t('accountAllocations.concurrentRequests') }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ usageScopeLabel(selectedAccount.usage.scope) }}</dt>
            <dd class="mt-1 space-y-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">
              <p>{{ formatNumber(selectedAccount.usage.request_count) }} {{ t('accountAllocations.requests') }}</p>
              <p v-if="hasLeaseUsage(selectedAccount) && selectedAccount.usage.account_cost != null" class="text-xs font-normal text-gray-500 dark:text-dark-400">{{ t('accountAllocations.leaseAccountCost') }} · ${{ formatCostFixed(selectedAccount.usage.account_cost ?? 0, 4) }}</p>
              <p v-if="hasLeaseUsage(selectedAccount) && selectedAccount.usage.user_cost != null" class="text-xs font-normal text-gray-500 dark:text-dark-400">{{ t('accountAllocations.leaseUserCost') }} · ${{ formatCostFixed(selectedAccount.usage.user_cost ?? 0, 4) }}</p>
            </dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.tokenUsage') }}</dt>
            <dd class="mt-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ formatCompactNumber(selectedAccount.usage.total_tokens) }} {{ t('accountAllocations.tokens') }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.lastActivity') }}</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white" :title="formatDateTime(selectedAccount.last_activity_at)">{{ formatRelativeTime(selectedAccount.last_activity_at) }}</dd>
          </div>
          <div v-if="selectedAccount.assigned_at">
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.assignedAt') }}</dt>
            <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ formatDateTime(selectedAccount.assigned_at) || '—' }}</dd>
          </div>
          <div v-if="selectedAccount.status === 'cooling' && selectedAccount.rate_limit_reset_at">
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.coolingUntilLabel') }}</dt>
            <dd class="mt-1 text-sm font-medium text-amber-700 dark:text-amber-300">{{ formatDateTime(selectedAccount.rate_limit_reset_at) }}</dd>
          </div>
          <div v-if="selectedAccount.upstream_quota" class="sm:col-span-2">
            <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountAllocations.upstreamQuota') }}</dt>
            <dd class="mt-2 max-w-sm space-y-1.5">
              <UsageProgressBar
                v-if="selectedAccount.upstream_quota.five_hour"
                label="5h"
                :utilization="selectedAccount.upstream_quota.five_hour.utilization"
                :resets-at="selectedAccount.upstream_quota.five_hour.resets_at"
                color="indigo"
                :show-now-when-idle="true"
              />
              <UsageProgressBar
                v-if="selectedAccount.upstream_quota.seven_day"
                label="7d"
                :utilization="selectedAccount.upstream_quota.seven_day.utilization"
                :resets-at="selectedAccount.upstream_quota.seven_day.resets_at"
                color="emerald"
                :show-now-when-idle="true"
              />
              <p class="text-xs text-gray-500 dark:text-dark-400" :title="formatDateTime(selectedAccount.upstream_quota.updated_at)">
                {{ t('accountAllocations.cachedQuotaSnapshot') }} · {{ formatRelativeTime(selectedAccount.upstream_quota.updated_at) }}
              </p>
            </dd>
          </div>
        </dl>

        <div class="border border-gray-200 bg-gray-50 px-4 py-3 text-sm text-gray-600 dark:border-dark-700 dark:bg-dark-800/70 dark:text-dark-300">
          <div class="flex gap-2">
            <Icon name="lock" size="sm" class="mt-0.5 shrink-0 text-gray-400 dark:text-dark-400" />
            <p>{{ t('accountAllocations.detailPrivacyNotice') }}</p>
          </div>
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn btn-secondary" @click="selectedAccount = null">{{ t('common.close') }}</button>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import accountAllocationsAPI, {
  type AccountAllocationUserStatus,
  type AccountAllocationVisibleSource,
  type AccountAllocationVisibleUsageScope,
  type UserVisibleAccount,
  type UserVisibleAccountOverview,
  type UserVisibleAccountSummary,
} from '@/api/accountAllocations'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import type { Column } from '@/components/common/types'
import UsageProgressBar from '@/components/account/UsageProgressBar.vue'
import Icon from '@/components/icons/Icon.vue'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatCompactNumber, formatCostFixed, formatDateTime, formatNumber, formatRelativeTime } from '@/utils/format'

const { t } = useI18n()

type AccountDirectoryViewMode = 'list' | 'grid'

const ACCOUNT_DIRECTORY_VIEW_MODE_STORAGE_KEY = 'user-account-directory-view-mode'
const GRID_PAGE_SIZE = 24

const emptySummary = (): UserVisibleAccountSummary => ({
  public_group_count: 0,
  dedicated_group_count: 0,
  public_account_count: 0,
  dedicated_account_count: 0,
  ready_account_count: 0,
})

const preferredViewMode = (): AccountDirectoryViewMode => {
  try {
    return window.localStorage.getItem(ACCOUNT_DIRECTORY_VIEW_MODE_STORAGE_KEY) === 'grid' ? 'grid' : 'list'
  } catch {
    return 'list'
  }
}

const accounts = ref<UserVisibleAccount[]>([])
const summary = ref<UserVisibleAccountSummary>(emptySummary())
const loading = ref(true)
const loadError = ref<string | null>(null)
const selectedAccount = ref<UserVisibleAccount | null>(null)
const searchQuery = ref('')
const sourceFilter = ref<'all' | AccountAllocationVisibleSource>('all')
const groupFilter = ref('all')
const platformFilter = ref('all')
const statusFilter = ref<'all' | AccountAllocationUserStatus>('all')
const viewMode = ref<AccountDirectoryViewMode>(preferredViewMode())
const gridLimit = ref(GRID_PAGE_SIZE)
let requestID = 0

const sourceOptions = computed<SelectOption[]>(() => [
  { value: 'all', label: t('accountAllocations.filters.allSources') },
  { value: 'public', label: t('accountAllocations.sourcePublic') },
  { value: 'dedicated', label: t('accountAllocations.sourceDedicated') },
])

const groupOptions = computed<SelectOption[]>(() => {
  const groups = new Map<number, string>()
  for (const account of accounts.value) groups.set(account.group_id, account.group_name)
  return [
    { value: 'all', label: t('accountAllocations.filters.allGroups') },
    ...Array.from(groups.entries())
      .sort(([, left], [, right]) => left.localeCompare(right))
      .map(([id, label]) => ({ value: String(id), label })),
  ]
})

const platformOptions = computed<SelectOption[]>(() => {
  const platforms = Array.from(new Set(accounts.value.map((account) => account.platform).filter(Boolean)))
    .sort((left, right) => left.localeCompare(right))
  return [
    { value: 'all', label: t('accountAllocations.filters.allPlatforms') },
    ...platforms.map((platform) => ({ value: platform, label: platformLabel(platform) })),
  ]
})

const statusOptions = computed<SelectOption[]>(() => [
  { value: 'all', label: t('accountAllocations.filters.allStatuses') },
  { value: 'ready', label: statusLabel('ready') },
  { value: 'cooling', label: statusLabel('cooling') },
  { value: 'unavailable', label: statusLabel('unavailable') },
])

const normalizedSearchQuery = computed(() => searchQuery.value.trim().toLocaleLowerCase())

const filteredAccounts = computed(() => accounts.value.filter((account) => {
  if (sourceFilter.value !== 'all' && account.source !== sourceFilter.value) return false
  if (groupFilter.value !== 'all' && String(account.group_id) !== groupFilter.value) return false
  if (platformFilter.value !== 'all' && account.platform !== platformFilter.value) return false
  if (statusFilter.value !== 'all' && account.status !== statusFilter.value) return false

  const query = normalizedSearchQuery.value
  if (!query) return true
  return [
    account.account_name,
    account.group_name,
    account.platform,
    account.account_type,
    sourceLabel(account.source),
  ].some((value) => value.toLocaleLowerCase().includes(query))
}))

const visibleAccountCount = computed(() => summary.value.public_account_count + summary.value.dedicated_account_count)
const gridAccounts = computed(() => filteredAccounts.value.slice(0, gridLimit.value))
const hasMoreGridAccounts = computed(() => gridAccounts.value.length < filteredAccounts.value.length)
const remainingGridAccountCount = computed(() => Math.min(GRID_PAGE_SIZE, Math.max(0, filteredAccounts.value.length - gridAccounts.value.length)))
const filtersActive = computed(() => (
  !!searchQuery.value.trim()
  || sourceFilter.value !== 'all'
  || groupFilter.value !== 'all'
  || platformFilter.value !== 'all'
  || statusFilter.value !== 'all'
))

const tableColumns = computed<Column[]>(() => [
  { key: 'account_name', label: t('accountAllocations.account') },
  { key: 'group_name', label: t('accountAllocations.group') },
  { key: 'platform_type', label: t('accountAllocations.platformType') },
  { key: 'capacity', label: t('accountAllocations.capacity') },
  { key: 'usage', label: t('accountAllocations.usage') },
  { key: 'last_activity_at', label: t('accountAllocations.lastActivity') },
  { key: 'status', label: t('common.status') },
])

const statusLabel = (status: AccountAllocationUserStatus): string => {
  if (status === 'ready') return t('accountAllocations.status.ready')
  if (status === 'cooling') return t('accountAllocations.status.cooling')
  return t('accountAllocations.status.unavailable')
}

const statusClass = (status: AccountAllocationUserStatus): string => {
  if (status === 'ready') return 'badge-success'
  if (status === 'cooling') return 'badge-warning'
  return 'badge-gray'
}

const sourceLabel = (source: AccountAllocationVisibleSource): string => (
  source === 'dedicated' ? t('accountAllocations.sourceDedicated') : t('accountAllocations.sourcePublic')
)

const subscriptionLabel = (subscriptionType: string): string => (
  subscriptionType === 'subscription'
    ? t('accountAllocations.groupTypes.subscription')
    : t('accountAllocations.groupTypes.standard')
)

const platformLabel = (platform: string): string => platform || t('accountAllocations.unknownPlatform')

const usageScopeLabel = (scope: AccountAllocationVisibleUsageScope): string => (
  scope === 'personal_lease'
    ? t('accountAllocations.usageScopes.personalLease')
    : t('accountAllocations.usageScopes.rolling24h')
)

const usageWindowLabel = (scope: AccountAllocationVisibleUsageScope): string => (
  scope === 'personal_lease'
    ? t('accountAllocations.usageWindows.personalLease')
    : t('accountAllocations.usageWindows.rolling24h')
)

const hasLeaseUsage = (account: UserVisibleAccount): boolean => (
  account.usage.scope === 'personal_lease'
  && (account.usage.request_count > 0 || account.usage.total_tokens > 0)
)

const availabilityHint = (account: UserVisibleAccount): string => {
  if (account.status === 'ready') return t('accountAllocations.readyHint')
  if (account.status === 'cooling' && account.rate_limit_reset_at) {
    return t('accountAllocations.coolingUntil', { time: formatDateTime(account.rate_limit_reset_at) })
  }
  return t('accountAllocations.unavailableHint')
}

const loadAccounts = async () => {
  const currentRequestID = ++requestID
  loading.value = true
  loadError.value = null
  try {
    const result: UserVisibleAccountOverview = await accountAllocationsAPI.listVisible()
    if (currentRequestID === requestID) {
      accounts.value = result.items
      summary.value = result.summary
    }
  } catch (error: unknown) {
    if (currentRequestID === requestID) {
      loadError.value = extractApiErrorMessage(error, t('accountAllocations.loadFailed'))
    }
  } finally {
    if (currentRequestID === requestID) loading.value = false
  }
}

const resetFilters = () => {
  searchQuery.value = ''
  sourceFilter.value = 'all'
  groupFilter.value = 'all'
  platformFilter.value = 'all'
  statusFilter.value = 'all'
}

const openDetails = (account: UserVisibleAccount) => {
  selectedAccount.value = account
}

const showMoreGridAccounts = () => {
  gridLimit.value += GRID_PAGE_SIZE
}

watch(viewMode, (mode) => {
  try {
    window.localStorage.setItem(ACCOUNT_DIRECTORY_VIEW_MODE_STORAGE_KEY, mode)
  } catch {
    // A blocked storage API should not affect the directory.
  }
})

watch([searchQuery, sourceFilter, groupFilter, platformFilter, statusFilter], () => {
  gridLimit.value = GRID_PAGE_SIZE
})

watch(groupOptions, (options) => {
  if (!options.some((option) => String(option.value) === groupFilter.value)) groupFilter.value = 'all'
})

watch(platformOptions, (options) => {
  if (!options.some((option) => String(option.value) === platformFilter.value)) platformFilter.value = 'all'
})

onMounted(() => { void loadAccounts() })
onUnmounted(() => { requestID += 1 })
</script>
