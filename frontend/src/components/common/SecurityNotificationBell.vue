<template>
  <div>
    <button
      type="button"
      class="relative flex h-9 w-9 items-center justify-center border border-transparent text-gray-600 transition-colors hover:border-gray-200 hover:bg-gray-100 dark:text-dark-300 dark:hover:border-dark-600 dark:hover:bg-dark-800"
      :class="unreadCount > 0 ? 'text-amber-600 dark:text-amber-400' : ''"
      :aria-label="t('securityNotifications.title')"
      @click="openPanel"
    >
      <Icon name="shield" size="md" />
      <span
        v-if="unreadCount > 0"
        class="absolute -right-1 -top-1 min-w-4 border border-white bg-amber-600 px-1 text-center text-[10px] font-semibold leading-[14px] text-white dark:border-dark-900"
      >
        {{ unreadCount > 99 ? '99+' : unreadCount }}
      </span>
    </button>

    <Teleport to="body">
      <Transition :css="false" @enter="enter" @leave="leave">
        <div
          v-if="open"
          class="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto p-3 pt-[6vh] sm:p-6 sm:pt-[9vh]"
          style="background: var(--ui-overlay)"
          @click.self="closePanel"
        >
          <section
            class="material-panel flex max-h-[calc(100dvh-1.5rem)] w-full max-w-2xl flex-col sm:max-h-[82vh]"
            role="dialog"
            aria-modal="true"
            :aria-label="t('securityNotifications.title')"
          >
            <header class="material-toolbar flex flex-wrap items-start justify-between gap-4 px-5 py-4 sm:px-6">
              <div class="flex min-w-0 items-center gap-3">
                <span class="flex h-9 w-9 shrink-0 items-center justify-center bg-amber-600 text-white">
                  <Icon name="shield" size="sm" />
                </span>
                <div class="min-w-0">
                  <h2 class="text-lg font-semibold tracking-tight text-gray-950 dark:text-white">
                    {{ t('securityNotifications.title') }}
                  </h2>
                  <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                    {{ unreadCount > 0
                      ? t('securityNotifications.newCount', { count: unreadCount })
                      : t('securityNotifications.allCaughtUp') }}
                  </p>
                </div>
              </div>
              <div class="flex items-center gap-2">
                <button
                  v-if="unreadCount > 0"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :disabled="loading"
                  @click="markAllRead"
                >
                  <Icon name="check" size="sm" />
                  {{ t('securityNotifications.markAllRead') }}
                </button>
                <button
                  type="button"
                  class="btn btn-ghost btn-icon h-9 w-9 p-0"
                  :aria-label="t('common.close')"
                  @click="closePanel"
                >
                  <Icon name="x" size="sm" />
                </button>
              </div>
            </header>

            <div class="flex items-center justify-between gap-3 border-b px-5 py-3 sm:px-6" style="border-color: var(--ui-separator)">
              <div class="tabs p-0" role="tablist" :aria-label="t('securityNotifications.filterLabel')">
                <button type="button" class="tab px-3 py-1.5 text-xs" :class="filter === 'all' ? 'tab-active' : ''" @click="filter = 'all'">
                  {{ t('securityNotifications.all') }}
                </button>
                <button type="button" class="tab px-3 py-1.5 text-xs" :class="filter === 'unread' ? 'tab-active' : ''" @click="filter = 'unread'">
                  {{ t('securityNotifications.unread') }} · {{ unreadCount }}
                </button>
              </div>
              <button type="button" class="btn btn-ghost btn-sm" :disabled="loading" @click="refresh">
                <Icon name="refresh" size="sm" />
                {{ t('common.refresh') }}
              </button>
            </div>

            <div class="min-h-0 flex-1 overflow-y-auto">
              <div v-if="loading && notifications.length === 0" class="flex min-h-64 items-center justify-center">
                <span class="spinner text-primary-500" aria-hidden="true" />
                <span class="sr-only">{{ t('common.loading') }}</span>
              </div>
              <div v-else-if="visibleNotifications.length" ref="listRef" class="divide-y divide-gray-200 dark:divide-dark-700">
                <article
                  v-for="item in visibleNotifications"
                  :key="item.id"
                  data-motion-item
                  class="grid grid-cols-[3px_minmax(0,1fr)]"
                >
                  <span :class="item.status === 'unread' ? severityBar(item.severity) : 'bg-transparent'" aria-hidden="true" />
                  <div class="min-w-0 px-5 py-4">
                    <div class="flex flex-wrap items-start justify-between gap-2">
                      <div class="min-w-0">
                        <div class="flex flex-wrap items-center gap-2">
                          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ item.title }}</h3>
                          <span :class="severityBadge(item.severity)">{{ severityLabel(item.severity) }}</span>
                          <span v-if="item.status === 'unread'" class="badge badge-warning">{{ t('securityNotifications.unread') }}</span>
                        </div>
                        <time class="mt-1 block text-[11px] tabular-nums text-gray-400 dark:text-dark-500">
                          {{ formatRelativeWithDateTime(item.created_at) }}
                        </time>
                      </div>
                      <div class="flex shrink-0 items-center gap-1">
                        <button v-if="item.status === 'unread'" type="button" class="btn btn-ghost btn-sm" @click="markRead(item.id)">
                          {{ t('securityNotifications.markRead') }}
                        </button>
                        <button type="button" class="btn btn-ghost btn-sm" @click="dismiss(item.id)">
                          {{ t('securityNotifications.dismiss') }}
                        </button>
                      </div>
                    </div>
                    <p class="mt-3 whitespace-pre-wrap break-words text-sm leading-6 text-gray-600 dark:text-dark-300">{{ item.body }}</p>
                  </div>
                </article>
              </div>
              <div v-else class="empty-state min-h-64 py-12">
                <Icon name="shield" size="xl" class="text-gray-400 dark:text-dark-500" />
                <h3 class="empty-state-title mt-4">{{ t('securityNotifications.empty') }}</h3>
                <p class="empty-state-description">{{ t('securityNotifications.emptyDescription') }}</p>
              </div>
            </div>
          </section>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useAnimeDialogTransition, animateListEntrance } from '@/composables/useInterfaceMotion'
