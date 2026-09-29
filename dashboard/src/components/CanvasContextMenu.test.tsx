import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import CanvasContextMenu, { MENU_MARGIN, placeMenu } from './CanvasContextMenu'

const viewport = { width: 1920, height: 1080 }

describe('placeMenu', () => {
  it('opens right and down from the click when there is room', () => {
    expect(placeMenu({ x: 300, y: 200 }, { width: 220, height: 300 }, viewport)).toEqual({ left: 300, top: 200, maxHeight: 1080 - 2 * MENU_MARGIN })
  })

  it('flips up and left near the bottom-right corner', () => {
    expect(placeMenu({ x: 1850, y: 950 }, { width: 220, height: 201 }, viewport)).toEqual({ left: 1630, top: 749, maxHeight: 1064 })
  })

  it('slides inside the edge when neither side has room', () => {
    expect(placeMenu({ x: 100, y: 500 }, { width: 220, height: 700 }, viewport).top).toBe(1080 - MENU_MARGIN - 700)
    expect(placeMenu({ x: 100, y: 500 }, { width: 220, height: 700 }, { width: 1920, height: 720 }).top).toBe(720 - MENU_MARGIN - 700)
    expect(placeMenu({ x: 0, y: 2 }, { width: 220, height: 100 }, viewport)).toEqual(expect.objectContaining({ left: MENU_MARGIN, top: MENU_MARGIN }))
  })

  it('caps a menu taller than the viewport and pins it to the top margin', () => {
    const placed = placeMenu({ x: 1400, y: 475 }, { width: 220, height: 1014 }, { width: 1920, height: 900 })
    expect(placed).toEqual({ left: 1400, top: MENU_MARGIN, maxHeight: 900 - 2 * MENU_MARGIN })
  })

  it('keeps every click position on screen', () => {
    for (let x = 0; x <= viewport.width; x += 160) {
      for (let y = 0; y <= viewport.height; y += 90) {
        for (const size of [{ width: 180, height: 120 }, { width: 240, height: 640 }, { width: 240, height: 1500 }]) {
          const placed = placeMenu({ x, y }, size, viewport)
          const height = Math.min(size.height, placed.maxHeight)
          expect(placed.left).toBeGreaterThanOrEqual(MENU_MARGIN)
          expect(placed.left + size.width).toBeLessThanOrEqual(viewport.width - MENU_MARGIN)
          expect(placed.top).toBeGreaterThanOrEqual(MENU_MARGIN)
          expect(placed.top + height).toBeLessThanOrEqual(viewport.height - MENU_MARGIN)
        }
      }
    }
  })
})

describe('CanvasContextMenu', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('renders section heads as presentation, and runs an item after closing', () => {
    const onClose = vi.fn()
    const action = vi.fn()
    render(<CanvasContextMenu menu={{ label: 'Slot · Agent', x: 10, y: 10, items: [
      { label: 'Assign agent', head: true },
      { label: 'Mason', action },
    ] }} onClose={onClose} />)
    const menu = screen.getByRole('menu', { name: 'Slot · Agent' })
    expect(menu.querySelector('.msection')).toHaveAttribute('role', 'presentation')
    expect(menu.querySelector('.msection')).toHaveTextContent('Assign agent')
    fireEvent.click(screen.getByRole('menuitem', { name: 'Mason' }))
    expect(onClose).toHaveBeenCalled()
    expect(action).toHaveBeenCalled()
  })
})
