<template>
  <section class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t('admin.settings.ipGeolocation.title') }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.settings.ipGeolocation.description') }}
      </p>
    </div>

    <div class="space-y-5 p-6">
      <div v-if="loading" class="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
        <LoadingSpinner size="sm" color="secondary" />
        {{ t('common.loading') }}
      </div>

      <div
        v-else-if="loadError"
        class="flex items-center justify-between gap-3 border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
        role="alert"
      >
        <span>{{ loadError }}</span>
        <button type="button" class="btn btn-secondary btn-sm" @click="loadSettings">
          {{ t('misc.retry') }}
        </button>
      </div>

      <template v-else>
        <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-8">
          <div>
            <label class="font-medium text-gray-900 dark:text-white">
              {{ t('admin.settings.ipGeolocation.provider') }}
            </label>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.ipGeolocation.providerHint') }}
            </p>
          </div>
          <div class="w-full sm:w-80">
            <Select v-model="draft.provider" :options="providerOptions" :searchable="false" />
          </div>
        </div>

        <div v-if="draft.provider === 'ip2region'" class="space-y-5 border-t border-gray-100 pt-5 dark:border-dark-700">
          <div>
            <label for="ip-geolocation-ipv4-xdb" class="input-label">
              {{ t('admin.settings.ipGeolocation.ipv4XdbPath') }}
            </label>
            <input
              id="ip-geolocation-ipv4-xdb"
              v-model.trim="draft.ipv4_xdb_path"
              type="text"
              class="input font-mono text-sm"
              :placeholder="t('admin.settings.ipGeolocation.ipv4XdbPlaceholder')"
              @keydown.enter.prevent
            />
          </div>

          <div>
            <label for="ip-geolocation-ipv6-xdb" class="input-label">
              {{ t('admin.settings.ipGeolocation.ipv6XdbPath') }}
            </label>
            <input
              id="ip-geolocation-ipv6-xdb"
              v-model.trim="draft.ipv6_xdb_path"
              type="text"
              class="input font-mono text-sm"
              :placeholder="t('admin.settings.ipGeolocation.ipv6XdbPlaceholder')"
              @keydown.enter.prevent
            />
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.settings.ipGeolocation.xdbPathHint') }}
            </p>
          </div>

          <div class="grid gap-5 sm:grid-cols-2">
            <div>
              <label class="input-label">{{ t('admin.settings.ipGeolocation.cachePolicy') }}</label>
              <Select v-model="draft.cache_policy" :options="cachePolicyOptions" :searchable="false" />
            </div>
            <div>
              <label for="ip-geolocation-searchers" class="input-label">
                {{ t('admin.settings.ipGeolocation.searchers') }}
              </label>
              <input
                id="ip-geolocation-searchers"
                v-model.number="draft.searchers"
                type="number"
                min="1"
                max="64"
                class="input"
                @keydown.enter.prevent
              />
              <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.ipGeolocation.searchersHint') }}
              </p>
            </div>
          </div>

          <div class="flex items-center justify-between gap-4 border-t border-gray-100 pt-5 dark:border-dark-700">
            <div>
              <label class="font-medium text-gray-900 dark:text-white">
                {{ t('admin.settings.ipGeolocation.compatibilityFallback') }}
              </label>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.ipGeolocation.compatibilityFallbackHint') }}
              </p>
            </div>
            <Toggle v-model="draft.compatibility_fallback_enabled" />
          </div>
        </div>

        <div
          class="border px-3 py-3 text-sm"
          :class="runtimeClasses"
          role="status"
        >
          <p class="font-medium">{{ t('admin.settings.ipGeolocation.runtimeTitle') }}</p>
          <p class="mt-1">{{ runtimeMessage }}</p>
        </div>

        <div class="flex justify-end border-t border-gray-100 pt-5 dark:border-dark-700">
          <button type="button" class="btn btn-primary" :disabled="saving" @click="saveSettings">
            <LoadingSpinner v-if="saving" size="sm" color="white" class="mr-2" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getIPGeolocationSettings,
  updateIPGeolocationSettings,
  type IPGeolocationSettings,
  type UpdateIPGeolocationSettingsRequest,
} from '@/api/admin/settings'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const emptySettings = (): IPGeolocationSettings => ({
  provider: 'ip2region',
  ipv4_xdb_path: '',
  ipv6_xdb_path: '',
  cache_policy: 'vectorindex',
  searchers: 4,
  compatibility_fallback_enabled: true,
  ipv4_database_loaded: false,
  ipv6_database_loaded: false,
  local_resolver_available: false,
})

const draft = reactive<IPGeolocationSettings>(emptySettings())
const loading = ref(true)
const saving = ref(false)
const loadError = ref('')

const providerOptions = computed(() => [
  { value: 'ip2region', label: t('admin.settings.ipGeolocation.providers.ip2region') },
  { value: 'geojs', label: t('admin.settings.ipGeolocation.providers.geojs') },
  { value: 'disabled', label: t('admin.settings.ipGeolocation.providers.disabled') },
])

const cachePolicyOptions = computed(() => [
  { value: 'vectorindex', label: t('admin.settings.ipGeolocation.cachePolicies.vectorindex') },
  { value: 'content', label: t('admin.settings.ipGeolocation.cachePolicies.content') },
  { value: 'file', label: t('admin.settings.ipGeolocation.cachePolicies.file') },
])

const runtimeMessage = computed(() => {
  if (draft.provider === 'disabled') {
    return t('admin.settings.ipGeolocation.runtimeDisabled')
  }
  if (draft.provider === 'geojs') {
    return t('admin.settings.ipGeolocation.runtimeGeojs')
  }
  if (draft.local_resolver_available) {
    return t('admin.settings.ipGeolocation.runtimeOfflineReady')
  }
  return draft.compatibility_fallback_enabled
    ? t('admin.settings.ipGeolocation.runtimeFallback')
    : t('admin.settings.ipGeolocation.runtimeUnavailable')
})

const runtimeClasses = computed(() => {
  if (draft.provider === 'disabled' || (draft.provider === 'ip2region' && !draft.local_resolver_available && !draft.compatibility_fallback_enabled)) {
    return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200'
  }
  if (draft.provider === 'ip2region' && draft.local_resolver_available) {
    return 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900/60 dark:bg-emerald-950/30 dark:text-emerald-200'
  }
  return 'border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-200'
})

function assignSettings(settings: IPGeolocationSettings): void {
  Object.assign(draft, settings)
}

async function loadSettings(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    assignSettings(await getIPGeolocationSettings())
  } catch (error) {
    loadError.value = extractApiErrorMessage(error, t('common.error'))
  } finally {
    loading.value = false
  }
}

async function saveSettings(): Promise<void> {
  saving.value = true
  try {
    const payload: UpdateIPGeolocationSettingsRequest = {
      provider: draft.provider,
      ipv4_xdb_path: draft.ipv4_xdb_path,
      ipv6_xdb_path: draft.ipv6_xdb_path,
      cache_policy: draft.cache_policy,
      searchers: Number(draft.searchers),
      compatibility_fallback_enabled: draft.compatibility_fallback_enabled,
    }
    assignSettings(await updateIPGeolocationSettings(payload))
    appStore.showSuccess(t('common.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  void loadSettings()
})
</script>
