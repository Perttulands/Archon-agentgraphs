// A preview parse runs in the UI thread; refuse oversized inputs before parsing.
export const MAX_MODEL_BYTES = 64 * 1024 * 1024
export class ModelPreviewError extends Error {}
const tooLarge = () => new ModelPreviewError('This file is too large to preview (maximum 64 MiB). Download it to open in a 3D application.')

export async function readModelData(response: Response): Promise<ArrayBuffer> {
  if (!response.ok) throw new ModelPreviewError(`The model file could not be loaded (HTTP ${response.status}).`)
  if (Number(response.headers.get('content-length')) > MAX_MODEL_BYTES) {
    await response.body?.cancel()
    throw tooLarge()
  }
  const reader = response.body?.getReader()
  if (!reader) throw new ModelPreviewError('The model file could not be read.')
  const chunks: Uint8Array[] = []
  let length = 0
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      length += value.byteLength
      if (length > MAX_MODEL_BYTES) { await reader.cancel(); throw tooLarge() }
      chunks.push(value)
    }
  } finally { reader.releaseLock() }
  const data = new Uint8Array(length)
  let offset = 0
  for (const chunk of chunks) { data.set(chunk, offset); offset += chunk.byteLength }
  return data.buffer
}

export function requireFbxMesh(data: ArrayBuffer) {
  // Both binary and ASCII FBX name the geometry's vertex array "Vertices".
  // Check before FBXLoader reaches animation curves referencing absent meshes.
  const bytes = new Uint8Array(data)
  const marker = [86, 101, 114, 116, 105, 99, 101, 115]
  for (let i = bytes.indexOf(marker[0]); i >= 0 && i <= bytes.length - marker.length; i = bytes.indexOf(marker[0], i + 1)) {
    if (marker.every((byte, j) => bytes[i + j] === byte)) return
  }
  throw new ModelPreviewError('No mesh in this file')
}
