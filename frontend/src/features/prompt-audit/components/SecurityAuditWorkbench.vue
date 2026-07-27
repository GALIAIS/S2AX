<template>
  <section class="py-6" data-test="security-audit-workbench">
    <div class="mb-5 flex flex-col gap-3 border-b border-gray-200 pb-4 dark:border-dark-700 lg:flex-row lg:items-center lg:justify-between">
      <div>
        <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('admin.promptAudit.core.title') }}</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.description') }}</p>
      </div>
      <button type="button" class="btn btn-secondary btn-sm self-start" :disabled="isLoading" @click="refreshActive">
        <Icon name="refresh" size="sm" :class="{ 'animate-spin': isLoading }" />
        {{ t('admin.promptAudit.actions.refresh') }}
      </button>
    </div>

    <div class="mb-5 overflow-x-auto">
      <div class="tabs inline-flex min-w-max" role="tablist" :aria-label="t('admin.promptAudit.core.title')">
        <button
          v-for="item in sections"
          :key="item.id"
          type="button"
          class="tab"
          :class="{ 'tab-active': section === item.id }"
          role="tab"
          :aria-selected="section === item.id"
          :data-test="`core-${item.id}`"
          @click="section = item.id"
        >
          {{ item.label }}
          <span v-if="item.count !== undefined" class="ml-2 text-xs opacity-70">{{ item.count }}</span>
        </button>
      </div>
    </div>

    <div v-if="activeError" role="alert" class="mb-5 border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
      {{ activeError }}
    </div>

    <div v-if="section === 'overview'">
      <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <article v-for="metric in overviewMetrics" :key="metric.key" class="border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-850">
          <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ metric.label }}</p>
          <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-950 dark:text-white">{{ formatNumber(metric.value) }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ metric.hint }}</p>
        </article>
      </div>

      <div class="mt-5 grid gap-4 xl:grid-cols-3">
        <article class="border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-850 xl:col-span-2">
          <div class="flex items-center justify-between gap-3">
            <h3 class="font-semibold text-gray-950 dark:text-white">{{ t('admin.promptAudit.core.overview.decisionBreakdown') }}</h3>
            <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.core.overview.window', { hours: overview?.window_hours ?? 24 }) }}</span>
          </div>
          <div class="mt-4 grid gap-3 sm:grid-cols-3">
            <div v-for="item in decisionBreakdown" :key="item.label" class="border border-gray-100 p-3 dark:border-dark-700">
              <p class="text-sm text-gray-500 dark:text-dark-300">{{ item.label }}</p>
              <p class="mt-1 text-xl font-semibold tabular-nums" :class="item.className">{{ formatNumber(item.value) }}</p>
            </div>
          </div>
          <div class="mt-5">
            <div v-for="item in sourceBreakdown" :key="item.key" class="mb-3 last:mb-0">
              <div class="mb-1 flex items-center justify-between text-xs text-gray-600 dark:text-dark-300">
                <span>{{ sourceLabel(item.key) }}</span><span class="tabular-nums">{{ formatNumber(item.value) }}</span>
              </div>
              <div class="h-1.5 bg-gray-100 dark:bg-dark-700">
                <div class="h-full bg-primary-500" :style="{ width: `${item.percent}%` }" />
              </div>
            </div>
          </div>
        </article>

        <article class="border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-850">
          <h3 class="font-semibold text-gray-950 dark:text-white">{{ t('admin.promptAudit.core.overview.signalHealth') }}</h3>
          <dl class="mt-4 space-y-3 text-sm">
            <div class="flex justify-between gap-4">
              <dt class="text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.overview.signalLag') }}</dt>
              <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ formatDuration(overview?.signal_lag_seconds ?? 0) }}</dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt class="text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.overview.lastAggregate') }}</dt>
              <dd class="text-right text-gray-900 dark:text-white">{{ formatDate(overview?.signal_last_aggregated_at) }}</dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt class="text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.overview.activePolicies') }}</dt>
              <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ overview?.active_policies ?? 0 }}</dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt class="text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.overview.activeExceptions') }}</dt>
              <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ overview?.active_exceptions ?? 0 }}</dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt class="text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.overview.detectorP95') }}</dt>
              <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ overview?.detector_p95_ms ?? 0 }} ms</dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt class="text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.overview.oldestAction') }}</dt>
              <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ formatDuration(overview?.oldest_pending_action_seconds ?? 0) }}</dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt class="text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.overview.reviewQuality') }}</dt>
              <dd class="font-medium tabular-nums text-gray-900 dark:text-white">{{ overview?.false_positive_count ?? 0 }} / {{ overview?.false_negative_count ?? 0 }}</dd>
            </div>
          </dl>
          <p v-if="overview?.signal_last_error" class="mt-4 border-l-2 border-red-500 pl-3 text-xs text-red-600 dark:text-red-300">
            {{ overview.signal_last_error }}
          </p>
          <p v-else class="mt-4 border-l-2 border-emerald-500 pl-3 text-xs text-emerald-700 dark:text-emerald-300">
            {{ t('admin.promptAudit.core.overview.signalHealthy') }}
          </p>
        </article>
      </div>

      <article class="mt-5 border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-850">
        <div class="flex items-center justify-between border-b border-gray-200 px-5 py-4 dark:border-dark-700">
          <h3 class="font-semibold text-gray-950 dark:text-white">{{ t('admin.promptAudit.core.notifications.title') }}</h3>
          <button type="button" class="btn btn-ghost btn-sm" @click="section = 'operations'; operationSection = 'notifications'">
            {{ t('admin.promptAudit.core.common.viewAll') }}
          </button>
        </div>
        <div v-if="notifications.length" class="divide-y divide-gray-100 dark:divide-dark-700">
          <button
            v-for="item in notifications.slice(0, 5)"
            :key="item.id"
            type="button"
            class="flex w-full items-start gap-3 px-5 py-3 text-left hover:bg-gray-50 dark:hover:bg-dark-800"
            @click="markNotification(item, 'read')"
          >
            <span class="mt-1 h-2 w-2 shrink-0" :class="item.status === 'unread' ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600'" />
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.title }}</span>
              <span class="mt-1 block line-clamp-2 text-xs text-gray-500 dark:text-dark-300">{{ item.body }}</span>
            </span>
            <span class="text-xs text-gray-400">{{ formatDate(item.created_at) }}</span>
          </button>
        </div>
        <EmptyState v-else :text="t('admin.promptAudit.core.notifications.empty')" />
      </article>
    </div>

    <div v-else-if="section === 'decisions'">
      <div class="mb-4 grid gap-3 md:grid-cols-2 xl:grid-cols-5">
        <input v-model.trim="decisionFilters.keyword" class="input xl:col-span-2" type="search" :placeholder="t('admin.promptAudit.core.decisions.search')" @keyup.enter="loadDecisions(1)" />
        <Select v-model="decisionFilters.risk_level" :options="riskOptions" :aria-label="t('admin.promptAudit.core.decisions.risk')" />
        <Select v-model="decisionFilters.source_type" :options="sourceOptions" :aria-label="t('admin.promptAudit.core.decisions.source')" />
        <Select v-model="decisionFilters.request_action" :options="requestActionOptions" :aria-label="t('admin.promptAudit.core.decisions.action')" />
      </div>
      <div class="mb-4 flex justify-end">
        <button type="button" class="btn btn-primary btn-sm" @click="loadDecisions(1)">{{ t('common.search') }}</button>
      </div>
      <div class="overflow-x-auto border border-gray-200 dark:border-dark-700">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
          <thead class="bg-gray-50 dark:bg-dark-800">
            <tr>
              <th class="table-th">{{ t('admin.promptAudit.core.decisions.time') }}</th>
              <th class="table-th">{{ t('admin.promptAudit.core.decisions.subject') }}</th>
              <th class="table-th">{{ t('admin.promptAudit.core.decisions.source') }}</th>
              <th class="table-th">{{ t('admin.promptAudit.core.decisions.policy') }}</th>
              <th class="table-th">{{ t('admin.promptAudit.core.decisions.result') }}</th>
              <th class="table-th">{{ t('admin.promptAudit.common.actions') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-850">
            <tr v-for="item in decisions.items" :key="item.id">
              <td class="table-td whitespace-nowrap">{{ formatDate(item.created_at) }}</td>
              <td class="table-td max-w-xs">
                <p class="truncate font-medium text-gray-900 dark:text-white">{{ decisionSubject(item) }}</p>
                <p class="mt-1 truncate text-xs text-gray-500 dark:text-dark-400">{{ item.request_id || item.decision_id }}</p>
              </td>
              <td class="table-td">{{ sourceLabel(item.source_type) }}</td>
              <td class="table-td"><span class="font-mono text-xs">{{ item.policy_key }}@{{ item.policy_version }}</span></td>
              <td class="table-td">
                <div class="flex flex-wrap items-center gap-2">
                  <span :class="riskBadge(item.risk_level)">{{ riskLabel(item.risk_level) }}</span>
                  <span :class="actionBadge(item.request_action)">{{ requestActionLabel(item.request_action) }}</span>
                </div>
              </td>
              <td class="table-td">
                <button type="button" class="btn btn-ghost btn-sm" @click="openDecision(item.id)">{{ t('common.view') }}</button>
              </td>
            </tr>
          </tbody>
        </table>
        <EmptyState v-if="!decisions.items.length && !loading.decisions" :text="t('admin.promptAudit.core.decisions.empty')" />
        <Pagination
          v-if="decisions.total > 0"
          :total="decisions.total"
          :page="decisions.page"
          :page-size="decisions.page_size"
          @update:page="loadDecisions"
          @update:page-size="changeDecisionPageSize"
        />
      </div>
    </div>

    <div v-else-if="section === 'cases'">
      <div class="mb-4 grid gap-3 md:grid-cols-3">
        <input v-model.trim="caseFilters.keyword" class="input" type="search" :placeholder="t('admin.promptAudit.core.cases.search')" @keyup.enter="loadCases(1)" />
        <Select v-model="caseFilters.status" :options="caseStatusOptions" :aria-label="t('admin.promptAudit.core.cases.status')" />
        <Select v-model="caseFilters.severity" :options="riskOptions" :aria-label="t('admin.promptAudit.core.cases.severity')" />
      </div>
      <div class="mb-4 flex justify-end">
        <button type="button" class="btn btn-primary btn-sm" @click="loadCases(1)">{{ t('common.search') }}</button>
      </div>
      <div class="grid gap-3">
        <button
          v-for="item in cases.items"
          :key="item.id"
          type="button"
          class="grid gap-3 border border-gray-200 bg-white p-4 text-left hover:border-primary-300 dark:border-dark-700 dark:bg-dark-850 dark:hover:border-primary-700 md:grid-cols-[minmax(0,1fr)_auto_auto]"
          @click="openCase(item.id)"
        >
          <span class="min-w-0">
            <span class="block truncate font-medium text-gray-950 dark:text-white">{{ item.title }}</span>
            <span class="mt-1 block truncate font-mono text-xs text-gray-500 dark:text-dark-400">{{ item.case_id }}</span>
          </span>
          <span :class="riskBadge(item.severity)">{{ riskLabel(item.severity) }}</span>
          <span class="text-sm text-gray-600 dark:text-dark-300">{{ caseStatusLabel(item.status) }}</span>
        </button>
        <EmptyState v-if="!cases.items.length && !loading.cases" :text="t('admin.promptAudit.core.cases.empty')" />
      </div>
      <Pagination
        v-if="cases.total > 0"
        class="mt-4 border border-gray-200 dark:border-dark-700"
        :total="cases.total"
        :page="cases.page"
        :page-size="cases.page_size"
        @update:page="loadCases"
        @update:page-size="changeCasePageSize"
      />
    </div>

    <div v-else-if="section === 'policies'">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
        <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.policies.hint') }}</p>
        <button type="button" class="btn btn-primary btn-sm" @click="openPolicyCreate">{{ t('admin.promptAudit.core.policies.create') }}</button>
      </div>
      <div class="grid gap-3 xl:grid-cols-[minmax(0,1fr)_minmax(360px,0.9fr)]">
        <div class="overflow-hidden border border-gray-200 dark:border-dark-700">
          <button
            v-for="item in policies"
            :key="item.policy_key"
            type="button"
            class="flex w-full items-center justify-between gap-4 border-b border-gray-100 bg-white px-4 py-3 text-left last:border-b-0 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-850 dark:hover:bg-dark-800"
            :class="{ 'bg-primary-50 dark:bg-primary-950/20': selectedPolicyKey === item.policy_key }"
            @click="selectPolicy(item.policy_key)"
          >
            <span class="min-w-0">
              <span class="block truncate font-medium text-gray-950 dark:text-white">{{ item.name }}</span>
              <span class="mt-1 block font-mono text-xs text-gray-500 dark:text-dark-400">{{ item.policy_key }}</span>
            </span>
            <span class="text-right text-xs text-gray-500 dark:text-dark-300">
              <span class="block">{{ policyStatusLabel(item.status) }}</span>
              <span class="mt-1 block">v{{ item.latest_version }} · {{ item.version_count }}</span>
            </span>
          </button>
          <EmptyState v-if="!policies.length && !loading.policies" :text="t('admin.promptAudit.core.policies.empty')" />
        </div>

        <div class="border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-850">
          <div v-if="selectedPolicyKey">
            <div class="mb-3">
              <label class="mb-1 block text-xs font-medium text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.policies.changeReason') }}</label>
              <input v-model.trim="policyTransitionReason" class="input" :placeholder="t('admin.promptAudit.core.policies.changeReasonPlaceholder')" />
            </div>
            <div class="space-y-3">
              <article v-for="version in policyVersions" :key="version.id" class="border border-gray-200 p-3 dark:border-dark-700">
                <div class="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <p class="font-medium text-gray-950 dark:text-white">v{{ version.version }} · {{ policyStatusLabel(version.status) }}</p>
                    <p class="mt-1 font-mono text-[11px] text-gray-400">{{ version.config_digest.slice(0, 16) }}</p>
                  </div>
                  <div class="flex flex-wrap gap-2">
                    <button v-if="version.status === 'draft'" type="button" class="btn btn-secondary btn-xs" @click="transitionPolicy(version, 'validate')">{{ t('admin.promptAudit.core.policies.validate') }}</button>
                    <button type="button" class="btn btn-secondary btn-xs" :disabled="policyReplayLoading === version.id" @click="runPolicyReplay(version)">{{ t('admin.promptAudit.core.policies.replay') }}</button>
                    <button v-if="version.shadowed_at" type="button" class="btn btn-secondary btn-xs" :disabled="policyShadowLoading === version.id" @click="loadPolicyShadow(version)">{{ t('admin.promptAudit.core.policies.liveShadow') }}</button>
                    <button v-if="version.status === 'validated'" type="button" class="btn btn-secondary btn-xs" @click="transitionPolicy(version, 'shadow')">{{ t('admin.promptAudit.core.policies.shadow') }}</button>
                    <button v-if="version.status === 'validated' || version.status === 'shadow'" type="button" class="btn btn-primary btn-xs" @click="transitionPolicy(version, 'activate')">{{ t('admin.promptAudit.core.policies.activate') }}</button>
                    <button v-if="version.status === 'retired'" type="button" class="btn btn-secondary btn-xs" @click="transitionPolicy(version, 'rollback')">{{ t('admin.promptAudit.core.policies.rollback') }}</button>
                  </div>
                </div>
                <div class="mt-3 grid grid-cols-2 gap-2 text-xs text-gray-500 dark:text-dark-300">
                  <span>{{ t('admin.promptAudit.core.policies.mode') }}: {{ version.config.mode }}</span>
                  <span>{{ t('admin.promptAudit.core.policies.priority') }}: {{ version.priority }}</span>
                  <span>{{ t('admin.promptAudit.core.policies.detectors') }}: {{ version.config.detectors.filter((item) => item.enabled).length }}</span>
                  <span>{{ t('admin.promptAudit.core.policies.signalRules') }}: {{ version.config.signals?.rules?.filter((item) => item.enabled).length ?? 0 }}</span>
                </div>
                <ul v-if="version.validation_errors.length" class="mt-3 list-disc pl-5 text-xs text-red-600 dark:text-red-300">
                  <li v-for="error in version.validation_errors" :key="error">{{ error }}</li>
                </ul>
              </article>
            </div>
            <section class="mt-4 border border-gray-200 p-3 dark:border-dark-700">
              <h4 class="text-sm font-medium text-gray-950 dark:text-white">{{ t('admin.promptAudit.core.policies.transitionHistory') }}</h4>
              <ol v-if="policyTransitions.length" class="mt-3 space-y-3">
                <li
                  v-for="transition in policyTransitions"
                  :key="transition.id"
                  class="grid gap-1 border-l-2 border-primary-400 pl-3 text-xs sm:grid-cols-[minmax(0,1fr)_auto]"
                >
                  <div class="min-w-0">
                    <p class="font-medium text-gray-900 dark:text-white">
                      v{{ transition.version }} · {{ policyStatusLabel(transition.from_status) }} → {{ policyStatusLabel(transition.to_status) }}
                    </p>
                    <p class="mt-1 break-words text-gray-600 dark:text-dark-300">{{ transition.reason || t('admin.promptAudit.core.policies.noReason') }}</p>
                  </div>
                  <div class="text-gray-400 sm:text-right">
                    <p>{{ formatDate(transition.created_at) }}</p>
                    <p v-if="transition.actor_id" class="mt-1 font-mono">{{ t('admin.promptAudit.core.policies.actor', { id: transition.actor_id }) }}</p>
                  </div>
                </li>
              </ol>
              <EmptyState v-else :text="t('admin.promptAudit.core.policies.noTransitions')" />
            </section>
            <article v-if="policyReplayResult" class="mt-4 border border-primary-200 bg-primary-50/40 p-4 dark:border-primary-900 dark:bg-primary-950/10">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h4 class="font-medium text-gray-950 dark:text-white">{{ t('admin.promptAudit.core.policies.replayResult', { version: policyReplayResult.policy_version }) }}</h4>
                <span class="text-xs text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.policies.replayWindow', { hours: policyReplayResult.window_hours }) }}</span>
              </div>
              <dl class="mt-3 grid grid-cols-2 gap-3 text-xs sm:grid-cols-4">
                <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.policies.replayAnalyzed') }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ formatNumber(policyReplayResult.analyzed) }}</dd></div>
                <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.policies.replayChanges') }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ formatNumber(policyReplayResult.action_changes) }}</dd></div>
                <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.policies.replayStricter') }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums text-red-600 dark:text-red-300">{{ formatNumber(policyReplayResult.stricter_changes) }}</dd></div>
                <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.policies.replayLooser') }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums text-amber-600 dark:text-amber-300">{{ formatNumber(policyReplayResult.looser_changes) }}</dd></div>
              </dl>
              <div v-if="policyReplayResult.examples.length" class="mt-4 overflow-x-auto border-t border-primary-200 pt-3 dark:border-primary-900">
                <table class="min-w-full text-xs">
                  <tbody>
                    <tr v-for="example in policyReplayResult.examples.slice(0, 8)" :key="example.decision_pk" class="border-b border-primary-100 last:border-b-0 dark:border-primary-950">
                      <td class="py-2 pr-3 font-mono">{{ example.decision_id }}</td>
                      <td class="py-2 pr-3">{{ sourceLabel(example.source_type) }}</td>
                      <td class="py-2">{{ requestActionLabel(example.previous_action) }} → {{ requestActionLabel(example.proposed_action) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </article>
            <article v-if="policyShadowResult" class="mt-4 border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h4 class="font-medium text-gray-950 dark:text-white">{{ t('admin.promptAudit.core.policies.liveShadowResult', { version: policyShadowResult.policy_version }) }}</h4>
                  <p class="mt-1 text-xs text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.policies.liveShadowHint') }}</p>
                </div>
                <span class="text-xs text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.core.policies.replayWindow', { hours: policyShadowResult.window_hours }) }}</span>
              </div>
              <dl class="mt-3 grid grid-cols-2 gap-3 text-xs sm:grid-cols-5">
                <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.policies.shadowTotal') }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ formatNumber(policyShadowResult.total) }}</dd></div>
                <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.policies.shadowRequestChanges') }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ formatNumber(policyShadowResult.request_action_changes) }}</dd></div>
                <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.policies.shadowActionChanges') }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums">{{ formatNumber(policyShadowResult.candidate_action_changes) }}</dd></div>
                <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.policies.replayStricter') }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums text-red-600 dark:text-red-300">{{ formatNumber(policyShadowResult.stricter_changes) }}</dd></div>
                <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.policies.replayLooser') }}</dt><dd class="mt-1 text-lg font-semibold tabular-nums text-amber-600 dark:text-amber-300">{{ formatNumber(policyShadowResult.looser_changes) }}</dd></div>
              </dl>
              <p v-if="policyShadowResult.last_error" class="mt-3 text-xs text-red-600 dark:text-red-300">{{ policyShadowResult.last_error }}</p>
              <div v-if="policyShadowResult.items.length" class="mt-4 overflow-x-auto border-t border-gray-200 pt-3 dark:border-dark-700">
                <table class="min-w-full text-xs">
                  <tbody>
                    <tr v-for="item in policyShadowResult.items.slice(0, 10)" :key="item.id" class="border-b border-gray-200 last:border-b-0 dark:border-dark-700">
                      <td class="py-2 pr-3 font-mono">{{ item.decision_id }}</td>
                      <td class="py-2 pr-3">{{ sourceLabel(item.source_type) }}</td>
                      <td class="py-2 pr-3">{{ requestActionLabel(item.baseline_request_action) }} → {{ requestActionLabel(item.proposed_request_action) }}</td>
                      <td class="py-2 text-gray-500 dark:text-dark-300">{{ formatDate(item.decision_created_at) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <EmptyState v-else :text="t('admin.promptAudit.core.policies.noShadowSamples')" />
            </article>
          </div>
          <EmptyState v-else :text="t('admin.promptAudit.core.policies.select')" />
        </div>
      </div>
    </div>

    <div v-else>
      <div class="mb-4 overflow-x-auto">
        <div class="inline-flex min-w-max gap-1 border border-gray-200 p-1 dark:border-dark-700">
          <button
            v-for="item in operationSections"
            :key="item.id"
            type="button"
            class="px-3 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-dark-300 dark:hover:bg-dark-700"
            :class="{ 'bg-gray-100 font-medium text-gray-950 dark:bg-dark-700 dark:text-white': operationSection === item.id }"
            @click="operationSection = item.id"
          >
            {{ item.label }}
          </button>
        </div>
      </div>

      <div v-if="operationSection === 'actions'" class="overflow-x-auto border border-gray-200 dark:border-dark-700">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
          <thead class="bg-gray-50 dark:bg-dark-800">
            <tr>
              <th class="table-th">{{ t('admin.promptAudit.core.operations.action') }}</th>
              <th class="table-th">{{ t('admin.promptAudit.core.operations.subject') }}</th>
              <th class="table-th">{{ t('admin.promptAudit.core.operations.attempts') }}</th>
              <th class="table-th">{{ t('admin.promptAudit.core.operations.status') }}</th>
              <th class="table-th">{{ t('admin.promptAudit.common.actions') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-850">
            <tr v-for="item in actions.items" :key="item.id">
              <td class="table-td"><p class="font-medium">{{ actionTypeLabel(item.action_type) }}</p><p class="mt-1 font-mono text-xs text-gray-400">{{ item.action_id }}</p></td>
              <td class="table-td">{{ item.subject_type }} #{{ item.subject_id || '—' }}</td>
              <td class="table-td tabular-nums">{{ item.attempts }} / {{ item.max_attempts }}</td>
              <td class="table-td"><span :class="actionStatusBadge(item.status)">{{ actionStatusLabel(item.status) }}</span><p v-if="item.error_message" class="mt-1 max-w-xs text-xs text-red-500">{{ item.error_message }}</p></td>
              <td class="table-td">
                <div class="flex flex-wrap gap-2">
                  <button v-if="item.status === 'failed'" type="button" class="btn btn-secondary btn-xs" @click="transitionAction(item, 'retry')">{{ t('admin.promptAudit.core.operations.retry') }}</button>
                  <button v-if="['pending', 'retry', 'failed'].includes(item.status)" type="button" class="btn btn-ghost btn-xs" @click="transitionAction(item, 'cancel')">{{ t('common.cancel') }}</button>
                  <button v-if="item.status === 'succeeded' && ['pause_user', 'pause_api_key', 'open_case'].includes(item.action_type)" type="button" class="btn btn-secondary btn-xs" @click="transitionAction(item, 'revert')">{{ t('admin.promptAudit.core.operations.revert') }}</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <EmptyState v-if="!actions.items.length && !loading.actions" :text="t('admin.promptAudit.core.operations.emptyActions')" />
        <Pagination v-if="actions.total > 0" :total="actions.total" :page="actions.page" :page-size="actions.page_size" @update:page="loadActions" @update:page-size="changeActionPageSize" />
      </div>

      <div v-else-if="operationSection === 'signals'">
        <div class="mb-4 flex flex-wrap gap-3">
          <div class="w-full sm:w-56"><Select v-model="signalSubjectType" :options="signalSubjectOptions" :aria-label="t('admin.promptAudit.core.signals.subjectType')" /></div>
          <label class="inline-flex items-center gap-2 text-sm text-gray-600 dark:text-dark-300">
            <input v-model="signalMatchedOnly" type="checkbox" class="checkbox" @change="loadSignals(1)" />
            {{ t('admin.promptAudit.core.signals.matchedOnly') }}
          </label>
          <button type="button" class="btn btn-secondary btn-sm" @click="loadSignals(1)">{{ t('common.search') }}</button>
        </div>
        <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          <article v-for="item in signals.items" :key="item.id" class="border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-850">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <p class="truncate font-medium text-gray-950 dark:text-white">{{ item.subject_snapshot }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ item.subject_type }} #{{ item.subject_id }} · {{ formatDate(item.bucket_start) }}</p>
              </div>
              <span v-if="item.matched_rules" :class="riskBadge(item.highest_severity)">{{ item.matched_rules }}</span>
            </div>
            <dl class="mt-4 grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
              <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.signals.requests') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ formatNumber(item.request_count) }}</dd></div>
              <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.signals.tokens') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ formatNumber(item.token_count) }}</dd></div>
              <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.signals.cost') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ formatMoney(item.actual_cost) }}</dd></div>
              <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.signals.errors') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ formatNumber(item.error_count) }}</dd></div>
              <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.signals.ipFanout') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ item.distinct_ip_count }}</dd></div>
              <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.signals.maxLatency') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ item.duration_max_ms }} ms</dd></div>
            </dl>
          </article>
          <EmptyState v-if="!signals.items.length && !loading.signals" class="sm:col-span-2 xl:col-span-3" :text="t('admin.promptAudit.core.signals.empty')" />
        </div>
        <Pagination v-if="signals.total > 0" class="mt-4 border border-gray-200 dark:border-dark-700" :total="signals.total" :page="signals.page" :page-size="signals.page_size" @update:page="loadSignals" @update:page-size="changeSignalPageSize" />
      </div>

      <div v-else-if="operationSection === 'exceptions'">
        <div class="mb-4 flex justify-end"><button type="button" class="btn btn-primary btn-sm" @click="showExceptionCreate = true">{{ t('admin.promptAudit.core.exceptions.create') }}</button></div>
        <div class="grid gap-3">
          <article v-for="item in exceptions" :key="item.id" class="flex flex-col gap-3 border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-850 md:flex-row md:items-center md:justify-between">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2"><p class="font-medium text-gray-950 dark:text-white">{{ item.name }}</p><span class="badge badge-gray">{{ item.status }}</span></div>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ item.scope_type }}={{ item.scope_id }} · {{ item.effect }}</p>
              <p class="mt-2 text-xs text-gray-400">{{ item.reason }}</p>
            </div>
            <button v-if="item.status === 'active'" type="button" class="btn btn-secondary btn-sm" @click="openExceptionExpire(item)">{{ t('admin.promptAudit.core.exceptions.expire') }}</button>
          </article>
          <EmptyState v-if="!exceptions.length && !loading.exceptions" :text="t('admin.promptAudit.core.exceptions.empty')" />
        </div>
      </div>

      <div v-else-if="operationSection === 'endpoints'" class="grid gap-3 md:grid-cols-2">
        <article v-for="item in endpointHealth" :key="item.endpoint_id" class="border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-850">
          <div class="flex items-start justify-between gap-3">
            <div><p class="font-medium text-gray-950 dark:text-white">{{ item.endpoint_id }}</p><p class="mt-1 text-xs text-gray-500 dark:text-dark-300">{{ item.network_scope }}</p></div>
            <span :class="endpointStatusBadge(item.status)">{{ item.status }}</span>
          </div>
          <dl class="mt-4 grid grid-cols-2 gap-3 text-xs">
            <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.endpoints.breaker') }}</dt><dd class="mt-1 font-medium">{{ item.breaker_state }}</dd></div>
            <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.endpoints.failures') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ item.consecutive_failures }}</dd></div>
            <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.endpoints.successRate') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ endpointSuccessRate(item) }}</dd></div>
            <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.endpoints.requests') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ formatNumber(item.request_count) }}</dd></div>
            <div><dt class="text-gray-500">HTTP</dt><dd class="mt-1 font-medium tabular-nums">{{ item.http_status || '—' }}</dd></div>
            <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.endpoints.latency') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ item.latency_ms }} ms</dd></div>
            <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.endpoints.timeouts') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ formatNumber(item.timeout_count) }}</dd></div>
            <div><dt class="text-gray-500">{{ t('admin.promptAudit.core.endpoints.rateLimited') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ formatNumber(item.rate_limited_count) }}</dd></div>
          </dl>
          <button v-if="item.breaker_state !== 'closed'" type="button" class="btn btn-secondary btn-sm mt-4" @click="resetEndpoint(item)">{{ t('admin.promptAudit.core.endpoints.reset') }}</button>
        </article>
        <EmptyState v-if="!endpointHealth.length && !loading.endpoints" class="md:col-span-2" :text="t('admin.promptAudit.core.endpoints.empty')" />
      </div>

      <div v-else>
        <div v-if="notifications.some((item) => item.audience === 'admin' && item.status === 'unread')" class="mb-3 flex justify-end">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="loading.mutation" @click="markAllNotificationsRead">
            {{ t('admin.promptAudit.core.notifications.markAllRead') }}
          </button>
        </div>
        <div class="grid gap-3">
        <article v-for="item in notifications" :key="item.id" class="flex flex-col gap-3 border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-850 md:flex-row md:items-start md:justify-between">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2"><p class="font-medium text-gray-950 dark:text-white">{{ item.title }}</p><span :class="riskBadge(item.severity)">{{ riskLabel(item.severity) }}</span></div>
            <p class="mt-2 text-sm text-gray-600 dark:text-dark-200">{{ item.body }}</p>
            <p class="mt-2 text-xs text-gray-400">{{ item.audience }} · {{ formatDate(item.created_at) }}</p>
          </div>
          <div class="flex shrink-0 gap-2">
            <button v-if="item.status === 'unread'" type="button" class="btn btn-secondary btn-sm" @click="markNotification(item, 'read')">{{ t('admin.promptAudit.core.notifications.markRead') }}</button>
            <button type="button" class="btn btn-ghost btn-sm" @click="markNotification(item, 'dismissed')">{{ t('admin.promptAudit.core.notifications.dismiss') }}</button>
          </div>
        </article>
        <EmptyState v-if="!notifications.length && !loading.notifications" :text="t('admin.promptAudit.core.notifications.empty')" />
        </div>
      </div>
    </div>

    <BaseDialog :show="showDecisionDetail" :title="t('admin.promptAudit.core.decisions.detailTitle')" width="extra-wide" @close="closeDecision">
      <div v-if="activeDecision" class="space-y-5">
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <InfoCell :label="t('admin.promptAudit.core.decisions.source')" :value="sourceLabel(activeDecision.source_type)" />
          <InfoCell :label="t('admin.promptAudit.core.decisions.result')" :value="`${riskLabel(activeDecision.risk_level)} · ${requestActionLabel(activeDecision.request_action)}`" />
          <InfoCell :label="t('admin.promptAudit.core.decisions.policy')" :value="`${activeDecision.policy_key}@${activeDecision.policy_version}`" />
          <InfoCell :label="t('admin.promptAudit.core.decisions.time')" :value="formatDate(activeDecision.created_at)" />
        </div>
        <div class="border border-gray-200 p-4 dark:border-dark-700">
          <p class="text-xs font-medium uppercase tracking-wide text-gray-500">{{ t('admin.promptAudit.core.decisions.safePreview') }}</p>
          <p class="mt-2 whitespace-pre-wrap break-words text-sm text-gray-800 dark:text-dark-100">{{ activeDecision.redacted_preview || '—' }}</p>
        </div>
        <div>
          <h4 class="font-medium text-gray-950 dark:text-white">{{ t('admin.promptAudit.core.decisions.evidence') }}</h4>
          <div class="mt-3 grid gap-3">
            <article v-for="item in activeDecision.evidence" :key="item.id" class="border border-gray-200 p-3 dark:border-dark-700">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span class="font-mono text-xs text-gray-700 dark:text-dark-200">{{ item.detector_id }}</span>
                <span :class="riskBadge(item.severity)">{{ riskLabel(item.severity) }}</span>
              </div>
              <p class="mt-2 text-sm text-gray-600 dark:text-dark-300">{{ item.safe_summary || '—' }}</p>
            </article>
          </div>
        </div>
        <div class="grid gap-4 lg:grid-cols-3">
          <div class="border border-gray-200 p-4 dark:border-dark-700">
            <label class="mb-2 block text-sm font-medium">{{ t('admin.promptAudit.core.decisions.revealReason') }}</label>
            <textarea v-model.trim="decisionRevealReason" class="input min-h-24" />
            <button type="button" class="btn btn-secondary btn-sm mt-3" :disabled="decisionRevealReason.length < 3 || loading.mutation" @click="revealDecisionEvidence">{{ t('admin.promptAudit.core.decisions.reveal') }}</button>
            <pre v-if="revealedDecisionPrompt" class="mt-3 max-h-56 overflow-auto whitespace-pre-wrap break-words border border-gray-200 p-3 text-xs dark:border-dark-700">{{ revealedDecisionPrompt }}</pre>
          </div>
          <div class="border border-gray-200 p-4 dark:border-dark-700">
            <label class="mb-2 block text-sm font-medium">{{ t('admin.promptAudit.core.decisions.openCaseReason') }}</label>
            <textarea v-model.trim="decisionCaseReason" class="input min-h-24" />
            <button type="button" class="btn btn-primary btn-sm mt-3" :disabled="decisionCaseReason.length < 3 || loading.mutation" @click="createCaseFromDecision">{{ t('admin.promptAudit.core.decisions.openCase') }}</button>
          </div>
          <div class="border border-gray-200 p-4 dark:border-dark-700">
            <label class="mb-2 block text-sm font-medium">{{ t('admin.promptAudit.core.decisions.feedback') }}</label>
            <Select v-model="decisionFeedback.conclusion" :options="feedbackOptions" :aria-label="t('admin.promptAudit.core.decisions.feedback')" />
            <textarea v-model.trim="decisionFeedback.note" class="input mt-3 min-h-16" :placeholder="t('admin.promptAudit.core.decisions.feedbackNote')" />
            <button type="button" class="btn btn-secondary btn-sm mt-3" :disabled="loading.mutation" @click="submitDecisionFeedback">{{ t('common.submit') }}</button>
          </div>
        </div>
      </div>
      <div v-else class="py-16 text-center"><span class="loading-spinner" /></div>
    </BaseDialog>

    <BaseDialog :show="showCaseDetail" :title="t('admin.promptAudit.core.cases.detailTitle')" width="wide" @close="closeCase">
      <div v-if="activeCase" class="space-y-5">
        <div class="grid gap-3 sm:grid-cols-3">
          <InfoCell :label="t('admin.promptAudit.core.cases.status')" :value="caseStatusLabel(activeCase.status)" />
          <InfoCell :label="t('admin.promptAudit.core.cases.severity')" :value="riskLabel(activeCase.severity)" />
          <InfoCell :label="t('admin.promptAudit.core.decisions.time')" :value="formatDate(activeCase.created_at)" />
        </div>
        <div class="border border-gray-200 p-4 dark:border-dark-700"><p class="text-sm text-gray-700 dark:text-dark-200">{{ activeCase.opened_reason }}</p></div>
        <div class="space-y-3">
          <article v-for="event in activeCase.timeline" :key="event.id" class="border-l-2 border-gray-300 pl-4 dark:border-dark-600">
            <p class="text-sm font-medium text-gray-900 dark:text-white">{{ event.summary }}</p>
            <p class="mt-1 text-xs text-gray-400">{{ formatDate(event.created_at) }}</p>
          </article>
        </div>
        <div v-if="['open', 'reviewing'].includes(activeCase.status)" class="border-t border-gray-200 pt-4 dark:border-dark-700">
          <div class="grid gap-3 md:grid-cols-2">
            <Select v-model="caseTransition.status" :options="caseTransitionOptions" :aria-label="t('admin.promptAudit.core.cases.status')" />
            <input v-model.trim="caseTransition.labels" class="input" :placeholder="t('admin.promptAudit.core.cases.labels')" />
          </div>
          <textarea v-model.trim="caseTransition.resolution_note" class="input mt-3 min-h-24" :placeholder="t('admin.promptAudit.core.cases.resolutionNote')" />
          <label class="mt-3 inline-flex items-center gap-2 text-sm"><input v-model="caseTransition.revert_actions" type="checkbox" class="checkbox" />{{ t('admin.promptAudit.core.cases.revertActions') }}</label>
          <div class="mt-4 flex justify-end"><button type="button" class="btn btn-primary" :disabled="loading.mutation" @click="submitCaseTransition">{{ t('common.save') }}</button></div>
        </div>
      </div>
      <div v-else class="py-16 text-center"><span class="loading-spinner" /></div>
    </BaseDialog>

    <BaseDialog :show="showPolicyCreate" :title="t('admin.promptAudit.core.policies.createTitle')" width="wide" @close="showPolicyCreate = false">
      <div class="space-y-4">
        <div>
          <label class="mb-1 block text-sm font-medium">{{ t('admin.promptAudit.core.policies.key') }}</label>
          <input v-model.trim="policyCreate.policy_key" class="input font-mono" placeholder="default_security" />
        </div>
        <div>
          <label class="mb-1 block text-sm font-medium">{{ t('admin.promptAudit.core.policies.changeReason') }}</label>
          <input v-model.trim="policyCreate.change_reason" class="input" />
        </div>
        <div>
          <label class="mb-1 block text-sm font-medium">{{ t('admin.promptAudit.core.policies.configJson') }}</label>
          <textarea v-model="policyCreate.config_json" class="input min-h-[420px] font-mono text-xs" spellcheck="false" />
          <p v-if="policyCreateError" class="mt-2 text-sm text-red-600 dark:text-red-300">{{ policyCreateError }}</p>
        </div>
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showPolicyCreate = false">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="loading.mutation" @click="createPolicy">{{ t('common.create') }}</button>
      </template>
    </BaseDialog>

    <BaseDialog :show="showExceptionCreate" :title="t('admin.promptAudit.core.exceptions.createTitle')" width="normal" @close="showExceptionCreate = false">
      <div class="space-y-4">
        <input v-model.trim="exceptionCreate.name" class="input" :placeholder="t('admin.promptAudit.core.exceptions.name')" />
        <div class="grid gap-3 sm:grid-cols-2">
          <Select v-model="exceptionCreate.scope_type" :options="exceptionScopeOptions" :aria-label="t('admin.promptAudit.core.exceptions.scope')" />
          <input v-model.trim="exceptionCreate.scope_id" class="input" :placeholder="t('admin.promptAudit.core.exceptions.scopeId')" />
        </div>
        <Select v-model="exceptionCreate.effect" :options="exceptionEffectOptions" :aria-label="t('admin.promptAudit.core.exceptions.effect')" />
        <textarea v-model.trim="exceptionCreate.reason" class="input min-h-24" :placeholder="t('admin.promptAudit.core.exceptions.reason')" />
        <label class="inline-flex items-center gap-2 text-sm"><input v-model="exceptionCreate.permanent" type="checkbox" class="checkbox" />{{ t('admin.promptAudit.core.exceptions.permanent') }}</label>
        <input v-if="!exceptionCreate.permanent" v-model="exceptionCreate.expires_at" type="datetime-local" class="input" />
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="showExceptionCreate = false">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="loading.mutation" @click="createException">{{ t('common.create') }}</button>
      </template>
    </BaseDialog>

    <BaseDialog :show="showExceptionExpire" :title="t('admin.promptAudit.core.exceptions.expireTitle')" width="normal" @close="closeExceptionExpire">
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-dark-200">
          {{ t('admin.promptAudit.core.exceptions.expireDescription', { name: exceptionPendingExpire?.name ?? '' }) }}
        </p>
        <div>
          <label class="mb-1 block text-sm font-medium">{{ t('admin.promptAudit.core.exceptions.expireReason') }}</label>
          <textarea v-model.trim="exceptionExpireReason" class="input min-h-24" :placeholder="t('admin.promptAudit.core.exceptions.expireReasonPlaceholder')" />
        </div>
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="closeExceptionExpire">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="loading.mutation || exceptionExpireReason.length < 3" @click="expireException">
          {{ t('admin.promptAudit.core.exceptions.expire') }}
        </button>
      </template>
    </BaseDialog>

    <TotpStepUpDialog :controller="stepUp" />
  </section>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import promptAuditAPI from '../api'
import type {
  SecurityActionPage,
  SecurityAuditCase,
  SecurityAuditException,
  SecurityAuditNotification,
  SecurityAuditOverview,
  SecurityBehaviorSignalPage,
  SecurityCasePage,
  SecurityEndpointHealth,
  SecurityEnforcementAction,
  SecurityPolicyConfig,
  SecurityPolicyReplayResult,
  SecurityPolicyShadowEvaluationSummary,
  SecurityPolicySummary,
  SecurityPolicyTransition,
  SecurityPolicyVersion,
  SecurityUnifiedDecision,
  SecurityDecisionPage,
} from '../types'

type WorkbenchSection = 'overview' | 'decisions' | 'cases' | 'policies' | 'operations'
type OperationSection = 'actions' | 'signals' | 'exceptions' | 'endpoints' | 'notifications'

const { t, locale } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()
const section = ref<WorkbenchSection>('overview')
const operationSection = ref<OperationSection>('actions')
const loading = reactive({
  overview: false, decisions: false, cases: false, policies: false, actions: false,
  signals: false, exceptions: false, endpoints: false, notifications: false,
  detail: false, mutation: false,
})
const errors = reactive<Record<string, string>>({})
const overview = ref<SecurityAuditOverview | null>(null)
const decisions = reactive<SecurityDecisionPage>({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
const cases = reactive<SecurityCasePage>({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
const actions = reactive<SecurityActionPage>({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
const signals = reactive<SecurityBehaviorSignalPage>({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
const policies = ref<SecurityPolicySummary[]>([])
const policyVersions = ref<SecurityPolicyVersion[]>([])
const policyTransitions = ref<SecurityPolicyTransition[]>([])
const selectedPolicyKey = ref('')
const policyTransitionReason = ref('')
const policyReplayResult = ref<SecurityPolicyReplayResult | null>(null)
const policyReplayLoading = ref(0)
const policyShadowResult = ref<SecurityPolicyShadowEvaluationSummary | null>(null)
const policyShadowLoading = ref(0)
const exceptions = ref<SecurityAuditException[]>([])
const endpointHealth = ref<SecurityEndpointHealth[]>([])
const notifications = ref<SecurityAuditNotification[]>([])
const decisionFilters = reactive({ keyword: '', risk_level: '', source_type: '', request_action: '' })
const caseFilters = reactive({ keyword: '', status: '', severity: '' })
const signalSubjectType = ref('')
const signalMatchedOnly = ref(false)
const activeDecision = ref<SecurityUnifiedDecision | null>(null)
const activeCase = ref<SecurityAuditCase | null>(null)
const showDecisionDetail = ref(false)
const showCaseDetail = ref(false)
const showPolicyCreate = ref(false)
const showExceptionCreate = ref(false)
const showExceptionExpire = ref(false)
const exceptionPendingExpire = ref<SecurityAuditException | null>(null)
const exceptionExpireReason = ref('')
const decisionRevealReason = ref('')
const decisionCaseReason = ref('')
const revealedDecisionPrompt = ref('')
const decisionFeedback = reactive({ conclusion: 'confirmed', note: '' })
const caseTransition = reactive({ status: 'reviewing', labels: '', resolution_note: '', revert_actions: false })
const policyCreateError = ref('')
const policyCreate = reactive({ policy_key: '', change_reason: '', config_json: '' })
const exceptionCreate = reactive({
  name: '', scope_type: 'user', scope_id: '', effect: 'warn_only', reason: '',
  permanent: false, expires_at: '',
})

const EmptyState = defineComponent({
  props: { text: { type: String, required: true } },
  setup(props) {
    return () => h('div', { class: 'px-5 py-12 text-center text-sm text-gray-500 dark:text-dark-400' }, props.text)
  },
})
const InfoCell = defineComponent({
  props: { label: { type: String, required: true }, value: { type: String, required: true } },
  setup(props) {
    return () => h('div', { class: 'border border-gray-200 p-3 dark:border-dark-700' }, [
      h('p', { class: 'text-xs text-gray-500 dark:text-dark-400' }, props.label),
      h('p', { class: 'mt-1 break-words text-sm font-medium text-gray-950 dark:text-white' }, props.value),
    ])
  },
})

const sections = computed(() => [
  { id: 'overview' as const, label: t('admin.promptAudit.core.tabs.overview') },
  { id: 'decisions' as const, label: t('admin.promptAudit.core.tabs.decisions'), count: overview.value?.total_decisions },
  { id: 'cases' as const, label: t('admin.promptAudit.core.tabs.cases'), count: overview.value?.open_cases },
  { id: 'policies' as const, label: t('admin.promptAudit.core.tabs.policies'), count: overview.value?.active_policies },
  { id: 'operations' as const, label: t('admin.promptAudit.core.tabs.operations'), count: overview.value?.pending_actions },
])
const operationSections = computed(() => [
  { id: 'actions' as const, label: t('admin.promptAudit.core.operations.actions') },
  { id: 'signals' as const, label: t('admin.promptAudit.core.operations.signals') },
  { id: 'exceptions' as const, label: t('admin.promptAudit.core.operations.exceptions') },
  { id: 'endpoints' as const, label: t('admin.promptAudit.core.operations.endpoints') },
  { id: 'notifications' as const, label: t('admin.promptAudit.core.operations.notifications') },
])
const isLoading = computed(() => Object.values(loading).some(Boolean))
const activeError = computed(() => {
  if (section.value === 'operations') return errors[operationSection.value] || ''
  return errors[section.value] || ''
})
const overviewMetrics = computed(() => [
  { key: 'total', label: t('admin.promptAudit.core.overview.total'), value: overview.value?.total_decisions ?? 0, hint: t('admin.promptAudit.core.overview.totalHint') },
  { key: 'cases', label: t('admin.promptAudit.core.overview.openCases'), value: overview.value?.open_cases ?? 0, hint: t('admin.promptAudit.core.overview.openCasesHint') },
  { key: 'actions', label: t('admin.promptAudit.core.overview.pendingActions'), value: overview.value?.pending_actions ?? 0, hint: t('admin.promptAudit.core.overview.pendingActionsHint', { failed: overview.value?.failed_actions ?? 0 }) },
  { key: 'behavior', label: t('admin.promptAudit.core.overview.behaviorMatches'), value: overview.value?.behavior_matches ?? 0, hint: t('admin.promptAudit.core.overview.behaviorMatchesHint') },
])
const decisionBreakdown = computed(() => [
  { label: t('admin.promptAudit.core.overview.allowed'), value: overview.value?.allowed ?? 0, className: 'text-emerald-600 dark:text-emerald-400' },
  { label: t('admin.promptAudit.core.overview.warned'), value: overview.value?.warned ?? 0, className: 'text-amber-600 dark:text-amber-400' },
  { label: t('admin.promptAudit.core.overview.blocked'), value: overview.value?.blocked ?? 0, className: 'text-red-600 dark:text-red-400' },
])
const sourceBreakdown = computed(() => {
  const entries = Object.entries(overview.value?.by_source ?? {}).sort((a, b) => b[1] - a[1])
  const maximum = Math.max(...entries.map((item) => item[1]), 1)
  return entries.map(([key, value]) => ({ key, value, percent: Math.max(2, Math.round((value / maximum) * 100)) }))
})
const riskOptions = computed(() => [
  { value: '', label: t('admin.promptAudit.core.common.all') },
  ...['low', 'medium', 'high', 'critical'].map((value) => ({ value, label: riskLabel(value) })),
])
const sourceOptions = computed(() => [
  { value: '', label: t('admin.promptAudit.core.common.all') },
  ...['prompt_audit', 'legacy_moderation', 'cyber_policy', 'behavior', 'manual'].map((value) => ({ value, label: sourceLabel(value) })),
])
const requestActionOptions = computed(() => [
  { value: '', label: t('admin.promptAudit.core.common.all') },
  ...['allow', 'warn', 'block'].map((value) => ({ value, label: requestActionLabel(value) })),
])
const caseStatusOptions = computed(() => [
  { value: '', label: t('admin.promptAudit.core.common.all') },
  ...['open', 'reviewing', 'confirmed', 'false_positive', 'dismissed', 'expired'].map((value) => ({ value, label: caseStatusLabel(value) })),
])
const caseTransitionOptions = computed(() => ['open', 'reviewing', 'confirmed', 'false_positive', 'dismissed'].map((value) => ({ value, label: caseStatusLabel(value) })))
const feedbackOptions = computed(() => ['confirmed', 'false_positive', 'false_negative', 'needs_more_info'].map((value) => ({ value, label: t(`admin.promptAudit.core.feedback.${value}`) })))
const signalSubjectOptions = computed(() => [
  { value: '', label: t('admin.promptAudit.core.common.all') },
  ...['user', 'api_key', 'group'].map((value) => ({ value, label: t(`admin.promptAudit.core.subjects.${value}`) })),
])
const exceptionScopeOptions = computed(() => ['user', 'api_key', 'group', 'model', 'endpoint', 'detector', 'category'].map((value) => ({ value, label: t(`admin.promptAudit.core.exceptionScopes.${value}`) })))
const exceptionEffectOptions = computed(() => ['allow_and_record', 'warn_only'].map((value) => ({ value, label: t(`admin.promptAudit.core.exceptionEffects.${value}`) })))

function apiError(error: unknown) { return extractApiErrorMessage(error, t('admin.promptAudit.core.common.requestFailed')) }
function clearError(key: string) { errors[key] = '' }
function queryParams(source: Record<string, unknown>) {
  return Object.fromEntries(Object.entries(source).filter(([, value]) => value !== '' && value !== undefined && value !== null))
}
async function loadOverview() {
  loading.overview = true; clearError('overview')
  try {
    overview.value = await promptAuditAPI.getSecurityOverview(24)
    notifications.value = await promptAuditAPI.listSecurityNotifications({ status: 'unread', audience: 'admin', limit: 10 })
  } catch (error) { errors.overview = apiError(error) }
  finally { loading.overview = false }
}
async function loadDecisions(page = decisions.page) {
  loading.decisions = true; clearError('decisions')
  try {
    Object.assign(decisions, await promptAuditAPI.listSecurityDecisions(queryParams({ ...decisionFilters, page, page_size: decisions.page_size })))
  } catch (error) { errors.decisions = apiError(error) }
  finally { loading.decisions = false }
}
async function loadCases(page = cases.page) {
  loading.cases = true; clearError('cases')
  try { Object.assign(cases, await promptAuditAPI.listSecurityCases(queryParams({ ...caseFilters, page, page_size: cases.page_size }))) }
  catch (error) { errors.cases = apiError(error) }
  finally { loading.cases = false }
}
async function loadPolicies() {
  loading.policies = true; clearError('policies')
  try {
    policies.value = await promptAuditAPI.listSecurityPolicies()
    if (selectedPolicyKey.value) await selectPolicy(selectedPolicyKey.value)
  } catch (error) { errors.policies = apiError(error) }
  finally { loading.policies = false }
}
async function loadActions(page = actions.page) {
  loading.actions = true; clearError('actions')
  try { Object.assign(actions, await promptAuditAPI.listSecurityActions({ page, page_size: actions.page_size })) }
  catch (error) { errors.actions = apiError(error) }
  finally { loading.actions = false }
}
async function loadSignals(page = signals.page) {
  loading.signals = true; clearError('signals')
  try { Object.assign(signals, await promptAuditAPI.listSecurityBehaviorSignals(queryParams({ page, page_size: signals.page_size, subject_type: signalSubjectType.value, matched_only: signalMatchedOnly.value || undefined }))) }
  catch (error) { errors.signals = apiError(error) }
  finally { loading.signals = false }
}
async function loadExceptions() {
  loading.exceptions = true; clearError('exceptions')
  try { exceptions.value = await promptAuditAPI.listSecurityExceptions(true) }
  catch (error) { errors.exceptions = apiError(error) }
  finally { loading.exceptions = false }
}
async function loadEndpoints() {
  loading.endpoints = true; clearError('endpoints')
  try { endpointHealth.value = await promptAuditAPI.listSecurityEndpointHealth() }
  catch (error) { errors.endpoints = apiError(error) }
  finally { loading.endpoints = false }
}
async function loadNotifications() {
  loading.notifications = true; clearError('notifications')
  try { notifications.value = await promptAuditAPI.listSecurityNotifications({ audience: 'admin', limit: 100 }) }
  catch (error) { errors.notifications = apiError(error) }
  finally { loading.notifications = false }
}
async function refreshActive() {
  if (section.value === 'overview') return loadOverview()
  if (section.value === 'decisions') return loadDecisions()
  if (section.value === 'cases') return loadCases()
  if (section.value === 'policies') return loadPolicies()
  return loadOperation()
}
async function loadOperation() {
  if (operationSection.value === 'actions') return loadActions()
  if (operationSection.value === 'signals') return loadSignals()
  if (operationSection.value === 'exceptions') return loadExceptions()
  if (operationSection.value === 'endpoints') return loadEndpoints()
  return loadNotifications()
}
function changeDecisionPageSize(value: number) { decisions.page_size = value; void loadDecisions(1) }
function changeCasePageSize(value: number) { cases.page_size = value; void loadCases(1) }
function changeActionPageSize(value: number) { actions.page_size = value; void loadActions(1) }
function changeSignalPageSize(value: number) { signals.page_size = value; void loadSignals(1) }
async function openDecision(id: number) {
  showDecisionDetail.value = true; activeDecision.value = null; loading.detail = true
  decisionRevealReason.value = ''; decisionCaseReason.value = ''; revealedDecisionPrompt.value = ''
  try { activeDecision.value = await promptAuditAPI.getSecurityDecision(id) }
  catch (error) { showDecisionDetail.value = false; appStore.showError(apiError(error)) }
  finally { loading.detail = false }
}
function closeDecision() { showDecisionDetail.value = false; activeDecision.value = null; revealedDecisionPrompt.value = '' }
async function openCase(id: number) {
  showCaseDetail.value = true; activeCase.value = null; loading.detail = true
  try {
    activeCase.value = await promptAuditAPI.getSecurityCase(id)
    caseTransition.status = activeCase.value.status === 'open' ? 'reviewing' : 'confirmed'
    caseTransition.labels = activeCase.value.labels.join(', ')
    caseTransition.resolution_note = ''
    caseTransition.revert_actions = false
  } catch (error) { showCaseDetail.value = false; appStore.showError(apiError(error)) }
  finally { loading.detail = false }
}
function closeCase() { showCaseDetail.value = false; activeCase.value = null }
async function selectPolicy(key: string) {
  selectedPolicyKey.value = key
  policyReplayResult.value = null
  policyShadowResult.value = null
  try {
    const [versions, transitions] = await Promise.all([
      promptAuditAPI.listSecurityPolicyVersions(key),
      promptAuditAPI.listSecurityPolicyTransitions(key),
    ])
    policyVersions.value = versions
    policyTransitions.value = transitions
  }
  catch (error) { errors.policies = apiError(error) }
}
async function loadPolicyShadow(version: SecurityPolicyVersion) {
  policyShadowLoading.value = version.id
  try {
    policyShadowResult.value = await promptAuditAPI.getSecurityPolicyShadowEvaluations(
      version.policy_key,
      version.version,
      { window_hours: 168, limit: 50 },
    )
  } catch (error) { appStore.showError(apiError(error)) }
  finally { policyShadowLoading.value = 0 }
}
async function runPolicyReplay(version: SecurityPolicyVersion) {
  policyReplayLoading.value = version.id
  try {
    policyReplayResult.value = await promptAuditAPI.replaySecurityPolicy(version.policy_key, version.version, {
      window_hours: 168,
      limit: 1000,
    })
  } catch (error) { appStore.showError(apiError(error)) }
  finally { policyReplayLoading.value = 0 }
}
async function sensitiveRun<T>(operation: () => Promise<T>): Promise<T | undefined> {
  try { return await stepUp.run(operation) }
  catch (error) {
    if (isStepUpCancelled(error)) return undefined
    if (isStepUpBlocked(error)) {
      appStore.showError(stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? t('stepUp.adminApiKeyForbidden') : t('stepUp.notEnabled'))
      return undefined
    }
    throw error
  }
}
async function revealDecisionEvidence() {
  if (!activeDecision.value) return
  loading.mutation = true
  try {
    const result = await sensitiveRun(() => promptAuditAPI.revealSecurityEvidence(activeDecision.value!.id, decisionRevealReason.value))
    if (result) revealedDecisionPrompt.value = result.full_prompt
  } catch (error) { appStore.showError(apiError(error)) }
  finally { loading.mutation = false }
}
async function createCaseFromDecision() {
  if (!activeDecision.value) return
  loading.mutation = true
  try {
    const created = await promptAuditAPI.openSecurityDecisionCase(activeDecision.value.id, decisionCaseReason.value)
    appStore.showSuccess(t('admin.promptAudit.core.messages.caseOpened', { id: created.case_id }))
    await Promise.allSettled([loadOverview(), loadCases(1)])
  } catch (error) { appStore.showError(apiError(error)) }
  finally { loading.mutation = false }
}
async function submitDecisionFeedback() {
  if (!activeDecision.value) return
  loading.mutation = true
  try {
    await promptAuditAPI.addSecurityDecisionFeedback(activeDecision.value.id, { ...decisionFeedback })
    appStore.showSuccess(t('admin.promptAudit.core.messages.feedbackSaved'))
    decisionFeedback.note = ''
  } catch (error) { appStore.showError(apiError(error)) }
  finally { loading.mutation = false }
}
async function submitCaseTransition() {
  if (!activeCase.value) return
  loading.mutation = true
  try {
    const result = await sensitiveRun(() => promptAuditAPI.transitionSecurityCase(activeCase.value!.id, {
      status: caseTransition.status,
      labels: caseTransition.labels.split(',').map((item) => item.trim()).filter(Boolean),
      resolution_note: caseTransition.resolution_note,
      revert_actions: caseTransition.revert_actions,
    }))
    if (!result) return
    activeCase.value = result
    appStore.showSuccess(t('admin.promptAudit.core.messages.caseUpdated'))
    await Promise.allSettled([loadOverview(), loadCases(cases.page)])
  } catch (error) { appStore.showError(apiError(error)) }
  finally { loading.mutation = false }
}
function defaultPolicyConfig(): SecurityPolicyConfig {
  return {
    name: t('admin.promptAudit.core.policies.defaultName'),
    priority: 100,
    scope: { all_groups: true, group_ids: [], user_ids: [], api_key_ids: [], protocols: [], endpoints: [], models: [] },
    mode: 'blocking',
    detectors: [
      { id: 'builtin_regex', enabled: true, timeout_ms: 20 },
      { id: 'remote_guard', enabled: true, timeout_ms: 2500 },
    ],
    failure: { local_error: 'allow_and_record', remote_timeout: 'fallback_local', remote_invalid: 'block_and_record' },
    actions: { low: [], medium: ['notify_admin'], high: ['open_case', 'notify_admin'], critical: ['open_case', 'notify_admin'] },
    evidence: { mode: 'findings_encrypted', retention_days: 30 },
    signals: {
      enabled: false,
      rules: [
        { id: 'request_burst', enabled: true, metric: 'request_count', subject_type: 'api_key', window_minutes: 1, threshold: 600, minimum_samples: 100, severity: 'medium' },
        { id: 'token_burst', enabled: true, metric: 'token_count', subject_type: 'user', window_minutes: 5, threshold: 5000000, minimum_samples: 20, severity: 'high' },
        { id: 'cost_burst', enabled: true, metric: 'actual_cost', subject_type: 'user', window_minutes: 60, threshold: 100, minimum_samples: 10, severity: 'high' },
        { id: 'error_ratio', enabled: true, metric: 'error_rate', subject_type: 'api_key', window_minutes: 5, threshold: 0.8, minimum_samples: 20, severity: 'medium' },
      ],
    },
  }
}
function openPolicyCreate() {
  policyCreate.policy_key = ''
  policyCreate.change_reason = ''
  policyCreate.config_json = JSON.stringify(defaultPolicyConfig(), null, 2)
  policyCreateError.value = ''
  showPolicyCreate.value = true
}
async function createPolicy() {
  policyCreateError.value = ''
  let config: SecurityPolicyConfig
  try { config = JSON.parse(policyCreate.config_json) as SecurityPolicyConfig }
  catch (error) { policyCreateError.value = error instanceof Error ? error.message : String(error); return }
  loading.mutation = true
  try {
    const created = await promptAuditAPI.createSecurityPolicy({ policy_key: policyCreate.policy_key, config, change_reason: policyCreate.change_reason })
    showPolicyCreate.value = false
    appStore.showSuccess(t('admin.promptAudit.core.messages.policyCreated', { version: created.version }))
    await loadPolicies()
    await selectPolicy(created.policy_key)
  } catch (error) { policyCreateError.value = apiError(error) }
  finally { loading.mutation = false }
}
async function transitionPolicy(version: SecurityPolicyVersion, transition: 'validate' | 'shadow' | 'activate' | 'rollback') {
  if (transition !== 'validate' && policyTransitionReason.value.trim().length < 3) {
    appStore.showError(t('admin.promptAudit.core.policies.changeReasonRequired'))
    return
  }
  loading.mutation = true
  try {
    const run = () => promptAuditAPI.transitionSecurityPolicy(version.policy_key, version.version, transition, policyTransitionReason.value)
    const result = transition === 'activate' || transition === 'rollback' ? await sensitiveRun(run) : await run()
    if (!result) return
    appStore.showSuccess(t('admin.promptAudit.core.messages.policyTransitioned'))
    await Promise.allSettled([loadPolicies(), loadOverview()])
  } catch (error) { appStore.showError(apiError(error)) }
  finally { loading.mutation = false }
}
async function transitionAction(item: SecurityEnforcementAction, transition: 'retry' | 'cancel' | 'revert') {
  loading.mutation = true
  try {
    const run = () => promptAuditAPI.transitionSecurityAction(item.id, transition)
    const result = await sensitiveRun(run)
    if (!result) return
    appStore.showSuccess(t('admin.promptAudit.core.messages.actionUpdated'))
    await Promise.allSettled([loadActions(actions.page), loadOverview()])
  } catch (error) { appStore.showError(apiError(error)) }
  finally { loading.mutation = false }
}
async function createException() {
  loading.mutation = true
  try {
    const expiresAt = exceptionCreate.permanent ? undefined : new Date(exceptionCreate.expires_at).toISOString()
    const result = await sensitiveRun(() => promptAuditAPI.createSecurityException({
      name: exceptionCreate.name, scope_type: exceptionCreate.scope_type, scope_id: exceptionCreate.scope_id,
      effect: exceptionCreate.effect, reason: exceptionCreate.reason, permanent: exceptionCreate.permanent,
      expires_at: expiresAt,
    }))
    if (!result) return
    showExceptionCreate.value = false
    appStore.showSuccess(t('admin.promptAudit.core.messages.exceptionCreated'))
    await Promise.allSettled([loadExceptions(), loadOverview()])
  } catch (error) { appStore.showError(apiError(error)) }
  finally { loading.mutation = false }
}
function openExceptionExpire(item: SecurityAuditException) {
  exceptionPendingExpire.value = item
  exceptionExpireReason.value = ''
  showExceptionExpire.value = true
}
function closeExceptionExpire() {
  showExceptionExpire.value = false
  exceptionPendingExpire.value = null
  exceptionExpireReason.value = ''
}
async function expireException() {
  if (!exceptionPendingExpire.value || exceptionExpireReason.value.length < 3) return
  loading.mutation = true
  try {
    const result = await sensitiveRun(() => promptAuditAPI.expireSecurityException(
      exceptionPendingExpire.value!.id,
      exceptionExpireReason.value,
    ))
    if (!result) return
    closeExceptionExpire()
    appStore.showSuccess(t('admin.promptAudit.core.messages.exceptionExpired'))
    await Promise.allSettled([loadExceptions(), loadOverview()])
  } catch (error) { appStore.showError(apiError(error)) }
  finally { loading.mutation = false }
}
async function resetEndpoint(item: SecurityEndpointHealth) {
  loading.mutation = true
  try {
    const result = await sensitiveRun(() => promptAuditAPI.resetSecurityEndpointBreaker(item.endpoint_id))
    if (!result) return
    await loadEndpoints()
  }
  catch (error) { appStore.showError(apiError(error)) }
  finally { loading.mutation = false }
}
async function markNotification(item: SecurityAuditNotification, status: 'read' | 'dismissed') {
  try {
    const updated = await promptAuditAPI.updateSecurityNotificationStatus(item.id, status)
    const index = notifications.value.findIndex((entry) => entry.id === item.id)
    if (status === 'dismissed') notifications.value = notifications.value.filter((entry) => entry.id !== item.id)
    else if (index >= 0) notifications.value[index] = updated
    if (overview.value && item.status === 'unread') overview.value.unread_notifications = Math.max(0, overview.value.unread_notifications - 1)
  } catch (error) { appStore.showError(apiError(error)) }
}
async function markAllNotificationsRead() {
  loading.mutation = true
  try {
    await promptAuditAPI.markAllSecurityNotificationsRead('admin')
    notifications.value = notifications.value.map((item) => (
      item.audience === 'admin' && item.status === 'unread'
        ? { ...item, status: 'read', read_at: item.read_at || new Date().toISOString() }
        : item
    ))
    if (overview.value) overview.value.unread_notifications = 0
  } catch (error) { appStore.showError(apiError(error)) }
  finally { loading.mutation = false }
}
function formatDate(value?: string) {
  if (!value) return t('admin.promptAudit.common.never')
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
function formatNumber(value: number) { return new Intl.NumberFormat(locale.value, { notation: value >= 100000 ? 'compact' : 'standard', maximumFractionDigits: 2 }).format(value) }
function formatMoney(value: number) { return new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value) }
function formatDuration(seconds: number) {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}
function riskLabel(value: string) { return t(`admin.promptAudit.riskLevels.${value}`) }
function sourceLabel(value: string) { return t(`admin.promptAudit.core.sources.${value}`) }
function requestActionLabel(value: string) { return t(`admin.promptAudit.core.requestActions.${value}`) }
function caseStatusLabel(value: string) { return t(`admin.promptAudit.core.caseStatuses.${value}`) }
function policyStatusLabel(value: string) { return t(`admin.promptAudit.core.policyStatuses.${value}`) }
function actionTypeLabel(value: string) { return t(`admin.promptAudit.core.actionTypes.${value}`) }
function actionStatusLabel(value: string) { return t(`admin.promptAudit.core.actionStatuses.${value}`) }
function decisionSubject(item: SecurityUnifiedDecision) { return item.user_snapshot || item.api_key_snapshot || item.group_snapshot || t('admin.promptAudit.core.common.unknownSubject') }
function riskBadge(value: string) {
  const color = { low: 'badge badge-gray', medium: 'badge badge-warning', high: 'badge badge-danger', critical: 'badge badge-danger' }[value]
  return color || 'badge badge-gray'
}
function actionBadge(value: string) {
  return { allow: 'badge badge-success', warn: 'badge badge-warning', block: 'badge badge-danger' }[value] || 'badge badge-gray'
}
function actionStatusBadge(value: string) {
  if (value === 'succeeded') return 'badge badge-success'
  if (value === 'failed') return 'badge badge-danger'
  if (value === 'pending' || value === 'processing' || value === 'retry') return 'badge badge-warning'
  return 'badge badge-gray'
}
function endpointStatusBadge(value: string) {
  if (value === 'healthy') return 'badge badge-success'
  if (value === 'unhealthy') return 'badge badge-danger'
  if (value === 'degraded') return 'badge badge-warning'
  return 'badge badge-gray'
}
function endpointSuccessRate(item: SecurityEndpointHealth) {
  if (!item.request_count) return '—'
  return `${((item.success_count / item.request_count) * 100).toFixed(1)}%`
}

watch(section, () => { void refreshActive() })
watch(operationSection, () => { if (section.value === 'operations') void loadOperation() })
onMounted(() => { void loadOverview() })
</script>
