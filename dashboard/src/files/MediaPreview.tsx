// Native video and lazy Three.js previews, adapted from CHROTE FileViewer
// and ModelPreview. No autoplay: the operator controls playback.
import { lazy, Suspense } from 'react'
import './MediaPreview.css'
const ModelViewer = lazy(() => import('./ModelViewer'))

export default function MediaPreview({ kind, src, name }: { kind: 'video' | 'model'; src: string; name: string }) {
  if (kind === 'video') return <video className="file-video" src={src} aria-label={name} controls preload="metadata" />
  return <Suspense fallback={<p role="status">Loading 3D viewer…</p>}><ModelViewer key={src} src={src} name={name} /></Suspense>
}
