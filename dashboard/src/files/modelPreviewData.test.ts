import { expect, it } from 'vitest'
import { MAX_MODEL_BYTES, readModelData, requireFbxMesh } from './modelPreviewData'

it('rejects oversized files from the header without reading and cancels chunked overflow', async () => {
  await expect(readModelData(new Response('', { headers: { 'content-length': String(MAX_MODEL_BYTES + 1) } }))).rejects.toThrow('too large to preview')
  let canceled = false
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new Uint8Array(MAX_MODEL_BYTES))
      controller.enqueue(new Uint8Array(1))
    },
    cancel() { canceled = true },
  })
  await expect(readModelData(new Response(body))).rejects.toThrow('too large to preview')
  expect(canceled).toBe(true)
})

it('reads model bytes and gives geometry-free FBX a plain message before animation parsing', async () => {
  const bytes = await readModelData(new Response('Objects: { AnimationCurve: 1 {} }'))
  expect(() => requireFbxMesh(bytes)).toThrow('No mesh in this file')
  const mesh = await readModelData(new Response('Vertices: *9 { a: 0,0,0,1,0,0,0,1,0 }'))
  expect(() => requireFbxMesh(mesh)).not.toThrow()
})
