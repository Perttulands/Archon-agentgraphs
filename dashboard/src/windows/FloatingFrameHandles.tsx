/**
 * The resize handles of a floating window: four edges and four corners,
 * invisible until the pointer is on one or the keyboard has focused it.
 *
 * Ported from CHROTE (chrote/dashboard/src/components/FloatingFrameHandles.tsx).
 * A window draws this inside itself with the handles useFloatingWindow returned.
 */

import type { FloatingFrameHandle, FrameHandleId } from './useFloatingWindow'
import './floatingWindows.css'

export default function FloatingFrameHandles({ handles, activeHandle }: {
  handles: readonly FloatingFrameHandle[]
  activeHandle: FrameHandleId | null
}) {
  return (
    <>
      {handles.map(handle => (
        <div
          key={handle.id}
          {...handle.props}
          className={`floating-frame-handle${activeHandle === handle.id ? ' dragging' : ''}`}
          data-handle={handle.id}
        />
      ))}
    </>
  )
}
