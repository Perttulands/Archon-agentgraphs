import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import DismissiblePanel from './DismissiblePanel'

export type MenuItem = { label: string; action?: () => void; destructive?: boolean; disabled?: boolean; head?: boolean }
export type MenuState = { label: string; x: number; y: number; items: MenuItem[] }

/** Space kept between a menu and the viewport edge. */
export const MENU_MARGIN = 8

export interface MenuPlacement {
  left: number
  top: number
  maxHeight: number
}

/**
 * Places a menu of the given natural size at a click point so all of it is on
 * screen: it opens right and down from the point, flips left or up when that
 * side has no room, and otherwise slides back inside the edge. A menu taller
 * than the viewport gets the viewport's height and scrolls.
 */
export function placeMenu(point: { x: number; y: number }, size: { width: number; height: number }, viewport: { width: number; height: number }, margin = MENU_MARGIN): MenuPlacement {
  const maxHeight = Math.max(0, viewport.height - margin * 2)
  const height = Math.min(size.height, maxHeight)
  const axis = (at: number, extent: number, room: number) => {
    const end = room - margin - extent
    const start = at + extent <= room - margin ? at : at - extent >= margin ? at - extent : end
    return Math.max(margin, Math.min(start, end))
  }
  return {
    left: axis(point.x, size.width, viewport.width),
    top: axis(point.y, height, viewport.height),
    maxHeight,
  }
}

/** The canvas right-click menu: on screen at any click position, scrolling when long. */
export default function CanvasContextMenu({ menu, onClose }: { menu: MenuState; onClose: () => void }) {
  const ref = useRef<HTMLDivElement | null>(null)
  // Until measured, the menu renders hidden at the top left, where nothing
  // narrows it, so its natural size is what gets placed.
  const [placement, setPlacement] = useState<MenuPlacement | null>(null)

  const place = useCallback(() => {
    const el = ref.current
    if (!el) return
    const viewport = { width: window.innerWidth, height: window.innerHeight }
    setPlacement(placeMenu({ x: menu.x, y: menu.y }, { width: el.offsetWidth, height: el.scrollHeight }, viewport))
  }, [menu])

  useLayoutEffect(() => {
    setPlacement(null)
  }, [menu])

  useLayoutEffect(() => {
    if (!placement) place()
  }, [place, placement])

  useLayoutEffect(() => {
    window.addEventListener('resize', place)
    return () => window.removeEventListener('resize', place)
  }, [place])

  return (
    <DismissiblePanel onDismiss={onClose} panelPosition="fixed">
      <div
        ref={ref}
        className="formations-context-menu ctxmenu"
        role="menu"
        aria-label={menu.label}
        style={placement
          ? { left: placement.left, top: placement.top, maxHeight: placement.maxHeight }
          : { left: 0, top: 0, visibility: 'hidden' }}
        onPointerDown={event => event.stopPropagation()}
      >
        <div className="mhead">{menu.label}</div>
        {menu.items.map((item, itemIndex) => item.head ? (
          <div key={`${item.label}-${itemIndex}`} className="msection" role="presentation">{item.label}</div>
        ) : (
          <button
            key={`${item.label}-${itemIndex}`}
            type="button"
            role="menuitem"
            disabled={item.disabled}
            className={item.destructive ? 'danger' : undefined}
            onClick={() => {
              onClose()
              item.action?.()
            }}
          >
            {item.label}
          </button>
        ))}
      </div>
    </DismissiblePanel>
  )
}
