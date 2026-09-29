import { describe, expect, it } from 'vitest'
import { rectsOverlap, type WindowRect, type Workspace } from './windowGeometry'
import { TITLE_STRIP, placeOpeningWindow, placementCandidateCount, type PlacementScene } from './windowPlacement'
import { FLOATING_WINDOW_MINIMUM } from './floatingWindowSize'
import scenes from './fixtures/wayfindingScenes.json'

// The fixtures were measured from the real cockpit (archond with the lab
// executor, Wayfinding as it opens) at 1920x1080 and 2560x1440: the workspace
// and avoided zones, the content under the windows, and for every node the
// anchor and the boxes a window about it keeps clear, as cockpitScene.ts
// measures them. The canvas is fitted to the screen, the zoom it opens at.

interface MeasuredNode { title: string; anchor: WindowRect | null; keepClear: WindowRect[] }
interface MeasuredScene { workspace: Workspace; landmarks: WindowRect[]; content: WindowRect[]; nodes: Record<string, MeasuredNode> }
type SceneName = 'wayfinding-canvas-1920' | 'wayfinding-flow-1920' | 'wayfinding-canvas-2560' | 'wayfinding-flow-2560'
const measured = scenes as unknown as Record<SceneName, MeasuredScene>

const NODE = { width: 540, height: 620 }
const NOTE = { width: 400, height: 440 }
const FILE = { width: 720, height: 560 }

const MAP = 'fmn_01M2N3G4PQCK21EXT7NC5CC3R9'
const FRAMING = 'gate_01M2N3KMP5CYV3BHBRNJSAGN9Q'
const PEERS = 'fmn_01M2N3HQ5A5W4CAD3VFN86MC31'
const DRAFT = 'fmn_01M2N9N1N69CYV5KYCSQ2M1DPD'
const ADVERSARIAL = 'gate_01M2N9N1PEPTXX7XV5QW96681W'
const SIGN_OFF = 'gate_01M2N9N1R4R184Y5G3R45BB5G1'

const right = (rect: WindowRect) => rect.left + rect.width
const bottom = (rect: WindowRect) => rect.top + rect.height
const strip = (rect: WindowRect): WindowRect => ({ ...rect, height: TITLE_STRIP })

function inside(rect: WindowRect, bounds: WindowRect) {
  return rect.left >= bounds.left && rect.top >= bounds.top && right(rect) <= right(bounds) && bottom(rect) <= bottom(bounds)
}

/** Opens windows about `nodeIds` one after another, as the operator clicks them, and returns where each opened. */
function openInTurn(name: SceneName, nodeIds: string[], size = NODE, minimum = FLOATING_WINDOW_MINIMUM.node) {
  const scene = measured[name]
  const opened: WindowRect[] = []
  for (const nodeId of nodeIds) {
    const node = scene.nodes[nodeId]
    opened.push(placeOpeningWindow(size, minimum, {
      workspace: scene.workspace,
      anchor: node.anchor,
      keepClear: node.keepClear,
      windows: [...opened],
      landmarks: scene.landmarks,
      content: scene.content,
    }))
  }
  return opened
}

/** What opening the window about `nodeId` as the `index`th window must leave visible. */
function expectReadable(name: SceneName, nodeIds: string[], opened: WindowRect[], size = NODE) {
  const scene = measured[name]
  opened.forEach((rect, index) => {
    const node = scene.nodes[nodeIds[index]]
    const label = `${name}: window ${index + 1} (${node.title})`
    expect(inside(rect, scene.workspace.bounds), `${label} stays in the workspace`).toBe(true)
    for (const zone of scene.workspace.avoid) expect(rectsOverlap(rect, zone), `${label} stays off the zoom controls`).toBe(false)
    expect(rectsOverlap(rect, node.anchor!), `${label} leaves its own node visible`).toBe(false)
    for (const box of node.keepClear) expect(rectsOverlap(rect, box), `${label} leaves ${JSON.stringify(box)} visible`).toBe(false)
    expect(rect.width, `${label} is readable`).toBeGreaterThanOrEqual(size.width / 2)
    expect(rect.height, `${label} is readable`).toBeGreaterThanOrEqual(size.height / 2)
    for (const earlier of opened.slice(0, index)) {
      expect(rectsOverlap(rect, strip(earlier)), `${label} leaves an earlier window's title bar visible`).toBe(false)
      expect(Math.abs(rect.left - earlier.left) + Math.abs(rect.top - earlier.top), `${label} does not stack on an earlier window`).toBeGreaterThan(16)
    }
  })
}

