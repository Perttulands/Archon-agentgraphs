import { describe, expect, it } from 'vitest'
import {
  LOOP_CHANNEL_GAP,
  clearLaneY,
  gateWireNeedsLabel,
  loopChannelSegments,
  loopConnectionIds,
  pathIntersectsRect,
  roundedOrthoPath,
  routeFormationWire,
  routeJudgeWire,
  routeLoopWires,
  routeOrthoWire,
  segmentHitsRect,
  segmentsShareChannel,
} from './formationsRouting'

describe('formationsRouting', () => {
  it('routes around an obstacle instead of under a card', () => {
    const obstacle = { id: 'middle', x: 90, y: 20, width: 60, height: 80 }
    const route = routeFormationWire(
      { x: 20, y: 60 },
      { x: 220, y: 60 },
      { fromId: 'source', toId: 'target', obstacles: [obstacle] }
    )

    expect(route.path).toMatch(/^M/)
    expect(pathIntersectsRect(route.points, obstacle, 6)).toBe(false)
  })

  it('freezes direct routing while a connected node is being dragged', () => {
    const obstacle = { id: 'middle', x: 90, y: 20, width: 60, height: 80 }
    const route = routeFormationWire(
      { x: 20, y: 60 },
      { x: 220, y: 60 },
      { fromId: 'source', toId: 'target', draggingNodeId: 'source', obstacles: [obstacle] }
    )

    expect(route.points).toEqual([{ x: 20, y: 60 }, { x: 220, y: 60 }])
  })
})

/* Reference contract: 03-formations.js routeOrtho/roundedOrtho/clearLaneY/routeJudge.
   These tests pin the prototype's committed-wire geometry: orthogonal stub-routed
   elbows with rounded corners, not free-form Beziers. */
describe('roundedOrthoPath (reference roundedOrtho)', () => {
  it('renders two points as a plain line', () => {
    expect(roundedOrthoPath([{ x: 0, y: 0 }, { x: 100, y: 0 }], 10)).toBe('M0,0 L100,0')
  })

  it('rounds 90-degree corners with quadratic curves', () => {
    const d = roundedOrthoPath([{ x: 0, y: 0 }, { x: 50, y: 0 }, { x: 50, y: 40 }], 10)
    expect(d).toBe('M0,0 L40,0 Q50,0 50,10 L50,40')
  })

  it('drops duplicate consecutive points before routing', () => {
    const d = roundedOrthoPath([{ x: 0, y: 0 }, { x: 0.2, y: 0.1 }, { x: 60, y: 0 }], 10)
    expect(d).toBe('M0,0 L60,0')
  })

  it('shrinks each corner side independently on short segments (reference d1/d2)', () => {
    const d = roundedOrthoPath([{ x: 0, y: 0 }, { x: 8, y: 0 }, { x: 8, y: 40 }], 10)
    expect(d).toBe('M0,0 L4,0 Q8,0 8,10 L8,40')
  })
})

describe('segmentHitsRect (reference Liang-Barsky segHitsRect)', () => {
  const rect = { id: 'card', x: 50, y: 50, width: 40, height: 40 }
  it('detects a crossing segment with the 6px inflation margin', () => {
    expect(segmentHitsRect({ x: 0, y: 70 }, { x: 200, y: 70 }, rect, 6)).toBe(true)
    expect(segmentHitsRect({ x: 0, y: 45 }, { x: 200, y: 45 }, rect, 6)).toBe(true)
  })
  it('clears a segment outside the inflated rect', () => {
    expect(segmentHitsRect({ x: 0, y: 40 }, { x: 200, y: 40 }, rect, 6)).toBe(false)
  })
})

describe('clearLaneY (reference lane clearance)', () => {
  it('returns the preferred lane when nothing blocks the span', () => {
    expect(clearLaneY([], 0, 200, 60)).toBe(60)
  })
  it('picks a lane 32px clear of blocking cards, nearer side wins', () => {
    const blockers = [{ id: 'b', x: 80, y: 50, width: 60, height: 40 }]
    expect(clearLaneY(blockers, 0, 200, 55)).toBe(18) // above = 50-32
    expect(clearLaneY(blockers, 0, 200, 100)).toBe(122) // below = 90+32
  })
})

