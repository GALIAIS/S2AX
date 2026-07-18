import { shallowRef } from 'vue'

export interface SavedView<T extends Record<string, unknown> = Record<string, unknown>> {
  id: string
  name: string
  state: T
  createdAt: string
}

const cloneState = <T extends Record<string, unknown>>(state: T): T => {
  try {
    return JSON.parse(JSON.stringify(state)) as T
  } catch {
    return {} as T
  }
}

const createId = () => {
  const random = typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : Math.random().toString(36).slice(2)
  return `view-${Date.now()}-${random}`
}

export function useSavedViews<T extends Record<string, unknown>>(storageKey: string, maxViews = 12) {
  const views = shallowRef<Array<SavedView<T>>>([])

  const persist = () => {
    if (typeof window === 'undefined') return
    try {
      window.localStorage.setItem(storageKey, JSON.stringify(views.value))
    } catch {
      // Local storage may be unavailable in private browsing or restricted webviews.
    }
  }

  const load = () => {
    if (typeof window === 'undefined') return
    try {
      const raw = window.localStorage.getItem(storageKey)
      const parsed = raw ? JSON.parse(raw) : []
      if (!Array.isArray(parsed)) return
      views.value = parsed
        .filter((view): view is SavedView<T> => {
          return Boolean(
            view &&
            typeof view === 'object' &&
            typeof view.id === 'string' &&
            typeof view.name === 'string' &&
            view.state &&
            typeof view.state === 'object'
          )
        })
        .slice(0, maxViews)
    } catch {
      views.value = []
    }
  }

  const save = (name: string, state: T) => {
    const normalizedName = name.trim().slice(0, 64)
    if (!normalizedName) return null
    const next: SavedView<T> = {
      id: createId(),
      name: normalizedName,
      state: cloneState(state),
      createdAt: new Date().toISOString()
    }
    views.value = [next, ...views.value.filter((view) => view.name !== normalizedName)].slice(0, maxViews)
    persist()
    return next
  }

  const remove = (id: string) => {
    views.value = views.value.filter((view) => view.id !== id)
    persist()
  }

  const clear = () => {
    views.value = []
    persist()
  }

  load()

  return { views, load, save, remove, clear }
}
