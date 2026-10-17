/** Resolve glTF buffers/textures beside either kind of raw file URL. */
export function modelResourceUrl(src: string, resource: string): string {
  if (/^(data:|blob:|https?:|\/\/)/i.test(resource)) return resource
  const base = new URL(src, window.location.origin)
  const path = base.searchParams.get('path')
  if (base.pathname === '/api/files/raw' && path) {
    const resolved = new URL(resource, new URL(path.split('/').map(encodeURIComponent).join('/'), 'http://file.local'))
    base.searchParams.set('path', decodeURIComponent(resolved.pathname))
    return base.pathname + base.search
  }
  return new URL(resource, base).href
}

export function mediaKind(path: string): 'video' | 'model' | null {
  if (/\.(mp4|webm)$/i.test(path)) return 'video'
  if (/\.(glb|gltf)$/i.test(path)) return 'model'
  return null
}
