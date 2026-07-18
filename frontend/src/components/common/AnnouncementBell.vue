<template>
  <div>
    <button
      type="button"
      class="relative flex h-9 w-9 items-center justify-center border border-transparent text-gray-600 transition-colors hover:border-gray-200 hover:bg-gray-100 dark:text-dark-300 dark:hover:border-dark-600 dark:hover:bg-dark-800"
      :class="unreadCount > 0 ? 'text-primary-600 dark:text-primary-400' : ''"
      :aria-label="t('announcements.title')"
      @click="openModal"
    >
      <Icon name="bell" size="md" />
      <span
        v-if="unreadCount > 0"
        class="absolute -right-1 -top-1 min-w-4 border border-white bg-red-600 px-1 text-center text-[10px] font-semibold leading-[14px] text-white dark:border-dark-900"
      >
        {{ unreadCount > 99 ? '99+' : unreadCount }}
      </span>
    </button>

    <Teleport to="body">
      <Transition :css="false" @enter="enter" @leave="leave">
        <div
          v-if="isModalOpen"
          class="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto p-3 pt-[6vh] backdrop-blur-sm sm:p-6 sm:pt-[9vh]"
          style="background: var(--ui-overlay)"
          @click.self="closeModal"
        >
          <section class="material-panel flex max-h-[82vh] w-full max-w-2xl flex-col" role="dialog" aria-modal="true" :aria-label="t('announcements.title')">
            <header class="material-toolbar flex flex-wrap items-start justify-between gap-4 px-5 py-4 sm:px-6">
              <div class="min-w-0">
                <div class="flex items-center gap-3">
                  <span class="flex h-9 w-9 items-center justify-center bg-primary-500 text-white">
                    <Icon name="bell" size="sm" />
                  </span>
                  <div>
                    <h2 class="text-lg font-semibold tracking-tight text-gray-950 dark:text-white">{{ t('announcements.title') }}</h2>
                    <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                      {{ unreadCount > 0 ? t('announcements.newCount', { count: unreadCount }) : t('announcements.allCaughtUp') }}
                    </p>
                  </div>
                </div>
              </div>
              <div class="flex items-center gap-2">
                <button v-if="unreadCount > 0" type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="markAllAsRead">
                  <Icon name="check" size="sm" />
                  {{ t('announcements.markAllRead') }}
                </button>
                <button type="button" class="btn btn-ghost btn-icon h-9 w-9 p-0" :aria-label="t('common.close')" @click="closeModal">
                  <Icon name="x" size="sm" />
                </button>
              </div>
            </header>

            <div class="flex items-center justify-between gap-4 border-b px-5 py-3 sm:px-6" style="border-color: var(--ui-separator)">
              <div class="tabs p-0" role="tablist" :aria-label="t('announcements.filterLabel')">
                <button type="button" class="tab px-3 py-1.5 text-xs" :class="listFilter === 'all' ? 'tab-active' : ''" @click="listFilter = 'all'">
                  {{ t('announcements.filterAll') }}
                </button>
                <button type="button" class="tab px-3 py-1.5 text-xs" :class="listFilter === 'unread' ? 'tab-active' : ''" @click="listFilter = 'unread'">
                  {{ t('announcements.unread') }} · {{ unreadCount }}
                </button>
              </div>
              <span class="text-xs tabular-nums text-gray-500 dark:text-dark-400">{{ visibleAnnouncements.length }} / {{ announcements.length }}</span>
            </div>

            <div class="min-h-0 flex-1 overflow-y-auto">
              <div v-if="loading && announcements.length === 0" class="flex min-h-64 items-center justify-center">
                <span class="spinner text-primary-500" aria-hidden="true" />
                <span class="sr-only">{{ t('common.loading') }}</span>
              </div>

              <div v-else-if="visibleAnnouncements.length > 0" ref="announcementListRef" class="divide-y divide-gray-200 dark:divide-dark-700">
                <button
                  v-for="item in visibleAnnouncements"
                  :key="item.id"
                  type="button"
                  data-motion-item
                  class="group grid w-full grid-cols-[3px_minmax(0,1fr)_auto] text-left transition-colors hover:bg-gray-50 dark:hover:bg-dark-800/70"
                  @click="openDetail(item)"
                >
                  <span :class="item.read_at ? 'bg-transparent' : 'bg-primary-500'" aria-hidden="true" />
                  <span class="min-w-0 px-4 py-4 sm:px-5">
                    <span class="flex items-start gap-2">
                      <span class="min-w-0 flex-1 truncate text-sm font-semibold text-gray-900 dark:text-white">{{ item.title }}</span>
                      <span v-if="!item.read_at" class="badge badge-primary shrink-0">{{ t('announcements.unread') }}</span>
                    </span>
                    <span class="mt-1.5 block line-clamp-2 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ contentPreview(item.content) }}</span>
                    <time class="mt-2 block text-[11px] tabular-nums text-gray-400 dark:text-dark-500">{{ formatRelativeTime(item.created_at) }}</time>
                  </span>
                  <span class="flex items-center px-4 text-gray-400 transition-transform group-hover:translate-x-0.5 dark:text-dark-500">
                    <Icon name="chevronRight" size="sm" />
                  </span>
                </button>
              </div>

              <div v-else class="empty-state min-h-64 py-12">
                <Icon name="inbox" size="xl" class="text-gray-400 dark:text-dark-500" />
                <h3 class="empty-state-title mt-4">{{ listFilter === 'unread' ? t('announcements.emptyUnread') : t('announcements.empty') }}</h3>
                <p class="empty-state-description">{{ t('announcements.emptyDescription') }}</p>
              </div>
            </div>
          </section>
        </div>
      </Transition>
    </Teleport>

    <Teleport to="body">
      <Transition :css="false" @enter="enter" @leave="leave">
        <div
          v-if="detailModalOpen && selectedAnnouncement"
          class="fixed inset-0 z-[110] flex items-start justify-center overflow-y-auto p-3 pt-[4vh] backdrop-blur-sm sm:p-6 sm:pt-[7vh]"
          style="background: var(--ui-overlay)"
          @click.self="closeDetail"
        >
          <article class="material-panel flex max-h-[88vh] w-full max-w-3xl flex-col" role="dialog" aria-modal="true" :aria-labelledby="`announcement-title-${selectedAnnouncement.id}`">
            <header class="material-toolbar flex items-start justify-between gap-5 px-5 py-5 sm:px-7">
              <div class="min-w-0">
                <div class="mb-2 flex flex-wrap items-center gap-2">
                  <span class="badge badge-primary">{{ t('announcements.title') }}</span>
                  <span :class="selectedAnnouncement.read_at ? 'badge badge-gray' : 'badge badge-warning'">
                    {{ selectedAnnouncement.read_at ? t('announcements.read') : t('announcements.unread') }}
                  </span>
                </div>
                <h2 :id="`announcement-title-${selectedAnnouncement.id}`" class="text-xl font-semibold leading-tight tracking-tight text-gray-950 dark:text-white sm:text-2xl">
                  {{ selectedAnnouncement.title }}
                </h2>
                <time class="mt-2 block text-xs tabular-nums text-gray-500 dark:text-dark-400">{{ formatRelativeWithDateTime(selectedAnnouncement.created_at) }}</time>
              </div>
              <button type="button" class="btn btn-ghost btn-icon h-9 w-9 shrink-0 p-0" :aria-label="t('common.close')" @click="closeDetail">
                <Icon name="x" size="sm" />
              </button>
            </header>

            <div class="min-h-0 flex-1 overflow-y-auto bg-white/70 px-5 py-6 dark:bg-dark-900/35 sm:px-7 sm:py-7">
              <div class="markdown-body" v-html="renderMarkdown(selectedAnnouncement.content)" />
            </div>

            <footer class="flex flex-wrap items-center justify-between gap-3 border-t px-5 py-4 sm:px-7" style="border-color: var(--ui-separator)">
              <p class="text-xs text-gray-500 dark:text-dark-400">
                {{ selectedAnnouncement.read_at ? t('announcements.readStatus') : t('announcements.markReadHint') }}
              </p>
              <div class="flex items-center gap-2">
                <button type="button" class="btn btn-secondary" @click="closeDetail">{{ t('common.close') }}</button>
                <button v-if="!selectedAnnouncement.read_at" type="button" class="btn btn-primary" @click="markAsReadAndClose(selectedAnnouncement.id)">
                  <Icon name="check" size="sm" />
                  {{ t('announcements.markRead') }}
                </button>
              </div>
            </footer>
          </article>
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
import { useAnnouncementStore } from '@/stores/announcements'
import { useAppStore } from '@/stores/app'
import { formatRelativeTime, formatRelativeWithDateTime } from '@/utils/format'
import { renderMarkdown } from '@/utils/markdown'
import type { UserAnnouncement } from '@/types'

