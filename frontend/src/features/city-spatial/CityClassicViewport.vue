<template>
  <div
    ref="hostRef"
    class="classic-viewport"
    :class="{ 'classic-viewport-dragging': dragging }"
    role="application"
    tabindex="0"
    :aria-label="viewportLabel"
    @pointerdown="handlePointerDown"
    @pointermove="handlePointerMove"
    @pointerup="handlePointerUp"
    @pointercancel="handlePointerCancel"
    @pointerleave="handlePointerLeave"
    @dblclick="handleDoubleClick"
    @wheel.prevent="handleWheel"
    @keydown="handleKeyDown"
  >
    <div v-if="rendererError" class="classic-renderer-error" role="alert">
      {{ rendererError }}
    </div>
    <div v-if="busy" class="classic-viewport-progress" aria-hidden="true">
      <span />
    </div>
    <div class="classic-viewport-corner classic-viewport-corner-top" aria-hidden="true" />
    <div class="classic-viewport-corner classic-viewport-corner-bottom" aria-hidden="true" />
  </div>
</template>

<script lang="ts">
let classicViewportInstance = 0
</script>

<script setup lang="ts">
import { Application, BitmapFont, BitmapText, Container, Graphics } from 'pixi.js'
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { CityOvermapTile } from '@/api/citySpatial'
import {
  cityLandUseColor,
  chunkKey,
  hitTestClassicScene,
  unloadedMapBackground,
  type ClassicLocalScene,
  type ClassicOvermapScene,
  type ClassicScene,
  type ProjectedCityCell
} from './projection'

interface CoordinateSelection {
  worldX: number
  worldY: number
  z: number
}

const props = withDefaults(defineProps<{
  scene: ClassicScene
  selectedCoordinate?: CoordinateSelection | null
  selectedTile?: CityOvermapTile | null
  generatedChunkKeys?: ReadonlySet<string>
  glyphCharacters?: string
  busy?: boolean
  viewportLabel: string
}>(), {
  selectedCoordinate: null,
  selectedTile: null,
  generatedChunkKeys: () => new Set<string>(),
  glyphCharacters: '',
  busy: false
})

const emit = defineEmits<{
  (event: 'resize', value: { width: number; height: number }): void
  (event: 'select-cell', value: ProjectedCityCell): void
  (event: 'hover-cell', value: ProjectedCityCell | null): void
  (event: 'select-tile', value: CityOvermapTile): void
  (event: 'activate-tile', value: CityOvermapTile): void
  (event: 'pan', value: { x: number; y: number }): void
  (event: 'zoom', direction: number): void
  (event: 'change-z', direction: number): void
  (event: 'surface'): void
  (event: 'toggle-mode'): void
  (event: 'show-overmap'): void
  (event: 'activate-selection'): void
  (event: 'show-help'): void
}>()

const hostRef = ref<HTMLDivElement | null>(null)
const dragging = ref(false)
const rendererError = ref<string | null>(null)

let app: Application | null = null
let sceneLayer: Container | null = null
let resizeObserver: ResizeObserver | null = null
let renderFrame: number | null = null
let fontInstalled = false
let pointerID: number | null = null
let pointerStart = { x: 0, y: 0 }
let pointerLast = { x: 0, y: 0 }
let dragRemainder = { x: 0, y: 0 }
let pointerMoved = false

const fontName = `Sub2APICityClassic${++classicViewportInstance}`

function pointFromPointer(event: PointerEvent | MouseEvent): { x: number; y: number } {
  const host = hostRef.value
  if (!host) return { x: 0, y: 0 }
  const rect = host.getBoundingClientRect()
  return {
    x: (event.clientX - rect.left) * (props.scene.width / Math.max(1, rect.width)),
    y: (event.clientY - rect.top) * (props.scene.height / Math.max(1, rect.height))
  }
}

