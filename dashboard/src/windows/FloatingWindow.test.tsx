import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import FloatingWindow from './FloatingWindow'
import { WindowManagerProvider, useWindowManager } from './WindowManager'
import { readFloatingWindowSize } from './floatingWindowSize'
import type { WindowRect, Workspace } from './windowGeometry'

// jsdom lays nothing out, so the workspace is given directly: a 1200 x 800
// canvas with the zoom column in its bottom-right corner.
const zoom = { left: 1120, top: 560, width: 80, height: 240 }
let canvas: Workspace = { bounds: { left: 0, top: 0, width: 1200, height: 800 }, avoid: [zoom] }
const workspace = () => canvas

function Harness({ initial, anchors = {} }: { initial: string[]; anchors?: Record<string, WindowRect> }) {
  const stack = useWindowManager(workspace)
  const [open, setOpen] = useState(initial)
  return (
    <>
      <button type="button" onClick={() => setOpen(ids => [...ids, `w${ids.length + 1}`])}>Open another</button>
      <WindowManagerProvider stack={stack}>
        {open.map(id => (
          <FloatingWindow key={id} id={id} kind="node" title={`Window ${id}`} label={`window ${id}`}
            defaultSize={{ width: 400, height: 300 }} anchor={anchors[id] ? () => anchors[id] : undefined} onClose={() => setOpen(ids => ids.filter(other => other !== id))}>
            <p>Body of {id}</p>
          </FloatingWindow>
        ))}
      </WindowManagerProvider>
    </>
  )
}

const windowOf = (id: string) => screen.getByRole('dialog', { name: `window ${id}` })
const rectOf = (id: string) => {
  const { left, top, width, height } = windowOf(id).style
  return { left: parseFloat(left), top: parseFloat(top), width: parseFloat(width), height: parseFloat(height) }
}
const zOf = (id: string) => Number(windowOf(id).style.zIndex)

function drag(target: HTMLElement, from: { x: number; y: number }, to: { x: number; y: number }) {
  fireEvent.pointerDown(target, { button: 0, pointerId: 1, clientX: from.x, clientY: from.y })
  fireEvent.pointerMove(target, { pointerId: 1, clientX: to.x, clientY: to.y })
  fireEvent.pointerUp(target, { pointerId: 1, clientX: to.x, clientY: to.y })
}

afterEach(() => {
  cleanup()
  localStorage.clear()
  canvas = { bounds: { left: 0, top: 0, width: 1200, height: 800 }, avoid: [zoom] }
})

