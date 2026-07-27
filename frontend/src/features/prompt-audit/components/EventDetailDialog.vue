<template>
  <BaseDialog :show="show" :title="t('admin.promptAudit.events.detailTitle')" width="extra-wide" @close="$emit('close')">
    <div v-if="loading" class="py-12 text-center text-sm text-gray-500" aria-busy="true">{{ t('common.loading') }}</div>
    <div v-else-if="event" class="flex flex-col">
      <div class="flex flex-wrap gap-2 border-b border-gray-200 pb-3 dark:border-dark-700" role="tablist">
        <button v-for="tab in tabs" :key="tab" type="button" role="tab" :aria-selected="activeTab === tab" class="rounded-md px-3 py-1.5 text-sm" :class="activeTab === tab ? 'bg-primary-50 text-primary-700 dark:bg-primary-950/40 dark:text-primary-300' : 'text-gray-600 dark:text-dark-300'" @click="activeTab = tab">
          {{ t(`admin.promptAudit.events.tabs.${tab}`) }}
        </button>
      </div>

      <!-- Fixed panel height so switching tabs does not resize the dialog -->
      <div class="mt-5 h-[min(62vh,36rem)] overflow-y-auto" data-test="event-detail-tab-panel">
        <section class="mb-5 border border-gray-200 bg-gray-50/60 p-4 dark:border-dark-700 dark:bg-dark-900/60" data-test="risk-prompt-preview">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h4 class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.promptAudit.events.promptEvidence') }}</h4>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ evidenceHint(event) }}</p>
            </div>
            <span class="border border-gray-300 px-2 py-1 text-xs text-gray-600 dark:border-dark-600 dark:text-dark-300">
              {{ evidenceStatusLabel(event) }}
            </span>
          </div>
          <pre class="mt-3 max-h-[min(28vh,18rem)] overflow-auto whitespace-pre-wrap break-words bg-white p-4 text-sm text-gray-700 dark:bg-dark-950 dark:text-dark-200" data-test="risk-prompt-full">{{ displayPrompt(event) }}</pre>
          <form v-if="event.evidence_available && !revealedPrompt" class="mt-3 flex flex-col gap-2 sm:flex-row" @submit.prevent="requestReveal">
            <input
              v-model.trim="revealReason"
              type="text"
              maxlength="256"
              class="input min-w-0 flex-1"
              :placeholder="t('admin.promptAudit.events.revealReasonPlaceholder')"
              :aria-label="t('admin.promptAudit.events.revealReason')"
              data-test="evidence-reason"
            />
            <button type="submit" class="btn btn-secondary shrink-0" :disabled="revealing || revealReasonLength < 3" data-test="reveal-evidence">
              {{ revealing ? t('admin.promptAudit.events.revealing') : t('admin.promptAudit.events.reveal') }}
            </button>
          </form>
          <p v-if="revealedPrompt" class="mt-2 text-xs text-amber-700 dark:text-amber-300">{{ t('admin.promptAudit.events.revealedWarning') }}</p>
        </section>

        <div v-show="activeTab === 'summary'" role="tabpanel">
          <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
            <dt class="text-gray-500">{{ t('admin.promptAudit.events.decision') }}</dt><dd class="font-medium text-gray-900 dark:text-white">{{ formatDecisionAction(event.decision, event.action) }}</dd>
            <dt class="text-gray-500">{{ t('admin.promptAudit.events.user') }}</dt><dd>{{ event.snapshot.username || '—' }}</dd>
            <dt class="text-gray-500">{{ t('admin.promptAudit.events.email') }}</dt><dd>{{ event.snapshot.user_email || '—' }}</dd>
            <dt class="text-gray-500">{{ t('admin.promptAudit.events.apiKey') }}</dt><dd>{{ event.snapshot.api_key_name || '—' }}</dd>
            <dt class="text-gray-500">{{ t('admin.promptAudit.events.group') }}</dt><dd>{{ event.snapshot.group_name || '—' }}</dd>
            <dt class="text-gray-500">{{ t('admin.promptAudit.events.model') }}</dt><dd>{{ event.snapshot.model || '—' }}</dd>
            <dt class="text-gray-500">{{ t('admin.promptAudit.events.categories') }}</dt><dd>{{ formatCategories(event.categories) }}</dd>
          </dl>
        </div>

        <div v-show="activeTab === 'risks'" class="space-y-5" role="tabpanel">
          <div>
            <section data-test="risk-guard-return">
              <h4 class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.promptAudit.events.guardReturn') }}</h4>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.events.guardReturnHint') }}</p>
              <pre class="mt-2 h-[min(46vh,26rem)] overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-4 font-mono text-xs text-gray-700 dark:bg-dark-900 dark:text-dark-200">{{ formatGuardReturn(event) }}</pre>
            </section>
          </div>

          <div class="space-y-3">
            <h4 class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.promptAudit.events.riskSummaries') }}</h4>
            <article v-for="issue in event.issue_summaries" :key="`${issue.scanner_id}-${issue.code}`" class="border-l-2 border-red-400 pl-4" data-test="risk-issue">
              <div class="flex flex-wrap items-center gap-2">
                <h5 class="font-medium text-gray-900 dark:text-white">{{ issueTitle(issue) }}</h5>
                <span class="text-xs text-red-600 dark:text-red-300">{{ issueSeverity(issue) }} · {{ issueAction(issue) }}</span>
              </div>
              <p class="mt-1 text-sm text-gray-600 dark:text-dark-300">{{ issueDescription(issue) }}</p>
              <dl class="mt-2 grid gap-1 text-xs text-gray-500 dark:text-dark-400 sm:grid-cols-2">
                <div><dt class="inline text-gray-400">{{ t('admin.promptAudit.events.categories') }} · </dt><dd class="inline">{{ translateCategory(issue.category || issue.scanner_id) }}</dd></div>
                <div><dt class="inline text-gray-400">{{ t('admin.promptAudit.events.score') }} · </dt><dd class="inline">{{ issue.score }}</dd></div>
                <div class="sm:col-span-2"><dt class="inline text-gray-400">{{ t('admin.promptAudit.events.evidence') }} · </dt><dd class="inline break-words">{{ issue.evidence ? translateEvidence(issue.evidence) : '—' }}</dd></div>
              </dl>
            </article>
            <p v-if="event.issue_summaries.length === 0" class="py-6 text-center text-sm text-gray-500">{{ t('admin.promptAudit.events.noRisks') }}</p>
          </div>
        </div>

        <dl v-show="activeTab === 'technical'" class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm" role="tabpanel">
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.requestId') }}</dt><dd class="break-all font-mono">{{ event.snapshot.request_id || '—' }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.promptHash') }}</dt><dd class="break-all font-mono">{{ event.snapshot.prompt_hash }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.technical.scanner') }}</dt><dd>{{ event.scanner_backend }} · {{ event.scanner_version }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.technical.adapter') }}</dt><dd>{{ event.detector_adapter || 'qwen3guard_chat' }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.technical.providerRequest') }}</dt><dd class="break-all font-mono">{{ event.provider_request_id || '—' }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.technical.finishReason') }}</dt><dd>{{ event.finish_reason || '—' }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.technical.modelDigest') }}</dt><dd class="break-all font-mono">{{ event.model_digest || '—' }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.technical.policy') }}</dt><dd>{{ event.policy_id }} · v{{ event.policy_version }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.technical.guardEndpoint') }}</dt><dd>{{ event.guard_endpoint_id }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.technical.config') }}</dt><dd>v{{ event.config_version }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.technical.chunks') }}</dt><dd>{{ event.chunk_total }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.technical.latency') }}</dt><dd>{{ event.latency_ms }} ms</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.stage') }}</dt><dd>{{ event.snapshot.stage || 'http' }}</dd>
          <dt class="text-gray-500">{{ t('admin.promptAudit.events.technical.protocol') }}</dt><dd>{{ event.snapshot.protocol }} · {{ event.snapshot.endpoint }}</dd>
        </dl>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { PromptAuditEvent, PromptIssueSummary } from '../types'
