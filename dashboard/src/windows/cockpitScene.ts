import type { BoardConnection } from '../components/formationsTypes'
import type { WindowRect } from './windowGeometry'
import type { ViewScene } from './WindowManager'

/**
 * The cockpit measured for window placement (see windowPlacement.ts): what the
 * view shows under its windows, and the parts of a node the operator reads and
 * clicks while a window about it is open. Everything is measured from the DOM
 * as a window opens, in viewport pixels, on the canvas or in Flow, whichever
 * is showing.
 */

const FLOW = '[data-testid="flow-view"]'
// A Flow row: the mission header and each step.
const FLOW_ROWS = `${FLOW} .flow-mission[data-flow-node], ${FLOW} .flow-step[data-flow-node]`
// What the operator reads and clicks in a Flow row: its number and title, the
// labels down its left edge, and its links to other steps.
const FLOW_ROW_HANDLES = '.flow-number, .flow-step-head .flow-title, .flow-mission-head .flow-title, .flow-label, .flow-link'
// A row's own run state; kept clear only for the row the window is about.
const FLOW_ROW_STATE = '.flow-state, [data-testid="run-point"]'
const CANVAS_CARDS = '.formation[data-node], .gatecard[data-node], .missioncard[data-node], .toolcard[data-node], .endcard[data-node], .limitcard[data-node]'
const CANVAS_LANDMARKS = `${CANVAS_CARDS}, .note-sticky`
const CANVAS_CONTENT = '.wire-label'

/**
 * An element's box in viewport pixels. Null when it has no size, unless
 * `always`: a control's box is still where the operator clicked.
 */
export function measureElement(element: Element, always = false): WindowRect | null {
  const { left, top, width, height } = element.getBoundingClientRect()
  return always || (width > 0 && height > 0) ? { left, top, width, height } : null
}

const measure = (element: Element) => measureElement(element)

function measureAll(elements: Iterable<Element>): WindowRect[] {
  return [...elements].map(measure).filter((rect): rect is WindowRect => rect !== null)
}

const quote = (id: string) => id.replace(/["\\]/g, '\\$&')

function flowView(root: ParentNode): Element | null {
  return root.querySelector(FLOW)
}

function flowRow(root: ParentNode, nodeId: string): Element | null {
  const flow = flowView(root)
  // Judges sit inside their gate's row; their own line is what they show.
  return flow?.querySelector(`.flow-mission[data-flow-node="${quote(nodeId)}"], .flow-step[data-flow-node="${quote(nodeId)}"], .flow-judge[data-flow-node="${quote(nodeId)}"]`) || null
}

/**
 * What the view shows under its windows. In Flow: each row, and as landmarks
 * every row's number, title, labels and links. On the canvas: the cards and
 * notes as landmarks, and the wire labels.
 */
export function cockpitScene(root: ParentNode = document): ViewScene {
  const flow = flowView(root)
  if (flow) {
    return {
      landmarks: measureAll(flow.querySelectorAll(`${FLOW_ROW_HANDLES}, .flow-judge .flow-title`)),
      content: measureAll(flow.querySelectorAll('.flow-mission, .flow-step')),
    }
  }
  return { landmarks: measureAll(root.querySelectorAll(CANVAS_LANDMARKS)), content: measureAll(root.querySelectorAll(CANVAS_CONTENT)) }
}

/** The nodes wired to `nodeId`, either way, judges included. */
export function wiredNeighbours(connections: readonly BoardConnection[], nodeId: string): string[] {
  const nodeOf = (endpoint: string) => endpoint.split(':')[0]
  const neighbours = new Set<string>()
  for (const connection of connections) {
    const from = nodeOf(connection.from)
    const to = nodeOf(connection.to)
    if (from === nodeId && to !== nodeId) neighbours.add(to)
    if (to === nodeId && from !== nodeId) neighbours.add(from)
  }
  return [...neighbours]
}

/**
 * What must stay readable and clickable of each node, measured now. On the
 * canvas: its card and its note sticky. In Flow: its row's number, title,
 * labels and links. This is the hint for "don't cover this node": pass
 * `keepClear={() => nodeBoxes([downstreamId])}` to a floating window.
 */
export function nodeBoxes(nodeIds: readonly string[], root: ParentNode = document): WindowRect[] {
  const flow = flowView(root)
  return nodeIds.flatMap(nodeId => {
    if (flow) {
      const row = flowRow(root, nodeId)
      if (!row) return []
      if (row.classList.contains('flow-judge')) return measureAll(row.querySelectorAll('.flow-title'))
      return measureAll(row.querySelectorAll(FLOW_ROW_HANDLES))
    }
    return measureAll(root.querySelectorAll(`[data-node="${quote(nodeId)}"]:is(${CANVAS_CARDS}), .note-sticky[data-note-node="${quote(nodeId)}"]`))
  })
}

/**
 * What a window about `nodeId` leaves visible as it opens: the node's own
 * title, run state and next links, its wired neighbours, and in Flow the rows
 * just above and below it.
 */
export function nodeWindowKeepClear(nodeId: string, connections: readonly BoardConnection[], root: ParentNode = document): WindowRect[] {
  const neighbours = new Set(wiredNeighbours(connections, nodeId))
  const flow = flowView(root)
  const own: WindowRect[] = []
  if (flow) {
    const rows = [...flow.querySelectorAll(FLOW_ROWS)]
    const row = flowRow(root, nodeId)
    const index = row ? rows.indexOf(row) : -1
    if (index > 0) neighbours.add(rows[index - 1].getAttribute('data-flow-node') || '')
    if (index >= 0 && index < rows.length - 1) neighbours.add(rows[index + 1].getAttribute('data-flow-node') || '')
    if (row) own.push(...measureAll(row.querySelectorAll(FLOW_ROW_STATE)))
  } else {
    // The node's wires out and in are labelled with where they go.
    for (const connection of connections) {
      if (connection.from.split(':')[0] !== nodeId && connection.to.split(':')[0] !== nodeId) continue
      own.push(...measureAll(root.querySelectorAll(`[data-testid="wire-label-${quote(connection.id)}"]`)))
    }
  }
  neighbours.delete('')
  neighbours.delete(nodeId)
  return [...nodeBoxes([nodeId], root), ...own, ...nodeBoxes([...neighbours], root)]
}

/**
 * Where a node sits for a window about it to open beside: its Flow row's title
 * in Flow, else its card and sticky on the canvas. Null when it is not drawn.
 */
export function nodeAnchor(nodeId: string, root: ParentNode = document): WindowRect | null {
  const flow = flowView(root)
  if (flow) {
    const row = flowRow(root, nodeId)
    const title = row?.querySelector('.flow-title')
    return title ? measure(title) : row ? measure(row) : null
  }
  const boxes = nodeBoxes([nodeId], root)
  if (!boxes.length) return null
  const left = Math.min(...boxes.map(box => box.left))
  const top = Math.min(...boxes.map(box => box.top))
  const right = Math.max(...boxes.map(box => box.left + box.width))
  const bottom = Math.max(...boxes.map(box => box.top + box.height))
  return { left, top, width: right - left, height: bottom - top }
}
