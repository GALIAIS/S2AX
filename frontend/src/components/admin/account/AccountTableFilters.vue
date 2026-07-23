<template>
  <div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
    <SearchInput
      :model-value="searchQuery"
      :placeholder="t('admin.accounts.searchAccounts')"
      class="w-full sm:w-64"
      @update:model-value="$emit('update:searchQuery', $event)"
      @search="$emit('change')"
    />
    <Select :model-value="filters.platform" class="w-full sm:w-40" :options="platformOptions" @update:model-value="updatePlatform" @change="$emit('change')" />
    <Select :model-value="filters.type" class="w-full sm:w-40" :options="typeOptions" @update:model-value="updateType" @change="$emit('change')" />
    <Select :model-value="filters.status" class="w-full sm:w-40" :options="statusOptions" @update:model-value="updateStatus" @change="$emit('change')" />
    <Select :model-value="filters.privacy_mode" class="w-full sm:w-40" :options="privacyOptions" @update:model-value="updatePrivacyMode" @change="$emit('change')" />
    <Select :model-value="filters.group" class="w-full sm:w-40" :options="groupOptions" @update:model-value="updateGroup" @change="$emit('change')" />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import type { AdminGroup, SelectOption } from '@/types'

type FilterValue = string | number | boolean | null
type AccountFilters = Record<string, FilterValue | undefined>

interface GroupFilterOption extends SelectOption {
  kind?: 'group'
  disabled?: boolean
}

const props = defineProps<{
  searchQuery: string
  filters: AccountFilters
  groups?: AdminGroup[]
}>()

const emit = defineEmits(['update:searchQuery', 'update:filters', 'change'])
const { t } = useI18n()

const updatePlatform = (value: FilterValue) => {
  emit('update:filters', { ...props.filters, platform: value })
}
const updateType = (value: FilterValue) => {
  emit('update:filters', { ...props.filters, type: value })
}
const updateStatus = (value: FilterValue) => {
  emit('update:filters', { ...props.filters, status: value })
}
const updatePrivacyMode = (value: FilterValue) => {
  emit('update:filters', { ...props.filters, privacy_mode: value })
}
const updateGroup = (value: FilterValue) => {
  emit('update:filters', { ...props.filters, group: value })
}

const platformOptions = computed(() => [
  { value: '', label: t('admin.accounts.allPlatforms') },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' }
])

const typeOptions = computed(() => [
  { value: '', label: t('admin.accounts.allTypes') },
  { value: 'oauth', label: t('admin.accounts.oauthType') },
  { value: 'setup-token', label: t('admin.accounts.setupToken') },
  { value: 'apikey', label: t('admin.accounts.apiKey') },
  { value: 'bedrock', label: 'AWS Bedrock' }
])

const statusOptions = computed(() => [
  { value: '', label: t('admin.accounts.allStatus') },
  { value: 'active', label: t('admin.accounts.status.active') },
  { value: 'inactive', label: t('admin.accounts.status.inactive') },
  { value: 'error', label: t('admin.accounts.status.error') },
  { value: 'rate_limited', label: t('admin.accounts.status.rateLimited') },
  { value: 'temp_unschedulable', label: t('admin.accounts.status.tempUnschedulable') },
  { value: 'unschedulable', label: t('admin.accounts.status.unschedulable') }
])

const privacyOptions = computed(() => [
  { value: '', label: t('admin.accounts.allPrivacyModes') },
  { value: '__unset__', label: t('admin.accounts.privacyUnset') },
  { value: 'training_off', label: t('admin.accounts.privacyTrainingOff') },
  { value: 'training_set_cf_blocked', label: t('admin.accounts.privacyCfBlocked') },
  { value: 'training_set_failed', label: t('admin.accounts.privacyFailed') }
])

const sortedGroups = (groups: AdminGroup[]) => {
  return [...groups].sort((left, right) => {
    const nameComparison = left.name.localeCompare(right.name, undefined, { sensitivity: 'base' })
    return nameComparison || left.id - right.id
  })
}

/**
 * Keep account-group filters scannable after large imports. Active routing
 * groups are separated by allocation semantics; inactive groups remain
 * filterable under their own section for maintenance and cleanup work.
 */
const groupOptions = computed<GroupFilterOption[]>(() => {
  const exclusive: AdminGroup[] = []
  const publicGroups: AdminGroup[] = []
  const subscription: AdminGroup[] = []
  const disabled: AdminGroup[] = []

  for (const group of props.groups || []) {
    if (group.status !== 'active') {
      disabled.push(group)
    } else if (group.subscription_type === 'subscription') {
      subscription.push(group)
    } else if (group.is_exclusive) {
      exclusive.push(group)
    } else {
      publicGroups.push(group)
    }
  }

  const options: GroupFilterOption[] = [
    { value: '', label: t('admin.accounts.allGroups') },
    { value: 'ungrouped', label: t('admin.accounts.ungroupedGroup') }
  ]
  const sections: Array<[string, string, AdminGroup[]]> = [
    ['exclusive', t('admin.accounts.groupFilterSections.exclusive'), exclusive],
    ['public', t('admin.accounts.groupFilterSections.public'), publicGroups],
    ['subscription', t('admin.accounts.groupFilterSections.subscription'), subscription],
    ['disabled', t('admin.accounts.groupFilterSections.disabled'), disabled]
  ]

  for (const [key, label, groups] of sections) {
    const items = sortedGroups(groups)
    if (items.length === 0) continue
    options.push({
      value: `__account-group-section-${key}__`,
      label,
      kind: 'group',
      disabled: true
    })
    options.push(...items.map((group) => ({ value: String(group.id), label: group.name })))
  }

  return options
})
</script>