import { SCANNER_CATALOG } from '../viewModel'

const props = withDefaults(defineProps<{
  show: boolean
  event: PromptAuditEvent | null
  loading: boolean
  revealedPrompt?: string
  revealing?: boolean
}>(), {
  revealedPrompt: '',
  revealing: false,
})
const emit = defineEmits<{ (event: 'close'): void; (event: 'reveal', reason: string): void }>()
const { t } = useI18n()
const tabs = ['summary', 'risks', 'technical'] as const
const activeTab = ref<(typeof tabs)[number]>('summary')
const revealReason = ref('')
const revealReasonLength = computed(() => Array.from(revealReason.value.trim()).length)
watch(() => props.event?.id, () => {
  activeTab.value = 'summary'
  revealReason.value = ''
})

const DECISIONS = new Set(['pass', 'flag', 'critical'])
const ACTIONS = new Set(['Allow', 'Warn', 'Block'])
const RISK_LEVELS = new Set(['low', 'medium', 'high', 'critical'])

function displayPrompt(event: PromptAuditEvent): string {
  return props.revealedPrompt || event.snapshot.redacted_preview || '—'
}

function evidenceStatusLabel(event: PromptAuditEvent): string {
  const key = `admin.promptAudit.events.evidenceStatuses.${event.evidence_status || 'not_stored'}`
  const translated = t(key)
  return translated === key ? event.evidence_status : translated
}

