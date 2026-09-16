import { describe, expect, it } from 'vitest'
import { CASCADE_STEP, keepInWorkspace, moveWindowRect, placeBeside, placeWindow, rectsOverlap, resizeWindowRect, type Workspace } from './windowGeometry'

const minimum = { width: 200, height: 100 }
const zoom = { left: 1120, top: 560, width: 80, height: 240 }
const workspace: Workspace = { bounds: { left: 0, top: 0, width: 1200, height: 800 }, avoid: [zoom] }

describe('window geometry', () => {
  it('centres a new window and steps each later one down and right', () => {
    expect(placeWindow({ width: 400, height: 300 }, workspace, minimum, 0)).toEqual({ left: 400, top: 250, width: 400, height: 300 })
    expect(placeWindow({ width: 400, height: 300 }, workspace, minimum, 2))
      .toEqual({ left: 400 + 2 * CASCADE_STEP, top: 250 + 2 * CASCADE_STEP, width: 400, height: 300 })
  })

  it('fits a window larger than the workspace and never below the minimum', () => {
    expect(placeWindow({ width: 5000, height: 5000 }, { ...workspace, avoid: [] }, minimum, 0)).toEqual({ left: 0, top: 0, width: 1200, height: 800 })
    expect(placeWindow({ width: 10, height: 10 }, workspace, minimum, 0)).toEqual({ left: 500, top: 350, width: 200, height: 100 })
  })

  it('opens a window beside its anchor, trying right, left, below and above in turn', () => {
    const size = { width: 400, height: 300 }
    expect(placeBeside(size, { left: 100, top: 120, width: 200, height: 100 }, workspace, minimum)).toEqual({ left: 312, top: 120, width: 400, height: 300 })
    // Too near the right edge: held inside, the right side would cover the anchor.
    expect(placeBeside(size, { left: 900, top: 100, width: 200, height: 100 }, workspace, minimum)).toEqual({ left: 488, top: 100, width: 400, height: 300 })
    expect(placeBeside(size, { left: 300, top: 100, width: 600, height: 120 }, workspace, minimum)).toEqual({ left: 300, top: 232, width: 400, height: 300 })
    expect(placeBeside(size, { left: 300, top: 560, width: 600, height: 200 }, workspace, minimum)).toEqual({ left: 300, top: 248, width: 400, height: 300 })
  })

  it('keeps a window placed beside its anchor off avoided zones and off the anchor', () => {
    // Right of the anchor meets the zoom column; stepping clear of it would cover the anchor, so the window goes left.
    const beside = placeBeside({ width: 400, height: 300 }, { left: 650, top: 500, width: 100, height: 100 }, workspace, minimum)
    expect(beside).toEqual({ left: 238, top: 500, width: 400, height: 300 })
  })

  it('opens a window below an anchor in a bar along the workspace\'s top edge', () => {
    const canvas: Workspace = { ...workspace, bounds: { left: 0, top: 60, width: 1200, height: 740 } }
    // A run bar chip just above the canvas: the side facing the canvas comes first.
    expect(placeBeside({ width: 400, height: 300 }, { left: 500, top: 20, width: 90, height: 24 }, canvas, minimum)).toEqual({ left: 500, top: 60, width: 400, height: 300 })
    expect(placeBeside({ width: 400, height: 300 }, { left: 500, top: -200, width: 90, height: 24 }, canvas, minimum)).toBeNull()
  })

  it('places nothing beside an anchor out of view, an empty one, or one no side can leave uncovered', () => {
    expect(placeBeside({ width: 400, height: 300 }, { left: 100, top: 120, width: 0, height: 0 }, workspace, minimum)).toBeNull()
    expect(placeBeside({ width: 400, height: 300 }, { left: -500, top: 100, width: 200, height: 100 }, workspace, minimum)).toBeNull()
    expect(placeBeside({ width: 400, height: 300 }, { left: 0, top: 0, width: 1200, height: 800 }, workspace, minimum)).toBeNull()
  })

  it('holds a moved window inside the workspace and steps it off an avoided zone the shortest way', () => {
    const start = { left: 400, top: 250, width: 400, height: 300 }
    expect(moveWindowRect(start, -2000, -2000, workspace, minimum)).toEqual({ left: 0, top: 0, width: 400, height: 300 })
    const pushed = moveWindowRect(start, 2000, 2000, workspace, minimum)
    expect(rectsOverlap(pushed, zoom)).toBe(false)
    // Held at the bottom right, the window is 80px from clearing the zoom column sideways and 240px upward.
    expect(pushed).toEqual({ left: zoom.left - 400, top: 500, width: 400, height: 300 })
  })

  it('narrows a window that cannot move clear of a zone', () => {
    const full = keepInWorkspace({ left: 0, top: 0, width: 1200, height: 800 }, workspace, minimum)
    expect(rectsOverlap(full, zoom)).toBe(false)
    expect(full).toEqual({ left: 0, top: 0, width: zoom.left, height: 800 })
  })

  it('moves only the edges a handle holds', () => {
    const start = { left: 400, top: 250, width: 400, height: 300 }
    expect(resizeWindowRect(start, { x: 1, y: 0 }, 100, 50, workspace, minimum)).toEqual({ left: 400, top: 250, width: 500, height: 300 })
    expect(resizeWindowRect(start, { x: -1, y: -1 }, -50, -40, workspace, minimum)).toEqual({ left: 350, top: 210, width: 450, height: 340 })
    expect(resizeWindowRect(start, { x: -1, y: 0 }, 1000, 0, workspace, minimum)).toEqual({ left: 600, top: 250, width: 200, height: 300 })
    expect(resizeWindowRect(start, { x: 0, y: 1 }, 0, 5000, workspace, minimum)).toEqual({ left: 400, top: 250, width: 400, height: 550 })
  })

  it('stops a resized edge at an avoided zone', () => {
    const start = { left: 600, top: 400, width: 400, height: 300 }
    expect(resizeWindowRect(start, { x: 1, y: 0 }, 400, 0, workspace, minimum)).toEqual({ left: 600, top: 400, width: zoom.left - 600, height: 300 })
    // A corner keeps whichever of its edges leaves the larger window.
    expect(resizeWindowRect(start, { x: 1, y: 1 }, 400, 400, workspace, minimum)).toEqual({ left: 600, top: 400, width: zoom.left - 600, height: 400 })
  })
})
