import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import Select from '@/components/common/Select.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  fallbackWarn: false,
  missingWarn: false,
  messages: {
    en: {
      common: {
        selectOption: 'Select an option',
        searchPlaceholder: 'Search',
        noOptionsFound: 'No options',
        search: 'Search',
        loading: 'Loading...',
      },
    },
  },
})

const originalInnerWidth = window.innerWidth
let wrapper: VueWrapper | null = null

function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width })
}

function mockTriggerRect(left: number, width: number) {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: left,
    y: 20,
    top: 20,
    right: left + width,
    bottom: 60,
    left,
    width,
    height: 40,
    toJSON: () => ({}),
  })
}

function mountSelect(props: Record<string, unknown>) {
  wrapper = mount(Select, {
    attachTo: document.body,
    props,
    global: { plugins: [i18n] },
  })
  return wrapper
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
  setViewportWidth(originalInnerWidth)
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('Select', () => {
  it('aligns the teleported dropdown and selects an option', async () => {
    const select = mountSelect({
      modelValue: 'user',
      options: [
        { value: 'user', label: 'User' },
        { value: 'admin', label: 'Administrator' },
      ],
      searchable: false,
    })
    mockTriggerRect(-20, 180)

    await select.get('.select-trigger').trigger('click')
    await nextTick()

    const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown?.style.left).toBe('8px')
    expect(dropdown?.style.top).toBe('64px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.querySelector('[aria-selected="true"]')?.textContent).toContain('User')

    dropdown?.querySelectorAll<HTMLElement>('[role="option"]')[1]?.click()
    await nextTick()
    expect(select.emitted('update:modelValue')?.[0]).toEqual(['admin'])
    expect(select.emitted('change')).toHaveLength(1)
  })

  it('keeps compact filters free of search and exposes remote loading', async () => {
    const select = mountSelect({
      modelValue: '0',
      options: Array.from({ length: 7 }, (_, index) => ({ value: String(index), label: `Option ${index + 1}` })),
    })
    await select.get('.select-trigger').trigger('click')
    await nextTick()
    expect(document.body.querySelector('.select-search')).toBeNull()

    select.unmount()
    wrapper = mountSelect({ modelValue: null, options: [], searchable: true, remoteSearch: true, loading: true })
    await wrapper.get('.select-trigger').trigger('click')
    await nextTick()

    const search = document.body.querySelector<HTMLInputElement>('.select-search-input')
    search!.value = 'alice'
    search!.dispatchEvent(new Event('input'))
    await nextTick()
    expect(wrapper.emitted('search')?.at(-1)).toEqual(['alice'])
    expect(document.body.querySelector('.select-empty .animate-spin')).not.toBeNull()
  })

  it('preserves a 200px minimum width when space is available', async () => {
    setViewportWidth(1024)
    mockTriggerRect(20, 80)
    const select = mountSelect({ modelValue: null, options: [{ value: 'example', label: 'example' }] })
    await select.get('.select-trigger').trigger('click')
    await nextTick()

    const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown?.style.left).toBe('20px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('996px')
  })

  it('shrinks and clamps dropdowns at viewport boundaries', async () => {
    setViewportWidth(320)
    mockTriggerRect(220, 80)
    const select = mountSelect({ modelValue: null, options: [{ value: 'example', label: 'example' }] })
    await select.get('.select-trigger').trigger('click')
    await nextTick()
    let dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown?.style.left).toBe('220px')
    expect(dropdown?.style.minWidth).toBe('92px')
    expect(dropdown?.style.maxWidth).toBe('92px')

    select.unmount()
    mockTriggerRect(-20, 80)
    wrapper = mountSelect({ modelValue: null, options: [{ value: 'example', label: 'example' }] })
    await wrapper.get('.select-trigger').trigger('click')
    await nextTick()
    dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown?.style.left).toBe('8px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('304px')
  })
})

describe('Select remote search', () => {
  const mountRemoteSelect = (props: Record<string, unknown> = {}) => {
    const mounted = mount(Select, {
      attachTo: document.body,
      props: {
        modelValue: null,
        remote: true,
        options: [
          { value: 'alpha', label: 'Alpha account' },
          { value: 'beta', label: 'Beta account' },
        ],
        ...props,
      },
      global: { plugins: [i18n] },
    })
    wrapper = mounted
    return mounted
  }

  const openDropdown = async () => {
    const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown).not.toBeNull()
    return dropdown as HTMLElement
  }

  const typeSearchQuery = async (query: string) => {
    const dropdown = await openDropdown()
    const input = dropdown.querySelector<HTMLInputElement>('.select-search-input')
    expect(input).not.toBeNull()
    input!.value = query
    input!.dispatchEvent(new Event('input'))
    await nextTick()
  }

  it('emits debounced search events and skips local filtering in remote mode', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('zzz')

    // 防抖窗口内不触发。
    expect(wrapper.emitted('search')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toEqual([['zzz']])
    // 远程模式不做本地过滤：无命中的 query 下选项仍完整展示（由父组件更新 options）。
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toContain('Alpha account')
    expect(labels).toContain('Beta account')
  })

  it('does not emit search when the dropdown closes and the query resets', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('hidden')

    // 关闭下拉：排队中的防抖定时器应被取消，也不应因 query 重置而尾随 emit。
    await wrapper.get('button').trigger('click')
    await nextTick()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toBeUndefined()
  })

  it('shows the loading text instead of empty text while loading with no options', async () => {
    const wrapper = mountRemoteSelect({ options: [], loading: true })
    await wrapper.get('button').trigger('click')
    await nextTick()

    const dropdown = await openDropdown()
    expect(dropdown.querySelector('.select-empty')?.textContent).toContain('common.loading')
  })

  it('keeps local filtering and emits nothing when remote is not set', async () => {
    vi.useFakeTimers()
    const select = mountSelect({
      modelValue: null,
      searchable: true,
      options: [
        { value: 'alpha', label: 'Alpha account' },
        { value: 'beta', label: 'Beta account' },
      ],
    })
    await select.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('alpha')
    await vi.advanceTimersByTimeAsync(300)

    expect(select.emitted('search')).toBeUndefined()
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toEqual(['Alpha account'])
  })
})
