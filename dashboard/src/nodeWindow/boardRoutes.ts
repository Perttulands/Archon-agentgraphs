import type { BoardConnection, BoardDocument } from '../components/formationsTypes'

/**
 * A board's routes in words: which step feeds a node, where its work goes,
 * what judges a gate, and where a run ends. Steps are numbered in the order a
 * run meets them, starting from the missions.
 */

export type RouteKind = 'starts' | 'fed-by' | 'feeds' | 'pass' | 'fail' | 'judged-by' | 'judges'

export interface Route {
  kind: RouteKind
  /** The node at the other end; absent when the route ends the run here. */
  nodeId?: string
  /** The port the route leaves or enters, when the node has more than one. */
  port?: string
  /** A fail route to a step the run has already passed. */
  back?: boolean
  text: string
}

type Board = Pick<BoardDocument, 'connections' | 'formations'> & Partial<Pick<BoardDocument, 'missions' | 'gates' | 'tools'>>

const nodeOf = (endpoint: string) => endpoint.split(':')[0]
const portOf = (endpoint: string) => endpoint.split(':').slice(1).join(':')

function nodeIds(board: Board): string[] {
  return [
    ...(board.missions || []).map(node => node.id),
    ...board.formations.map(node => node.id),
    ...(board.gates || []).map(node => node.id),
    ...(board.tools || []).map(node => node.id),
  ]
}

export function nodeTitle(board: Board, nodeId: string): string {
  const node = [...(board.missions || []), ...board.formations, ...(board.gates || []), ...(board.tools || [])].find(item => item.id === nodeId)
  return node?.title || nodeId
}

// A gate's judge runs before its routes, and its pass before its fail.
const routeRank = (connection: BoardConnection) => {
  const port = portOf(connection.from)
  return port === 'judge' ? 0 : port === 'fail' ? 2 : 1
}

/** Step numbers in the order a run meets the nodes; unreachable nodes follow in board order. */
export function stepNumbers(board: Board): Map<string, number> {
  const steps = new Map<string, number>()
  const connections = board.connections || []
  const walk = (start: string) => {
    const queue = [start]
    while (queue.length) {
      const id = queue.shift() as string
      if (steps.has(id)) continue
      steps.set(id, steps.size + 1)
      const next = connections
        .filter(connection => nodeOf(connection.from) === id)
        .sort((a, b) => routeRank(a) - routeRank(b))
      for (const connection of next) queue.push(nodeOf(connection.to))
    }
  }
  for (const mission of board.missions || []) walk(mission.id)
  for (const id of nodeIds(board)) walk(id)
  return steps
}

/** The formations wired from a gate's judge port back to it, in order. */
export function judgeChain(board: Board, gateId: string): string[] {
  const connections = board.connections || []
  const socket = `${gateId}:judge`
  const send = connections.find(connection => connection.from === socket)
  if (!send) return []
  const chain: string[] = []
  let current = nodeOf(send.to)
  while (current && !chain.includes(current) && current !== gateId) {
    chain.push(current)
    const onward = connections.find(connection => nodeOf(connection.from) === current && connection.to !== socket)
    const returns = connections.some(connection => nodeOf(connection.from) === current && connection.to === socket)
    if (returns || !onward) break
    current = nodeOf(onward.to)
  }
  return chain
}

/** Everything a node's routes say, in reading order: what feeds it, then where its work goes. */
export function nodeRoutes(board: Board, nodeId: string, steps = stepNumbers(board)): Route[] {
  const connections = board.connections || []
  const named = (id: string) => `${steps.get(id) ?? '?'} ${nodeTitle(board, id)}`
  const routes: Route[] = []
  const mission = (board.missions || []).find(node => node.id === nodeId)
  const formation = board.formations.find(node => node.id === nodeId)
  const gate = (board.gates || []).find(node => node.id === nodeId)
  const outgoing = connections.filter(connection => nodeOf(connection.from) === nodeId)

  if (mission) {
    for (const connection of outgoing) {
      const target = nodeOf(connection.to)
      routes.push({ kind: 'starts', nodeId: target, text: `Starts → ${named(target)}` })
    }
    if (!outgoing.length) routes.push({ kind: 'starts', text: 'Starts → nothing yet' })
    return routes
  }

  const judged = new Set((board.gates || []).filter(item => judgeChain(board, item.id).includes(nodeId)).map(item => item.id))
  for (const gateId of judged) routes.push({ kind: 'judges', nodeId: gateId, text: `Judges ${named(gateId)}` })

  if (formation) {
    const manyInputs = formation.inputs.length > 1
    const sentBack: Route[] = []
    for (const port of formation.inputs) {
      for (const connection of connections.filter(item => item.to === `${nodeId}:${port.id}`)) {
        const source = nodeOf(connection.from)
        if (judged.has(source) && portOf(connection.from) === 'judge') continue
        const failed = portOf(connection.from) === 'fail'
        const into = manyInputs ? ` into ${port.label}` : ''
        const route: Route = { kind: 'fed-by', nodeId: source, port: manyInputs ? port.label : undefined, text: `${failed ? 'Sent back by' : 'Fed by'} ${named(source)}${into}` }
        if (failed) sentBack.push(route)
        else routes.push(route)
      }
    }
    routes.push(...sentBack)
    const manyOutputs = formation.outputs.length > 1
    for (const port of formation.outputs) {
      const lead = manyOutputs ? `${port.label} →` : 'Feeds →'
      const leaving = connections.filter(item => item.from === `${nodeId}:${port.id}` && !(judged.has(nodeOf(item.to)) && portOf(item.to) === 'judge'))
      for (const connection of leaving) {
        const target = nodeOf(connection.to)
        routes.push({ kind: 'feeds', nodeId: target, port: manyOutputs ? port.label : undefined, text: `${lead} ${named(target)}` })
      }
      const returnsVerdict = connections.some(item => item.from === `${nodeId}:${port.id}` && judged.has(nodeOf(item.to)) && portOf(item.to) === 'judge')
      if (!leaving.length && !returnsVerdict) routes.push({ kind: 'feeds', port: manyOutputs ? port.label : undefined, text: `${lead} run ends here` })
    }
    return routes
  }

  if (gate) {
    for (const connection of connections.filter(item => item.to === `${nodeId}:in`)) {
      const source = nodeOf(connection.from)
      routes.push({ kind: 'fed-by', nodeId: source, text: `Fed by ${named(source)}` })
    }
    for (const judge of judgeChain(board, nodeId)) routes.push({ kind: 'judged-by', nodeId: judge, text: `Judged by ${named(judge)}` })
    const own = steps.get(nodeId) ?? 0
    const pass = connections.filter(item => item.from === `${nodeId}:pass`)
    for (const connection of pass) {
      const target = nodeOf(connection.to)
      routes.push({ kind: 'pass', nodeId: target, text: `Pass → ${named(target)}` })
    }
    if (!pass.length) routes.push({ kind: 'pass', text: 'Pass → run ends here' })
    const fail = connections.filter(item => item.from === `${nodeId}:fail`)
    for (const connection of fail) {
      const target = nodeOf(connection.to)
      const back = (steps.get(target) ?? Infinity) < own
      routes.push({ kind: 'fail', nodeId: target, back, text: back ? `Fail ↺ back to ${named(target)}` : `Fail → ${named(target)}` })
    }
    if (!fail.length) routes.push({ kind: 'fail', text: 'Fail → the run blocks here' })
  }
  return routes
}
