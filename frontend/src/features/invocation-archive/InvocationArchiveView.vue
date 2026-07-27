<template>
  <AppLayout>
    <div class="mx-auto max-w-[1600px] pb-8">
      <header class="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <p class="text-xs font-semibold uppercase tracking-[0.16em] text-primary-600 dark:text-primary-400">{{ t('nav.invocationArchive') }}</p>
          <h1 class="mt-1 text-2xl font-semibold tracking-tight text-gray-950 dark:text-white">{{ t('admin.invocationArchive.title') }}</h1>
          <p class="mt-2 max-w-4xl text-sm text-gray-500 dark:text-dark-300">{{ t('admin.invocationArchive.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loading.runtime" @click="loadRuntime">
          {{ loading.runtime ? t('common.refreshing') : t('common.refresh') }}
        </button>
      </header>

      <div v-if="errors.config && !draft" role="alert" class="mb-6 rounded-xl border border-red-200 bg-red-50 p-5 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
        <p>{{ errors.config }}</p>
        <button type="button" class="btn btn-secondary btn-sm mt-3" @click="loadConfig">{{ t('common.retry') }}</button>
      </div>

      <template v-else>
        <div class="mb-5" role="tablist" :aria-label="t('admin.invocationArchive.title')">
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
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-transparent">
                <tr v-if="loading.records"><td colspan="7" class="px-4 py-12 text-center text-gray-500" aria-busy="true">{{ t('common.loading') }}</td></tr>
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
                <input :value="bytesToMiB(draft.max_request_bytes)" type="number" min="0.001" max="16" step="0.001" class="input mt-1 w-full" @input="setDraftMiB('max_request_bytes', $event)" />
              </label>
              <label class="text-sm text-gray-700 dark:text-dark-200">
                <span>{{ t('admin.invocationArchive.config.responseLimit') }}</span>
                <input :value="bytesToMiB(draft.max_response_bytes)" type="number" min="0.001" max="16" step="0.001" class="input mt-1 w-full" @input="setDraftMiB('max_response_bytes', $event)" />
              </label>
            </div>

            <label class="mt-5 flex items-start gap-3 rounded-xl border border-gray-200 p-4 text-sm dark:border-dark-700">
              <input v-model="draft.direct_view_enabled" type="checkbox" class="mt-0.5 h-4 w-4" />
              <span>
                <span class="font-medium text-gray-900 dark:text-white">{{ t('admin.invocationArchive.config.directView') }}</span>
                <span class="mt-1 block text-gray-500 dark:text-dark-300">{{ t('admin.invocationArchive.config.directViewHint') }}</span>
              </span>
            </label>

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
            <button type="button" class="btn btn-secondary btn-sm" :disabled="loading.runtime" @click="loadRuntime">{{ t('common.refresh') }}</button>
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
          <div v-if="runtime?.last_config_error || runtime?.last_persist_error" class="mt-6 grid gap-4 lg:grid-cols-2">
            <div v-if="runtime.last_config_error" class="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200"><p class="font-medium">{{ t('admin.invocationArchive.runtime.configError') }}</p><p class="mt-1 break-words">{{ runtime.last_config_error }}</p></div>
            <div v-if="runtime.last_persist_error" class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800 dark:border-red-900/70 dark:bg-red-950/30 dark:text-red-200"><p class="font-medium">{{ t('admin.invocationArchive.runtime.persistError') }}</p><p class="mt-1 break-words">{{ runtime.last_persist_error }}</p></div>
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
            <label class="mt-4 block text-sm text-gray-700 dark:text-dark-200">
              <span>{{ t('admin.invocationArchive.detail.revealReason') }}</span>
              <textarea v-model="revealReason" rows="3" maxlength="256" class="input mt-1 w-full resize-y" :placeholder="t('admin.invocationArchive.detail.revealReasonPlaceholder')" />
            </label>
            <div class="mt-3 flex flex-wrap items-center gap-3">
              <button type="button" class="btn btn-primary btn-sm" :disabled="loading.revealing || revealReason.trim().length < 3" @click="revealPayloads">{{ loading.revealing ? t('common.verifying') : t('admin.invocationArchive.detail.reveal') }}</button>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.invocationArchive.detail.revealHint') }}</p>
            </div>
          </template>
          <div v-if="reveal" class="mt-5 grid gap-4 xl:grid-cols-2">
            <article v-for="payload in revealedPayloads" :key="payload.label" class="min-w-0 rounded-xl border border-gray-200 dark:border-dark-700">
              <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-dark-700">
                <div><h4 class="text-sm font-semibold text-gray-950 dark:text-white">{{ payload.label }}</h4><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ payloadMeta(payload.payload) }}</p></div>
                <button v-if="payload.payload.available" type="button" class="btn btn-secondary btn-sm" @click="copyText(payload.payload.data || '')">{{ t('common.copy') }}</button>
              </div>
              <pre class="max-h-[28rem] overflow-auto whitespace-pre-wrap break-words bg-gray-50 p-4 text-xs leading-6 text-gray-800 dark:bg-dark-900/70 dark:text-dark-100">{{ payload.payload.available ? payload.payload.data : payloadUnavailableLabel(payload.payload.status) }}</pre>
            </article>
          </div>
        </section>

        <section class="mt-6">
          <h3 class="text-sm font-semibold text-gray-950 dark:text-white">{{ t('admin.invocationArchive.detail.accesses') }}</h3>
          <div class="mt-3 overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700/60">
            <table class="min-w-[760px] w-full text-left text-sm">
              <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-900/70 dark:text-dark-400"><tr><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.detail.accessTime') }}</th><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.detail.admin') }}</th><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.detail.accessOutcome') }}</th><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.detail.reason') }}</th><th class="px-3 py-3 font-medium">{{ t('admin.invocationArchive.detail.client') }}</th></tr></thead>
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-transparent">
                <tr v-if="accessLogs.length === 0"><td colspan="5" class="px-4 py-9 text-center text-sm text-gray-500">{{ t('admin.invocationArchive.detail.noAccesses') }}</td></tr>
                <tr v-for="access in accessLogs" v-else :key="access.id"><td class="whitespace-nowrap px-3 py-3 text-xs text-gray-600 dark:text-dark-300">{{ formatDate(access.created_at) }}</td><td class="px-3 py-3">{{ access.admin_name || '—' }}</td><td class="px-3 py-3">{{ access.outcome }}</td><td class="max-w-80 px-3 py-3 break-words">{{ access.reason || '—' }}</td><td class="px-3 py-3 text-xs text-gray-500">{{ access.client_ip || '—' }}</td></tr>
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
import { computed, onMounted, reactive, ref } from 'vue'
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
  emptyInvocationArchiveFilters,
  type InvocationArchiveAccessLog,
  type InvocationArchiveConfig,
  type InvocationArchiveFilters,
  type InvocationArchiveMode,
  type InvocationArchiveOutcome,
  type InvocationArchivePayloadView,
  type InvocationArchiveRecord,
  type InvocationArchiveRecordPage,
  type InvocationArchiveReveal,
  type InvocationArchiveRuntime,
  type InvocationArchiveScope,
  type InvocationArchiveScopeRule,
  type InvocationArchiveSubject,
} from './types'