describe('routeOrthoWire (reference routeOrtho)', () => {
  it('draws a dead-straight line for level unobstructed endpoints', () => {
    const d = routeOrthoWire({ x: 0, y: 60 }, { x: 200, y: 60 }, { fromId: 'a', toId: 'b', obstacles: [] })
    expect(d).toBe('M0,60 L200,60')
  })

  it('routes forward wires through a vertical mid-split with rounded corners', () => {
    const d = routeOrthoWire({ x: 0, y: 0 }, { x: 200, y: 100 }, { fromId: 'a', toId: 'b', obstacles: [] })
    // stubs at x=24 / x=176, midX = 100; corners rounded → contains Q segments, no C beziers
    expect(d).toContain('Q')
    expect(d).not.toContain('C')
    expect(d.startsWith('M0,0 L')).toBe(true)
    expect(d).toContain('100')
  })

  it('honors a hand-routed lane Y over everything else', () => {
    const d = routeOrthoWire({ x: 0, y: 0 }, { x: 200, y: 0 }, { fromId: 'a', toId: 'b', obstacles: [], laneY: 140 })
    expect(d).toContain('140')
  })

  it('routes genuinely-backward targets through a clearance lane, not an S-curve', () => {
    const d = routeOrthoWire({ x: 200, y: 60 }, { x: 0, y: 60 }, { fromId: 'a', toId: 'b', obstacles: [] })
    expect(d).not.toContain('C')
    // must leave the level band via a lane rather than connecting directly
    expect(d).not.toBe('M200,60 L0,60')
  })

  it('detours via a clearance lane around an obstructing card', () => {
    const obstacle = { id: 'mid', x: 80, y: 30, width: 60, height: 60 }
    const d = routeOrthoWire({ x: 0, y: 60 }, { x: 220, y: 60 }, { fromId: 'a', toId: 'b', obstacles: [obstacle] })
    expect(d).not.toBe('M0,60 L220,60')
    // lane must clear the card: either above 30-32=-2 or below 90+32=122
    expect(/(-2|122)/.test(d)).toBe(true)
  })

  it('skips obstacle detours while a node is dragged (frozen)', () => {
    const obstacle = { id: 'mid', x: 80, y: 30, width: 60, height: 60 }
    const d = routeOrthoWire({ x: 0, y: 60 }, { x: 220, y: 60 }, { fromId: 'a', toId: 'b', obstacles: [obstacle], frozen: true })
    expect(d).toBe('M0,60 L220,60')
  })
})

describe('routeJudgeWire (reference routeJudge brackets)', () => {
  it('routes a send wire as a bracket rising 26px above the gate socket', () => {
    const d = routeJudgeWire({ x: 300, y: 200 }, { x: 100, y: 120 }, {
      direction: 'send',
      nodeRect: { id: 'judge', x: 130, y: 100, width: 200, height: 80 },
    })
    expect(d.startsWith('M300,200')).toBe(true)
    expect(d).toContain('174') // riseY = 200 - 26
    expect(d).toContain('100') // approach lane x = 130 - 30
  })

  it('routes a return wire around the source card side into the socket top', () => {
    const d = routeJudgeWire({ x: 330, y: 140 }, { x: 300, y: 200 }, {
      direction: 'return',
      nodeRect: { id: 'judge', x: 130, y: 100, width: 200, height: 80 },
    })
    expect(d.startsWith('M330,140')).toBe(true)
    expect(d).toContain('360') // Rx = 130 + 200 + 30
    expect(d).toContain('174') // riseY = 200 - 26
  })
})

