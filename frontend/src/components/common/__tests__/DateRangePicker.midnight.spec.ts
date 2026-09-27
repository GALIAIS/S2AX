import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import DateRangePicker from '../DateRangePicker.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: ref('en') }) }))
enableAutoUnmount(afterEach)
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date(2026, 8, 30, 23, 59)) })
afterEach(() => vi.useRealTimers())
async function reopenNextDay() {
  const w = mount(DateRangePicker, { props: { startDate: '2026-09-30', endDate: '2026-09-30' }, global: { stubs: { Icon: true } } })
  await w.get('.date-picker-trigger').trigger('click')
  await nextTick()
  expect(dateInputs()[1]?.getAttribute('max')).toBe('2026-10-01')
  await w.get('.date-picker-trigger').trigger('click')
  vi.setSystemTime(new Date(2026, 9, 1, 0, 1))
  await w.get('.date-picker-trigger').trigger('click')
  return w
}

function dateInputs() {
  const dropdowns = document.body.querySelectorAll<HTMLElement>('.date-picker-dropdown')
  const dropdown = dropdowns[dropdowns.length - 1]
  return [...(dropdown?.querySelectorAll<HTMLInputElement>('input[type="date"]') ?? [])]
}

function clickPreset(label: string) {
  const dropdowns = document.body.querySelectorAll<HTMLElement>('.date-picker-dropdown')
  const dropdown = dropdowns[dropdowns.length - 1]
  const preset = [...(dropdown?.querySelectorAll<HTMLButtonElement>('.date-picker-preset') ?? [])]
    .find(button => button.textContent === label)
  if (!preset) throw new Error(`date range preset not found: ${label}`)
  preset.click()
}

function clickApply() {
  const dropdowns = document.body.querySelectorAll<HTMLElement>('.date-picker-dropdown')
  const dropdown = dropdowns[dropdowns.length - 1]
  const button = dropdown?.querySelector<HTMLButtonElement>('.date-picker-apply')
  if (!button) throw new Error('date range apply button not found')
  button.click()
}
describe('date presets after midnight', () => {
  it.each([
    ['dates.today', '2026-10-01'],
    ['dates.last7Days', '2026-09-25'],
    ['dates.thisMonth', '2026-10-01'],
  ])('refreshes %s when the page stays mounted overnight', async (label, startDate) => {
    const w = await reopenNextDay()
    clickPreset(label)
    await nextTick()
    clickApply()
    await nextTick()
    expect(w.emitted('change')?.[0]?.[0]).toMatchObject({ startDate, endDate: '2026-10-01' })
  })
  it('updates the maximum selectable date when reopened', async () => {
    await reopenNextDay()
    await nextTick()
    expect(dateInputs()[1]?.getAttribute('max')).toBe('2026-10-02')
  })
})
