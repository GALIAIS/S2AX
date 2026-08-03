import { defineComponent } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountTableFilters from '../AccountTableFilters.vue'
import type { AdminGroup } from '@/types'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const AccountSelectStub = defineComponent({
  name: 'AccountSelectStub',
  props: {
    modelValue: { type: [String, Number, Boolean], default: null },
    options: { type: Array, default: () => [] }
  },
  template: '<div />'
})

function makeGroup(
  id: number,
  name: string,
  overrides: Partial<AdminGroup> = {}
): AdminGroup {
  return {
    id,
    name,
    status: 'active',
    subscription_type: 'standard',
    is_exclusive: false,
    ...overrides
  } as AdminGroup
}

describe('AccountTableFilters', () => {
  it('groups account filters by allocation semantics and keeps disabled groups filterable', () => {
    const wrapper = mount(AccountTableFilters, {
      props: {
        searchQuery: '',
        filters: {
          platform: '',
          type: '',
          status: '',
          privacy_mode: '',
          group: ''
        },
        groups: [
          makeGroup(1, 'Zulu exclusive', { is_exclusive: true }),
          makeGroup(2, 'Alpha public'),
          makeGroup(3, 'Subscription', { subscription_type: 'subscription' }),
          makeGroup(4, 'Retired', { status: 'inactive' })
        ]
      },
      global: {
        stubs: {
          Select: AccountSelectStub,
          SearchInput: true
        }
      }
    })

    const groupOptions = wrapper.findAllComponents(AccountSelectStub)[4]?.props('options') as Array<{
      value: string
      label: string
      kind?: string
      disabled?: boolean
    }>

    expect(groupOptions.map((option) => option.label)).toEqual([
      'admin.accounts.allGroups',
      'admin.accounts.ungroupedGroup',
      'admin.accounts.groupFilterSections.exclusive',
      'Zulu exclusive',
      'admin.accounts.groupFilterSections.public',
      'Alpha public',
      'admin.accounts.groupFilterSections.subscription',
      'Subscription',
      'admin.accounts.groupFilterSections.disabled',
      'Retired'
    ])
    expect(groupOptions.filter((option) => option.kind === 'group')).toEqual([
      expect.objectContaining({ value: '__account-group-section-exclusive__', disabled: true }),
      expect.objectContaining({ value: '__account-group-section-public__', disabled: true }),
      expect.objectContaining({ value: '__account-group-section-subscription__', disabled: true }),
      expect.objectContaining({ value: '__account-group-section-disabled__', disabled: true })
    ])
  })
})
