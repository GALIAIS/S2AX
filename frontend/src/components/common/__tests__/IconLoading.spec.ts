import { mount } from '@vue/test-utils'
import { defineComponent, nextTick, ref } from 'vue'
import { describe, expect, it } from 'vitest'

import Icon from '../../icons/Icon.vue'

describe('Icon loading state', () => {
  it('changes every animated icon into a circular loading ring', async () => {
    const loading = ref(false)
    const Host = defineComponent({
      components: { Icon },
      setup() {
        return { loading }
      },
      template: '<Icon name="refresh" :class="{ \'animate-spin\': loading }" />'
    })
    const wrapper = mount(Host)

    expect(wrapper.find('circle').exists()).toBe(false)

    loading.value = true
    await nextTick()

    const ring = wrapper.find('circle')
    expect(ring.exists()).toBe(true)
    expect(ring.attributes('stroke-dasharray')).toBe('42 15')
    expect(wrapper.find('path').exists()).toBe(false)
  })
})
