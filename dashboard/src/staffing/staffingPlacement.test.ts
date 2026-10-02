import { describe, expect, it } from 'vitest'
import { DROP_GAP, placeDrop } from './staffingPlacement'

const rect = (left: number, top: number, width: number, height: number) => ({ left, top, width, height })
const bounds = rect(240, 100, 1672, 960)
const size = { width: 440, height: 280 }

describe('placeDrop: the sentence opens like a dropdown on its row', () => {
  it('opens directly below the row, left-aligned with it, when it fits', () => {
    expect(placeDrop(rect(500, 300, 280, 50), size, bounds, true)).toEqual({ left: 500, top: 300 + 50 + DROP_GAP, height: 280, above: false, pan: 0 })
  })

  it('pans up by exactly the room the window needs below a row near the bottom', () => {
    const row = rect(500, 900, 280, 50)
    const place = placeDrop(row, size, bounds, true)
    const missing = row.top + row.height + DROP_GAP + size.height - (bounds.top + bounds.height)
    expect(place).toEqual({ left: 500, top: 954 - missing, height: 280, above: false, pan: -missing })
    expect(place.top + place.height).toBe(bounds.top + bounds.height)
  })

  it('opens directly above where it cannot pan, as in a node window', () => {
    const word = rect(600, 950, 60, 18)
    expect(placeDrop(word, size, bounds, false)).toEqual({ left: 600, top: 950 - DROP_GAP - 280, height: 280, above: true, pan: 0 })
  })

  it('stays inside the bounds across, under a row at the right edge, and never covers the row', () => {
    const row = rect(1700, 300, 280, 50)
    const place = placeDrop(row, size, bounds, true)
    expect(place.left).toBe(240 + 1672 - 440)
    expect(place.top).toBeGreaterThanOrEqual(row.top + row.height)
  })

  it('takes the roomier side, that much shorter, where neither side holds it', () => {
    const small = rect(0, 0, 800, 400)
    const place = placeDrop(rect(10, 150, 100, 20), size, small, false)
    expect(place).toEqual({ left: 10, top: 174, height: 226, above: false, pan: 0 })
  })
})
