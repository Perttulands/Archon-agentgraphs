import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'

// An attempt's image, drawn whole in its frame. A click zooms it to the
// image's own pixels at the point clicked, and every image of the same output
// in the window zooms to the same point, so the attempts show the same detail
// side by side; a drag pans them together and another click fits them again.

/**
 * The zoom the window's images share: which output's images, the point of the
 * image at the frame's centre (0 to 1 across and down), and how many times the
 * fitted size. A factor of 0 shows each image at its own pixels.
 */
export interface ImageZoom {
  group: string
  x: number
  y: number
  factor: number
}

interface Size { width: number; height: number }

/** How far a press moves before it pans instead of clicking. */
const DRAG = 3

const clamp = (value: number, low: number, high: number) => Math.min(high, Math.max(low, value))

/** The image fitted whole into the frame, centred: its size and offset. */
export function fitImage(frame: Size, natural: Size): Size & { left: number; top: number } {
  if (!frame.width || !frame.height || !natural.width || !natural.height) return { width: 0, height: 0, left: 0, top: 0 }
  const scale = Math.min(frame.width / natural.width, frame.height / natural.height)
  const width = natural.width * scale
  const height = natural.height * scale
  return { width, height, left: (frame.width - width) / 2, top: (frame.height - height) / 2 }
}

/** The zoom that shows an image at its own pixels, or at twice its fitted size when it is smaller than its frame. */
export function nativeFactor(frame: Size, natural: Size): number {
  const fit = fitImage(frame, natural)
  return fit.width ? Math.max(2, natural.width / fit.width) : 2
}

/** Where a zoomed image sits so the zoom's point is at the frame's centre, held so no empty edge shows past the image. */
export function zoomedImage(frame: Size, natural: Size, zoom: Pick<ImageZoom, 'x' | 'y' | 'factor'>): Size & { left: number; top: number } {
  const fit = fitImage(frame, natural)
  const factor = zoom.factor || nativeFactor(frame, natural)
  const width = fit.width * factor
  const height = fit.height * factor
  const place = (span: number, size: number, at: number) => (size <= span ? (span - size) / 2 : clamp(span / 2 - at * size, span - size, 0))
  return { width, height, left: place(frame.width, width, zoom.x), top: place(frame.height, height, zoom.y) }
}

/** A natural frame holds the image's proportions and is no wider than the image at the strip's image height, so no bars show beside it. */
function naturalFrame(natural: Size) {
  const ratio = natural.width / natural.height
  return { aspectRatio: `${natural.width} / ${natural.height}`, width: `min(100%, calc(var(--attempt-image-height) * ${ratio.toFixed(4)}))` }
}

export default function AttemptImage({ src, label, group, zoom, onZoom, frame = 'natural', onError }: {
  src: string
  /** What the image is, for its alt text and its zoom control. */
  label: string
  /** The images that zoom together: one output's images across the attempts. */
  group: string
  zoom: ImageZoom | null
  onZoom: (zoom: ImageZoom | null) => void
  /** `natural` frames the image at its own proportions; `fixed` leaves the frame's size to its container, as side by side does. */
  frame?: 'natural' | 'fixed'
  onError?: () => void
}) {
  const frameRef = useRef<HTMLButtonElement>(null)
  const [box, setBox] = useState<Size>({ width: 0, height: 0 })
  const [natural, setNatural] = useState<Size | null>(null)
  const drag = useRef<{ x: number; y: number; from: ImageZoom | null; moved: boolean } | null>(null)
  const zoomed = zoom && zoom.group === group && natural ? zoom : null

  useEffect(() => {
    const element = frameRef.current
    if (!element) return
    const measure = () => setBox(current => {
      const next = { width: element.clientWidth, height: element.clientHeight }
      return current.width === next.width && current.height === next.height ? current : next
    })
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  const placed = natural && zoomed ? zoomedImage(box, natural, zoomed) : null

  // The point of the image under the pointer, 0 to 1 across and down.
  const pointAt = (event: { clientX: number; clientY: number }) => {
    const rect = frameRef.current?.getBoundingClientRect()
    if (!rect || !natural) return { x: .5, y: .5 }
    const fit = fitImage(box, natural)
    return { x: clamp((event.clientX - rect.left - fit.left) / (fit.width || 1), 0, 1), y: clamp((event.clientY - rect.top - fit.top) / (fit.height || 1), 0, 1) }
  }
  const zoomIn = (at: { x: number; y: number }) => onZoom({ group, ...at, factor: natural ? nativeFactor(box, natural) : 2 })

  const down = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (event.button !== 0) return
    drag.current = { x: event.clientX, y: event.clientY, from: zoomed, moved: false }
    event.currentTarget.setPointerCapture?.(event.pointerId)
  }
  const move = (event: ReactPointerEvent<HTMLButtonElement>) => {
    const start = drag.current
    if (!start) return
    const dx = event.clientX - start.x
    const dy = event.clientY - start.y
    if (!start.moved && Math.hypot(dx, dy) < DRAG) return
    start.moved = true
    if (!start.from || !natural) return
    const size = zoomedImage(box, natural, start.from)
    onZoom({ ...start.from, x: clamp(start.from.x - dx / (size.width || 1), 0, 1), y: clamp(start.from.y - dy / (size.height || 1), 0, 1) })
  }
  const up = (event: ReactPointerEvent<HTMLButtonElement>) => {
    const start = drag.current
    drag.current = null
    if (!start || start.moved) return
    if (zoomed) onZoom(null)
    else zoomIn(pointAt(event))
  }

  return (
    <button
      ref={frameRef}
      type="button"
      className={`attempt-image${zoomed ? ' zoomed' : ''}`}
      style={natural && frame === 'natural' ? naturalFrame(natural) : undefined}
      aria-label={zoomed ? `Fit ${label}` : `Zoom into ${label}`}
      aria-pressed={Boolean(zoomed)}
      onPointerDown={down}
      onPointerMove={move}
      onPointerUp={up}
      onPointerCancel={() => { drag.current = null }}
      onKeyDown={event => {
        if (event.key !== 'Enter' && event.key !== ' ') return
        event.preventDefault()
        event.stopPropagation()
        if (zoomed) onZoom(null)
        else zoomIn({ x: .5, y: .5 })
      }}
    >
      <img
        src={src}
        alt={label}
        draggable={false}
        loading="lazy"
        decoding="async"
        style={placed ? { position: 'absolute', maxWidth: 'none', maxHeight: 'none', left: placed.left, top: placed.top, width: placed.width, height: placed.height } : undefined}
        onLoad={event => setNatural({ width: event.currentTarget.naturalWidth, height: event.currentTarget.naturalHeight })}
        onError={onError}
      />
    </button>
  )
}