import { useAppStore } from '@/stores/app'
import { useSecurityNotificationStore } from '@/stores/securityNotifications'
import { formatRelativeWithDateTime } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()
const store = useSecurityNotificationStore()
const { notifications, loading } = storeToRefs(store)
const { enter, leave } = useAnimeDialogTransition()
const open = ref(false)
const filter = ref<'all' | 'unread'>('all')
const listRef = ref<HTMLElement | null>(null)
let listAnimation: ReturnType<typeof animateListEntrance> = null

const unreadCount = computed(() => store.unreadCount)
const visibleNotifications = computed(() =>
  filter.value === 'unread'
    ? notifications.value.filter((item) => item.status === 'unread')
    : notifications.value
)

async function openPanel(): Promise<void> {
  open.value = true
  try {
    await store.fetchNotifications(true)
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('common.unknownError'))
  }
}

function closePanel(): void {
  open.value = false
}

async function refresh(): Promise<void> {
  try {
    await store.fetchNotifications(true)
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('common.unknownError'))
  }
}

async function markRead(id: number): Promise<void> {
  try {
    await store.updateStatus(id, 'read')
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('common.unknownError'))
  }
}

async function dismiss(id: number): Promise<void> {
  try {
    await store.updateStatus(id, 'dismissed')
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('common.unknownError'))
  }
}

async function markAllRead(): Promise<void> {
  try {
    await store.markAllRead()
    appStore.showSuccess(t('securityNotifications.allMarkedRead'))
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : t('common.unknownError'))
  }
}

function severityLabel(severity: string): string {
  const key = `securityNotifications.severity.${severity}`
  const value = t(key)
  return value === key ? severity : value
}

function severityBadge(severity: string): string {
  if (severity === 'critical' || severity === 'high') return 'badge badge-danger'
  if (severity === 'medium') return 'badge badge-warning'
  return 'badge badge-gray'
}

function severityBar(severity: string): string {
  if (severity === 'critical' || severity === 'high') return 'bg-red-500'
  if (severity === 'medium') return 'bg-amber-500'
  return 'bg-primary-500'
}

function handleEscape(event: KeyboardEvent): void {
  if (event.key === 'Escape' && open.value) closePanel()
}

watch([open, filter, () => visibleNotifications.value.length], async ([isOpen]) => {
  if (!isOpen) return
  await nextTick()
  listAnimation?.revert()
  listAnimation = animateListEntrance(listRef.value)
}, { flush: 'post' })

watch(open, (isOpen) => {
  document.body.style.overflow = isOpen ? 'hidden' : ''
})

onMounted(() => {
  document.addEventListener('keydown', handleEscape)
  void store.fetchNotifications().catch(() => undefined)
})

onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleEscape)
  document.body.style.overflow = ''
  listAnimation?.revert()
})
</script>
