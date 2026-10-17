import { describe, expect, it } from 'vitest'
import { mediaKind, modelResourceUrl } from './mediaUrls'

describe('model sidecar URLs', () => {
  it('resolves buffers and textures beside artifact models, preserving escaped names', () => {
    const src = '/api/runs/run_1/artifacts/step/1/models/chair.gltf'
    expect(modelResourceUrl(src, '../textures/wood%20grain.png')).toBe(`${window.location.origin}/api/runs/run_1/artifacts/step/1/textures/wood%20grain.png`)
    expect(modelResourceUrl(src, 'chair.bin')).toBe(`${window.location.origin}/api/runs/run_1/artifacts/step/1/models/chair.bin`)
  })

  it('resolves referenced-file sidecars using the path query rather than the API directory', () => {
    const src = '/api/files/raw?path=' + encodeURIComponent('/work/My models/chair.gltf')
    const resolved = new URL(modelResourceUrl(src, '../textures/wood%20grain.png'), window.location.origin)
    expect(resolved.pathname).toBe('/api/files/raw')
    expect(resolved.searchParams.get('path')).toBe('/work/textures/wood grain.png')
  })

  it.each(['data:application/octet-stream;base64,AA==', 'blob:https://example.com/id', 'https://example.com/texture.png', '//example.com/texture.png'])(
    'keeps an embedded or remote resource: %s', resource => {
      expect(modelResourceUrl('/api/files/raw?path=%2Fmodels%2Fchair.gltf', resource)).toBe(resource)
    },
  )
})

it('recognizes supported media case insensitively while leaving other downloads alone', () => {
  for (const path of ['turntable.mp4', 'turntable.WEBM']) expect(mediaKind(path)).toBe('video')
  for (const path of ['scene.glb', 'scene.GLTF', 'scene.fbx', 'rig.FBX']) expect(mediaKind(path)).toBe('model')
  for (const path of ['movie.mp4.txt', 'photo.png']) expect(mediaKind(path)).toBeNull()
})
