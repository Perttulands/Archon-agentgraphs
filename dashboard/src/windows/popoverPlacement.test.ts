import { describe, expect, it } from 'vitest'
import { POPOVER_REACH, placePopover } from './popoverPlacement'
import { rectsOverlap, type WindowRect } from './windowGeometry'

const rect = (left: number, top: number, width: number, height: number): WindowRect => ({ left, top, width, height })
const bounds = rect(240, 98, 1672, 974)
const size = { width: 400, openHeight: 140, height: 300, minHeight: 200 }
const free = (place: { rect: WindowRect }, obstacles: WindowRect[]) => obstacles.every(obstacle => !rectsOverlap(place.rect, obstacle))

describe('a popover beside what it belongs to', () => {
  it('opens right beside the slot\'s card when that side is free, with room for its whole list', () => {
    const card = rect(1254, 429, 208, 302)
    const slot = rect(1265, 559, 187, 41)
    const place = placePopover(size, { bounds, anchor: slot, home: card, obstacles: [card] })
    expect(place.covered).toBe(0)
    expect(place.rect.left).toBeGreaterThanOrEqual(card.left + card.width)
    expect(place.rect.height).toBe(300)
    expect(place.distance).toBeLessThan(30)
  })

  it('covers no card or note: a neighbour on the right sends it below its card, past the note under it', () => {
    const card = rect(490, 429, 195, 148)
    const slot = rect(497, 500, 181, 41)
    const neighbour = rect(745, 429, 195, 148)
    const note = rect(529, 581, 156, 59)
    const obstacles = [card, neighbour, note, rect(1000, 593, 195, 148)]
    const place = placePopover(size, { bounds: rect(240, 380, 1672, 692), anchor: slot, home: card, obstacles })
    expect(place.covered).toBe(0)
    expect(free(place, [...obstacles, slot])).toBe(true)
    expect(place.rect.top).toBeGreaterThanOrEqual(note.top + note.height)
    expect(place.distance).toBeLessThanOrEqual(POPOVER_REACH)
  })

  // archon-o7p.17 rv-slots4: three new formations in a row at the 1920 fit, operator notes under the cards
  // above. There is no room for a full list anywhere near a slot, so each sentence opens beside its own slot
  // at its real size, in the free room below the row, and its list scrolls within it.
  it('opens beside its slot in a crowded row, covering nothing, with its list in the room actually free', () => {
    const canvas = rect(244, 138, 1668, 934)
    const row = [rect(272, 740, 195, 148), rect(490, 740, 195, 148), rect(708, 740, 195, 148)]
    const above = [rect(272, 449, 153, 102), rect(490, 449, 195, 148), rect(745, 449, 195, 148), rect(1000, 613, 195, 148), rect(1254, 449, 208, 302), rect(1527, 449, 195, 148), rect(1000, 449, 184, 56)]
    const notes = [rect(272, 555, 156, 69), rect(529, 601, 156, 69), rect(784, 601, 156, 69), rect(1039, 765, 156, 69), rect(1306, 755, 156, 69), rect(1566, 601, 156, 69), rect(1028, 509, 156, 69)]
    const obstacles = [...row, ...above, ...notes]
    for (const [index, card] of row.entries()) {
      const slot = rect(card.left + 7, 812, 181, 41)
      const place = placePopover(size, { bounds: canvas, anchor: slot, home: card, obstacles })
      expect(place.covered, `slot ${index + 1}`).toBe(0)
      expect(free(place, [...obstacles, slot]), `slot ${index + 1}`).toBe(true)
      expect(place.distance, `slot ${index + 1}`).toBeLessThanOrEqual(60)
      expect(place.rect.height).toBeGreaterThanOrEqual(size.openHeight)
    }
  })

  it('shortens its room to stay near rather than opening far away, and above its anchor grows upward', () => {
    const card = rect(490, 429, 195, 148)
    const slot = rect(497, 500, 181, 41)
    // Cards fill the row on both sides; room above the card is 290 high, and the next free room is far below.
    const row = [rect(240, 429, 240, 148), rect(695, 429, 1217, 148)]
    const place = placePopover(size, { bounds: rect(240, 131, 1672, 941), anchor: slot, home: card, obstacles: [card, ...row, rect(240, 590, 1672, 400)] })
    expect(place.covered).toBe(0)
    expect(place.growsUp).toBe(true)
    expect(place.rect.top + place.rect.height).toBeLessThanOrEqual(card.top)
    expect(place.rect.height).toBeLessThanOrEqual(300)
    expect(place.distance).toBeLessThan(80)
  })

  it('drops down from a word over its own window, which hides the cards under it, but never over the word', () => {
    const window = rect(450, 652, 540, 420)
    const word = rect(865, 872, 46, 19)
    const hidden = rect(500, 700, 195, 148)
    const place = placePopover({ ...size, height: 260 }, { bounds, anchor: word, home: window, homeCovers: true, obstacles: [hidden, window] })
    expect(place.covered).toBe(0)
    expect(rectsOverlap(place.rect, word)).toBe(false)
    expect(place.distance).toBeLessThanOrEqual(8)
  })

  it('never opens beyond its reach: with no free room it takes the place within reach that covers least', () => {
    const slot = rect(900, 500, 180, 40)
    const crowd = [rect(240, 98, 1672, 400), rect(240, 560, 1672, 512), rect(240, 490, 640, 70), rect(1100, 490, 812, 70)]
    const place = placePopover(size, { bounds, anchor: slot, obstacles: crowd })
    expect(place.covered).toBeGreaterThan(0)
    expect(place.distance).toBeLessThanOrEqual(POPOVER_REACH)
  })

  it('is pure: the same scene gives the same place', () => {
    const scene = { bounds, anchor: rect(1265, 559, 187, 41), home: rect(1254, 429, 208, 302), obstacles: [rect(1527, 429, 195, 148)] }
    expect(placePopover(size, scene)).toEqual(placePopover(size, scene))
  })
})