describe('loop wires', () => {
  // Wayfinding after Arrange: one row of cards, two gates failing back to Draft the brief.
  const connections = [
    { id: 'start', from: 'mis:out', to: 'map:in' },
    { id: 'map-framing', from: 'map:out', to: 'framing:in' },
    { id: 'framing-pass', from: 'framing:pass', to: 'peers:in' },
    { id: 'framing-fail', from: 'framing:fail', to: 'map:in' },
    { id: 'peers-answer', from: 'peers:out', to: 'answer:in' },
    { id: 'answer-pass', from: 'answer:pass', to: 'draft:in' },
    { id: 'answer-fail', from: 'answer:fail', to: 'peers:in' },
    { id: 'draft-adversarial', from: 'draft:out', to: 'adversarial:in' },
    { id: 'judge-send', from: 'adversarial:judge', to: 'critic:in' },
    { id: 'judge-return', from: 'critic:out', to: 'adversarial:judge' },
    { id: 'adversarial-pass', from: 'adversarial:pass', to: 'signoff:in' },
    { id: 'adversarial-fail', from: 'adversarial:fail', to: 'draft:in' },
    { id: 'signoff-fail', from: 'signoff:fail', to: 'draft:in' },
    { id: 'fallback', from: 'signoff:fail', to: 'rescue:in' },
  ]

  it('calls a fail edge a loop only when its target leads back to the gate', () => {
    expect([...loopConnectionIds(connections)].sort()).toEqual(['adversarial-fail', 'answer-fail', 'framing-fail', 'signoff-fail'])
  })

  const card = (id: string, x: number) => ({ id, x, y: 112, width: 300, height: 180 })
  const obstacles = [card('draft', 2044), card('adversarial', 2436), card('signoff', 2828)]
  const draftIn = { x: 2044, y: 150 }

  it('gives parallel loops into one step separate channels, the wider loop outside', () => {
    const routes = routeLoopWires([
      { id: 'signoff-fail', source: { x: 3128, y: 176 }, target: draftIn },
      { id: 'adversarial-fail', source: { x: 2736, y: 176 }, target: draftIn },
    ], obstacles)
    const inner = routes.get('adversarial-fail')!
    const outer = routes.get('signoff-fail')!
    expect(inner.channel).toBe(0)
    expect(outer.channel).toBe(1)
    // Lanes clear the cards they span, and the outer lane sits one channel higher.
    expect(inner.points[2].y).toBe(112 - 32)
    expect(outer.points[2].y).toBe(112 - 32 - LOOP_CHANNEL_GAP)
    // They drop into the shared input on separate legs.
    expect(inner.points[4].x).toBe(2044 - 24)
    expect(outer.points[4].x).toBe(2044 - 24 - LOOP_CHANNEL_GAP)
    for (const a of loopChannelSegments(inner.points)) {
      for (const b of loopChannelSegments(outer.points)) expect(segmentsShareChannel(a, b)).toBe(false)
    }
    expect(inner.d).toMatch(/^M2736,176 /)
    expect(inner.label).toEqual({ x: 2736 + 24 - 8, y: 112 - 32 - 6 })
  })

  it('moves a loop out a channel when two loops leave one gate', () => {
    const routes = routeLoopWires([
      { id: 'a', source: { x: 700, y: 176 }, target: { x: 100, y: 150 } },
      { id: 'b', source: { x: 700, y: 176 }, target: { x: 300, y: 150 } },
    ], [card('one', 100), card('two', 400)])
    expect(routes.get('b')!.channel).toBe(0)
    expect(routes.get('a')!.channel).toBe(1)
    expect(routes.get('a')!.points[1].x).toBe(700 + 24 + LOOP_CHANNEL_GAP)
  })

  it('runs a loop below its cards when the world has no room above them', () => {
    const route = routeLoopWires([{ id: 'top', source: { x: 700, y: 60 }, target: { x: 100, y: 40 } }], [{ id: 'row', x: 100, y: 20, width: 600, height: 100 }]).get('top')!
    expect(route.points[2].y).toBe(20 + 100 + 32)
  })

  it('labels a gate wire unless it runs into the card just right of the gate', () => {
    const port = { x: 1140, y: 176 }
    expect(gateWireNeedsLabel(port, { x: 1232, y: 150 })).toBe(false)
    expect(gateWireNeedsLabel(port, { x: 1700, y: 176 })).toBe(true)
    expect(gateWireNeedsLabel(port, { x: 1232, y: 500 })).toBe(true)
    expect(gateWireNeedsLabel(port, { x: 900, y: 176 })).toBe(true)
  })

  it('detects shared channel segments only on one line with a real overlap', () => {
    expect(segmentsShareChannel([{ x: 10, y: 0 }, { x: 10, y: 50 }], [{ x: 10, y: 40 }, { x: 10, y: 90 }])).toBe(true)
    expect(segmentsShareChannel([{ x: 10, y: 0 }, { x: 10, y: 50 }], [{ x: 10, y: 50 }, { x: 10, y: 90 }])).toBe(false)
    expect(segmentsShareChannel([{ x: 0, y: 5 }, { x: 50, y: 5 }], [{ x: 20, y: 5 }, { x: 80, y: 5 }])).toBe(true)
    expect(segmentsShareChannel([{ x: 0, y: 5 }, { x: 50, y: 5 }], [{ x: 20, y: 19 }, { x: 80, y: 19 }])).toBe(false)
  })
})
