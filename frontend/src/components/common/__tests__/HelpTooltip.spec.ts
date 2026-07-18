import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'

function getTooltipElement(): HTMLDivElement {
  const tooltip = document.body.querySelector('[role="tooltip"]')
  if (!(tooltip instanceof HTMLDivElement)) {
    throw new Error('tooltip element not found')
  }
  return tooltip
}

describe('HelpTooltip', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('keeps the existing hover interaction by default', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'hover details',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('mouseenter')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    await trigger.trigger('mouseleave')
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    wrapper.unmount()
  })

  it('supports click-to-toggle details and closes on outside click', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'click details',
        trigger: 'click',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('click')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')
    expect(tooltip.textContent).toContain('click details')

    const closeButton = tooltip.querySelector('button[aria-label="Close"]')
    if (!(closeButton instanceof HTMLButtonElement)) {
      throw new Error('close button not found')
    }
    closeButton.click()
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('click')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    wrapper.unmount()
  })

  it('positions a fixed tooltip below a trigger near the viewport top after scrolling', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: { content: 'position details' },
    })
    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()
    const originalScrollY = Object.getOwnPropertyDescriptor(window, 'scrollY')
    Object.defineProperty(window, 'scrollY', { configurable: true, value: 240 })
    Object.defineProperty(trigger.element, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ left: 300, right: 316, top: 40, bottom: 56, width: 16, height: 16 }) as DOMRect,
    })
    Object.defineProperty(tooltip, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ width: 256, height: 64 }) as DOMRect,
    })

    await trigger.trigger('mouseenter')
    await nextTick()

    expect(tooltip.style.top).toBe('64px')
    expect(tooltip.style.left).toBe('308px')
    expect(tooltip.dataset.placement).toBe('bottom')
    expect(tooltip.classList.contains('-translate-y-full')).toBe(false)
    expect(tooltip.querySelector('.-top-1')).not.toBeNull()

    if (originalScrollY) {
      Object.defineProperty(window, 'scrollY', originalScrollY)
    } else {
      delete (window as Window & { scrollY?: number }).scrollY
    }
    wrapper.unmount()
  })

  it('keeps a tooltip above the trigger when enough space is available', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: { content: 'position details' },
    })
    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()
    Object.defineProperty(trigger.element, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ left: 300, right: 316, top: 300, bottom: 316, width: 16, height: 16 }) as DOMRect,
    })
    Object.defineProperty(tooltip, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ width: 256, height: 64 }) as DOMRect,
    })

    await trigger.trigger('mouseenter')
    await nextTick()

    expect(tooltip.style.top).toBe('292px')
    expect(tooltip.style.left).toBe('308px')
    expect(tooltip.dataset.placement).toBe('top')
    expect(tooltip.classList.contains('-translate-y-full')).toBe(true)
    expect(tooltip.querySelector('.-bottom-1')).not.toBeNull()

    wrapper.unmount()
  })
})
