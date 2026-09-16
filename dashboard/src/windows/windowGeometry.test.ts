import { describe, expect, it } from 'vitest'
import { CASCADE_STEP, keepInWorkspace, moveWindowRect, placeWindow, rectsOverlap, resizeWindowRect, type Workspace } from './windowGeometry'

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
