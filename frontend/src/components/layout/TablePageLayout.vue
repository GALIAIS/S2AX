<template>
  <div class="table-page-layout" :class="{ 'mobile-mode': isMobile }">
    <!-- 固定区域：操作按钮 -->
    <div v-if="$slots.actions" class="layout-section-fixed">
      <slot name="actions" />
    </div>

    <!-- 固定区域：搜索和过滤器 -->
    <div v-if="$slots.filters" class="layout-section-fixed">
      <slot name="filters" />
    </div>

    <!-- 滚动区域：表格 -->
    <div class="layout-section-scrollable">
      <div class="card table-scroll-container">
        <slot name="table" />
      </div>
    </div>

    <!-- 固定区域：分页器 -->
    <div v-if="$slots.pagination" class="layout-section-fixed">
      <slot name="pagination" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'

const isMobile = ref(false)
const compactViewportQuery = '(max-width: 767px)'
let compactViewport: MediaQueryList | null = null

// Keep this in lockstep with DataTable's mobile-card breakpoint. Previously
// the shell switched at 1024px while its table stayed in desktop mode until
// 768px, which left tablet-width pages with a mismatched fixed-height layout.
const checkMobile = () => {
  isMobile.value = compactViewport?.matches ?? window.matchMedia(compactViewportQuery).matches
}

onMounted(() => {
  compactViewport = window.matchMedia(compactViewportQuery)
  checkMobile()
  compactViewport.addEventListener('change', checkMobile)
})

onUnmounted(() => {
  compactViewport?.removeEventListener('change', checkMobile)
  compactViewport = null
})
</script>

<style scoped>
/* 桌面端：Flexbox 布局 */
.table-page-layout {
  @apply flex min-w-0 flex-col gap-4 sm:gap-6;
  height: calc(100dvh - 64px - 2rem); /* header + base main padding */
}

@media (min-width: 768px) and (max-width: 1023px) {
  .table-page-layout {
    height: calc(100dvh - 64px - 3rem); /* md main padding */
  }
}

@media (min-width: 1024px) {
  .table-page-layout {
    height: calc(100dvh - 64px - 4rem); /* lg main padding */
  }
}

.layout-section-fixed {
  @apply min-w-0 flex-shrink-0;
}

.layout-section-scrollable {
  @apply flex flex-1 min-h-0 min-w-0 flex-col;
}

/* 表格滚动容器 - 增强版表体滚动方案 */
.table-scroll-container {
  @apply flex h-full min-w-0 flex-col overflow-hidden border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800;
}

.table-scroll-container :deep(.table-wrapper) {
  @apply flex-1 overflow-x-auto overflow-y-auto;
  /* 确保横向滚动条显示在最底部 */
  scrollbar-gutter: stable;
}

.table-scroll-container :deep(table) {
  @apply w-full;
  min-width: max-content; /* 关键：确保表格宽度根据内容撑开，从而触发横向滚动 */
  display: table; /* 使用标准 table 布局以支持 sticky 列 */
}

.table-scroll-container :deep(thead) {
  @apply bg-gray-50 dark:bg-dark-800;
}

.table-scroll-container :deep(tbody) {
  /* 保持默认 table-row-group 显示，不使用 block */
}

.table-scroll-container :deep(th) {
  @apply px-5 py-4 text-left text-sm font-medium text-gray-600 dark:text-dark-300 border-b border-gray-200 dark:border-dark-700;
}

.table-scroll-container :deep(td) {
  @apply px-5 py-4 text-sm text-gray-700 dark:text-gray-300 border-b border-gray-100 dark:border-dark-800;
}

/* 移动端：恢复正常滚动 */
.table-page-layout.mobile-mode .table-scroll-container {
  @apply h-auto overflow-x-auto overflow-y-visible border-none bg-transparent shadow-none;
}

.table-page-layout.mobile-mode .layout-section-scrollable {
  @apply flex-none min-h-0;
}

.table-page-layout.mobile-mode {
  height: auto;
  min-height: 0;
}

.table-page-layout.mobile-mode .table-scroll-container :deep(.table-wrapper) {
  @apply min-w-0 flex-none overflow-x-auto overflow-y-visible;
}

.table-page-layout.mobile-mode .table-scroll-container :deep(table) {
  @apply min-w-full flex-none;
  display: table;
}
</style>
