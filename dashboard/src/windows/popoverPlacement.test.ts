import { describe, expect, it } from 'vitest'
import { POPOVER_REACH, placePopover } from './popoverPlacement'
import { rectsOverlap, type WindowRect } from './windowGeometry'

const rect = (left: number, top: number, width: number, height: number): WindowRect => ({ left, top, width, height })
const bounds = rect(240, 98, 1672, 974)

describe('a popover beside what it belongs to', () => {
  it('opens right beside the slot\'s card when that side is free', () => {
    const card = rect(1254, 429, 208, 302)
    const slot = rect(1265, 559, 187, 41)
    const place = placePopover(400, 300, 220, { bounds, anchor: slot, home: card, obstacles: [card] })
    expect(place.covered).toBe(0)
    expect(place.rect.left).toBeGreaterThanOrEqual(card.left + card.width)
    expect(place.distance).toBeLessThan(30)
  })

  it('covers no card or note: a neighbour on the right sends it below its card, past the note under it', () => {
    const card = rect(490, 429, 195, 148)
    const slot = rect(497, 500, 181, 41)
    const neighbour = rect(745, 429, 195, 148)
    const note = rect(529, 581, 156, 59)
    const obstacles = [card, neighbour, note, rect(1000, 593, 195, 148)]
    const place = placePopover(400, 300, 220, { bounds: rect(240, 380, 1672, 692), anchor: slot, home: card, obstacles })
    expect(place.covered).toBe(0)
    for (const obstacle of [...obstacles, slot]) expect(rectsOverlap(place.rect, obstacle)).toBe(false)
    expect(place.rect.top).toBeGreaterThanOrEqual(note.top + note.height)
    expect(place.distance).toBeLessThanOrEqual(POPOVER_REACH)
  })

  it('shortens to stay near rather than opening far away', () => {
    const card = rect(490, 429, 195, 148)
    const slot = rect(497, 500, 181, 41)
    // Cards fill the row on both sides; room above the card is 290 high, and the next free room is far below.
    const row = [rect(240, 429, 240, 148), rect(695, 429, 1217, 148)]
    const place = placePopover(400, 330, 220, { bounds: rect(240, 131, 1672, 941), anchor: slot, home: card, obstacles: [card, ...row, rect(240, 590, 1672, 400)] })
    expect(place.covered).toBe(0)
    expect(place.rect.top + place.rect.height).toBeLessThanOrEqual(card.top)
    expect(place.rect.height).toBeLessThan(330)
    expect(place.distance).toBeLessThan(80)
  })

  it('drops down from a word over its own window, which hides the cards under it, but never over the word', () => {
    const window = rect(450, 652, 540, 420)
    const word = rect(865, 872, 46, 19)
    const hidden = rect(500, 700, 195, 148)
    const place = placePopover(400, 260, 200, { bounds, anchor: word, home: window, homeCovers: true, obstacles: [hidden, window] })
    expect(place.covered).toBe(0)
    expect(rectsOverlap(place.rect, word)).toBe(false)
    expect(place.distance).toBeLessThanOrEqual(8)
  })

  it('never opens beyond its reach: with no free room it takes the place within reach that covers least', () => {
    const slot = rect(900, 500, 180, 40)
    const crowd = [rect(240, 98, 1672, 400), rect(240, 560, 1672, 512), rect(240, 490, 640, 70), rect(1100, 490, 812, 70)]
    const place = placePopover(400, 300, 220, { bounds, anchor: slot, obstacles: crowd })
    expect(place.covered).toBeGreaterThan(0)
    expect(place.distance).toBeLessThanOrEqual(POPOVER_REACH)
  })

  it('is pure: the same scene gives the same place', () => {
    const scene = { bounds, anchor: rect(1265, 559, 187, 41), home: rect(1254, 429, 208, 302), obstacles: [rect(1527, 429, 195, 148)] }
    expect(placePopover(400, 300, 220, scene)).toEqual(placePopover(400, 300, 220, scene))
  })
})
