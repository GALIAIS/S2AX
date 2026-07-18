import { onMounted, onUnmounted, ref, watch, type WatchSource } from 'vue'

export interface UrlQueryBinding<T> {
  key: string
  get: () => T
  set: (value: T) => void
  parse: (value: string) => T | undefined
  serialize?: (value: T) => string | null | undefined
  omit?: (value: T) => boolean
}

interface UseUrlQueryBindingsOptions {
  debounceMs?: number
  enabled?: boolean
}

/**
 * Keeps non-sensitive list state in the current URL without remounting the page.
 * It deliberately uses history.replaceState so typing in a filter does not add
 * a browser-history entry and does not trigger a full router navigation.
 */
export function useUrlQueryBindings<T extends readonly UrlQueryBinding<any>[]>(
  bindings: T,
  options: UseUrlQueryBindingsOptions = {}
) {
  const hydrated = ref(false)
  const enabled = options.enabled ?? true
  const debounceMs = Math.max(0, options.debounceMs ?? 80)
  let writeTimer: ReturnType<typeof setTimeout> | null = null
  let hydrating = false

  const read = () => {
    if (!enabled || typeof window === 'undefined') return
    const query = new URLSearchParams(window.location.search)
    hydrating = true
    try {
      for (const binding of bindings) {
        const raw = query.get(binding.key)
        if (raw === null) continue
        const parsed = binding.parse(raw)
        if (parsed !== undefined) binding.set(parsed)
      }
    } finally {
      hydrating = false
      hydrated.value = true
    }
  }

  const write = () => {
    if (!enabled || typeof window === 'undefined' || !hydrated.value || hydrating) return
    const query = new URLSearchParams(window.location.search)
    for (const binding of bindings) {
      const value = binding.get()
      if (binding.omit?.(value)) {
        query.delete(binding.key)
        continue
      }
      const serialized = binding.serialize ? binding.serialize(value) : String(value ?? '')
      if (!serialized) query.delete(binding.key)
      else query.set(binding.key, serialized)
    }

    const search = query.toString()
    const nextUrl = `${window.location.pathname}${search ? `?${search}` : ''}${window.location.hash}`
    const currentUrl = `${window.location.pathname}${window.location.search}${window.location.hash}`
    if (nextUrl !== currentUrl) {
      window.history.replaceState(window.history.state, '', nextUrl)
    }
  }

  const flush = () => {
    if (writeTimer) {
      clearTimeout(writeTimer)
      writeTimer = null
    }
    write()
  }

  const scheduleWrite = () => {
    if (!enabled || !hydrated.value || hydrating) return
    if (writeTimer) clearTimeout(writeTimer)
    writeTimer = setTimeout(() => {
      writeTimer = null
      write()
    }, debounceMs)
  }

  const source: WatchSource<unknown[]> = () => bindings.map((binding) => binding.get())
  watch(source, scheduleWrite, { deep: true })

  const handlePopState = () => read()

  onMounted(() => {
    read()
    if (enabled) window.addEventListener('popstate', handlePopState)
  })

  onUnmounted(() => {
    if (writeTimer) clearTimeout(writeTimer)
    if (enabled && typeof window !== 'undefined') window.removeEventListener('popstate', handlePopState)
  })

  return {
    hydrated,
    read,
    write: flush
  }
}

export const parseStringQuery = (value: string) => value

export const parseNumberQuery = (value: string): number | undefined => {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : undefined
}

export const parseBooleanQuery = (value: string): boolean | undefined => {
  if (value === '1' || value === 'true') return true
  if (value === '0' || value === 'false') return false
  return undefined
}
