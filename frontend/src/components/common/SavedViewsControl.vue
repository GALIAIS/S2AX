<template>
  <div class="flex min-w-0 flex-wrap items-center gap-2" :aria-label="t('common.savedViews')">
    <span class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('common.savedViews') }}</span>
    <div class="min-w-40 flex-1 basis-44">
      <Select
        v-model="selectedId"
        :options="viewOptions"
        :placeholder="t('common.savedViewsPlaceholder')"
        :clearable="true"
        :disabled="disabled || viewOptions.length === 0"
        @change="applySelected"
      />
    </div>
    <button
      type="button"
      class="btn btn-secondary btn-sm shrink-0"
      :disabled="disabled"
      :title="t('common.saveView')"
      @click="openSaveDialog"
    >
      <Icon name="plus" size="sm" />
      <span class="hidden md:inline">{{ t('common.saveView') }}</span>
    </button>
    <button
      v-if="selectedId"
      type="button"
      class="btn btn-ghost btn-sm shrink-0"
      :disabled="disabled"
      :title="t('common.deleteSavedView')"
      @click="deleteSelected"
    >
      <Icon name="trash" size="sm" />
      <span class="sr-only">{{ t('common.deleteSavedView') }}</span>
    </button>

    <BaseDialog
      :show="showSaveDialog"
      :title="t('common.saveView')"
      width="narrow"
      @close="showSaveDialog = false"
    >
      <form class="space-y-4" @submit.prevent="saveCurrentView">
        <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('common.savedViewDescription') }}</p>
        <label class="input-label" for="saved-view-name">{{ t('common.savedViewName') }}</label>
        <input
          id="saved-view-name"
          ref="nameInput"
          v-model="viewName"
          class="input"
          type="text"
          maxlength="64"
          autocomplete="off"
          :placeholder="t('common.savedViewNamePlaceholder')"
          required
        />
        <div class="flex justify-end gap-2 border-t border-gray-200 pt-4 dark:border-dark-700">
          <button type="button" class="btn btn-secondary" @click="showSaveDialog = false">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" class="btn btn-primary" :disabled="!viewName.trim()">
            {{ t('common.save') }}
          </button>
        </div>
      </form>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from './BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from './Select.vue'
import { useSavedViews } from '@/composables/useSavedViews'

const props = withDefaults(defineProps<{
  storageKey: string
  state: Record<string, unknown>
  disabled?: boolean
}>(), {
  disabled: false
})

const emit = defineEmits<{
  (event: 'apply', state: Record<string, unknown>): void
}>()

const { t } = useI18n()
const { views, save, remove } = useSavedViews<Record<string, unknown>>(props.storageKey)
const selectedId = ref<string | null>(null)
const showSaveDialog = ref(false)
const viewName = ref('')
const nameInput = ref<HTMLInputElement | null>(null)

const viewOptions = computed(() => views.value.map((view) => ({
  value: view.id,
  label: view.name
})))

const applySelected = (value: string | number | boolean | null) => {
  if (!value) return
  const view = views.value.find((item) => item.id === String(value))
  if (view) emit('apply', { ...view.state })
}

const openSaveDialog = async () => {
  viewName.value = ''
  showSaveDialog.value = true
  await nextTick()
  nameInput.value?.focus()
}

const saveCurrentView = () => {
  const saved = save(viewName.value, props.state)
  if (!saved) return
  selectedId.value = saved.id
  showSaveDialog.value = false
}

const deleteSelected = () => {
  if (!selectedId.value) return
  remove(selectedId.value)
  selectedId.value = null
}
</script>
