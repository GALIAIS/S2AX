import { afterEach, describe, expect, it } from 'vitest'
import { collectMotionTargets } from '@/composables/useInterfaceMotion'

describe('collectMotionTargets', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('selects only visible route-level motion targets', () => {
    document.body.innerHTML = `
      <main>
        <div id="page"></div>
        <section id="summary"></section>
        <div aria-hidden="true" id="hidden"></div>
      </main>
    `

    expect(collectMotionTargets().map((item) => item.id)).toEqual(['page', 'summary'])
  })
})
