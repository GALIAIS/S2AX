<template>
  <AppLayout>
    <div class="mx-auto max-w-[1600px] pb-8">
      <div v-if="errors.config && !draft" role="alert" class="mb-6 rounded-xl border border-red-200 bg-red-50 p-5 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
        <p>{{ errors.config }}</p>
        <button type="button" class="btn btn-secondary btn-sm mt-3" @click="loadConfig()">{{ t('common.retry') }}</button>
      </div>

      <template v-else>
        <div class="mb-5 flex flex-wrap items-center justify-between gap-3">
          <div role="tablist" :aria-label="t('admin.invocationArchive.title')">
            <div class="tabs inline-flex">
              <button
                v-for="tab in tabs"
                :key="tab.id"
                type="button"
                role="tab"
                class="tab"
                :class="{ 'tab-active': activeTab === tab.id }"
                :aria-selected="activeTab === tab.id"
                @click="activeTab = tab.id"
              >
                {{ tab.label }}
              </button>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <span class="text-xs text-gray-500 dark:text-dark-400" aria-live="polite">{{ refreshStatus }}</span>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="refreshing" @click="refreshWorkspace">
              {{ refreshing ? t('common.refreshing') : t('common.refresh') }}
            </button>
          </div>
        </div>

        <section v-show="activeTab === 'records'" class="card px-4 py-6 sm:px-6 lg:px-8">
          <div class="flex flex-wrap items-start justify-between gap-4">
            <div>
              <h2 class="text-base font-semibold text-gray-950 dark:text-white">{{ t('admin.invocationArchive.records.title') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('admin.invocationArchive.records.description') }}</p>
            </div>
            <button type="button" class="btn btn-danger btn-sm" :disabled="selectedIDs.length === 0" @click="requestDelete(selectedIDs)">
              {{ t('admin.invocationArchive.records.deleteSelected', { count: selectedIDs.length }) }}
            </button>
          </div>

          <form class="mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-5" @submit.prevent="applyFilters">
            <label class="text-xs text-gray-600 dark:text-dark-200">
              <span>{{ t('admin.invocationArchive.records.search') }}</span>
              <input v-model="filters.q" class="input mt-1 w-full" :placeholder="t('admin.invocationArchive.records.searchPlaceholder')" />
            </label>
            <label class="text-xs text-gray-600 dark:text-dark-200">
              <span>{{ t('admin.invocationArchive.records.mode') }}</span>
              <select v-model="filters.mode" class="input mt-1 w-full">
                <option value="">{{ t('common.all') }}</option>
                <option value="request_only">{{ t('admin.invocationArchive.modes.request_only') }}</option>
                <option value="full">{{ t('admin.invocationArchive.modes.full') }}</option>
              </select>
            </label>
            <label class="text-xs text-gray-600 dark:text-dark-200">
              <span>{{ t('admin.invocationArchive.records.outcome') }}</span>
              <select v-model="filters.outcome" class="input mt-1 w-full">
                <option value="">{{ t('common.all') }}</option>
                <option v-for="outcome in outcomes" :key="outcome" :value="outcome">{{ outcomeLabel(outcome) }}</option>
              </select>
            </label>
            <label class="text-xs text-gray-600 dark:text-dark-200">
              <span>{{ t('admin.invocationArchive.records.userId') }}</span>
              <input v-model="filters.user_id" inputmode="numeric" class="input mt-1 w-full" />
            </label>
            <label class="text-xs text-gray-600 dark:text-dark-200">
              <span>{{ t('admin.invocationArchive.records.groupId') }}</span>
              <input v-model="filters.group_id" inputmode="numeric" class="input mt-1 w-full" />
            </label>
            <label class="text-xs text-gray-600 dark:text-dark-200">
              <span>{{ t('admin.invocationArchive.records.apiKeyId') }}</span>
              <input v-model="filters.api_key_id" inputmode="numeric" class="input mt-1 w-full" />
            </label>
            <label class="text-xs text-gray-600 dark:text-dark-200">
              <span>{{ t('admin.invocationArchive.records.from') }}</span>
              <input v-model="filters.from" type="datetime-local" class="input mt-1 w-full" />
            </label>
            <label class="text-xs text-gray-600 dark:text-dark-200">
              <span>{{ t('admin.invocationArchive.records.to') }}</span>
              <input v-model="filters.to" type="datetime-local" class="input mt-1 w-full" />
            </label>
            <div class="flex items-end gap-2 sm:col-span-2">
              <button type="submit" class="btn btn-primary btn-sm">{{ t('common.search') }}</button>
              <button type="button" class="btn btn-ghost btn-sm" @click="resetFilters">{{ t('common.reset') }}</button>
            </div>
          </form>

          <div v-if="errors.records" role="alert" class="mt-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ errors.records }}</div>
          <div class="mt-5 overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700/60">
            <table class="min-w-[1280px] w-full text-left text-sm">
              <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-900/70 dark:text-dark-400">
                <tr>
                  <th class="w-10 px-3 py-3"><input type="checkbox" :checked="allSelected" :aria-label="t('admin.invocationArchive.records.selectAll')" @change="toggleAll" /></th>
                  <th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.records.time') }}</th>
                  <th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.records.identity') }}</th>
                  <th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.records.route') }}</th>
                  <th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.records.capture') }}</th>
                  <th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.records.result') }}</th>
                  <th class="px-3 py-3 text-right font-medium">{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-transparent" :aria-busy="loading.records">
                <tr v-if="loading.records && !recordsLoaded"><td colspan="7" class="px-4 py-12 text-center text-gray-500">{{ t('common.loading') }}</td></tr>
                <tr v-else-if="records.items.length === 0"><td colspan="7" class="px-4 py-12 text-center text-gray-500">{{ t('admin.invocationArchive.records.empty') }}</td></tr>
                <tr v-for="record in records.items" v-else :key="record.id" class="align-top hover:bg-gray-50/70 dark:hover:bg-dark-800/70">
                  <td class="px-3 py-3"><input type="checkbox" :checked="selectedIDs.includes(record.id)" :aria-label="t('admin.invocationArchive.records.selectRecord', { id: record.id })" @change="toggleOne(record.id)" /></td>
                  <td class="whitespace-nowrap px-3 py-3 text-xs text-gray-600 dark:text-dark-300">
                    <p>{{ formatDate(record.created_at) }}</p>
                    <p class="mt-1 text-gray-400 dark:text-dark-500">#{{ record.id }} · {{ record.transport }}<template v-if="record.websocket_turn">/{{ record.websocket_turn }}</template></p>
                  </td>
                  <td class="px-3 py-3">
                    <p class="max-w-64 truncate font-medium text-gray-900 dark:text-white" :title="record.user_label">{{ record.user_label || '—' }}</p>
                    <p class="mt-1 max-w-64 truncate text-xs text-gray-500" :title="record.api_key_name">{{ record.api_key_name || '—' }}</p>
                    <p v-if="record.group_name" class="mt-1 max-w-64 truncate text-xs text-gray-500">{{ record.group_name }}</p>
                  </td>
                  <td class="px-3 py-3">
                    <p class="max-w-72 truncate font-mono text-xs text-gray-900 dark:text-white" :title="record.path">{{ record.method }} {{ record.path }}</p>
                    <p class="mt-1 max-w-72 truncate text-xs text-gray-500" :title="record.model">{{ record.model || '—' }}</p>
                  </td>
                  <td class="px-3 py-3 text-xs text-gray-600 dark:text-dark-300">
                    <p>{{ t('admin.invocationArchive.records.request') }} · {{ captureLabel(record.request_status, record.request_captured_bytes, record.request_total_bytes, record.request_truncated) }}</p>
                    <p class="mt-1">{{ t('admin.invocationArchive.records.response') }} · {{ captureLabel(record.response_status, record.response_captured_bytes, record.response_total_bytes, record.response_truncated) }}</p>
                  </td>
                  <td class="px-3 py-3">
                    <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="outcomeClass(record.outcome)">{{ outcomeLabel(record.outcome) }}</span>
                    <p class="mt-2 font-mono text-xs text-gray-500">HTTP {{ record.http_status || '—' }}</p>
                  </td>
                  <td class="whitespace-nowrap px-3 py-3 text-right">
                    <button type="button" class="btn btn-ghost btn-sm" @click="openRecord(record.id)">{{ t('common.view') }}</button>
                    <button type="button" class="btn btn-ghost btn-sm text-red-600" @click="requestDelete([record.id])">{{ t('common.delete') }}</button>
                  </td>
                </tr>
              </tbody>
            </table>
            <Pagination :total="records.total" :page="records.page" :page-size="records.page_size" @update:page="changePage" @update:page-size="changePageSize" />
          </div>
        </section>

        <section v-show="activeTab === 'config'" class="card px-4 py-6 sm:px-6 lg:px-8">
          <template v-if="draft">
            <div class="flex flex-wrap items-start justify-between gap-4">
              <div>
                <h2 class="text-base font-semibold text-gray-950 dark:text-white">{{ t('admin.invocationArchive.config.title') }}</h2>
                <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-dark-300">{{ t('admin.invocationArchive.config.description') }}</p>
              </div>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.invocationArchive.config.version', { version: draft.config_version }) }}</p>
            </div>

            <div class="mt-6 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200">
              {{ t('admin.invocationArchive.config.privacyNotice') }}
            </div>

            <div class="mt-6 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
              <label class="text-sm text-gray-700 dark:text-dark-200">
                <span>{{ t('admin.invocationArchive.config.defaultMode') }}</span>
                <select v-model="draft.default_mode" class="input mt-1 w-full">
                  <option value="off">{{ t('admin.invocationArchive.modes.off') }}</option>
                  <option value="request_only">{{ t('admin.invocationArchive.modes.request_only') }}</option>
                  <option value="full">{{ t('admin.invocationArchive.modes.full') }}</option>
                </select>
              </label>
              <label class="text-sm text-gray-700 dark:text-dark-200">
                <span>{{ t('admin.invocationArchive.config.retentionDays') }}</span>
                <input :value="draft.retention_days" type="number" min="1" max="3650" class="input mt-1 w-full" @input="setDraftNumber('retention_days', $event)" />
              </label>
              <label class="text-sm text-gray-700 dark:text-dark-200">
                <span>{{ t('admin.invocationArchive.config.requestLimit') }}</span>
                <input :value="bytesToMiB(draft.max_request_bytes)" type="number" min="0.001" max="256" step="0.001" class="input mt-1 w-full" @input="setDraftMiB('max_request_bytes', $event)" />
              </label>
              <label class="text-sm text-gray-700 dark:text-dark-200">
                <span>{{ t('admin.invocationArchive.config.responseLimit') }}</span>
                <input :value="bytesToMiB(draft.max_response_bytes)" type="number" min="0.001" max="256" step="0.001" class="input mt-1 w-full" @input="setDraftMiB('max_response_bytes', $event)" />
              </label>
            </div>

            <label class="mt-5 flex items-start gap-3 rounded-xl border border-gray-200 p-4 text-sm dark:border-dark-700">
              <input v-model="draft.direct_view_enabled" type="checkbox" class="mt-0.5 h-4 w-4" />
              <span>
                <span class="font-medium text-gray-900 dark:text-white">{{ t('admin.invocationArchive.config.directView') }}</span>
                <span class="mt-1 block text-gray-500 dark:text-dark-300">{{ t('admin.invocationArchive.config.directViewHint') }}</span>
              </span>
            </label>

            <section class="mt-6 border-t border-gray-200 pt-6 dark:border-dark-700">
              <div class="flex flex-wrap items-start justify-between gap-4">
                <div>
                  <h3 class="text-base font-semibold text-gray-950 dark:text-white">{{ t('admin.invocationArchive.compression.title') }}</h3>
                  <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-dark-300">{{ t('admin.invocationArchive.compression.description') }}</p>
                </div>
              </div>
              <label class="mt-4 flex items-start gap-3 rounded-xl border border-gray-200 p-4 text-sm dark:border-dark-700">
                <input v-model="draft.compression.enabled" type="checkbox" class="mt-0.5 h-4 w-4" />
                <span>
                  <span class="font-medium text-gray-900 dark:text-white">{{ t('admin.invocationArchive.compression.enabled') }}</span>
                  <span class="mt-1 block text-gray-500 dark:text-dark-300">{{ t('admin.invocationArchive.compression.enabledHint') }}</span>
                </span>
              </label>
              <div class="mt-4 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                <label class="text-sm text-gray-700 dark:text-dark-200">
                  <span>{{ t('admin.invocationArchive.compression.afterHours') }}</span>
                  <input :value="draft.compression.after_hours" type="number" min="0" max="8760" class="input mt-1 w-full" @input="setCompressionNumber('after_hours', $event)" />
                  <span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">{{ t('admin.invocationArchive.compression.zeroDisabled') }}</span>
                </label>
                <label class="text-sm text-gray-700 dark:text-dark-200">
                  <span>{{ t('admin.invocationArchive.compression.minBytes') }}</span>
                  <input :value="bytesToMiB(draft.compression.min_bytes)" type="number" min="0.001" max="256" step="0.001" class="input mt-1 w-full" @input="setCompressionMiB('min_bytes', $event)" />
                </label>
                <label class="text-sm text-gray-700 dark:text-dark-200">
                  <span>{{ t('admin.invocationArchive.compression.triggerBytes') }}</span>
                  <input :value="bytesToMiB(draft.compression.trigger_bytes)" type="number" min="0" max="1048576" step="1" class="input mt-1 w-full" @input="setCompressionMiB('trigger_bytes', $event)" />
                  <span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">{{ t('admin.invocationArchive.compression.zeroDisabled') }}</span>
                </label>
                <label class="text-sm text-gray-700 dark:text-dark-200">
                  <span>{{ t('admin.invocationArchive.compression.triggerRecords') }}</span>
                  <input :value="draft.compression.trigger_records" type="number" min="0" max="1000000" class="input mt-1 w-full" @input="setCompressionNumber('trigger_records', $event)" />
                  <span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">{{ t('admin.invocationArchive.compression.zeroDisabled') }}</span>
                </label>
                <label class="text-sm text-gray-700 dark:text-dark-200">
                  <span>{{ t('admin.invocationArchive.compression.batchSize') }}</span>
                  <input :value="draft.compression.batch_size" type="number" min="1" max="100" class="input mt-1 w-full" @input="setCompressionNumber('batch_size', $event)" />
                </label>
                <label class="text-sm text-gray-700 dark:text-dark-200">
                  <span>{{ t('admin.invocationArchive.compression.intervalMinutes') }}</span>
                  <input :value="draft.compression.interval_minutes" type="number" min="1" max="1440" class="input mt-1 w-full" @input="setCompressionNumber('interval_minutes', $event)" />
                </label>
              </div>
            </section>

            <div class="mt-8 border-t border-gray-200 pt-6 dark:border-dark-700">
              <div class="flex flex-wrap items-end justify-between gap-4">
                <div>
                  <h3 class="text-base font-semibold text-gray-950 dark:text-white">{{ t('admin.invocationArchive.rules.title') }}</h3>
                  <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('admin.invocationArchive.rules.description') }}</p>
                </div>
                <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.invocationArchive.rules.count', { count: draft.rules.length }) }}</span>
              </div>

              <div class="mt-4 grid gap-3 rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/30 lg:grid-cols-[160px_minmax(180px,1fr)_minmax(220px,1fr)_160px_auto]">
                <label class="text-xs text-gray-600 dark:text-dark-200">
                  <span>{{ t('admin.invocationArchive.rules.scope') }}</span>
                  <select v-model="newRule.scope" class="input mt-1 w-full" @change="onScopeChanged">
                    <option value="user">{{ t('admin.invocationArchive.scopes.user') }}</option>
                    <option value="group">{{ t('admin.invocationArchive.scopes.group') }}</option>
                    <option value="api_key">{{ t('admin.invocationArchive.scopes.api_key') }}</option>
                  </select>
                </label>
                <label class="text-xs text-gray-600 dark:text-dark-200">
                  <span>{{ t('admin.invocationArchive.rules.subjectSearch') }}</span>
                  <div class="mt-1 flex gap-2">
                    <input v-model="newRule.query" class="input min-w-0 flex-1" :placeholder="t('admin.invocationArchive.rules.subjectSearchPlaceholder')" @keyup.enter.prevent="searchSubjects" />
                    <button type="button" class="btn btn-secondary btn-sm" :disabled="loading.subjects" @click="searchSubjects">{{ t('common.search') }}</button>
                  </div>
                </label>
                <label class="text-xs text-gray-600 dark:text-dark-200">
                  <span>{{ t('admin.invocationArchive.rules.subject') }}</span>
                  <select v-model.number="newRule.subjectID" class="input mt-1 w-full" :disabled="loading.subjects">
                    <option :value="0" disabled>{{ t('admin.invocationArchive.rules.selectSubject') }}</option>
                    <option v-for="subject in subjects" :key="subject.id" :value="subject.id">#{{ subject.id }} · {{ subject.label }}<template v-if="subject.secondary"> · {{ subject.secondary }}</template></option>
                  </select>
                </label>
                <label class="text-xs text-gray-600 dark:text-dark-200">
                  <span>{{ t('admin.invocationArchive.rules.mode') }}</span>
                  <select v-model="newRule.mode" class="input mt-1 w-full">
                    <option value="off">{{ t('admin.invocationArchive.modes.off') }}</option>
                    <option value="request_only">{{ t('admin.invocationArchive.modes.request_only') }}</option>
                    <option value="full">{{ t('admin.invocationArchive.modes.full') }}</option>
                  </select>
                </label>
                <button type="button" class="btn btn-primary btn-sm self-end" :disabled="!newRule.subjectID" @click="addRule">{{ t('common.add') }}</button>
              </div>
              <p v-if="errors.subjects" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-300">{{ errors.subjects }}</p>

              <div class="mt-4 overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700/60">
                <table class="min-w-[640px] w-full text-left text-sm">
                  <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-900/70 dark:text-dark-400">
                    <tr><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.rules.scope') }}</th><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.rules.subject') }}</th><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.rules.mode') }}</th><th class="px-3 py-3 text-right font-medium">{{ t('common.actions') }}</th></tr>
                  </thead>
                  <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-transparent">
                    <tr v-if="draft.rules.length === 0"><td colspan="4" class="px-4 py-9 text-center text-sm text-gray-500">{{ t('admin.invocationArchive.rules.empty') }}</td></tr>
                    <tr v-for="rule in draft.rules" v-else :key="`${rule.scope}:${rule.subject_id}`">
                      <td class="px-3 py-3">{{ scopeLabel(rule.scope) }}</td>
                      <td class="px-3 py-3 font-mono text-xs">#{{ rule.subject_id }}</td>
                      <td class="px-3 py-3"><select v-model="rule.mode" class="input h-8 min-w-44 py-1 text-sm"><option value="off">{{ t('admin.invocationArchive.modes.off') }}</option><option value="request_only">{{ t('admin.invocationArchive.modes.request_only') }}</option><option value="full">{{ t('admin.invocationArchive.modes.full') }}</option></select></td>
                      <td class="px-3 py-3 text-right"><button type="button" class="btn btn-ghost btn-sm text-red-600" @click="removeRule(rule.scope, rule.subject_id)">{{ t('common.delete') }}</button></td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <div class="mt-8 flex flex-wrap items-center justify-between gap-3 border-t border-gray-200 pt-5 dark:border-dark-700">
              <span class="text-sm" :class="dirty ? 'text-amber-700 dark:text-amber-300' : 'text-gray-500 dark:text-dark-400'">{{ dirty ? t('admin.invocationArchive.config.unsaved') : t('admin.invocationArchive.config.synced') }}</span>
              <div class="flex gap-2">
                <button type="button" class="btn btn-secondary" :disabled="!dirty || loading.saving" @click="resetDraft">{{ t('common.reset') }}</button>
                <button type="button" class="btn btn-primary" :disabled="!dirty || loading.saving" @click="saveConfig">{{ loading.saving ? t('common.saving') : t('common.save') }}</button>
              </div>
            </div>
          </template>
        </section>

        <section v-show="activeTab === 'runtime'" class="card px-4 py-6 sm:px-6 lg:px-8">
          <div class="flex flex-wrap items-start justify-between gap-4">
            <div>
              <h2 class="text-base font-semibold text-gray-950 dark:text-white">{{ t('admin.invocationArchive.runtime.title') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('admin.invocationArchive.runtime.description') }}</p>
            </div>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="refreshing" @click="refreshWorkspace">{{ refreshing ? t('common.refreshing') : t('common.refresh') }}</button>
          </div>
          <div v-if="errors.runtime" role="alert" class="mt-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ errors.runtime }}</div>
          <div v-else-if="runtime" class="mt-6 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <article v-for="metric in runtimeMetrics" :key="metric.label" class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
              <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ metric.label }}</p>
              <p class="mt-2 text-2xl font-semibold tracking-tight text-gray-950 dark:text-white">{{ metric.value }}</p>
              <p v-if="metric.hint" class="mt-1 text-xs text-gray-500 dark:text-dark-300">{{ metric.hint }}</p>
            </article>
          </div>
          <div v-else class="mt-6 py-12 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
          <div v-if="runtime?.last_config_error || runtime?.last_persist_error || runtime?.last_storage_error || runtime?.compression?.last_error" class="mt-6 grid gap-4 lg:grid-cols-2">
            <div v-if="runtime.last_config_error" class="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200"><p class="font-medium">{{ t('admin.invocationArchive.runtime.configError') }}</p><p class="mt-1 break-words">{{ runtime.last_config_error }}</p></div>
            <div v-if="runtime.last_persist_error" class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800 dark:border-red-900/70 dark:bg-red-950/30 dark:text-red-200"><p class="font-medium">{{ t('admin.invocationArchive.runtime.persistError') }}</p><p class="mt-1 break-words">{{ runtime.last_persist_error }}</p></div>
            <div v-if="runtime.last_storage_error" class="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200"><p class="font-medium">{{ t('admin.invocationArchive.runtime.storageError') }}</p><p class="mt-1 break-words">{{ runtime.last_storage_error }}</p></div>
            <div v-if="runtime.compression?.last_error" class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800 dark:border-red-900/70 dark:bg-red-950/30 dark:text-red-200"><p class="font-medium">{{ t('admin.invocationArchive.runtime.compressionError') }}</p><p class="mt-1 break-words">{{ runtime.compression.last_error }}</p></div>
          </div>
        </section>
      </template>
    </div>

    <BaseDialog :show="detailOpen" :title="t('admin.invocationArchive.detail.title', { id: activeRecord?.id || '' })" width="full" @close="closeDetail">
      <div v-if="loading.detail" class="py-16 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <template v-else-if="activeRecord">
        <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <article v-for="item in detailMetadata" :key="item.label" class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
            <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ item.label }}</p>
            <p class="mt-1 break-words text-sm text-gray-900 dark:text-white">{{ item.value || '—' }}</p>
          </article>
        </div>

        <section class="mt-6 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h3 class="text-sm font-semibold text-gray-950 dark:text-white">{{ t('admin.invocationArchive.detail.payloads') }}</h3>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('admin.invocationArchive.detail.payloadsHint') }}</p>
            </div>
            <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="serverConfig?.direct_view_enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'">{{ serverConfig?.direct_view_enabled ? t('common.enabled') : t('common.disabled') }}</span>
          </div>
          <div v-if="!serverConfig?.direct_view_enabled" class="mt-4 rounded-lg bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:bg-amber-950/30 dark:text-amber-200">{{ t('admin.invocationArchive.detail.directViewDisabled') }}</div>
          <template v-else>
            <div class="mt-4 flex flex-wrap items-center gap-3">
              <button type="button" class="btn btn-primary btn-sm" :disabled="loading.revealing" @click="revealPayloads">{{ loading.revealing ? t('common.verifying') : t('admin.invocationArchive.detail.reveal') }}</button>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.invocationArchive.detail.revealHint') }}</p>
            </div>
          </template>
          <div v-if="hasPayloadChunks" class="mt-5 flex flex-col gap-4 xl:flex-row xl:items-start">
            <article v-for="payload in payloadPanels" :key="payload.slot" class="w-full min-w-0 self-start rounded-xl border border-gray-200 xl:flex-1 dark:border-dark-700">
              <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-dark-700">
                <div>
                  <h4 class="text-sm font-semibold text-gray-950 dark:text-white">{{ payload.label }}</h4>
                  <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ payloadMeta(payload.payload) }}</p>
                  <p v-if="payload.payload.available" class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ payloadFormatLabel(payload.presentation.format) }} · {{ payloadEncodingLabel(payload.payload.encoding, payload.presentation.charset) }}</p>
                  <p v-if="payload.payload.available" class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ payloadSegmentLabel(payload.payload) }}</p>
                </div>
                <div v-if="payload.payload.available" class="flex flex-wrap items-center gap-2">
                  <button type="button" class="btn btn-secondary btn-sm" @click="copyText(payload.display)">{{ payload.payload.complete ? t('admin.invocationArchive.detail.copyCurrent') : t('admin.invocationArchive.detail.copyLoaded') }}</button>
                  <button v-if="payload.mode !== 'raw'" type="button" class="btn btn-ghost btn-sm" @click="copyText(payload.presentation.raw)">{{ payload.payload.complete ? t('admin.invocationArchive.detail.copyRaw') : t('admin.invocationArchive.detail.copyRawLoaded') }}</button>
                  <button v-if="hasPreviousPayload(payload.slot)" type="button" class="btn btn-ghost btn-sm" :disabled="payloadLoading[payload.slot]" @click="loadPreviousPayload(payload.slot)">{{ t('admin.invocationArchive.detail.previousSegment') }}</button>
                  <button v-if="!payload.payload.complete" type="button" class="btn btn-ghost btn-sm" :disabled="payloadLoading[payload.slot]" @click="loadNextPayload(payload.slot)">{{ payloadLoading[payload.slot] ? t('common.loading') : t('admin.invocationArchive.detail.nextSegment') }}</button>
                </div>
              </div>
              <template v-if="payload.payload.available">
                <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 bg-gray-50/70 px-4 py-2.5 dark:border-dark-700 dark:bg-dark-900/40">
                  <div class="flex flex-wrap items-center gap-2" role="group" :aria-label="t('admin.invocationArchive.detail.viewMode')">
                    <button v-if="payload.presentation.transcript.length > 0" type="button" class="btn btn-sm" :class="payload.mode === 'structured' ? 'btn-primary' : 'btn-secondary'" @click="payloadViewModes[payload.slot] = 'structured'">{{ t('admin.invocationArchive.detail.structured') }}</button>
                    <button v-if="payload.presentation.canFormat" type="button" class="btn btn-sm" :class="payload.mode === 'formatted' ? 'btn-primary' : 'btn-secondary'" @click="payloadViewModes[payload.slot] = 'formatted'">{{ t('admin.invocationArchive.detail.formatted') }}</button>
                    <button v-if="payload.presentation.repaired" type="button" class="btn btn-sm" :class="payload.mode === 'repaired' ? 'btn-primary' : 'btn-secondary'" @click="payloadViewModes[payload.slot] = 'repaired'">{{ t('admin.invocationArchive.detail.repaired') }}</button>
                    <button type="button" class="btn btn-sm" :class="payload.mode === 'raw' ? 'btn-primary' : 'btn-secondary'" @click="payloadViewModes[payload.slot] = 'raw'">{{ t('admin.invocationArchive.detail.raw') }}</button>
                  </div>
                  <label v-if="payload.presentation.canSelectCharset" class="flex items-center gap-2 text-xs text-gray-600 dark:text-dark-300">
                    <span>{{ t('admin.invocationArchive.detail.charset') }}</span>
                    <select v-model="payloadCharsets[payload.slot]" class="input h-8 min-w-40 py-1 text-xs">
                      <option v-for="charset in invocationArchivePayloadCharsets" :key="charset" :value="charset">{{ payloadCharsetLabel(charset) }}</option>
                    </select>
                  </label>
                </div>
                <div v-if="payload.presentation.warnings.length" class="space-y-2 border-b border-gray-200 px-4 py-3 dark:border-dark-700">
                  <p v-for="warning in payload.presentation.warnings" :key="warning" role="status" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200">{{ payloadWarningLabel(warning) }}</p>
                </div>
                <div v-if="payload.mode === 'structured' && payload.presentation.transcript.length" class="max-h-[28rem] space-y-3 overflow-auto bg-gray-50 p-4 dark:bg-dark-900/70">
                  <article v-for="(entry, index) in payload.presentation.transcript" :key="`${entry.role}:${entry.title || ''}:${index}`" class="border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-800/70">
                    <div class="flex flex-wrap items-center gap-2 text-xs">
                      <span class="rounded-full bg-primary-100 px-2 py-0.5 font-medium text-primary-700 dark:bg-primary-950/50 dark:text-primary-300">{{ entry.role }}</span>
                      <span v-if="entry.title" class="font-mono text-gray-700 dark:text-dark-200">{{ entry.title }}</span>
                      <span v-for="item in entry.metadata" :key="item" class="text-gray-500 dark:text-dark-400">{{ item }}</span>
                    </div>
                    <pre class="m-0 mt-3 whitespace-pre-wrap break-words font-mono text-xs leading-6 text-gray-800 dark:text-dark-100">{{ entry.content }}</pre>
                  </article>
                </div>
                <pre v-else class="m-0 max-h-[28rem] overflow-auto whitespace-pre-wrap break-words bg-gray-50 p-4 text-xs leading-6 text-gray-800 dark:bg-dark-900/70 dark:text-dark-100">{{ payload.display }}</pre>
              </template>
              <pre v-else class="m-0 max-h-[28rem] overflow-auto whitespace-pre-wrap break-words bg-gray-50 p-4 text-xs leading-6 text-gray-800 dark:bg-dark-900/70 dark:text-dark-100">{{ payload.display }}</pre>
            </article>
          </div>
        </section>

        <section class="mt-6">
          <h3 class="text-sm font-semibold text-gray-950 dark:text-white">{{ t('admin.invocationArchive.detail.accesses') }}</h3>
          <div class="mt-3 overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700/60">
            <table class="min-w-[760px] w-full text-left text-sm">
              <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-900/70 dark:text-dark-400"><tr><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.detail.accessTime') }}</th><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.detail.admin') }}</th><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.detail.accessOutcome') }}</th><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.detail.client') }}</th></tr></thead>
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-transparent">
                <tr v-if="accessLogs.length === 0"><td colspan="4" class="px-4 py-9 text-center text-sm text-gray-500">{{ t('admin.invocationArchive.detail.noAccesses') }}</td></tr>
                <tr v-for="access in accessLogs" v-else :key="access.id"><td class="whitespace-nowrap px-3 py-3 text-xs text-gray-600 dark:text-dark-300">{{ formatDate(access.created_at) }}</td><td class="px-3 py-3">{{ access.admin_name || '—' }}</td><td class="px-3 py-3">{{ accessOutcomeLabel(access.outcome) }}</td><td class="px-3 py-3 text-xs text-gray-500">{{ access.client_ip || '—' }}</td></tr>
              </tbody>
            </table>
          </div>
        </section>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="deleteIDs.length > 0"
      :title="t('admin.invocationArchive.records.deleteTitle')"
      :message="t('admin.invocationArchive.records.deleteMessage', { count: deleteIDs.length })"
      :confirm-text="t('common.delete')"
      danger
      @confirm="confirmDelete"
      @cancel="deleteIDs = []"
    />
    <TotpStepUpDialog :controller="stepUp" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useAppStore } from '@/stores/app'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, useStepUp } from '@/composables/useStepUp'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import invocationArchiveAPI from './api'
