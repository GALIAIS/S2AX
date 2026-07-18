import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import Pagination from '@/components/common/Pagination.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('Pagination', () => {
  it('uses non-submit buttons so paging cannot submit a surrounding form', () => {
    const wrapper = mount(Pagination, {
      props: { total: 100, page: 2, pageSize: 20 },
      global: { stubs: { Icon: true, Select: true } },
    })

    expect(wrapper.findAll('button').every((button) => button.attributes('type') === 'button')).toBe(true)
  })
})
