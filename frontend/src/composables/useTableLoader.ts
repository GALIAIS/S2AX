import { ref, reactive, onUnmounted, toRaw } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import type { BasePaginationResponse, FetchOptions } from '@/types'
import { getPersistedPageSize, setPersistedPageSize } from './usePersistedPageSize'
import { useInitialLoading } from './useInitialLoading'

interface PaginationState {
  page: number
  page_size: number
  total: number
  pages: number
}

interface TableLoaderOptions<T, P> {
  fetchFn: (page: number, pageSize: number, params: P, options?: FetchOptions) => Promise<BasePaginationResponse<T>>
  initialParams?: P
  pageSize?: number
  debounceMs?: number
}

/**
 * 通用表格数据加载 Composable
 * 统一处理分页、筛选、搜索防抖和请求取消
 */
export function useTableLoader<T, P extends Record<string, any>>(options: TableLoaderOptions<T, P>) {
  const { fetchFn, initialParams, pageSize, debounceMs = 300 } = options

  const items = ref<T[]>([])
  // Start in the initial-loading state so the first paint is stable instead of
  // rendering an empty table before the mounted request begins.
  const loading = ref(true)
  const { isInitialLoading, isRefreshing } = useInitialLoading(loading)
  const error = ref<unknown>(null)
  const params = reactive<P>({ ...(initialParams || {}) } as P)
  const pagination = reactive<PaginationState>({
    page: 1,
    page_size: pageSize ?? getPersistedPageSize(),
    total: 0,
    pages: 0
  })

  let abortController: AbortController | null = null
  let requestSequence = 0

  const isAbortError = (value: unknown) => {
    if (!value || typeof value !== 'object') return false
    const candidate = value as { name?: unknown; code?: unknown }
    return candidate.name === 'AbortError' || candidate.code === 'ERR_CANCELED' || candidate.name === 'CanceledError'
  }

  const load = async () => {
    if (abortController) {
      abortController.abort()
    }
    const currentController = new AbortController()
    abortController = currentController
    const currentSequence = ++requestSequence
    loading.value = true
    error.value = null

    try {
      const response = await fetchFn(
        pagination.page,
        pagination.page_size,
        toRaw(params) as P,
        { signal: currentController.signal }
      )

      if (currentSequence !== requestSequence) return
      items.value = response.items || []
      pagination.total = response.total || 0
      pagination.pages = response.pages || Math.ceil(pagination.total / pagination.page_size)
    } catch (caughtError) {
      if (!isAbortError(caughtError) && currentSequence === requestSequence) {
        error.value = caughtError
        console.error('Table load error:', caughtError)
        throw caughtError
      }
    } finally {
      if (abortController === currentController) {
        loading.value = false
      }
    }
  }

  const reload = () => {
    pagination.page = 1
    return load()
  }

  const debouncedReload = useDebounceFn(() => {
    void reload().catch(() => undefined)
  }, debounceMs)

  const handlePageChange = (page: number) => {
    // 确保页码在有效范围内
    const validPage = Math.max(1, Math.min(page, pagination.pages || 1))
    pagination.page = validPage
    void load().catch(() => undefined)
  }

  const handlePageSizeChange = (size: number) => {
    pagination.page_size = size
    pagination.page = 1
    setPersistedPageSize(size)
    void load().catch(() => undefined)
  }

  onUnmounted(() => {
    abortController?.abort()
  })

  return {
    items,
    loading,
    isInitialLoading,
    isRefreshing,
    params,
    pagination,
    error,
    load,
    reload,
    debouncedReload,
    handlePageChange,
    handlePageSizeChange
  }
}