function installBitmapFont(): void {
  if (fontInstalled) return
  const baseCharacters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.,:;!?@#$%^&*()[]{}<>+-=/\\\\|_~`\\\"\'·≈♣π□╫↕■§•╵╶└╷│┌├╴┘─┴┐┤┬┼"
  const characters = [...new Set(`${baseCharacters}${props.glyphCharacters}`)].join('')
  BitmapFont.install({
    name: fontName,
    style: {
      fontFamily: 'Consolas, Menlo, Monaco, monospace',
      fontSize: 32,
      fill: '#ffffff'
    },
    chars: characters,
    resolution: Math.min(2, globalThis.devicePixelRatio || 1),
    padding: 2,
    textureStyle: { scaleMode: 'nearest' }
  })
  fontInstalled = true
}

function destroySceneChildren(): void {
  if (!sceneLayer) return
  const children = sceneLayer.removeChildren()
  for (const child of children) child.destroy({ children: true })
}

function addGlyph(
  layer: Container,
  glyph: string,
  color: string,
  x: number,
  y: number,
  size: number
): void {
  const text = new BitmapText({
    text: glyph,
    style: {
      fontFamily: fontName,
      fontSize: Math.max(8, Math.floor(size * 0.78)),
      fill: color
    }
  })
  text.anchor.set(0.5)
  text.position.set(Math.round(x), Math.round(y))
  layer.addChild(text)
}

function addBackgroundGroups(
  layer: Container,
  groups: Map<string, Array<{ x: number; y: number; width: number; height: number }>>
): void {
  for (const [color, rectangles] of groups) {
    const graphics = new Graphics()
    for (const rectangle of rectangles) {
      graphics.rect(rectangle.x, rectangle.y, rectangle.width, rectangle.height)
    }
    graphics.fill(color)
    layer.addChild(graphics)
  }
}

function renderLocalScene(layer: Container, scene: ClassicLocalScene): void {
  const backgrounds = new Map<string, Array<{ x: number; y: number; width: number; height: number }>>()
  for (const cell of scene.cells) {
    if (!cell) continue
    const items = backgrounds.get(cell.background) ?? []
    items.push({
      x: cell.column * scene.cellSize,
      y: cell.row * scene.cellSize,
      width: scene.cellSize,
      height: scene.cellSize
    })
    backgrounds.set(cell.background, items)
  }
  addBackgroundGroups(layer, backgrounds)

  for (const cell of scene.cells) {
    if (!cell) continue
    addGlyph(
      layer,
      cell.glyph,
      cell.foreground,
      (cell.column + 0.5) * scene.cellSize,
      (cell.row + 0.5) * scene.cellSize,
      scene.cellSize
    )
  }

  const selected = props.selectedCoordinate
  if (selected && selected.z === scene.cells.find(Boolean)?.z) {
    const column = selected.worldX - scene.startWorldX
    const row = selected.worldY - scene.startWorldY
    if (column >= 0 && column < scene.columns && row >= 0 && row < scene.rows) {
      const cursor = new Graphics()
        .rect(column * scene.cellSize + 1, row * scene.cellSize + 1, scene.cellSize - 2, scene.cellSize - 2)
        .stroke({ color: '#5aa2ff', width: 2 })
      layer.addChild(cursor)
    }
  }
}