const overlapsAny = (rect: WindowRect, others: WindowRect[]) => others.some(other => rectsOverlap(rect, other))

describe('window placement on Wayfinding as it opens', () => {
  it('opens Map the territory on the canvas at 1920 in the empty canvas, clear of the graph', () => {
    const [map] = openInTurn('wayfinding-canvas-1920', [MAP])
    expectReadable('wayfinding-canvas-1920', [MAP], [map])
    // The report's window sat on Framing review and Question peers; this one covers no card, note or wire label.
    expect(overlapsAny(map, measured['wayfinding-canvas-1920'].content)).toBe(false)
  })

  it('opens three windows in sequence on the canvas at 1920 without covering their steps, neighbours or each other', () => {
    const ids = [MAP, DRAFT, SIGN_OFF]
    const opened = openInTurn('wayfinding-canvas-1920', ids)
    expectReadable('wayfinding-canvas-1920', ids, opened)
    // The next click the report aimed, at Adversarial review, lands on its card.
    for (const rect of opened) expect(overlapsAny(rect, measured['wayfinding-canvas-1920'].nodes[ADVERSARIAL].keepClear.slice(0, 1))).toBe(false)
    for (const [index, rect] of opened.entries()) expect(overlapsAny(rect, opened.slice(0, index))).toBe(false)
  })

  it('opens three windows in sequence on the canvas at 2560 beside one another in free space', () => {
    const ids = [MAP, DRAFT, SIGN_OFF]
    const opened = openInTurn('wayfinding-canvas-2560', ids)
    expectReadable('wayfinding-canvas-2560', ids, opened)
    for (const [index, rect] of opened.entries()) expect(overlapsAny(rect, opened.slice(0, index))).toBe(false)
    // The Map window no longer blocks a click on Question peers.
    expect(overlapsAny(opened[0], measured['wayfinding-canvas-2560'].nodes[PEERS].keepClear)).toBe(false)
  })

  it('opens Flow windows at 1920 clear of each row\'s number, title, labels and links', () => {
    const ids = [MAP, FRAMING, PEERS]
    const opened = openInTurn('wayfinding-flow-1920', ids)
    expectReadable('wayfinding-flow-1920', ids, opened)
    // The gutters are narrower than a window, so the first takes the right edge of the column, where row text ends.
    const column = measured['wayfinding-flow-1920'].content[0]
    expect(opened[0].left).toBeGreaterThan(column.left + column.width / 2)
    expect(rectsOverlap(opened[0], opened[1])).toBe(false)
  })

  it('opens Flow windows at 2560 in the gutters, off the column', () => {
    const ids = [MAP, FRAMING, PEERS]
    const opened = openInTurn('wayfinding-flow-2560', ids)
    expectReadable('wayfinding-flow-2560', ids, opened)
    for (const rect of opened) expect(overlapsAny(rect, measured['wayfinding-flow-2560'].content)).toBe(false)
    for (const [index, rect] of opened.entries()) expect(overlapsAny(rect, opened.slice(0, index))).toBe(false)
  })

  it('opens a note window and a file window beside a node window, clear of the node and its neighbours', () => {
    const scene = measured['wayfinding-canvas-1920']
    const [map] = openInTurn('wayfinding-canvas-1920', [MAP])
    const node = scene.nodes[MAP]
    const base: PlacementScene = { workspace: scene.workspace, anchor: node.anchor, keepClear: node.keepClear, landmarks: scene.landmarks, content: scene.content }
    const note = placeOpeningWindow(NOTE, FLOATING_WINDOW_MINIMUM.note, { ...base, windows: [map] })
    expect(overlapsAny(note, [node.anchor!, ...node.keepClear, map])).toBe(false)
    // A file opened from inside the node window opens beside that window.
    const file = placeOpeningWindow(FILE, FLOATING_WINDOW_MINIMUM.file, { ...base, anchor: map, keepClear: [], windows: [map, note] })
    expect(overlapsAny(file, [map, strip(note)])).toBe(false)
  })
})

