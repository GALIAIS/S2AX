<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, useTemplateRef, nextTick } from 'vue'

const props = withDefaults(defineProps<{
  content?: string
  trigger?: 'hover' | 'click'
  widthClass?: string
}>(), {
  trigger: 'hover',
  widthClass: 'w-64',
})

const show = ref(false)
const triggerRef = useTemplateRef<HTMLElement>('trigger')
const tooltipRef = useTemplateRef<HTMLElement>('tooltip')
const tooltipStyle = ref({ top: '0px', left: '0px' })
const placement = ref<'top' | 'bottom'>('top')

const TOOLTIP_GAP = 8
const VIEWPORT_MARGIN = 12

function openTooltip() {
  show.value = true
  nextTick(updatePosition)
}

function closeTooltip() {
  show.value = false
}

function onEnter() {
  if (props.trigger !== 'hover') return
  openTooltip()
}

function onLeave() {
  if (props.trigger !== 'hover') return
  closeTooltip()
}

function onClick(event: MouseEvent) {
  if (props.trigger !== 'click') return
  event.stopPropagation()
  if (show.value) {
    closeTooltip()
    return
  }
  openTooltip()
}

function onDocumentClick(event: MouseEvent) {
  if (props.trigger !== 'click' || !show.value) return
  const target = event.target as Node | null
  if (!target) return
  if (triggerRef.value?.contains(target) || tooltipRef.value?.contains(target)) return
  closeTooltip()
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (props.trigger !== 'click') return
  if (event.key === 'Escape') {
    closeTooltip()
  }
}

function onViewportChange() {
  if (!show.value) return
  updatePosition()
}

function updatePosition() {
  const el = triggerRef.value
  const tooltip = tooltipRef.value
  if (!el || !tooltip) return

  const rect = el.getBoundingClientRect()
  const tooltipRect = tooltip.getBoundingClientRect()
  const tooltipWidth = tooltipRect.width || tooltip.offsetWidth || 256
  const tooltipHeight = tooltipRect.height || tooltip.offsetHeight || 48
  const viewportWidth = document.documentElement.clientWidth || window.innerWidth
  const viewportHeight = document.documentElement.clientHeight || window.innerHeight
  const spaceAbove = rect.top - VIEWPORT_MARGIN
  const spaceBelow = viewportHeight - rect.bottom - VIEWPORT_MARGIN
  const placeAbove = spaceAbove >= tooltipHeight + TOOLTIP_GAP || spaceAbove >= spaceBelow
  const centeredLeft = rect.left + rect.width / 2
  const minLeft = VIEWPORT_MARGIN + tooltipWidth / 2
  const maxLeft = Math.max(minLeft, viewportWidth - VIEWPORT_MARGIN - tooltipWidth / 2)

  placement.value = placeAbove ? 'top' : 'bottom'
  tooltipStyle.value = {
    top: `${placeAbove ? rect.top - TOOLTIP_GAP : rect.bottom + TOOLTIP_GAP}px`,
    left: `${Math.min(Math.max(centeredLeft, minLeft), maxLeft)}px`,
  }
}

onMounted(() => {
  document.addEventListener('click', onDocumentClick, true)
  document.addEventListener('keydown', onDocumentKeydown)
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('scroll', onViewportChange, true)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocumentClick, true)
  document.removeEventListener('keydown', onDocumentKeydown)
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('scroll', onViewportChange, true)
})
</script>

<template>
  <div
    ref="trigger"
    class="group relative ml-1 inline-flex h-5 w-5 shrink-0 items-center justify-center align-middle leading-none"
    @mouseenter="onEnter"
    @mouseleave="onLeave"
    @click="onClick"
  >
    <!-- Trigger Icon -->
    <slot name="trigger">
      <svg
        class="block h-4 w-4 cursor-help text-gray-400 transition-colors hover:text-primary-600 dark:text-gray-500 dark:hover:text-primary-400"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
        />
      </svg>
    </slot>

    <!-- Teleport to body to escape modal overflow clipping -->
    <Teleport to="body">
      <div
        ref="tooltip"
        v-show="show"
        role="tooltip"
        :data-placement="placement"
        :class="[
          'fixed z-[99999] -translate-x-1/2 bg-gray-900 p-3 text-xs leading-relaxed text-white shadow-xl ring-1 ring-white/10 dark:bg-gray-800',
          placement === 'top' && '-translate-y-full',
          props.widthClass,
        ]"
        :style="tooltipStyle"
      >
        <button
          v-if="props.trigger === 'click'"
          type="button"
          class="absolute right-1.5 top-1.5 rounded p-1 text-gray-300 transition-colors hover:bg-white/10 hover:text-white"
          aria-label="Close"
          @click.stop="closeTooltip"
        >
          <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
        <slot>{{ content }}</slot>
        <div
          :class="[
            'absolute left-1/2 h-2 w-2 -translate-x-1/2 rotate-45 bg-gray-900 dark:bg-gray-800',
            placement === 'top' ? '-bottom-1' : '-top-1',
          ]"
        ></div>
      </div>
    </Teleport>
  </div>
</template>
