import { afterEach, describe, expect, it } from 'vitest'
import { attemptsAnchor } from './attemptsPlacement'
import { attemptsWindowSize } from './AttemptsWindow'
import { placeOpeningWindow } from '../windows/windowPlacement'
import { openingWindowMinimum } from '../windows/floatingWindowSize'
import { rectsOverlap, type WindowRect } from '../windows/windowGeometry'

function place(element: Element, box: WindowRect) {
  element.getBoundingClientRect = () => ({ ...box, x: box.left, y: box.top, right: box.left + box.width, bottom: box.top + box.height, toJSON: () => ({}) })
}

afterEach(() => { document.body.innerHTML = '' })

describe('attempts opening placement', () => {
  it('keeps a readable strip beside the replay status, covering other Flow content before shrinking', () => {
    const width = 1920, height = 1080
    const status = { left: 1393, top: 362, width: 240, height: 213 }
    const gateStatus = { left: 1393, top: 608, width: 240, height: 224 }
    const verdict = { left: 549, top: 719, width: 828, height: 18 }
    const handles = [
      { left: 547, top: 362, width: 24, height: 24 },
      { left: 581, top: 365, width: 152, height: 18 },
      { left: 547, top: 433, width: 72, height: 17 },
      { left: 547, top: 456, width: 72, height: 17 },
      { left: 627, top: 455, width: 224, height: 18 },
      { left: 547, top: 479, width: 72, height: 17 },
      { left: 627, top: 478, width: 217, height: 18 },
    ]
    const window = placeOpeningWindow(attemptsWindowSize(3, { width: 1920, height: 1080 }), openingWindowMinimum('attempts', { width, height }), {
      workspace: { bounds: { left: 268, top: 85, width: 1644, height: 987 }, avoid: [] },
      anchorFirst: true, anchor: status, keepClear: [gateStatus, verdict], landmarks: handles,
    })
    for (const protectedBox of [status, gateStatus]) expect(rectsOverlap(window, protectedBox)).toBe(false)
    expect(window.width).toBeGreaterThanOrEqual(560)
    expect(window.height).toBeGreaterThanOrEqual(Math.ceil(height * .7))
  })

  it('leaves the whole Flow status visible, including state and produced files', () => {
    document.body.innerHTML = '<div data-testid="flow-view"><li class="flow-step" data-flow-node="replay"><div class="flow-step-head"><button class="flow-title">Replay</button></div><div class="flow-status"><span class="flow-state">done</span><button class="attempts-open">1 attempt</button><button>replay/</button></div></li></div>'
    const title = { left: 580, top: 380, width: 200, height: 20 }
    const status = { left: 1393, top: 367, width: 240, height: 150 }
    place(document.querySelector('.flow-title')!, title)
    place(document.querySelector('.flow-status')!, status)
    place(document.querySelector('.attempts-open')!, { left: 1408, top: 405, width: 70, height: 18 })
    expect(attemptsAnchor(document.querySelector('.attempts-open')!)).toEqual({ anchor: status, keepClear: [], anchorKind: 'node' })
  })

  for (const [width, height] of [[1920, 1080], [2560, 1440]]) {
    it(`fits beside its opening card at ${width}`, () => {
      const card = { left: width * .39, top: 254, width: 352, height: 342 }
      const bounds = { left: 268, top: 85, width: width - 276, height: height - 170 }
      const window = placeOpeningWindow(attemptsWindowSize(3, { width, height }), openingWindowMinimum('attempts', { width, height }), {
        workspace: { bounds, avoid: [] }, anchor: card, keepClear: [card], landmarks: [card],
      })
      expect(rectsOverlap(window, card)).toBe(false)
      expect(window.left).toBeGreaterThanOrEqual(bounds.left)
      expect(window.top).toBeGreaterThanOrEqual(bounds.top)
      expect(window.left + window.width).toBeLessThanOrEqual(bounds.left + bounds.width)
      expect(window.top + window.height).toBeLessThanOrEqual(bounds.top + bounds.height)
      expect(window.width).toBeGreaterThanOrEqual(560)
      expect(window.height).toBeGreaterThanOrEqual(Math.ceil(height * .7))
    })

    it(`keeps its opening Flow status readable at ${width}`, () => {
      const status = { left: (width + 260) / 2 + 303, top: 367, width: 240, height: 150 }
      const bounds = { left: 268, top: 85, width: width - 276, height: height - 100 }
      const window = placeOpeningWindow(attemptsWindowSize(1, { width, height }), openingWindowMinimum('attempts', { width, height }), {
        workspace: { bounds, avoid: [] }, anchor: status, keepClear: [status],
      })
      expect(rectsOverlap(window, status)).toBe(false)
    })
  }
})
