import type {
  CityRealtimeChunkPayload,
  CityRealtimeSemanticLayer
} from '@/api/citySpatial'

export interface DecodedRealtimeChunkPayload {
  width: number
  height: number
  terrain: string[]
  layersByCell: Map<number, CityRealtimeSemanticLayer[]>
}

const maximumChunkCellCount = 16_384

export function realtimeChunkKey(chunkX: number, chunkY: number, z = 0): string {
  return `${chunkX}:${chunkY}:${z}`
}

export function floorDivide(value: number, divisor: number): number {
  if (!Number.isInteger(value) || !Number.isInteger(divisor) || divisor <= 0) {
    throw new Error('Invalid realtime world coordinate')
  }
  return Math.floor(value / divisor)
}

// Semantic chunk payloads are server-validated. The browser performs this
// smaller structural validation before drawing so a malformed response cannot
// allocate an unbounded canvas buffer or make coordinate lookup ambiguous.
export function decodeRealtimeChunkPayload(payload: CityRealtimeChunkPayload): DecodedRealtimeChunkPayload {
  const { width, height, terrain_runs: terrainRuns, layers } = payload
  const cellCount = width * height
  if (!Number.isInteger(width) || !Number.isInteger(height) || width <= 0 || height <= 0 ||
      cellCount > maximumChunkCellCount || !Array.isArray(terrainRuns) || !Array.isArray(layers)) {
    throw new Error('Invalid realtime chunk payload')
  }

  const terrain: string[] = []
  for (const run of terrainRuns) {
    if (!run || typeof run.definition_id !== 'string' || !run.definition_id ||
        !Number.isInteger(run.length) || run.length <= 0 || terrain.length + run.length > cellCount) {
      throw new Error('Invalid realtime terrain run')
    }
    terrain.push(...Array<string>(run.length).fill(run.definition_id))
  }
  if (terrain.length !== cellCount) throw new Error('Incomplete realtime terrain payload')

  const layersByCell = new Map<number, CityRealtimeSemanticLayer[]>()
  for (const layer of layers) {
    if (!layer || !Number.isInteger(layer.x) || !Number.isInteger(layer.y) ||
        layer.x < 0 || layer.x >= width || layer.y < 0 || layer.y >= height ||
        typeof layer.kind !== 'string' || typeof layer.definition_id !== 'string' || !layer.definition_id) {
      throw new Error('Invalid realtime map layer')
    }
    const index = layer.y * width + layer.x
    const stack = layersByCell.get(index) ?? []
    stack.push(layer)
    layersByCell.set(index, stack)
  }

  return { width, height, terrain, layersByCell }
}
