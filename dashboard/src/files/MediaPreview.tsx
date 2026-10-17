// Native video and lazy Three.js previews, adapted from CHROTE FileViewer
// and ModelPreview. Archon mounts one model at a time, after opening its file or choosing a folder tile.
import { lazy, Suspense, useEffect, useId, useSyncExternalStore } from 'react'
import { formatBytes } from '../evidence/runEvidenceApi'
import './MediaPreview.css'
const ModelViewer = lazy(() => import('./ModelViewer'))

// Shared across floating windows: switching tiles unmounts the previous viewer,
// whose effect cleanup releases its renderer, geometry and textures.
let activeModel: string | null = null
const listeners = new Set<() => void>()
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener) } }
const snapshot = () => activeModel
function selectModel(id: string | null) {
  activeModel = id
  listeners.forEach(listener => listener())
}

function ModelTile({ src, name, size, autoLoad }: { src: string; name: string; size?: number; autoLoad?: boolean }) {
  const id = useId() + src
  const active = useSyncExternalStore(subscribe, snapshot) === id
  useEffect(() => {
    if (autoLoad && size !== undefined && size <= 64 * 1024 * 1024) selectModel(id)
    return () => { if (activeModel === id) selectModel(null) }
  }, [autoLoad, id, size])
  return <div className="model-tile">
    {!autoLoad || !active ? <button type="button" className="model-tile-open" aria-label={`${active ? 'Close' : 'Preview'} 3D model ${name}`}
      onClick={() => selectModel(active ? null : id)}>
      <span>{name}</span><span>{size === undefined ? '3D model' : formatBytes(size)} · {active ? 'Close preview' : 'Preview 3D'}</span>
    </button> : null}
    {active ? <Suspense fallback={<p role="status">Loading 3D viewer…</p>}><ModelViewer key={src} src={src} name={name} /></Suspense> : null}
  </div>
}

export default function MediaPreview({ kind, src, name, size, autoLoad = false }: { autoLoad?: boolean; kind: 'video' | 'model'; src: string; name: string; size?: number }) {
  if (kind === 'video') return <video className="file-video" src={src} aria-label={name} controls preload="metadata" />
  return <ModelTile src={src} name={name} size={size} autoLoad={autoLoad} />
}
