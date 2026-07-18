import { describe, expect, it } from 'vitest'
import { ref } from 'vue'
import { useInitialLoading } from '@/composables/useInitialLoading'

describe('useInitialLoading', () => {
  it('keeps a completed view mounted during later refreshes', () => {
    const loading = ref(false)
    const state = useInitialLoading(loading)

    loading.value = true
    expect(state.isInitialLoading.value).toBe(true)
    expect(state.isRefreshing.value).toBe(false)

    loading.value = false
    expect(state.hasLoadedOnce.value).toBe(true)

    loading.value = true
    expect(state.isInitialLoading.value).toBe(false)
    expect(state.isRefreshing.value).toBe(true)
  })
})