describe('window placement rules', () => {
  const workspace: Workspace = { bounds: { left: 0, top: 60, width: 1600, height: 900 }, avoid: [] }
  const minimum = FLOATING_WINDOW_MINIMUM.file

  it('opens run bar files one after another side by side, not on top of each other', () => {
    // The run bar's Produced chips sit just above the workspace.
    const chips = { left: 900, top: 20, width: 300, height: 24 }
    const first = placeOpeningWindow(FILE, minimum, { workspace, anchor: chips })
    const second = placeOpeningWindow(FILE, minimum, { workspace, anchor: chips, windows: [first] })
    expect(first.top).toBe(60)
    expect(rectsOverlap(first, second)).toBe(false)
  })

  it('opens run bar files right below the bar on a crowded canvas, over cards but never over a window', () => {
    // Wayfinding at 100% fills the canvas: no free space lies near the run bar.
    const scene = measured['wayfinding-canvas-1920']
    const crowded = { ...scene, landmarks: [...scene.landmarks, { left: 244, top: 138, width: 1668, height: 300 }] }
    const chip = { left: 440, top: 100, width: 90, height: 24 }
    const bar = { left: 236, top: 90, width: 1684, height: 40 }
    const opened: WindowRect[] = []
    for (let index = 0; index < 2; index += 1) {
      opened.push(placeOpeningWindow(FILE, minimum, { workspace: crowded.workspace, anchor: chip, anchorKind: 'control', windows: [...opened], landmarks: crowded.landmarks, content: crowded.content }))
    }
    const gap = (a: WindowRect, b: WindowRect) => Math.hypot(Math.max(0, b.left - right(a), a.left - right(b)), Math.max(0, b.top - bottom(a), a.top - bottom(b)))
    for (const rect of opened) {
      expect(gap(rect, chip)).toBeLessThanOrEqual(240)
      expect(rectsOverlap(rect, bar)).toBe(false)
    }
    expect(opened[0].top).toBe(crowded.workspace.bounds.top)
    expect(rectsOverlap(opened[0], opened[1])).toBe(false)
    // The same chip as a node would send the window off to free space instead.
    const asNode = placeOpeningWindow(FILE, minimum, { workspace: crowded.workspace, anchor: chip, landmarks: crowded.landmarks, content: crowded.content })
    expect(gap(asNode, chip)).toBeGreaterThan(240)
  })

  it('cascades windows with nowhere free to go, each earlier title bar left showing', () => {
    // A 1920 canvas already holding two file windows side by side: the next ones have to overlap.
    const canvas: Workspace = { bounds: { left: 244, top: 138, width: 1668, height: 934 }, avoid: [] }
    const opened: WindowRect[] = []
    for (let index = 0; index < 5; index += 1) opened.push(placeOpeningWindow(FILE, minimum, { workspace: canvas, windows: [...opened] }))
    expect(rectsOverlap(opened[0], opened[1])).toBe(false)
    opened.forEach((rect, index) => {
      expect(inside(rect, canvas.bounds)).toBe(true)
      for (const earlier of opened.slice(0, index)) {
        expect(rectsOverlap(rect, strip(earlier)), `window ${index + 1} leaves window ${opened.indexOf(earlier) + 1}'s title bar showing`).toBe(false)
        expect(Math.abs(rect.left - earlier.left) + Math.abs(rect.top - earlier.top)).toBeGreaterThan(16)
      }
    })
  })

  it('keeps a downstream node clear when asked to', () => {
    // A gate's answer window: beside the gate, but never over the step its answer passes to.
    const gate = { left: 600, top: 400, width: 160, height: 110 }
    const downstream = { left: 820, top: 400, width: 170, height: 220 }
    const size = { width: 460, height: 520 }
    // Content to the gate's left and below leaves the right, over the downstream step, the obvious place.
    const content = [{ left: 0, top: 60, width: 590, height: 900 }, { left: 590, top: 520, width: 1010, height: 440 }]
    const without = placeOpeningWindow(size, FLOATING_WINDOW_MINIMUM.node, { workspace, anchor: gate, content })
    expect(rectsOverlap(without, downstream)).toBe(true)
    const withHint = placeOpeningWindow(size, FLOATING_WINDOW_MINIMUM.node, { workspace, anchor: gate, keepClear: [downstream], content })
    expect(rectsOverlap(withHint, downstream)).toBe(false)
    expect(rectsOverlap(withHint, gate)).toBe(false)
  })

  it('opens near the centre with nothing to go by, and ignores an anchor scrolled far out of view', () => {
    const centred = placeOpeningWindow({ width: 400, height: 300 }, minimum, { workspace })
    expect(centred).toEqual({ left: 600, top: 360, width: 400, height: 300 })
    expect(placeOpeningWindow({ width: 400, height: 300 }, minimum, { workspace, anchor: { left: -900, top: 300, width: 100, height: 100 } })).toEqual(centred)
  })

  it('never goes below the minimum, and fits a window larger than the workspace', () => {
    expect(placeOpeningWindow({ width: 5000, height: 5000 }, minimum, { workspace })).toEqual({ ...workspace.bounds })
    expect(placeOpeningWindow({ width: 10, height: 10 }, minimum, { workspace })).toMatchObject({ width: minimum.width, height: minimum.height })
  })

  it('stays off avoided zones', () => {
    const zoom = { left: 1500, top: 700, width: 100, height: 260 }
    const anchor = { left: 1300, top: 640, width: 160, height: 120 }
    const rect = placeOpeningWindow({ width: 400, height: 300 }, minimum, { workspace: { ...workspace, avoid: [zoom] }, anchor })
    expect(rectsOverlap(rect, zoom)).toBe(false)
    expect(rectsOverlap(rect, anchor)).toBe(false)
  })

  it('weighs only what is within reach of the workspace, so a long Flow costs no more than a short one', () => {
    const scene = measured['wayfinding-flow-1920']
    const node = scene.nodes[FRAMING]
    const rowsTall = 230
    // Eighty rows: the measured ones and seventy-two more below, scrolled out of view.
    const more = (rects: WindowRect[]) => Array.from({ length: 9 }, (_, page) => rects.map(rect => ({ ...rect, top: rect.top + 2000 + page * 8 * rowsTall }))).flat()
    const short: PlacementScene = { workspace: scene.workspace, anchor: node.anchor, keepClear: node.keepClear, landmarks: scene.landmarks, content: scene.content }
    const long: PlacementScene = { ...short, landmarks: [...scene.landmarks, ...more(scene.landmarks)], content: [...scene.content, ...more(scene.content)] }
    expect(long.content!.length).toBe(80)
    const shortCount = placementCandidateCount(NODE, FLOATING_WINDOW_MINIMUM.node, short)
    expect(placementCandidateCount(NODE, FLOATING_WINDOW_MINIMUM.node, long)).toBe(shortCount)
    expect(placeOpeningWindow(NODE, FLOATING_WINDOW_MINIMUM.node, long)).toEqual(placeOpeningWindow(NODE, FLOATING_WINDOW_MINIMUM.node, short))
    // What is in view bounds the work: a few thousand places, not hundreds of thousands.
    expect(shortCount).toBeLessThan(20000)
  })
})