import {
  invocationArchivePayloadCharsets,
  presentInvocationArchivePayload,
  type InvocationArchivePayloadPresentation,
  type InvocationArchivePayloadViewMode,
  type InvocationArchivePayloadWarning,
} from './payloadPresentation'
import {
  defaultInvocationArchiveCompression,
  emptyInvocationArchiveFilters,
  type InvocationArchiveAccessLog,
  type InvocationArchiveConfig,
  type InvocationArchiveFilters,
  type InvocationArchiveMode,
  type InvocationArchiveOutcome,
  type InvocationArchivePayloadChunk,
  type InvocationArchivePayloadView,
  type InvocationArchiveRecord,
  type InvocationArchiveRecordPage,
  type InvocationArchiveRuntime,
  type InvocationArchiveScope,
  type InvocationArchiveScopeRule,
  type InvocationArchiveSubject,
} from './types'

const MEBIBYTE = 1024 * 1024
const AUTO_REFRESH_INTERVAL_MS = 15_000
const { t, locale } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()

type ArchiveTab = 'records' | 'config' | 'runtime'
type ArchivePayloadSlot = 'request' | 'response'
const activeTab = ref<ArchiveTab>('records')
const tabs = computed(() => [
  { id: 'records' as const, label: t('admin.invocationArchive.tabs.records') },
  { id: 'config' as const, label: t('admin.invocationArchive.tabs.config') },
  { id: 'runtime' as const, label: t('admin.invocationArchive.tabs.runtime') },
])
const outcomes: InvocationArchiveOutcome[] = ['completed', 'client_error', 'server_error', 'websocket_error']