function renderOvermapScene(layer: Container, scene: ClassicOvermapScene): void {
  const backgrounds = new Map<string, Array<{ x: number; y: number; width: number; height: number }>>()
  for (const cell of scene.cells) {
    const items = backgrounds.get(cell.background) ?? []
    items.push({ x: cell.x, y: cell.y, width: cell.size, height: cell.size })
    backgrounds.set(cell.background, items)
  }
  addBackgroundGroups(layer, backgrounds)

  for (const cell of scene.cells) {
    addGlyph(layer, cell.glyph, cell.foreground, cell.x + cell.size / 2, cell.y + cell.size / 2, cell.size)
    const border = new Graphics()
      .rect(cell.x + 0.5, cell.y + 0.5, cell.size - 1, cell.size - 1)
      .stroke({ color: '#343941', width: 1 })
    layer.addChild(border)
    if (cell.landUses.length > 0) {
      const zoning = new Graphics()
      const segmentWidth = (cell.size - 4) / cell.landUses.length
      cell.landUses.forEach((use, index) => {
        zoning
          .rect(cell.x + 2 + segmentWidth * index, cell.y + cell.size - 5, segmentWidth, 3)
          .fill(cityLandUseColor(use))
      })
      layer.addChild(zoning)
    }
    if (cell.buildingCount > 0) {
      const countBackground = new Graphics()
        .rect(cell.x + 2, cell.y + 2, Math.max(12, cell.size * 0.3), Math.max(10, cell.size * 0.24))
        .fill('#111318')
      layer.addChild(countBackground)
      addGlyph(
        layer,
        String(cell.buildingCount),
        '#d4d4d8',
        cell.x + Math.max(8, cell.size * 0.17),
        cell.y + Math.max(7, cell.size * 0.14),
        Math.max(10, cell.size * 0.26)
      )
    }
    if (cell.activeProjectCount > 0) {
      const markerHeight = Math.max(10, cell.size * 0.28)
      const marker = new Graphics()
        .rect(cell.x + 2, cell.y + cell.size - markerHeight - 7, 3, markerHeight)
        .fill('#d99b52')
      layer.addChild(marker)
    }
    if (cell.activeEnterpriseSiteCount > 0) {
      const markerHeight = Math.max(10, cell.size * 0.28)
      const marker = new Graphics()
        .rect(cell.x + cell.size - 5, cell.y + cell.size - markerHeight - 7, 3, markerHeight)
        .fill('#6fa8c7')
      layer.addChild(marker)
    }
    if (props.generatedChunkKeys.has(chunkKey(cell.tile.chunk_x, cell.tile.chunk_y, cell.tile.z))) {
      const marker = new Graphics()
        .rect(cell.x + cell.size - 7, cell.y + 3, 4, 4)
        .fill('#31d17c')
      layer.addChild(marker)
    }
    if (
      props.selectedTile &&
      props.selectedTile.chunk_x === cell.tile.chunk_x &&
      props.selectedTile.chunk_y === cell.tile.chunk_y &&
      props.selectedTile.z === cell.tile.z
    ) {
      const cursor = new Graphics()
        .rect(cell.x + 1, cell.y + 1, cell.size - 2, cell.size - 2)
        .stroke({ color: '#5aa2ff', width: 3 })
      layer.addChild(cursor)
    }
  }
}

function renderScene(): void {
  if (!app || !sceneLayer) return
  app.renderer.resize(props.scene.width, props.scene.height)
  destroySceneChildren()
  const backdrop = new Graphics()
    .rect(0, 0, props.scene.width, props.scene.height)
    .fill(unloadedMapBackground())
  sceneLayer.addChild(backdrop)
  if (props.scene.mode === 'local') renderLocalScene(sceneLayer, props.scene)
  else renderOvermapScene(sceneLayer, props.scene)
  app.render()
}

function scheduleRender(): void {
  if (renderFrame !== null) cancelAnimationFrame(renderFrame)
  renderFrame = requestAnimationFrame(() => {
    renderFrame = null
    renderScene()
  })
}

async function initializeRenderer(): Promise<void> {
  const host = hostRef.value
  if (!host || app) return
  rendererError.value = null
  try {
    const nextApp = new Application()
    await nextApp.init({
      width: props.scene.width,
      height: props.scene.height,
      background: unloadedMapBackground(),
      antialias: false,
      autoDensity: true,
      resolution: Math.min(2, globalThis.devicePixelRatio || 1),
      preference: 'webgl',
      autoStart: false
    })
    if (!hostRef.value) {
      nextApp.destroy(true)
      return
    }
    app = nextApp
    app.canvas.className = 'classic-viewport-canvas'
    app.canvas.setAttribute('aria-hidden', 'true')
    sceneLayer = new Container()
    app.stage.addChild(sceneLayer)
    host.appendChild(app.canvas)
    installBitmapFont()
    renderScene()
  } catch (error: unknown) {
    rendererError.value = error instanceof Error ? error.message : 'CLASSIC renderer unavailable'
  }
}

