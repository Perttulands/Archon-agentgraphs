import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { Profiler, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import FloatingWindow from './FloatingWindow'
import FloatingFrameHandles from './FloatingFrameHandles'
import { useFloatingWindow } from './useFloatingWindow'
import { WindowManagerProvider, useWindowManager } from './WindowManager'
import { readFloatingWindowSize } from './floatingWindowSize'
import { rectsOverlap, type WindowRect, type Workspace } from './windowGeometry'

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
      <button type="button" onClick={stack.reflow}>Reflow</button>
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
  it('opens centred, opens the next window clear of it, and raises a window when it is focused', () => {
    render(<Harness initial={['w1']} />)
    expect(rectOf('w1')).toEqual({ left: 400, top: 250, width: 400, height: 300 })
    expect(windowOf('w1')).toHaveFocus()

    fireEvent.click(screen.getByRole('button', { name: 'Open another' }))
    expect(rectOf('w2')).toMatchObject({ width: 400, height: 300 })
    expect(rectsOverlap(rectOf('w1'), rectOf('w2'))).toBe(false)
    expect(zOf('w2')).toBeGreaterThan(zOf('w1'))
    expect(windowOf('w2')).toHaveClass('focused')

    fireEvent.pointerDown(screen.getByText('Body of w1'), { button: 0, pointerId: 2 })
    expect(zOf('w1')).toBeGreaterThan(zOf('w2'))
    expect(windowOf('w1')).toHaveClass('focused')
    expect(windowOf('w1')).toHaveFocus()
  })

  it('opens windows that open in the same render clear of one another', () => {
    render(<Harness initial={['w1', 'w2', 'w3']} />)
    expect(rectOf('w1')).toEqual({ left: 400, top: 250, width: 400, height: 300 })
    expect(rectsOverlap(rectOf('w1'), rectOf('w2'))).toBe(false)
    expect(rectsOverlap(rectOf('w3'), rectOf('w1')) || rectsOverlap(rectOf('w3'), rectOf('w2'))).toBe(false)
    expect(zOf('w3')).toBeGreaterThan(zOf('w2'))
    expect(zOf('w2')).toBeGreaterThan(zOf('w1'))
  })

  it('opens beside its anchor without covering it, and near the centre when the anchor is out of view', () => {
    const anchors = {
      w1: { left: 100, top: 120, width: 200, height: 100 },
      w2: { left: 900, top: 100, width: 200, height: 100 },
      w3: { left: -500, top: 100, width: 200, height: 100 },
    }
    render(<Harness initial={['w1']} anchors={anchors} />)
    const w1 = rectOf('w1')
    expect(rectsOverlap(w1, anchors.w1)).toBe(false)
    expect(Math.max(0, w1.top - 220, 120 - (w1.top + w1.height), w1.left - 300, 100 - (w1.left + w1.width))).toBeLessThanOrEqual(12)

    fireEvent.click(screen.getByRole('button', { name: 'Open another' }))
    expect(rectsOverlap(rectOf('w2'), anchors.w2)).toBe(false)
    expect(rectsOverlap(rectOf('w2'), w1)).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Open another' }))
    const w3 = rectOf('w3')
    expect(rectsOverlap(w3, w1) || rectsOverlap(w3, rectOf('w2'))).toBe(false)
    expect(w3.left).toBeGreaterThan(0)
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

    // The next window of the kind opens at the remembered size where there is room for it.
    fireEvent.click(screen.getByRole('button', { name: 'Close window w1' }))
    fireEvent.click(screen.getByRole('button', { name: 'Open another' }))
    expect(rectOf('w1')).toMatchObject({ width: 550, height: 366 })
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

  it('moves open windows back inside the workspace when it changes without the page resizing', () => {
    render(<Harness initial={['w1']} />)
    expect(rectOf('w1')).toEqual({ left: 400, top: 250, width: 400, height: 300 })
    // The run bar appears over the canvas's top 300 pixels.
    canvas = { bounds: { left: 0, top: 300, width: 1200, height: 500 }, avoid: [zoom] }
    fireEvent.click(screen.getByRole('button', { name: 'Reflow' }))
    expect(rectOf('w1')).toEqual({ left: 400, top: 300, width: 400, height: 300 })
  })

  it('re-renders nothing when a reflow finds every window already inside', () => {
    let commits = 0
    function Counted() {
      const stack = useWindowManager(workspace)
      return (
        <>
          <button type="button" onClick={stack.reflow}>Reflow</button>
          <WindowManagerProvider stack={stack}>
            <Profiler id="w1" onRender={() => { commits += 1 }}>
              <FloatingWindow id="w1" kind="node" title="Window w1" label="window w1" defaultSize={{ width: 400, height: 300 }} onClose={() => {}}>
                <p>Body of w1</p>
              </FloatingWindow>
            </Profiler>
          </WindowManagerProvider>
        </>
      )
    }
    render(<Counted />)
    // React may render once before it bails out of an unchanged state; after that, frames cost nothing.
    fireEvent.click(screen.getByRole('button', { name: 'Reflow' }))
    const before = commits
    for (let frame = 0; frame < 5; frame += 1) fireEvent.click(screen.getByRole('button', { name: 'Reflow' }))
    expect(commits).toBe(before)
    // A reflow that does move the window still renders it.
    canvas = { bounds: { left: 0, top: 300, width: 1200, height: 500 }, avoid: [zoom] }
    fireEvent.click(screen.getByRole('button', { name: 'Reflow' }))
    expect(commits).toBeGreaterThan(before)
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

  // CHROTE's Peek wraps its window around the grid its font fit drew
  // (chrote-8eyu), until the operator sizes it; then the operator's size wins.
  it('wraps a window around its content until the operator sizes one of its kind', () => {
    let fit: (size: { width: number; height: number }) => void = () => {}
    function Fitted() {
      const win = useFloatingWindow<HTMLElement>({ id: 'fit', kind: 'peek', label: 'fitted', defaultSize: { width: 1000, height: 700 }, onClose: () => {} })
      fit = win.fitTo
      return <section {...win.rootProps} role="dialog" aria-label="fitted"><FloatingFrameHandles handles={win.handles} activeHandle={win.activeHandle} /></section>
    }
    function FittedHarness() {
      const stack = useWindowManager(workspace)
      return <WindowManagerProvider stack={stack}><Fitted /></WindowManagerProvider>
    }
    const { unmount } = render(<FittedHarness />)
    const size = () => { const { width, height } = screen.getByRole('dialog', { name: 'fitted' }).style; return [parseFloat(width), parseFloat(height)] }
    const left = () => parseFloat(screen.getByRole('dialog', { name: 'fitted' }).style.left)
    const before = left()

    act(() => fit({ width: 900, height: 600 }))
    expect(size()).toEqual([900, 600])
    expect(left()).toBe(before)

    fireEvent.keyDown(screen.getByRole('separator', { name: 'Resize the fitted from the bottom' }), { key: 'ArrowDown' })
    const sized = size()
    act(() => fit({ width: 700, height: 500 }))
    expect(size()).toEqual(sized)

    // The remembered size is the operator's from the next window's first paint.
    unmount()
    render(<FittedHarness />)
    act(() => fit({ width: 700, height: 500 }))
    expect(size()).toEqual(sized)
  })
})