const serverConfig = ref<InvocationArchiveConfig | null>(null)
const draft = ref<InvocationArchiveConfig | null>(null)
const runtime = ref<InvocationArchiveRuntime | null>(null)
const records = reactive<InvocationArchiveRecordPage>({ items: [], page: 1, page_size: 20, total: 0 })
const filters = ref<InvocationArchiveFilters>(emptyInvocationArchiveFilters())
const appliedFilters = ref<InvocationArchiveFilters>(emptyInvocationArchiveFilters())
const selectedIDs = ref<number[]>([])
const deleteIDs = ref<number[]>([])
const subjects = ref<InvocationArchiveSubject[]>([])
const newRule = reactive<{ scope: InvocationArchiveScope; query: string; subjectID: number; mode: InvocationArchiveMode }>({
  scope: 'user', query: '', subjectID: 0, mode: 'full',
})
const detailOpen = ref(false)
const activeRecord = ref<InvocationArchiveRecord | null>(null)
const accessLogs = ref<InvocationArchiveAccessLog[]>([])
const payloadChunks = reactive<Record<ArchivePayloadSlot, InvocationArchivePayloadChunk | null>>({ request: null, response: null })
const payloadHistory = reactive<Record<ArchivePayloadSlot, number[]>>({ request: [], response: [] })
const payloadViewModes = reactive<Record<ArchivePayloadSlot, InvocationArchivePayloadViewMode>>({ request: 'formatted', response: 'formatted' })
const payloadCharsets = reactive<Record<ArchivePayloadSlot, string>>({ request: 'auto', response: 'auto' })
const payloadLoading = reactive<Record<ArchivePayloadSlot, boolean>>({ request: false, response: false })
const loading = reactive({ config: false, runtime: false, records: false, saving: false, subjects: false, detail: false, revealing: false, deleting: false })
const errors = reactive({ config: '', runtime: '', records: '', subjects: '' })
const recordsLoaded = ref(false)
const refreshing = ref(false)
const lastRefreshedAt = ref<Date | null>(null)
let recordRequestSequence = 0
let autoRefreshTimer: number | undefined