describe('floating windows', () => {
  it('opens centred, cascades the next window on top, and raises a window when it is focused', () => {
    render(<Harness initial={['w1']} />)
    expect(rectOf('w1')).toEqual({ left: 400, top: 250, width: 400, height: 300 })
    expect(windowOf('w1')).toHaveFocus()

    fireEvent.click(screen.getByRole('button', { name: 'Open another' }))
    expect(rectOf('w2')).toEqual({ left: 428, top: 278, width: 400, height: 300 })
    expect(zOf('w2')).toBeGreaterThan(zOf('w1'))
    expect(windowOf('w2')).toHaveClass('focused')

    fireEvent.pointerDown(screen.getByText('Body of w1'), { button: 0, pointerId: 2 })
    expect(zOf('w1')).toBeGreaterThan(zOf('w2'))
    expect(windowOf('w1')).toHaveClass('focused')
    expect(windowOf('w1')).toHaveFocus()
  })

  it('cascades windows that open in the same render', () => {
    render(<Harness initial={['w1', 'w2']} />)
    expect(rectOf('w1')).toEqual({ left: 400, top: 250, width: 400, height: 300 })
    expect(rectOf('w2')).toEqual({ left: 428, top: 278, width: 400, height: 300 })
    expect(zOf('w2')).toBeGreaterThan(zOf('w1'))
  })

  it('opens beside its anchor, and centred when the anchor is out of view', () => {
    render(<Harness initial={['w1']} anchors={{
      w1: { left: 100, top: 120, width: 200, height: 100 },
      w2: { left: 900, top: 100, width: 200, height: 100 },
      w3: { left: -500, top: 100, width: 200, height: 100 },
    }} />)
    expect(rectOf('w1')).toEqual({ left: 312, top: 120, width: 400, height: 300 })

    fireEvent.click(screen.getByRole('button', { name: 'Open another' }))
    expect(rectOf('w2')).toEqual({ left: 488, top: 100, width: 400, height: 300 })

    fireEvent.click(screen.getByRole('button', { name: 'Open another' }))
    expect(rectOf('w3')).toEqual({ left: 456, top: 306, width: 400, height: 300 })
  })

  it('moves by its title bar and stays inside the workspace', () => {
    render(<Harness initial={['w1']} />)
    const head = windowOf('w1').querySelector('.fwin-head') as HTMLElement
    drag(head, { x: 500, y: 260 }, { x: 560, y: 300 })
    expect(rectOf('w1')).toMatchObject({ left: 460, top: 290 })

    drag(head, { x: 500, y: 300 }, { x: -3000, y: -3000 })
    expect(rectOf('w1')).toMatchObject({ left: 0, top: 0 })

    // The close button is a control: pressing it never starts a move.
    fireEvent.pointerDown(screen.getByRole('button', { name: 'Close window w1' }), { button: 0, pointerId: 3, clientX: 390, clientY: 10 })
    fireEvent.pointerMove(head, { pointerId: 3, clientX: 900, clientY: 500 })
    expect(rectOf('w1')).toMatchObject({ left: 0, top: 0 })
  })

  it('resizes from an edge and from a corner, and remembers the size for its kind', () => {
    render(<Harness initial={['w1']} />)
    drag(screen.getByRole('separator', { name: 'Resize the window w1 from the right' }), { x: 800, y: 400 }, { x: 900, y: 420 })
    expect(rectOf('w1')).toEqual({ left: 400, top: 250, width: 500, height: 300 })

    drag(screen.getByRole('separator', { name: 'Resize the window w1 from the top left' }), { x: 400, y: 250 }, { x: 350, y: 200 })
    expect(rectOf('w1')).toEqual({ left: 350, top: 200, width: 550, height: 350 })
    expect(readFloatingWindowSize('node')).toEqual({ width: 550, height: 350 })

    fireEvent.keyDown(screen.getByRole('separator', { name: 'Resize the window w1 from the bottom' }), { key: 'ArrowDown' })
    expect(rectOf('w1')).toEqual({ left: 350, top: 200, width: 550, height: 366 })
    expect(readFloatingWindowSize('node')).toEqual({ width: 550, height: 366 })

    fireEvent.click(screen.getByRole('button', { name: 'Open another' }))
    expect(rectOf('w2')).toMatchObject({ width: 550, height: 366 })
  })

  it('opens at the remembered size', () => {
    localStorage.setItem('archon.floatingWindowSize.v1', JSON.stringify({ version: 1, sizes: { node: { width: 640, height: 480 } } }))
    render(<Harness initial={['w1']} />)
    expect(rectOf('w1')).toEqual({ left: 280, top: 160, width: 640, height: 480 })
  })

  it('closes the focused window on Escape, and keeps keys other than undo from the view', () => {
    const viewKeys = vi.fn()
    window.addEventListener('keydown', viewKeys)
    try {
      render(<Harness initial={['w1', 'w2']} />)
      fireEvent.pointerDown(screen.getByText('Body of w1'), { button: 0, pointerId: 4 })
      fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'Delete' })
      expect(viewKeys).not.toHaveBeenCalled()
      // Undo is the one shortcut that still reaches the view.
      fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'z', ctrlKey: true })
      expect(viewKeys).toHaveBeenCalledTimes(1)
      fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'Escape' })
      expect(screen.queryByRole('dialog', { name: 'window w1' })).toBeNull()
      expect(windowOf('w2')).toBeInTheDocument()
      expect(viewKeys).toHaveBeenCalledTimes(1)
    } finally {
      window.removeEventListener('keydown', viewKeys)
    }
  })

  it('never lets a move or a resize cover the zoom column', () => {
    render(<Harness initial={['w1']} />)
    const head = windowOf('w1').querySelector('.fwin-head') as HTMLElement
    drag(head, { x: 500, y: 260 }, { x: 3000, y: 3000 })
    expect(rectOf('w1')).toEqual({ left: zoom.left - 400, top: 500, width: 400, height: 300 })

    drag(head, { x: 900, y: 510 }, { x: 700, y: 360 })
    expect(rectOf('w1')).toEqual({ left: 520, top: 350, width: 400, height: 300 })
    drag(screen.getByRole('separator', { name: 'Resize the window w1 from the right' }), { x: 920, y: 400 }, { x: 1190, y: 400 })
    expect(rectOf('w1')).toEqual({ left: 520, top: 350, width: zoom.left - 520, height: 300 })
  })

  it('holds open windows inside the viewport when it shrinks', () => {
    render(<Harness initial={['w1']} />)
    expect(rectOf('w1')).toEqual({ left: 400, top: 250, width: 400, height: 300 })
    const smallZoom = { left: 620, top: 260, width: 80, height: 240 }
    canvas = { bounds: { left: 0, top: 0, width: 700, height: 500 }, avoid: [smallZoom] }
    act(() => { window.dispatchEvent(new Event('resize')) })
    // Held at the new right edge, then stepped left of the zoom column.
    expect(rectOf('w1')).toEqual({ left: smallZoom.left - 400, top: 200, width: 400, height: 300 })
  })
})
