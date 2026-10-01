import { endPathWords, endTitleSuffix } from '../components/endNode'
import type { BoardDocument } from '../components/formationsTypes'
import { buildFlow, judgeChain } from '../flow/flowModel'

export { judgeChain }

/**
 * A board's routes in words: which step feeds a node, where its work goes,
 * what judges a gate, and where each path ends. Steps carry the Flow view's
 * numbers; missions, judge formations and End nodes have none.
 */

export type RouteKind = 'starts' | 'fed-by' | 'feeds' | 'pass' | 'fail' | 'judged-by' | 'judges' | 'ended-by'

export interface Route {
  kind: RouteKind
  /** The node at the other end; absent when the route leads nowhere yet. */
  nodeId?: string
  /** The port the route leaves or enters, when the node has more than one. */
  port?: string
  /** A fail route to a step the run has already passed. */
  back?: boolean
  text: string
}

type Board = Pick<BoardDocument, 'connections' | 'formations'> & Partial<Pick<BoardDocument, 'missions' | 'gates' | 'tools' | 'ends'>>

/** A route that leads nowhere, which a draft may hold until it is wired. */
export const NOWHERE = 'leads nowhere: wire it to a step or an End node'

const nodeOf = (endpoint: string) => endpoint.split(':')[0]
const portOf = (endpoint: string) => endpoint.split(':').slice(1).join(':')

export function nodeTitle(board: Board, nodeId: string): string {
  const node = [...(board.missions || []), ...board.formations, ...(board.gates || []), ...(board.tools || []), ...(board.ends || [])].find(item => item.id === nodeId)
  return node?.title || nodeId
}

/** Step numbers from the flow model, so windows and the Flow view count steps the same way. */
export function stepNumbers(board: Board): ReadonlyMap<string, number> {
  return buildFlow(board).numbers
}

/** Everything a node's routes say, in reading order: what feeds it, then where its work goes. */
export function nodeRoutes(board: Board, nodeId: string, steps = stepNumbers(board)): Route[] {
  const connections = board.connections || []
  const named = (id: string) => (steps.has(id) ? `${steps.get(id)} ${nodeTitle(board, id)}` : nodeTitle(board, id))
  const endOf = (id: string) => (board.ends || []).find(node => node.id === id)
  // Where a route leads: a named step, or "this path ends (done)" at an End node.
  const toward = (id: string) => {
    const end = endOf(id)
    return end ? `${endPathWords(end.outcome)}${endTitleSuffix(end.title, end.outcome)}` : named(id)
  }
  const routes: Route[] = []
  const mission = (board.missions || []).find(node => node.id === nodeId)
  const formation = board.formations.find(node => node.id === nodeId)
  const gate = (board.gates || []).find(node => node.id === nodeId)
  const end = endOf(nodeId)
  const outgoing = connections.filter(connection => nodeOf(connection.from) === nodeId)

  if (end) {
    for (const connection of connections.filter(item => item.to === `${nodeId}:in`)) {
      const source = nodeOf(connection.from)
      const port = portOf(connection.from)
      const via = port === 'pass' || port === 'fail' ? `${named(source)}'s ${port} route` : named(source)
      routes.push({ kind: 'ended-by', nodeId: source, text: `Ends ${via}` })
    }
    if (!routes.length) routes.push({ kind: 'ended-by', text: 'No route leads here yet' })
    return routes
  }

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
        routes.push({ kind: 'feeds', nodeId: target, port: manyOutputs ? port.label : undefined, text: `${lead} ${toward(target)}` })
      }
      const returnsVerdict = connections.some(item => item.from === `${nodeId}:${port.id}` && judged.has(nodeOf(item.to)) && portOf(item.to) === 'judge')
      if (!leaving.length && !returnsVerdict) routes.push({ kind: 'feeds', port: manyOutputs ? port.label : undefined, text: `${lead} ${NOWHERE}` })
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
      routes.push({ kind: 'pass', nodeId: target, text: `Pass → ${toward(target)}` })
    }
    if (!pass.length) routes.push({ kind: 'pass', text: `Pass → ${NOWHERE}` })
    const fail = connections.filter(item => item.from === `${nodeId}:fail`)
    for (const connection of fail) {
      const target = nodeOf(connection.to)
      const back = (steps.get(target) ?? Infinity) < own
      routes.push({ kind: 'fail', nodeId: target, back, text: back ? `Fail ↺ back to ${named(target)}` : `Fail → ${toward(target)}` })
    }
    if (!fail.length) routes.push({ kind: 'fail', text: `Fail → ${NOWHERE}` })
  }
  return routes
}
