import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, patch, put } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
  put: vi.fn()
}))

vi.mock('../client', () => ({
  apiClient: { get, post, patch, put }
}))

import {
  createVisualGenerationJob,
  createVisualPack,
  listVisualPacks,
  setVisualReleasePolicy,
  type CityVisualProceduralManifest
} from '@/api/admin/cityVisualPacks'

const manifest: CityVisualProceduralManifest = {
  schema_version: 1,
  render_mode: 'procedural_pixel_v1',
  logical_tile_px: 16,
  profile_palettes: { default: { ground: '#5f8259' } },
  assets: []
}

describe('admin city visual packs API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    patch.mockReset()
    put.mockReset()
  })

  it('lists the bounded visual pack inventory', async () => {
    get.mockResolvedValue({ data: [] })

    await listVisualPacks()

    expect(get).toHaveBeenCalledWith('/admin/city/visual-packs', { params: { limit: 50 } })
  })

  it('creates a data-only procedural staging pack with an idempotency key', async () => {
    post.mockResolvedValue({ data: { pack_id: 'city-pixel-japan', pack_version: '1.0.0' } })

    await createVisualPack({
      pack_id: 'city-pixel-japan',
      pack_version: '1.0.0',
      spatial_profile_ids: ['japan'],
      manifest
    })

    expect(post).toHaveBeenCalledWith('/admin/city/visual-packs', expect.objectContaining({
      pack_id: 'city-pixel-japan',
      manifest
    }), expect.objectContaining({
      headers: expect.objectContaining({ 'Idempotency-Key': expect.stringMatching(/^city-visual-pack-create-/) })
    }))
  })

  it('submits only structured generation fields', async () => {
    post.mockResolvedValue({ data: { id: 7, status: 'queued' } })

    await createVisualGenerationJob('city-pixel-japan', '1.0.0', {
      asset_class: 'terrain',
      semantic_tags: ['residential', 'day'],
      pixel_width: 16,
      pixel_height: 16,
      frame_count: 1
    })

    expect(post).toHaveBeenCalledWith(
      '/admin/city/visual-packs/city-pixel-japan/1.0.0/generation-jobs',
      {
        asset_class: 'terrain',
        semantic_tags: ['residential', 'day'],
        pixel_width: 16,
        pixel_height: 16,
        frame_count: 1
      },
      expect.objectContaining({
        headers: expect.objectContaining({ 'Idempotency-Key': expect.stringMatching(/^city-visual-generation-create-/) })
      })
    )
  })

  it('assigns a profile-specific policy through an encoded path', async () => {
    put.mockResolvedValue({ data: { spatial_profile_id: 'jp.metropolitan' } })

    await setVisualReleasePolicy('jp.metropolitan', {
      pack_id: 'city-pixel-japan',
      pack_version: '1.0.0'
    })

    expect(put).toHaveBeenCalledWith(
      '/admin/city/visual-release-policies/jp.metropolitan',
      { pack_id: 'city-pixel-japan', pack_version: '1.0.0' },
      expect.objectContaining({
        headers: expect.objectContaining({ 'Idempotency-Key': expect.stringMatching(/^city-visual-release-policy-/) })
      })
    )
  })
})