function evidenceHint(event: PromptAuditEvent): string {
  if (props.revealedPrompt) return t('admin.promptAudit.events.promptRevealedHint')
  if (event.evidence_available) return t('admin.promptAudit.events.promptEncryptedHint')
  return t('admin.promptAudit.events.promptRedactedHint')
}

function requestReveal() {
  if (revealReasonLength.value < 3 || props.revealing) return
  emit('reveal', revealReason.value.trim())
}

function formatDecisionAction(decision: string, action: string): string {
  const decisionLabel = DECISIONS.has(decision) ? t(`admin.promptAudit.decisions.${decision}`) : decision
  const actionLabel = ACTIONS.has(action) ? t(`admin.promptAudit.actions.${action}`) : action
  return `${decisionLabel} · ${actionLabel}`
}
function translateCategory(category: string): string {
  return SCANNER_CATALOG.some((scanner) => scanner.id === category)
    ? t(`admin.promptAudit.scanners.${category}`)
    : category
}
function formatCategories(categories: string[]): string {
  if (!categories.length) return '—'
  return categories.map(translateCategory).join(', ')
}
function translateEvidence(value: string): string {
  const byId = SCANNER_CATALOG.find((scanner) => scanner.id === value)
  if (byId) return t(`admin.promptAudit.scanners.${byId.id}`)
  const byLabel = SCANNER_CATALOG.find((scanner) => scanner.label === value)
  if (byLabel) return t(`admin.promptAudit.scanners.${byLabel.id}`)
  return value
}
function formatGuardReturn(event: PromptAuditEvent): string {
  const evidence: Record<string, string> = {}
  for (const [key, value] of Object.entries(event.scanner_evidence || {})) {
    evidence[key] = translateEvidence(value)
  }
  return JSON.stringify({
    decision: DECISIONS.has(event.decision) ? t(`admin.promptAudit.decisions.${event.decision}`) : event.decision,
    risk_level: RISK_LEVELS.has(event.risk_level) ? t(`admin.promptAudit.riskLevels.${event.risk_level}`) : event.risk_level,
    action: ACTIONS.has(event.action) ? t(`admin.promptAudit.actions.${event.action}`) : event.action,
    categories: event.categories.map(translateCategory),
    matched_scanners: event.matched_scanners.map(translateCategory),
    scanner_scores: event.scanner_scores,
    scanner_evidence: evidence,
    scanner_backend: event.scanner_backend,
    scanner_version: event.scanner_version,
    guard_endpoint_id: event.guard_endpoint_id,
    chunk_total: event.chunk_total,
    latency_ms: event.latency_ms,
  }, null, 2)
}
function issueTitle(issue: PromptIssueSummary): string {
  return translateCategory(issue.category || issue.scanner_id) || issue.title
}
function issueDescription(issue: PromptIssueSummary): string {
  const category = issue.category || issue.scanner_id
  const key = `admin.promptAudit.scannerDescriptions.${category}`
  const label = t(key)
  return label === key ? issue.description : label
}
function issueSeverity(issue: PromptIssueSummary): string {
  return RISK_LEVELS.has(issue.severity) ? t(`admin.promptAudit.riskLevels.${issue.severity}`) : issue.severity_label || issue.severity
}
function issueAction(issue: PromptIssueSummary): string {
  return ACTIONS.has(issue.action) ? t(`admin.promptAudit.actions.${issue.action}`) : issue.action_label || issue.action
}
</script>
