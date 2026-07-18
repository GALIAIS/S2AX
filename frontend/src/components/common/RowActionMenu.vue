<template>
  <div ref="triggerRef" class="inline-flex">
    <button
      type="button"
      class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 focus:outline-none focus:ring-2 focus:ring-primary-500 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-white"
      :aria-label="menuLabel"
      :aria-expanded="open"
      :aria-controls="open ? menuId : undefined"
      :title="menuLabel"
      @click.stop="toggle"
    >
      <Icon name="more" size="sm" />
    </button>

    <Teleport to="body">
      <template v-if="open && position">
        <div class="fixed inset-0 z-[9998]" aria-hidden="true" @click="close" />
        <div
          :id="menuId"
          ref="menuRef"
          class="fixed z-[9999] max-h-[min(360px,calc(100vh-24px))] min-w-44 max-w-[min(240px,calc(100vw-24px))] overflow-y-auto rounded-xl bg-white py-1 shadow-lg ring-1 ring-black/5 dark:bg-dark-800 dark:ring-white/10"
          role="menu"
          :aria-label="menuLabel"
          :style="{ top: `${position.top}px`, left: `${position.left}px` }"
          @click.stop
        >
          <button
            v-for="item in items"
            :key="item.key"
            type="button"
            role="menuitem"
            :data-testid="`row-action-${item.key}`"
            :disabled="item.disabled"
            :title="item.title"
            class="flex min-h-10 w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 focus:bg-gray-100 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:text-gray-200 dark:hover:bg-dark-700 dark:focus:bg-dark-700"
            :class="{
              'text-amber-700 dark:text-amber-300': item.tone === 'warning',
              'text-red-600 dark:text-red-400': item.tone === 'danger'
            }"
            @click="select(item.key)"
          >
            <Icon v-if="item.icon" :name="item.icon" size="sm" />
            <span>{{ item.label }}</span>
          </button>
        </div>
      </template>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

export interface RowActionMenuItem {
  key: string
  label: string
  icon?:
    | 'dollar'
    | 'bolt'
    | 'trash'
    | 'checkCircle'
    | 'shield'
    | 'edit'
    | 'play'
    | 'eye'
    | 'copy'
    | 'refresh'
    | 'link'
    | 'clock'
  tone?: 'default' | 'warning' | 'danger'
  disabled?: boolean
  title?: string
}

const props = withDefaults(defineProps<{
  items: RowActionMenuItem[]
  ariaLabel?: string
}>(), {
  ariaLabel: '',
})

const emit = defineEmits<{
  (event: 'select', key: string): void
}>()

const { t } = useI18n()
const open = ref(false)
const triggerRef = ref<HTMLElement | null>(null)
const menuRef = ref<HTMLElement | null>(null)
const position = ref<{ top: number; left: number } | null>(null)
const menuId = `row-action-menu-${Math.random().toString(36).slice(2, 9)}`

const menuLabel = computed(() => props.ariaLabel || t('common.more'))

const updatePosition = () => {
  const trigger = triggerRef.value
  if (!trigger) return
  const rect = trigger.getBoundingClientRect()
  const menuWidth = Math.min(240, Math.max(180, window.innerWidth - 24))
  const estimatedHeight = Math.min(360, Math.max(44, props.items.length * 40 + 8))
  const spaceBelow = window.innerHeight - rect.bottom - 12
  const top = spaceBelow >= estimatedHeight || rect.top < estimatedHeight
    ? rect.bottom + 4
    : Math.max(12, rect.top - estimatedHeight - 4)
  const left = Math.min(Math.max(12, rect.right - menuWidth), Math.max(12, window.innerWidth - menuWidth - 12))
  position.value = { top, left }
}

const toggle = async () => {
  open.value = !open.value
  if (!open.value) {
    position.value = null
    return
  }
  await nextTick()
  updatePosition()
}

const close = () => {
  open.value = false
  position.value = null
}

const select = (key: string) => {
  const item = props.items.find((candidate) => candidate.key === key)
  if (item?.disabled) return
  emit('select', key)
  close()
}

const handleKeydown = (event: KeyboardEvent) => {
  if (!open.value) return
  if (event.key === 'Escape') {
    event.preventDefault()
    close()
    triggerRef.value?.querySelector('button')?.focus()
  }
}

const handleViewportChange = () => {
  if (open.value) updatePosition()
}

onMounted(() => {
  document.addEventListener('keydown', handleKeydown)
  window.addEventListener('resize', handleViewportChange)
  window.addEventListener('scroll', handleViewportChange, true)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleKeydown)
  window.removeEventListener('resize', handleViewportChange)
  window.removeEventListener('scroll', handleViewportChange, true)
})
</script>
