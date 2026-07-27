<template>
  <Teleport to="body">
    <Transition :css="false" @enter="enter" @leave="leave">
      <div
        v-if="displayedAnnouncement"
        class="fixed inset-0 z-[120] flex items-start justify-center overflow-y-auto p-3 pt-[6vh] backdrop-blur-sm sm:p-6 sm:pt-[9vh]"
        style="background: var(--ui-overlay)"
      >
        <article class="material-panel flex max-h-[82vh] w-full max-w-2xl flex-col" role="alertdialog" aria-modal="true" :aria-label="displayedAnnouncement.title">
          <header class="material-toolbar flex items-start gap-4 px-5 py-5 sm:px-7">
            <span class="flex h-10 w-10 shrink-0 items-center justify-center bg-amber-500 text-gray-950">
              <Icon name="bell" size="md" />
            </span>
            <div class="min-w-0 flex-1">
              <div class="mb-2 flex flex-wrap items-center gap-2">
                <span class="badge badge-warning">{{ t('announcements.priorityNotice') }}</span>
                <span class="badge badge-gray">{{ t('announcements.unread') }}</span>
              </div>
              <h2 class="text-xl font-semibold leading-tight tracking-tight text-gray-950 dark:text-white sm:text-2xl">
                {{ displayedAnnouncement.title }}
              </h2>
              <time class="mt-2 block text-xs tabular-nums text-gray-500 dark:text-dark-400">
                {{ formatRelativeWithDateTime(displayedAnnouncement.created_at) }}
              </time>
            </div>
          </header>

          <div class="min-h-0 flex-1 overflow-y-auto border-l-[3px] border-amber-500 bg-white/70 px-5 py-6 dark:bg-dark-900/35 sm:px-7 sm:py-7">
            <div class="markdown-body" v-html="renderMarkdown(displayedAnnouncement.content)" />
          </div>

          <footer class="flex flex-wrap items-center justify-between gap-3 border-t px-5 py-4 sm:px-7" style="border-color: var(--ui-separator)">
            <p v-if="!preview" class="text-xs text-gray-500 dark:text-dark-400">{{ t('announcements.popupReadHint') }}</p>
            <button type="button" data-testid="announcement-popup-dismiss" class="btn btn-primary" :disabled="dismissing" @click="handleDismiss">
              <Icon :name="preview ? 'x' : 'check'" size="sm" />
              {{ preview ? t('common.close') : t('announcements.markRead') }}
            </button>
          </footer>
        </article>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useAnimeDialogTransition } from '@/composables/useInterfaceMotion'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeWithDateTime } from '@/utils/format'
import { renderMarkdown } from '@/utils/markdown'
import type { Announcement, UserAnnouncement } from '@/types'
import '@/styles/announcement-markdown.css'

type PreviewAnnouncement = Pick<Announcement | UserAnnouncement, 'title' | 'content' | 'created_at'>

const props = withDefaults(defineProps<{
  announcement?: PreviewAnnouncement | null
  preview?: boolean
}>(), {
  announcement: null,
  preview: false,
})

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const announcementStore = useAnnouncementStore()
const { enter, leave } = useAnimeDialogTransition()
const dismissing = ref(false)
const displayedAnnouncement = computed(() => (
  props.preview ? props.announcement : announcementStore.currentPopup
))

async function handleDismiss() {
  if (props.preview) {
    emit('close')
    return
  }
  if (dismissing.value) return
  dismissing.value = true
  try {
    await announcementStore.dismissPopup()
  } finally {
    dismissing.value = false
  }
}

watch(
  displayedAnnouncement,
  (popup) => {
    if (popup) document.body.style.overflow = 'hidden'
    else if (props.preview) document.body.style.overflow = ''
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  if (props.preview) document.body.style.overflow = ''
})
</script>
