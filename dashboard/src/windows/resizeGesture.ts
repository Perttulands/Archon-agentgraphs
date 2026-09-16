/**
 * The pointer mechanics a window gesture shares: capture, the listeners a
 * captured drag needs, and the keyboard step.
 *
 * Ported from CHROTE (chrote/dashboard/src/hooks/resizeGesture.ts). Moving a
 * window by its title bar and resizing it from a handle both sit on this.
 */

export const RESIZE_KEYBOARD_STEP = 16

export interface PointerDragHandlers {
  move: (event: PointerEvent) => void
  /** The pointer was released: the drag's last value is the operator's. */
  finish: () => void
  /** The gesture was taken away: nothing is committed. */
  cancel: () => void
}

/**
 * Capture `pointerId` on `handle` and route its move, up and cancel events to
 * the caller. The returned function removes the listeners and releases the
 * capture; call it exactly once, whatever ended the drag.
 */
export function capturePointerDrag(
  handle: HTMLElement,
  pointerId: number,
  handlers: PointerDragHandlers,
): () => void {
  const move = (event: PointerEvent) => {
    if (event.pointerId === pointerId) handlers.move(event)
  }
  const finish = (event: PointerEvent) => {
    if (event.pointerId === pointerId) handlers.finish()
  }
  const cancel = (event: PointerEvent) => {
    if (event.pointerId === pointerId) handlers.cancel()
  }

  const cleanup = () => {
    handle.removeEventListener('pointermove', move)
    handle.removeEventListener('pointerup', finish)
    handle.removeEventListener('pointercancel', cancel)
    if (handle.hasPointerCapture?.(pointerId)) handle.releasePointerCapture(pointerId)
  }

  handle.setPointerCapture?.(pointerId)
  handle.addEventListener('pointermove', move)
  handle.addEventListener('pointerup', finish)
  handle.addEventListener('pointercancel', cancel)
  return cleanup
}
