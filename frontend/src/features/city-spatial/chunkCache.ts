import type { ProjectedCityChunk } from './projection'

export class CityChunkCache {
  private readonly entries = new Map<string, ProjectedCityChunk>()

  constructor(private readonly capacity = 64) {
    if (!Number.isInteger(capacity) || capacity <= 0) throw new Error('Chunk cache capacity must be positive')
  }

  get size(): number {
    return this.entries.size
  }

  get(key: string): ProjectedCityChunk | undefined {
    const value = this.entries.get(key)
    if (!value) return undefined
    this.entries.delete(key)
    this.entries.set(key, value)
    return value
  }

  peek(key: string): ProjectedCityChunk | undefined {
    return this.entries.get(key)
  }

  set(value: ProjectedCityChunk): void {
    this.entries.delete(value.key)
    this.entries.set(value.key, value)
    while (this.entries.size > this.capacity) {
      const oldest = this.entries.keys().next().value as string | undefined
      if (oldest === undefined) break
      this.entries.delete(oldest)
    }
  }

  delete(key: string): boolean {
    return this.entries.delete(key)
  }

  clear(): void {
    this.entries.clear()
  }

  snapshot(): ReadonlyMap<string, ProjectedCityChunk> {
    return new Map(this.entries)
  }
}