function handleResize(entries: ResizeObserverEntry[]): void {
  const entry = entries[0]
  if (!entry) return
  const width = Math.max(240, Math.floor(entry.contentRect.width))
  const height = Math.max(240, Math.floor(entry.contentRect.height))
  emit('resize', { width, height })
}

function handlePointerDown(event: PointerEvent): void {
  if (event.button !== 0) return
  pointerID = event.pointerId
  pointerStart = pointFromPointer(event)
  pointerLast = pointerStart
  dragRemainder = { x: 0, y: 0 }
  pointerMoved = false
  dragging.value = true
  if (typeof hostRef.value?.setPointerCapture === 'function') {
    hostRef.value.setPointerCapture(event.pointerId)
  }
  hostRef.value?.focus({ preventScroll: true })
}

function handlePointerMove(event: PointerEvent): void {
  const point = pointFromPointer(event)
  if (pointerID === event.pointerId && dragging.value) {
    const deltaX = point.x - pointerLast.x
    const deltaY = point.y - pointerLast.y
    pointerLast = point
    if (Math.hypot(point.x - pointerStart.x, point.y - pointerStart.y) > 4) pointerMoved = true
    if (props.scene.mode === 'local') {
      dragRemainder.x += deltaX
      dragRemainder.y += deltaY
      const cellsX = Math.trunc(dragRemainder.x / props.scene.cellSize)
      const cellsY = Math.trunc(dragRemainder.y / props.scene.cellSize)
      if (cellsX !== 0 || cellsY !== 0) {
        emit('pan', { x: -cellsX, y: -cellsY })
        dragRemainder.x -= cellsX * props.scene.cellSize
        dragRemainder.y -= cellsY * props.scene.cellSize
      }
    }
    return
  }
  const hit = hitTestClassicScene(props.scene, point.x, point.y)
  emit('hover-cell', hit && 'worldX' in hit ? hit : null)
}

function selectAtPoint(point: { x: number; y: number }, activate = false): void {
  const hit = hitTestClassicScene(props.scene, point.x, point.y)
  if (!hit) return
  if ('worldX' in hit) emit('select-cell', hit)
  else if (activate) emit('activate-tile', hit)
  else emit('select-tile', hit)
}

function handlePointerUp(event: PointerEvent): void {
  if (pointerID !== event.pointerId) return
  if (!pointerMoved) selectAtPoint(pointFromPointer(event))
  if (typeof hostRef.value?.releasePointerCapture === 'function') {
    hostRef.value.releasePointerCapture(event.pointerId)
  }
  pointerID = null
  dragging.value = false
}

function handlePointerCancel(event: PointerEvent): void {
  if (pointerID !== event.pointerId) return
  pointerID = null
  dragging.value = false
}

function handlePointerLeave(): void {
  if (!dragging.value) emit('hover-cell', null)
}

function handleDoubleClick(event: MouseEvent): void {
  selectAtPoint(pointFromPointer(event), true)
}

function handleWheel(event: WheelEvent): void {
  if (event.shiftKey) emit('change-z', event.deltaY < 0 ? 1 : -1)
  else emit('zoom', event.deltaY < 0 ? 1 : -1)
}

function isTypingTarget(target: EventTarget | null): boolean {
  const element = target instanceof HTMLElement ? target : null
  return Boolean(element?.closest('input, textarea, select, [contenteditable="true"]'))
}