const MEBIBYTE = 1024 * 1024
const { t, locale } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()

type ArchiveTab = 'records' | 'config' | 'runtime'
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
const reveal = ref<InvocationArchiveReveal | null>(null)
const revealReason = ref('')
const loading = reactive({ config: false, runtime: false, records: false, saving: false, subjects: false, detail: false, revealing: false, deleting: false })
const errors = reactive({ config: '', runtime: '', records: '', subjects: '' })

const dirty = computed(() => configFingerprint(draft.value) !== configFingerprint(serverConfig.value))
const allSelected = computed(() => records.items.length > 0 && records.items.every((record) => selectedIDs.value.includes(record.id)))
const runtimeMetrics = computed(() => {
  if (!runtime.value) return []
  return [
    { label: t('admin.invocationArchive.runtime.status'), value: runtime.value.started ? t('admin.invocationArchive.runtime.running') : t('admin.invocationArchive.runtime.stopped'), hint: t('admin.invocationArchive.runtime.version', { version: runtime.value.config_version }) },
    { label: t('admin.invocationArchive.runtime.queue'), value: `${runtime.value.queue_depth} / ${runtime.value.queue_capacity}`, hint: t('admin.invocationArchive.runtime.queueHint') },
    { label: t('admin.invocationArchive.runtime.persisted'), value: number(runtime.value.persisted), hint: t('admin.invocationArchive.runtime.acceptedDropped', { accepted: number(runtime.value.accepted), dropped: number(runtime.value.dropped) }) },
    { label: t('admin.invocationArchive.runtime.purge'), value: number(runtime.value.expired_purged), hint: t('admin.invocationArchive.runtime.failures', { count: number(runtime.value.persist_failures) }) },
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
const revealedPayloads = computed(() => reveal.value ? [
  { label: t('admin.invocationArchive.records.request'), payload: reveal.value.request },
  { label: t('admin.invocationArchive.records.response'), payload: reveal.value.response },
] : [])

function cloneConfig(config: InvocationArchiveConfig | null): InvocationArchiveConfig | null {
  return config ? { ...config, rules: config.rules.map((rule) => ({ ...rule })) } : null
}
function configFingerprint(config: InvocationArchiveConfig | null): string {
  if (!config) return ''
  return JSON.stringify({
    default_mode: config.default_mode,
    retention_days: config.retention_days,
    max_request_bytes: config.max_request_bytes,
    max_response_bytes: config.max_response_bytes,
    direct_view_enabled: config.direct_view_enabled,
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

async function loadConfig() {
  loading.config = true
  errors.config = ''
  try {
    const config = await invocationArchiveAPI.getConfig()
    serverConfig.value = cloneConfig(config)
    draft.value = cloneConfig(config)
  } catch (error) {
    errors.config = errorMessage(error, 'admin.invocationArchive.errors.loadConfig')
  } finally {
    loading.config = false
  }
}
async function loadRuntime() {
  loading.runtime = true
  errors.runtime = ''
  try {
    runtime.value = await invocationArchiveAPI.getRuntime()
  } catch (error) {
    errors.runtime = errorMessage(error, 'admin.invocationArchive.errors.loadRuntime')
  } finally {
    loading.runtime = false
  }
}
async function loadRecords() {
  loading.records = true
  errors.records = ''
  try {
    const result = await invocationArchiveAPI.listRecords(appliedFilters.value, records.page, records.page_size)
    Object.assign(records, result)
    selectedIDs.value = []
  } catch (error) {
    errors.records = errorMessage(error, 'admin.invocationArchive.errors.loadRecords')
  } finally {
    loading.records = false
  }
}
async function loadInitial() {
  await Promise.allSettled([loadConfig(), loadRuntime(), loadRecords()])
}
function applyFilters() {
  appliedFilters.value = { ...filters.value }
  records.page = 1
  void loadRecords()
}
function resetFilters() {
  filters.value = emptyInvocationArchiveFilters()
  applyFilters()
}
function changePage(page: number) {
  records.page = page
  void loadRecords()
}
function changePageSize(pageSize: number) {
  records.page_size = pageSize
  records.page = 1
  void loadRecords()
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
  reveal.value = null
  revealReason.value = ''
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
  reveal.value = null
  revealReason.value = ''
}
async function revealPayloads() {
  if (!activeRecord.value || revealReason.value.trim().length < 3 || loading.revealing) return
  loading.revealing = true
  try {
    const result = await runSensitive(() => invocationArchiveAPI.revealRecord(activeRecord.value!.id, revealReason.value.trim()))
    if (!result) return
    reveal.value = result
    accessLogs.value = await invocationArchiveAPI.listAccessLogs(activeRecord.value.id)
  } catch (error) {
    appStore.showError(errorMessage(error, 'admin.invocationArchive.errors.reveal'))
  } finally {
    loading.revealing = false
  }
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
function number(value: number): string { return new Intl.NumberFormat(locale.value).format(value) }
function scopeLabel(scope: InvocationArchiveScope): string { return t(`admin.invocationArchive.scopes.${scope}`) }
function outcomeLabel(outcome: InvocationArchiveOutcome): string { return t(`admin.invocationArchive.outcomes.${outcome}`) }
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
  return `${payload.content_type || t('common.notAvailable')} · ${captureLabel(payload.status, payload.captured_bytes, payload.total_bytes, payload.truncated)}${payload.encoding ? ` · ${payload.encoding}` : ''}`
}
function payloadUnavailableLabel(status: string): string {
  const key = `admin.invocationArchive.capture.${status}`
  const label = t(key)
  return label === key ? status : label
}
async function copyText(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    appStore.showSuccess(t('common.copiedToClipboard'))
  } catch {
    appStore.showError(t('common.copyFailed'))
  }
}

onMounted(loadInitial)
</script>
