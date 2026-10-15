<template>
  <div class="space-y-3 rounded-lg border border-cyan-200 bg-cyan-50/50 p-4 dark:border-cyan-800 dark:bg-cyan-950/20">
    <p class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ platform === 'workbuddy_ai' ? 'WorkBuddy AI' : 'WorkBuddy' }} · OAuth</p>
    <p class="input-hint">{{ t('admin.accounts.workbuddy.authHint') }}</p>
    <div class="flex flex-wrap items-center gap-3">
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="start">{{ t('admin.accounts.workbuddy.signIn') }}</button>
      <a v-if="authUrl" :href="authUrl" target="_blank" rel="noopener noreferrer" class="text-sm text-cyan-700 underline dark:text-cyan-300">{{ t('admin.accounts.workbuddy.openLogin') }}</a>
      <span v-if="polling" role="status" class="text-xs text-gray-500">{{ t('admin.accounts.workbuddy.waiting') }}</span>
      <button v-if="polling" type="button" class="text-xs text-gray-500 underline" @click="cancel">{{ t('common.cancel') }}</button>
    </div>
    <p v-if="modelValue" role="status" class="text-sm text-green-700 dark:text-green-300">{{ t('admin.accounts.workbuddy.ready') }} · {{ String(modelValue.nickname || modelValue.uid || '') }}</p>
    <p v-else-if="existing" class="input-hint">{{ t('admin.accounts.workbuddy.keepCredentials') }}</p>
    <details class="text-sm">
      <summary class="cursor-pointer text-gray-600 dark:text-gray-400">{{ t('admin.accounts.workbuddy.importTitle') }}</summary>
      <p class="input-hint my-2">{{ t('admin.accounts.workbuddy.importHint') }}</p>
      <textarea v-model="importText" rows="4" class="input font-mono text-xs" :aria-label="t('admin.accounts.workbuddy.importTitle')" autocomplete="off" spellcheck="false" placeholder='{"auth":{...},"account":{...}}'></textarea>
      <button type="button" class="btn btn-secondary mt-2" :disabled="busy || !importText.trim()" @click="importCredentials">{{ t('admin.accounts.workbuddy.importAction') }}</button>
    </details>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { importWorkBuddyCredentials, pollWorkBuddyLogin, startWorkBuddyLogin } from '@/api/admin/workbuddy'
import type { WorkBuddyCredentials, WorkBuddyPlatform } from '@/api/admin/workbuddy'

const props = defineProps<{ platform: WorkBuddyPlatform; proxyId: number | null; modelValue: WorkBuddyCredentials | null; existing?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: WorkBuddyCredentials | null] }>()
const { t } = useI18n()
const busy = ref(false)
const polling = ref(false)
const authUrl = ref('')
const error = ref('')
const importText = ref('')
let controller: AbortController | null = null
let timer: ReturnType<typeof setTimeout> | null = null

function cancel() {
  controller?.abort()
  controller = null
  if (timer) clearTimeout(timer)
  timer = null
  busy.value = false
  polling.value = false
}

function messageOf(err: unknown): string {
  const apiError = err as { response?: { data?: { message?: string } }; message?: string }
  return apiError.response?.data?.message || apiError.message || t('admin.accounts.workbuddy.authError')
}

async function start() {
  cancel()
  const attempt = new AbortController()
  controller = attempt
  busy.value = true
  error.value = ''
  authUrl.value = ''
  emit('update:modelValue', null)
  try {
    const result = await startWorkBuddyLogin(props.platform, props.proxyId, attempt.signal)
    if (attempt.signal.aborted) return
    if (!result.auth_url || !result.session_id || new URL(result.auth_url).protocol !== 'https:') throw new Error(t('admin.accounts.workbuddy.authError'))
    authUrl.value = result.auth_url
    busy.value = false
    polling.value = true
    const expires = Date.parse(result.expires_at)
    if (!Number.isFinite(expires)) throw new Error(t('admin.accounts.workbuddy.authError'))
    const session = result.session_id
    const poll = async () => {
      if (attempt.signal.aborted) return
      try {
        if (Date.now() >= expires) throw new Error(t('admin.accounts.workbuddy.expired'))
        const next = await pollWorkBuddyLogin(session, attempt.signal)
        if (attempt.signal.aborted) return
        if (next.status === 'ready') {
          if (!next.credentials?.access_token || !next.credentials.uid) throw new Error(t('admin.accounts.workbuddy.authError'))
          emit('update:modelValue', next.credentials)
          authUrl.value = ''
          cancel()
          return
        }
        timer = setTimeout(poll, 1500)
      } catch (err) {
        if (!attempt.signal.aborted) { error.value = messageOf(err); cancel() }
      }
    }
    timer = setTimeout(poll, 1000)
  } catch (err) {
    if (!attempt.signal.aborted) { error.value = messageOf(err); cancel() }
  }
}

async function importCredentials() {
  cancel()
  const attempt = new AbortController()
  controller = attempt
  busy.value = true
  error.value = ''
  authUrl.value = ''
  try {
    const raw: unknown = JSON.parse(importText.value)
    const credentials = await importWorkBuddyCredentials(raw, attempt.signal)
    if (!attempt.signal.aborted) { emit('update:modelValue', credentials); importText.value = '' }
  } catch (err) {
    if (!attempt.signal.aborted) error.value = messageOf(err)
  } finally {
    if (controller === attempt) cancel()
  }
}

watch(() => [props.platform, props.proxyId], () => {
  cancel(); authUrl.value = ''; error.value = ''; importText.value = ''; emit('update:modelValue', null)
})
onBeforeUnmount(cancel)
</script>