function handleKeyDown(event: KeyboardEvent): void {
  if (isTypingTarget(event.target)) return
  const action = (): void => event.preventDefault()
  switch (event.key) {
    case 'ArrowLeft': action(); emit('pan', { x: -1, y: 0 }); break
    case 'ArrowRight': action(); emit('pan', { x: 1, y: 0 }); break
    case 'ArrowUp': action(); emit('pan', { x: 0, y: -1 }); break
    case 'ArrowDown': action(); emit('pan', { x: 0, y: 1 }); break
    case '[': action(); emit('change-z', -1); break
    case ']': action(); emit('change-z', 1); break
    case '0': action(); emit('surface'); break
    case 'm':
    case 'M': action(); emit('toggle-mode'); break
    case 'Enter': action(); emit('activate-selection'); break
    case 'Escape': action(); emit('show-overmap'); break
    case '+':
    case '=': action(); emit('zoom', 1); break
    case '-': action(); emit('zoom', -1); break
    case '?': action(); emit('show-help'); break
  }
}

watch(
  () => [props.scene, props.selectedCoordinate, props.selectedTile, props.generatedChunkKeys],
  scheduleRender,
  { flush: 'post' }
)

watch(() => props.glyphCharacters, () => {
  if (fontInstalled) {
    BitmapFont.uninstall(fontName)
    fontInstalled = false
    installBitmapFont()
    scheduleRender()
  }
})

onMounted(async () => {
  await nextTick()
  resizeObserver = new ResizeObserver(handleResize)
  if (hostRef.value) resizeObserver.observe(hostRef.value)
  await initializeRenderer()
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  if (renderFrame !== null) cancelAnimationFrame(renderFrame)
  destroySceneChildren()
  if (app) app.destroy(true, { children: true })
  app = null
  sceneLayer = null
  if (fontInstalled) BitmapFont.uninstall(fontName)
  fontInstalled = false
})
</script>

<style scoped>
.classic-viewport {
  position: relative;
  min-height: 31rem;
  overflow: hidden;
  cursor: crosshair;
  background: #111318;
  outline: none;
  touch-action: none;
  user-select: none;
}

.classic-viewport:focus-visible {
  box-shadow: inset 0 0 0 2px var(--ui-accent);
}

.classic-viewport-dragging {
  cursor: grabbing;
}

:deep(.classic-viewport-canvas) {
  display: block;
  width: 100%;
  height: 100%;
  image-rendering: pixelated;
}

.classic-renderer-error {
  position: absolute;
  inset: 0;
  z-index: 3;
  display: grid;
  place-items: center;
  padding: 2rem;
  color: #ff7b72;
  background: #111318;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  text-align: center;
}

.classic-viewport-progress {
  position: absolute;
  z-index: 4;
  top: 0;
  right: 0;
  left: 0;
  height: 2px;
  overflow: hidden;
  background: rgb(90 162 255 / 18%);
}

.classic-viewport-progress span {
  display: block;
  width: 32%;
  height: 100%;
  background: #5aa2ff;
  animation: classic-progress 1.1s linear infinite;
}

.classic-viewport-corner {
  position: absolute;
  z-index: 2;
  width: 16px;
  height: 16px;
  pointer-events: none;
}

.classic-viewport-corner-top {
  top: 8px;
  left: 8px;
  border-top: 1px solid rgb(255 255 255 / 32%);
  border-left: 1px solid rgb(255 255 255 / 32%);
}

.classic-viewport-corner-bottom {
  right: 8px;
  bottom: 8px;
  border-right: 1px solid rgb(255 255 255 / 32%);
  border-bottom: 1px solid rgb(255 255 255 / 32%);
}

@keyframes classic-progress {
  from { transform: translateX(-110%); }
  to { transform: translateX(330%); }
}

@media (prefers-reduced-motion: reduce) {
  .classic-viewport-progress span { animation: none; width: 100%; }
}
</style>
