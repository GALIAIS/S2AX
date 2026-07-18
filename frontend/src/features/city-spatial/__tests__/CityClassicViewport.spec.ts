import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import type { ClassicOvermapScene } from '../projection'

vi.mock('pixi.js', () => {
  class MockDisplayObject {
    destroy = vi.fn()
  }

  class Container extends MockDisplayObject {
    children: MockDisplayObject[] = []

    addChild<T extends MockDisplayObject>(child: T): T {
      this.children.push(child)
      return child
    }

    removeChildren(): MockDisplayObject[] {
      return this.children.splice(0)
    }
  }

  class Graphics extends MockDisplayObject {
    rect(): this { return this }
    fill(): this { return this }
    stroke(): this { return this }
  }

  class BitmapText extends MockDisplayObject {
    anchor = { set: vi.fn() }
    position = { set: vi.fn() }
  }

  class Application {
    canvas = document.createElement('canvas')
    stage = new Container()
    renderer = { resize: vi.fn() }
    render = vi.fn()
    init = vi.fn().mockResolvedValue(undefined)
    destroy = vi.fn()
  }

  return {
    Application,
    BitmapFont: { install: vi.fn(), uninstall: vi.fn() },
    BitmapText,
    Container,
    Graphics
  }
})

import CityClassicViewport from '../CityClassicViewport.vue'

const tile = {
  chunk_x: 2,
  chunk_y: -1,
  z: 0,
  district_code: 'harbor',
  terrain_definition_id: 'terrain.road',
  road_mask: 5,
  river_mask: 0,
  variant: 0,
  tile_hash: 'tile-hash',
  metadata: {}
}

const scene: ClassicOvermapScene = {
  mode: 'overmap',
  width: 320,
  height: 240,
  cellSize: 40,
  offsetX: 20,
  offsetY: 30,
  cells: [{
    tile,
    glyph: '│',
    foreground: '#ffffff',
    background: '#111318',
    landUses: [],
    parcelCount: 0,
    buildingCount: 0,
    activeProjectCount: 0,
    completedProjectCount: 0,
    activeEnterpriseSiteCount: 0,
    enterpriseFirmCount: 0,
    enterpriseOccupiedUnits: 0,
    x: 20,
    y: 30,
    size: 40
  }]
}

describe('CityClassicViewport', () => {
  beforeEach(() => {
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      callback(0)
      return 1
    })
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('exposes CLASSIC map controls without routing or remounting the page', async () => {
    const wrapper = mount(CityClassicViewport, {
      props: { scene, viewportLabel: 'City CLASSIC viewport' }
    })
    await nextTick()

    const viewport = wrapper.get('[role="application"]')
    expect(viewport.attributes('aria-label')).toBe('City CLASSIC viewport')

    await viewport.trigger('keydown', { key: 'ArrowRight' })
    await viewport.trigger('keydown', { key: ']' })
    await viewport.trigger('keydown', { key: 'm' })
    await viewport.trigger('keydown', { key: '?' })
    viewport.element.dispatchEvent(new WheelEvent('wheel', {
      bubbles: true,
      cancelable: true,
      deltaY: -20
    }))
    viewport.element.dispatchEvent(new WheelEvent('wheel', {
      bubbles: true,
      cancelable: true,
      deltaY: 20,
      shiftKey: true
    }))
    await nextTick()

    expect(wrapper.emitted('pan')).toEqual([[{ x: 1, y: 0 }]])
    expect(wrapper.emitted('change-z')).toEqual([[1], [-1]])
    expect(wrapper.emitted('toggle-mode')).toHaveLength(1)
    expect(wrapper.emitted('show-help')).toHaveLength(1)
    expect(wrapper.emitted('zoom')).toEqual([[1]])
    wrapper.unmount()
  })

  it('hit-tests an Overmap cell for selection and activation', async () => {
    const wrapper = mount(CityClassicViewport, {
      attachTo: document.body,
      props: { scene, viewportLabel: 'City CLASSIC viewport' }
    })
    await nextTick()
    const viewport = wrapper.get('[role="application"]')
    Object.defineProperty(viewport.element, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ left: 0, top: 0, width: 320, height: 240 }) as DOMRect
    })

    await viewport.trigger('pointerdown', { button: 0, pointerId: 3, clientX: 25, clientY: 35 })
    await viewport.trigger('pointerup', { button: 0, pointerId: 3, clientX: 25, clientY: 35 })
    await viewport.trigger('dblclick', { clientX: 25, clientY: 35 })

    expect(wrapper.emitted('select-tile')).toEqual([[tile]])
    expect(wrapper.emitted('activate-tile')).toEqual([[tile]])
    wrapper.unmount()
  })
})
