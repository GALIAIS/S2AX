import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { AdminUser } from '@/types'
import UserBalanceModal from '../UserBalanceModal.vue'

const api = vi.hoisted(() => ({
  listCurrencies: vi.fn(),
  userLedger: vi.fn(),
  adjust: vi.fn(),
  updateBalance: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: { updateBalance: api.updateBalance },
    virtualCurrencies: {
      list: api.listCurrencies,
      userLedger: api.userLedger,
      adjust: api.adjust
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: api.showError, showSuccess: api.showSuccess })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const user = {
  id: 42,
  email: 'player@example.com',
  balance: 10
} as AdminUser

const mountModal = () => mount(UserBalanceModal, {
  props: { show: false, user, operation: 'add' },
  global: {
    stubs: {
      BaseDialog: {
        props: ['show'],
        template: '<div v-if="show"><slot /><slot name="footer" /></div>'
      },
      Select: {
        props: ['modelValue', 'options'],
        emits: ['update:modelValue'],
        template: `
          <select :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
            <option v-for="option in options" :key="option.value" :value="option.value" :disabled="option.disabled">
              {{ option.label }}
            </option>
          </select>
        `
      }
    }
  }
})

describe('UserBalanceModal virtual currency adjustments', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.listCurrencies.mockResolvedValue([
      { id: 7, code: 'gold', name: 'Gold', symbol: 'G', scale: 2, status: 'active', metadata: {} }
    ])
    api.userLedger.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 1, pages: 0 })
    api.adjust.mockResolvedValue({})
  })

  it('converts the displayed amount to units and lets the backend resolve the group', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    await wrapper.findAll('select')[0].setValue('virtual_currency')
    await flushPromises()
    await wrapper.get('input[type="number"]').setValue('12.34')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(api.adjust).toHaveBeenCalledWith('gold', expect.objectContaining({
      user_id: 42,
      amount_units: 1234,
      entry_type: 'grant'
    }))
    expect(api.adjust.mock.calls[0][1]).not.toHaveProperty('group_id')
    expect(api.updateBalance).not.toHaveBeenCalled()
  })
})
