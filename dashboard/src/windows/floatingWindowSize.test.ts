import { afterEach, describe, expect, it } from 'vitest'
import {
  FLOATING_WINDOW_MINIMUM,
  clampFrameSize,
  clearFloatingWindowSize,
  readFloatingWindowSize,
  writeFloatingWindowSize,
} from './floatingWindowSize'

// Ported from CHROTE (chrote/dashboard/src/hooks/floatingWindowSize.test.ts).

const minimum = FLOATING_WINDOW_MINIMUM.node
const workspace = { width: 1280, height: 800 }

afterEach(() => {
  localStorage.clear()
})

describe('the floating window size rule', () => {
  it('holds a dragged size inside the workspace and above the minimum', () => {
    expect(clampFrameSize({ width: 4000, height: 4000 }, minimum, workspace)).toEqual(workspace)
    expect(clampFrameSize({ width: 10, height: 10 }, minimum, workspace)).toEqual(minimum)
    expect(clampFrameSize({ width: 600, height: 400 }, minimum, workspace)).toEqual({ width: 600, height: 400 })
  })

  it('keeps the minimum when the workspace itself is smaller than it', () => {
    expect(clampFrameSize({ width: 500, height: 500 }, minimum, { width: 100, height: 60 })).toEqual(minimum)
  })
})

describe('the remembered size', () => {
  it('is nothing until a window is resized, is kept per kind, and can be cleared', () => {
    expect(readFloatingWindowSize('node')).toBeNull()

    writeFloatingWindowSize('node', { width: 900.4, height: 500.6 })
    expect(readFloatingWindowSize('node')).toEqual({ width: 900, height: 501 })
    expect(readFloatingWindowSize('peek')).toBeNull()

    writeFloatingWindowSize('peek', { width: 700, height: 400 })
    clearFloatingWindowSize('node')
    expect(readFloatingWindowSize('node')).toBeNull()
    expect(readFloatingWindowSize('peek')).toEqual({ width: 700, height: 400 })
  })

  it('ignores a stored value that is not a size, rather than opening a broken window', () => {
    localStorage.setItem('archon.floatingWindowSize.v1', JSON.stringify({
      version: 1,
      sizes: { node: { width: 'wide', height: 200 }, peek: { width: 0, height: 0 } },
    }))
    expect(readFloatingWindowSize('node')).toBeNull()
    expect(readFloatingWindowSize('peek')).toBeNull()
  })
})