const dirty = computed(() => configFingerprint(draft.value) !== configFingerprint(serverConfig.value))
const allSelected = computed(() => records.items.length > 0 && records.items.every((record) => selectedIDs.value.includes(record.id)))
const refreshStatus = computed(() => {
  if (refreshing.value) return t('admin.invocationArchive.refresh.refreshing')
  if (!lastRefreshedAt.value) return t('admin.invocationArchive.refresh.waiting')
  return t('admin.invocationArchive.refresh.updatedAt', { time: formatRefreshTime(lastRefreshedAt.value) })
})
const runtimeMetrics = computed(() => {
  if (!runtime.value) return []
  const storage = runtime.value.storage || {
    record_count: 0, block_count: 0, captured_bytes: 0, payload_bytes: 0, database_bytes: 0,
    compressed_records: 0, compressed_payloads: 0, saved_bytes: 0,
  }
  const compression = runtime.value.compression
  return [
    { label: t('admin.invocationArchive.runtime.status'), value: runtime.value.started ? t('admin.invocationArchive.runtime.running') : t('admin.invocationArchive.runtime.stopped'), hint: t('admin.invocationArchive.runtime.version', { version: runtime.value.config_version }) },
    { label: t('admin.invocationArchive.runtime.queue'), value: `${runtime.value.queue_depth} / ${runtime.value.queue_capacity}`, hint: t('admin.invocationArchive.runtime.queueHint') },
    { label: t('admin.invocationArchive.runtime.persisted'), value: number(runtime.value.persisted), hint: t('admin.invocationArchive.runtime.acceptedDropped', { accepted: number(runtime.value.accepted), dropped: number(runtime.value.dropped) }) },
    { label: t('admin.invocationArchive.runtime.purge'), value: number(runtime.value.expired_purged), hint: t('admin.invocationArchive.runtime.failures', { count: number(runtime.value.persist_failures) }) },
    { label: t('admin.invocationArchive.runtime.records'), value: number(storage.record_count), hint: t('admin.invocationArchive.runtime.capturedBytes', { value: formatBytes(storage.captured_bytes) }) },
    { label: t('admin.invocationArchive.runtime.payloadBlocks'), value: number(storage.block_count), hint: t('admin.invocationArchive.runtime.payloadBlocksHint') },
    { label: t('admin.invocationArchive.runtime.payloadBytes'), value: formatBytes(storage.payload_bytes), hint: t('admin.invocationArchive.runtime.payloadBytesHint') },
    { label: t('admin.invocationArchive.runtime.databaseBytes'), value: formatBytes(storage.database_bytes), hint: storage.updated_at ? t('admin.invocationArchive.runtime.storageUpdatedAt', { time: formatDate(storage.updated_at) }) : t('common.notAvailable') },
    { label: t('admin.invocationArchive.runtime.compressed'), value: `${number(storage.compressed_records)} / ${number(storage.compressed_payloads)}`, hint: compression?.enabled ? t('admin.invocationArchive.runtime.compressionRuns', { count: number(compression.runs) }) : t('common.disabled') },
    { label: t('admin.invocationArchive.runtime.savedBytes'), value: formatBytes(storage.saved_bytes), hint: compression?.last_compressed_at ? t('admin.invocationArchive.runtime.lastCompressedAt', { time: formatDate(compression.last_compressed_at) }) : t('common.notAvailable') },
  ]
})
const detailMetadata = computed(() => {
  const record = activeRecord.value
  if (!record) return []
  return [
    { label: t('admin.invocationArchive.detail.createdAt'), value: formatDate(record.created_at) },
    { label: t('admin.invocationArchive.detail.expiresAt'), value: formatDate(record.expires_at) },
    { label: t('admin.invocationArchive.detail.outcome'), value: `${outcomeLabel(record.outcome)} · HTTP ${record.http_status || '—'}` },
    { label: t('admin.invocationArchive.detail.identity'), value: [record.user_label, record.api_key_name].filter(Boolean).join(' · ') },
    { label: t('admin.invocationArchive.detail.group'), value: record.group_name },
    { label: t('admin.invocationArchive.detail.route'), value: `${record.method} ${record.path}` },
    { label: t('admin.invocationArchive.detail.model'), value: record.model },
    { label: t('admin.invocationArchive.detail.requestId'), value: record.request_id || record.client_request_id },
    { label: t('admin.invocationArchive.detail.client'), value: [record.client_ip, record.user_agent].filter(Boolean).join(' · ') },
  ]
})
const hasPayloadChunks = computed(() => Boolean(payloadChunks.request || payloadChunks.response))
const revealedPayloads = computed(() => (['request', 'response'] as const)
  .map((slot) => {
    const chunk = payloadChunks[slot]
    if (!chunk) return null
    return { slot, label: t(`admin.invocationArchive.records.${slot}`), payload: chunk.payload, nextOffset: chunk.next_offset }
  })
  .filter((payload): payload is { slot: ArchivePayloadSlot; label: string; payload: InvocationArchivePayloadView; nextOffset: number } => payload !== null))
