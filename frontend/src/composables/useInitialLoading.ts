import { computed, ref, watch, type Ref } from 'vue'

/** Keep existing content mounted while a completed view is refreshed. */
export function useInitialLoading(loading: Readonly<Ref<boolean>>) {
  const hasLoadedOnce = ref(false)
  let hasStartedLoading = loading.value

  watch(
    loading,
    (isLoading) => {
      if (isLoading) {
        hasStartedLoading = true
      } else if (hasStartedLoading) {
        hasLoadedOnce.value = true
      }
    },
    { flush: 'sync' },
  )

  return {
    hasLoadedOnce,
    isInitialLoading: computed(() => loading.value && !hasLoadedOnce.value),
    isRefreshing: computed(() => loading.value && hasLoadedOnce.value),
  }
}
