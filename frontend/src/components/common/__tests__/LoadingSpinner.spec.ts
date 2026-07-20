import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'

import LoadingSpinner from '../LoadingSpinner.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { common: { loading: 'Loading' } } },
})

describe('LoadingSpinner', () => {
  it('renders a circular SVG ring that is immune to the global hard-edge reset', () => {
    const wrapper = mount(LoadingSpinner, {
      props: { size: 'sm', color: 'primary' },
      global: { plugins: [i18n] },
    })

    expect(wrapper.element.tagName).toBe('svg')
    expect(wrapper.attributes('role')).toBe('status')
    expect(wrapper.attributes('aria-label')).toBeTruthy()
    expect(wrapper.classes()).toContain('animate-spin')
    expect(wrapper.find('circle').exists()).toBe(true)
    expect(wrapper.find('path').exists()).toBe(true)
  })
})