const payloadPanels = computed(() => revealedPayloads.value.map((payload) => {
  const presentation = presentInvocationArchivePayload(payload.payload, payloadCharsets[payload.slot])
  const mode = supportedPayloadViewMode(payloadViewModes[payload.slot], presentation)
  return { ...payload, presentation, mode, display: payloadDisplayText(payload.payload, presentation, mode) }
}))

function cloneConfig(config: InvocationArchiveConfig | null): InvocationArchiveConfig | null {
  return config ? { ...config, compression: { ...defaultInvocationArchiveCompression(), ...(config.compression || {}) }, rules: config.rules.map((rule) => ({ ...rule })) } : null
}
function configFingerprint(config: InvocationArchiveConfig | null): string {
  if (!config) return ''
  return JSON.stringify({
    default_mode: config.default_mode,
    retention_days: config.retention_days,
    max_request_bytes: config.max_request_bytes,
    max_response_bytes: config.max_response_bytes,
    direct_view_enabled: config.direct_view_enabled,
    compression: config.compression,
    rules: [...config.rules].sort(ruleSort),
  })
}
function ruleSort(left: InvocationArchiveScopeRule, right: InvocationArchiveScopeRule): number {
  return left.scope.localeCompare(right.scope) || left.subject_id - right.subject_id
}
function errorMessage(error: unknown, fallback: string): string {
  const code = extractApiErrorCode(error)
  if (code) {
    const key = `admin.invocationArchive.errors.${code}`
    const translated = t(key)
    if (translated !== key) return translated
  }
  return extractApiErrorMessage(error, t(fallback))
}
async function runSensitive<T>(operation: () => Promise<T>): Promise<T | undefined> {
  try {
    return await stepUp.run(operation)
  } catch (error) {
    if (isStepUpCancelled(error)) return undefined
    if (isStepUpBlocked(error)) {
      appStore.showError(stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? t('stepUp.adminApiKeyForbidden') : t('stepUp.notEnabled'))
      return undefined
    }
    throw error
  }
}