const { t } = useI18n()
const appStore = useAppStore()
const announcementStore = useAnnouncementStore()
const { announcements, loading } = storeToRefs(announcementStore)
const { enter, leave } = useAnimeDialogTransition()

const isModalOpen = ref(false)
const detailModalOpen = ref(false)
const selectedAnnouncement = ref<UserAnnouncement | null>(null)
const listFilter = ref<'all' | 'unread'>('all')
const announcementListRef = ref<HTMLElement | null>(null)
let listAnimation: ReturnType<typeof animateListEntrance> = null

const unreadCount = computed(() => announcementStore.unreadCount)
const visibleAnnouncements = computed(() => (
  listFilter.value === 'unread'
    ? announcements.value.filter((announcement) => !announcement.read_at)
    : announcements.value
))

function contentPreview(content: string): string {
  return content
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/[\[\]#>*_`~()-]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}

function openModal() {
  listFilter.value = 'all'
  isModalOpen.value = true
}

function closeModal() {
  isModalOpen.value = false
}

function openDetail(announcement: UserAnnouncement) {
  isModalOpen.value = false
  selectedAnnouncement.value = announcement
  detailModalOpen.value = true
}

function closeDetail() {
  detailModalOpen.value = false
  selectedAnnouncement.value = null
}

async function markAsReadAndClose(id: number) {
  try {
    await announcementStore.markAsRead(id)
    appStore.showSuccess(t('announcements.markedAsRead'))
    closeDetail()
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('common.unknownError'))
  }
}

async function markAllAsRead() {
  try {
    await announcementStore.markAllAsRead()
    appStore.showSuccess(t('announcements.allMarkedAsRead'))
  } catch (error: unknown) {
    appStore.showError(error instanceof Error ? error.message : t('common.unknownError'))
  }
}

function handleEscape(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  if (detailModalOpen.value) closeDetail()
  else if (isModalOpen.value) closeModal()
}

watch(
  [isModalOpen, listFilter, () => visibleAnnouncements.value.length],
  async ([open]) => {
    if (!open) return
    await nextTick()
    listAnimation?.revert()
    listAnimation = animateListEntrance(announcementListRef.value)
  },
  { flush: 'post' },
)

watch(
  [isModalOpen, detailModalOpen, () => announcementStore.currentPopup],
  ([listOpen, detailOpen, popup]) => {
    document.body.style.overflow = listOpen || detailOpen || popup ? 'hidden' : ''
  },
)

onMounted(() => document.addEventListener('keydown', handleEscape))
onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleEscape)
  document.body.style.overflow = ''
  listAnimation?.revert()
})
</script>
