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
