import { describe, expect, it } from 'vitest'
import { tetherPath } from './Tether'

const box = (left: number, top: number, width: number, height: number) => ({ left, top, right: left + width, bottom: top + height })

describe('the tether from a slot to its window or note', () => {
  it('runs from the slot\'s facing side to the popover beside it, bending once when the rows differ', () => {
    const path = tetherPath(box(1265, 559, 187, 41), box(1467, 600, 440, 200))!
    expect(path.from).toEqual([1452, 579.5])
    expect(path.to).toEqual([1467, 612])
    expect(path.d).toBe('M1452,579.5 H1459.5 V612 H1467')
  })

  it('drops straight down to a popover below that overlaps it across', () => {
    const path = tetherPath(box(279, 739, 181, 41), box(279, 821, 440, 200))!
    expect(path.d).toBe('M291,780 V821')
  })

  it('bends once to a popover above and to one side, meeting it on its near edge', () => {
    const path = tetherPath(box(497, 466, 181, 41), box(686, 331, 360, 59))!
    expect(path.from).toEqual([678, 486.5])
    expect(path.to).toEqual([686, 378])
  })

  it('bends once to a popover above that sits beyond its ends across', () => {
    const path = tetherPath(box(497, 466, 100, 41), box(480, 300, 10, 60))
    expect(path).not.toBeNull()
  })

  it('draws nothing when the popover touches or covers its slot', () => {
    expect(tetherPath(box(100, 100, 50, 20), box(150, 100, 200, 100))).toBeNull()
    expect(tetherPath(box(100, 100, 50, 20), box(90, 90, 200, 100))).toBeNull()
  })
})