async function loadConfig({ preserveDraft = false }: { preserveDraft?: boolean } = {}): Promise<boolean> {
  const preserveDirtyDraft = preserveDraft && dirty.value
  loading.config = true
  errors.config = ''
  try {
    const config = await invocationArchiveAPI.getConfig()
    serverConfig.value = cloneConfig(config)
    if (!preserveDirtyDraft) draft.value = cloneConfig(config)
    return true
  } catch (error) {
    errors.config = errorMessage(error, 'admin.invocationArchive.errors.loadConfig')
    return false
  } finally {
    loading.config = false
  }
}
async function loadRuntime(): Promise<boolean> {
  loading.runtime = true
  errors.runtime = ''
  try {
    runtime.value = await invocationArchiveAPI.getRuntime()
    return true
  } catch (error) {
    errors.runtime = errorMessage(error, 'admin.invocationArchive.errors.loadRuntime')
    return false
  } finally {
    loading.runtime = false
  }
}
async function loadRecords({ resetSelection = false }: { resetSelection?: boolean } = {}): Promise<boolean> {
  const requestSequence = ++recordRequestSequence
  const page = records.page
  const pageSize = records.page_size
  const filter = { ...appliedFilters.value }
  loading.records = true
  errors.records = ''
  try {
    const result = await invocationArchiveAPI.listRecords(filter, page, pageSize)
    if (requestSequence !== recordRequestSequence) return false
    Object.assign(records, result)
    recordsLoaded.value = true
    const recordIDs = new Set(result.items.map((record) => record.id))
    selectedIDs.value = resetSelection ? [] : selectedIDs.value.filter((id) => recordIDs.has(id))
    return true
  } catch (error) {
    if (requestSequence !== recordRequestSequence) return false
    errors.records = errorMessage(error, 'admin.invocationArchive.errors.loadRecords')
    return false
  } finally {
    if (requestSequence === recordRequestSequence) loading.records = false
  }
}
async function refreshWorkspace() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    const operations = [loadRuntime(), loadRecords()]
    if (!dirty.value) operations.push(loadConfig({ preserveDraft: true }))
    const results = await Promise.allSettled(operations)
    if (results.some((result) => result.status === 'fulfilled' && result.value)) lastRefreshedAt.value = new Date()
  } finally {
    refreshing.value = false
  }
}
function applyFilters() {
  appliedFilters.value = { ...filters.value }
  records.page = 1
  void loadRecords({ resetSelection: true })
}
function resetFilters() {
  filters.value = emptyInvocationArchiveFilters()
  applyFilters()
}
function changePage(page: number) {
  records.page = page
  void loadRecords({ resetSelection: true })
}
function changePageSize(pageSize: number) {
  records.page_size = pageSize
  records.page = 1
  void loadRecords({ resetSelection: true })
}
function toggleOne(id: number) {
  const selection = new Set(selectedIDs.value)
  if (selection.has(id)) selection.delete(id)
  else selection.add(id)
  selectedIDs.value = [...selection]
}
function toggleAll() {
  selectedIDs.value = allSelected.value ? [] : records.items.map((record) => record.id)
}

