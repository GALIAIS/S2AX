import { beforeEach, describe, expect, it } from 'vitest'
import { useSavedViews } from '@/composables/useSavedViews'

describe('useSavedViews', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  it('persists a cloned view and restores it on the next instance', () => {
    const first = useSavedViews<Record<string, unknown>>('saved-view-test')
    const saved = first.save('Active accounts', { search: 'anthropic', page: 2 })

    expect(saved?.name).toBe('Active accounts')
    expect(first.views.value).toHaveLength(1)

    const second = useSavedViews<Record<string, unknown>>('saved-view-test')
    expect(second.views.value[0]).toMatchObject({
      name: 'Active accounts',
      state: { search: 'anthropic', page: 2 }
    })
  })

  it('replaces duplicate names and removes views', () => {
    const savedViews = useSavedViews<Record<string, unknown>>('saved-view-test')
    const first = savedViews.save('Default', { status: 'active' })
    const second = savedViews.save('Default', { status: 'disabled' })

    expect(first?.id).not.toBe(second?.id)
    expect(savedViews.views.value).toHaveLength(1)
    expect(savedViews.views.value[0].state).toEqual({ status: 'disabled' })

    savedViews.remove(second!.id)
    expect(savedViews.views.value).toHaveLength(0)
  })
})
