import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
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
      },
    },
  },
})

describe('Select', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('aligns the teleported dropdown to the trigger and keeps it inside the viewport', async () => {
    const wrapper = mount(Select, {
      attachTo: document.body,
      props: {
        modelValue: 'user',
        options: [
          { value: 'user', label: 'User' },
          { value: 'admin', label: 'Administrator' },
        ],
        searchable: false,
      },
      global: { plugins: [i18n] },
    })

    Object.defineProperty(wrapper.element, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({
        left: -20,
        right: 160,
        top: 100,
        bottom: 142,
        width: 180,
        height: 42,
      }) as DOMRect,
    })

    await wrapper.get('.select-trigger').trigger('click')
    await nextTick()

    const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('8px')
    expect(dropdown?.style.top).toBe('146px')
    expect(dropdown?.style.width).toBe('180px')
    expect(dropdown?.querySelector('[aria-selected="true"]')?.textContent).toContain('User')

    const options = dropdown?.querySelectorAll<HTMLElement>('[role="option"]')
    options?.[1]?.click()
    await nextTick()

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['admin'])
    await new Promise((resolve) => window.setTimeout(resolve, 220))
    expect(document.body.querySelector('.select-dropdown-portal')).toBeNull()

    wrapper.unmount()
  })
})