function setDraftNumber(field: 'retention_days', event: Event) {
  if (!draft.value) return
  const value = Number((event.target as HTMLInputElement).value)
  if (Number.isFinite(value)) draft.value[field] = Math.round(value)
}
function setDraftMiB(field: 'max_request_bytes' | 'max_response_bytes', event: Event) {
  if (!draft.value) return
  const value = Number((event.target as HTMLInputElement).value)
  if (Number.isFinite(value)) draft.value[field] = Math.round(value * MEBIBYTE)
}
function setCompressionNumber(field: 'after_hours' | 'trigger_records' | 'batch_size' | 'interval_minutes', event: Event) {
  if (!draft.value) return
  const value = Number((event.target as HTMLInputElement).value)
  if (Number.isFinite(value)) draft.value.compression[field] = Math.round(value)
}
function setCompressionMiB(field: 'min_bytes' | 'trigger_bytes', event: Event) {
  if (!draft.value) return
  const value = Number((event.target as HTMLInputElement).value)
  if (Number.isFinite(value)) draft.value.compression[field] = Math.round(value * MEBIBYTE)
}
function bytesToMiB(value: number): string { return (value / MEBIBYTE).toFixed(3).replace(/\.0+$/, '').replace(/(\.\d*?)0+$/, '$1') }
function resetDraft() { draft.value = cloneConfig(serverConfig.value) }
async function searchSubjects() {
  loading.subjects = true
  errors.subjects = ''
  try {
    subjects.value = await invocationArchiveAPI.listSubjects(newRule.scope, newRule.query)
    newRule.subjectID = subjects.value.some((subject) => subject.id === newRule.subjectID) ? newRule.subjectID : 0
  } catch (error) {
    subjects.value = []
    errors.subjects = errorMessage(error, 'admin.invocationArchive.errors.loadSubjects')
  } finally {
    loading.subjects = false
  }
}
function onScopeChanged() {
  newRule.query = ''
  newRule.subjectID = 0
  subjects.value = []
  errors.subjects = ''
  void searchSubjects()
}
function addRule() {
  if (!draft.value || newRule.subjectID <= 0) return
  if (draft.value.rules.some((rule) => rule.scope === newRule.scope && rule.subject_id === newRule.subjectID)) {
    errors.subjects = t('admin.invocationArchive.errors.invocation_archive_rule_duplicate')
    return
  }
  draft.value.rules.push({ scope: newRule.scope, subject_id: newRule.subjectID, mode: newRule.mode })
  draft.value.rules.sort(ruleSort)
  newRule.subjectID = 0
}
function removeRule(scope: InvocationArchiveScope, subjectID: number) {
  if (!draft.value) return
  draft.value.rules = draft.value.rules.filter((rule) => rule.scope !== scope || rule.subject_id !== subjectID)
}
async function saveConfig() {
  if (!draft.value || !dirty.value) return
  loading.saving = true
  try {
    const saved = await runSensitive(() => invocationArchiveAPI.updateConfig({
      expected_config_version: draft.value!.config_version,
      default_mode: draft.value!.default_mode,
      retention_days: draft.value!.retention_days,
      max_request_bytes: draft.value!.max_request_bytes,
      max_response_bytes: draft.value!.max_response_bytes,
      direct_view_enabled: draft.value!.direct_view_enabled,
      compression: { ...draft.value!.compression },
      rules: [...draft.value!.rules].sort(ruleSort),
    }))
    if (!saved) return
    serverConfig.value = cloneConfig(saved)
    draft.value = cloneConfig(saved)
    appStore.showSuccess(t('admin.invocationArchive.messages.saved'))
    await loadRuntime()
  } catch (error) {
    appStore.showError(errorMessage(error, 'admin.invocationArchive.errors.saveConfig'))
  } finally {
    loading.saving = false
  }
}

async function openRecord(id: number) {
  detailOpen.value = true
  loading.detail = true
  activeRecord.value = null
  accessLogs.value = []
  clearPayloadChunks()
  resetPayloadReview()
  try {
    const [record, accesses] = await Promise.all([invocationArchiveAPI.getRecord(id), invocationArchiveAPI.listAccessLogs(id)])
    activeRecord.value = record
    accessLogs.value = accesses
  } catch (error) {
    appStore.showError(errorMessage(error, 'admin.invocationArchive.errors.loadDetail'))
    detailOpen.value = false
  } finally {
    loading.detail = false
  }
}
function closeDetail() {
  detailOpen.value = false
  activeRecord.value = null
  accessLogs.value = []
  clearPayloadChunks()
  resetPayloadReview()
}
async function revealPayloads() {
  if (!activeRecord.value || loading.revealing) return
  loading.revealing = true
  try {
    const recordID = activeRecord.value.id
    const result = await runSensitive(async () => ({
      request: await invocationArchiveAPI.revealPayloadChunk(recordID, 'request'),
      response: await invocationArchiveAPI.revealPayloadChunk(recordID, 'response'),
    }))
    if (!result) return
    payloadChunks.request = result.request
    payloadChunks.response = result.response
    payloadHistory.request.splice(0)
    payloadHistory.response.splice(0)
    resetPayloadReview({ request: result.request.payload, response: result.response.payload })
    accessLogs.value = await invocationArchiveAPI.listAccessLogs(recordID)
  } catch (error) {
    appStore.showError(errorMessage(error, 'admin.invocationArchive.errors.reveal'))
  } finally {
    loading.revealing = false
  }
}
function clearPayloadChunks() {
  payloadChunks.request = null
  payloadChunks.response = null
  payloadHistory.request.splice(0)
  payloadHistory.response.splice(0)
  payloadLoading.request = false
  payloadLoading.response = false
}
async function loadPayloadSegment(slot: ArchivePayloadSlot, offset: number): Promise<boolean> {
  if (!activeRecord.value || payloadLoading[slot] || loading.revealing) return false
  payloadLoading[slot] = true
  try {
    const chunk = await runSensitive(() => invocationArchiveAPI.revealPayloadChunk(activeRecord.value!.id, slot, offset))
    if (!chunk) return false
    payloadChunks[slot] = chunk
    return true
  } catch (error) {
    appStore.showError(errorMessage(error, 'admin.invocationArchive.errors.reveal'))
    return false
  } finally {
    payloadLoading[slot] = false
  }
}
function hasPreviousPayload(slot: ArchivePayloadSlot): boolean { return payloadHistory[slot].length > 0 }
async function loadNextPayload(slot: ArchivePayloadSlot) {
  const current = payloadChunks[slot]
  if (!current || current.payload.complete || payloadLoading[slot]) return
  const currentOffset = current.payload.offset || 0
  payloadHistory[slot].push(currentOffset)
  if (!await loadPayloadSegment(slot, current.next_offset)) payloadHistory[slot].pop()
}
async function loadPreviousPayload(slot: ArchivePayloadSlot) {
  if (payloadLoading[slot]) return
  const previous = payloadHistory[slot].pop()
  if (previous === undefined) return
  if (!await loadPayloadSegment(slot, previous)) payloadHistory[slot].push(previous)
}
function requestDelete(ids: number[]) {
  deleteIDs.value = [...new Set(ids)].filter((id) => id > 0)
}
async function confirmDelete() {
  const ids = [...deleteIDs.value]
  deleteIDs.value = []
  if (ids.length === 0 || loading.deleting) return
  loading.deleting = true
  try {
    const result = await runSensitive(() => ids.length === 1 ? invocationArchiveAPI.deleteRecord(ids[0]) : invocationArchiveAPI.batchDeleteRecords(ids))
    if (!result) return
    appStore.showSuccess(t('admin.invocationArchive.messages.deleted', { count: result.deleted }))
    if (activeRecord.value && ids.includes(activeRecord.value.id)) closeDetail()
    await Promise.allSettled([loadRecords(), loadRuntime()])
  } catch (error) {
    appStore.showError(errorMessage(error, 'admin.invocationArchive.errors.delete'))
  } finally {
    loading.deleting = false
  }
}

function formatDate(value: string): string {
  if (!value || value.startsWith('0001-')) return t('common.notAvailable')
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? t('common.notAvailable') : new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'medium' }).format(date)
}
function formatRefreshTime(value: Date): string {
  return new Intl.DateTimeFormat(locale.value, { timeStyle: 'medium' }).format(value)
}
function number(value: number): string { return new Intl.NumberFormat(locale.value).format(value) }
function scopeLabel(scope: InvocationArchiveScope): string { return t(`admin.invocationArchive.scopes.${scope}`) }
function outcomeLabel(outcome: InvocationArchiveOutcome): string { return t(`admin.invocationArchive.outcomes.${outcome}`) }
function accessOutcomeLabel(outcome: string): string {
  const key = `admin.invocationArchive.accessOutcomes.${outcome}`
  const label = t(key)
  return label === key ? outcome : label
}
function outcomeClass(outcome: InvocationArchiveOutcome): string {
  if (outcome === 'completed') return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300'
  if (outcome === 'client_error') return 'bg-amber-100 text-amber-700 dark:bg-amber-950/50 dark:text-amber-300'
  return 'bg-red-100 text-red-700 dark:bg-red-950/50 dark:text-red-300'
}
function captureLabel(status: string, captured: number, total: number, truncated: boolean): string {
  const key = `admin.invocationArchive.capture.${status}`
  const label = t(key)
  const size = total > 0 ? `${formatBytes(captured)} / ${formatBytes(total)}` : formatBytes(captured)
  return `${label === key ? status : label}${truncated ? ` · ${t('admin.invocationArchive.capture.truncated')}` : ''}${captured || total ? ` · ${size}` : ''}`
}
function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`
  if (value < MEBIBYTE) return `${(value / 1024).toFixed(1)} KiB`
  return `${(value / MEBIBYTE).toFixed(2)} MiB`
}
function payloadMeta(payload: InvocationArchivePayloadView): string {
  const compression = payload.compression && payload.compression !== 'none' ? ` · ${payload.compression}` : ''
  return `${payload.content_type || t('common.notAvailable')} · ${captureLabel(payload.status, payload.captured_bytes, payload.total_bytes, payload.truncated)}${payload.encoding ? ` · ${payload.encoding}` : ''}${compression}`
}
function payloadSegmentLabel(payload: InvocationArchivePayloadView): string {
  const offset = payload.offset || 0
  const loaded = payload.loaded_bytes ?? 0
  return t('admin.invocationArchive.detail.loadedRange', {
    from: formatBytes(offset),
    to: formatBytes(offset + loaded),
    total: formatBytes(payload.captured_bytes),
  })
}
function payloadUnavailableLabel(status: string): string {
  const key = `admin.invocationArchive.capture.${status}`
  const label = t(key)
  return label === key ? status : label
}
function supportedPayloadViewMode(mode: InvocationArchivePayloadViewMode, presentation: InvocationArchivePayloadPresentation): InvocationArchivePayloadViewMode {
  if (mode === 'structured' && presentation.transcript.length > 0) return mode
  if (mode === 'formatted' && presentation.canFormat) return mode
  if (mode === 'repaired' && presentation.repaired) return mode
  return 'raw'
}
function payloadDisplayText(payload: InvocationArchivePayloadView, presentation: InvocationArchivePayloadPresentation, mode: InvocationArchivePayloadViewMode): string {
  if (!payload.available) return payloadUnavailableLabel(payload.status)
  if (mode === 'repaired' && presentation.repaired) return presentation.repaired
  if (mode === 'raw') return presentation.raw
  return presentation.formatted
}
function resetPayloadReview(value: Partial<Record<ArchivePayloadSlot, InvocationArchivePayloadView>> = {}) {
  for (const slot of ['request', 'response'] as const) {
    payloadCharsets[slot] = 'auto'
    const payload = value[slot]
    if (!payload) {
      payloadViewModes[slot] = 'formatted'
      continue
    }
    const presentation = presentInvocationArchivePayload(payload)
    payloadViewModes[slot] = presentation.transcript.length > 0 ? 'structured' : presentation.canFormat ? 'formatted' : 'raw'
  }
}
function payloadFormatLabel(format: InvocationArchivePayloadPresentation['format']): string {
  const key = `admin.invocationArchive.detail.formats.${format}`
  const label = t(key)
  return label === key ? format : label
}
function payloadEncodingLabel(encoding: string | undefined, charset: string): string {
  const key = encoding?.toLowerCase() === 'base64' ? 'admin.invocationArchive.detail.encodings.base64' : 'admin.invocationArchive.detail.encodings.utf8'
  return t(key, { charset })
}
function payloadCharsetLabel(charset: string): string {
  const key = `admin.invocationArchive.detail.charsets.${charset.replace(/-/g, '_')}`
  const label = t(key)
  return label === key ? charset : label
}
function payloadWarningLabel(warning: InvocationArchivePayloadWarning): string {
  const key = `admin.invocationArchive.detail.warnings.${warning}`
  const label = t(key)
  return label === key ? warning : label
}
async function copyText(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    appStore.showSuccess(t('common.copiedToClipboard'))
  } catch {
    appStore.showError(t('common.copyFailed'))
  }
}

function canAutoRefresh(): boolean {
  return document.visibilityState === 'visible'
    && !refreshing.value
    && !loading.config
    && !loading.runtime
    && !loading.records
    && !loading.saving
    && !loading.detail
    && !loading.revealing
    && !payloadLoading.request
    && !payloadLoading.response
    && !loading.deleting
    && !dirty.value
}
function handleVisibilityChange() {
  if (canAutoRefresh()) void refreshWorkspace()
}

onMounted(() => {
  void refreshWorkspace()
  document.addEventListener('visibilitychange', handleVisibilityChange)
  autoRefreshTimer = window.setInterval(() => {
    if (canAutoRefresh()) void refreshWorkspace()
  }, AUTO_REFRESH_INTERVAL_MS)
})
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', handleVisibilityChange)
  if (autoRefreshTimer !== undefined) window.clearInterval(autoRefreshTimer)
})
</script>
