import type { BoardDocument, FormationNode, GateNode, MissionNode, ToolNode } from '../components/formationsTypes'

/**
 * A board as an ordered sequence: each mission's steps numbered in the order a
 * run meets them, gates inline with their deciders and routes, judges nested
 * under their gate, and loops as references back to earlier steps.
 *
 * The walk extends the Agents tab's reachable ordering (AgentsView
 * reachableMissionItems) with judge ports and back edges: it goes breadth
 * first from the mission along outputs and passes before fails, never enters a
 * judge chain as a step, and numbers a node once, where it is first reached.
 */

type Board = Pick<BoardDocument, 'connections' | 'formations'> & Partial<Pick<BoardDocument, 'missions' | 'gates' | 'tools'>>

export type FlowTarget =
  | { kind: 'step'; nodeId: string; number: number | null; title: string; back: boolean }
  /** An unwired output or pass: the run ends here. */
  | { kind: 'end' }
  /** An unwired fail: the run blocks here. */
  | { kind: 'blocks' }

export type GateDecider = 'you' | 'judge' | 'code'

interface StepBase { id: string; number: number }

export type FlowStep =
  | StepBase & { kind: 'formation'; node: FormationNode; next: FlowTarget[] }
  | StepBase & { kind: 'gate'; node: GateNode; deciders: GateDecider[]; judges: FormationNode[]; pass: FlowTarget[]; fail: FlowTarget[] }
  | StepBase & { kind: 'tool'; node: ToolNode; next: FlowTarget[] }

export interface FlowSection {
  /** Null for the steps no mission reaches. */
  mission: MissionNode | null
  start: FlowTarget[]
  steps: FlowStep[]
}

export interface FlowModel {
  sections: FlowSection[]
  /** Step numbers; missions and judge formations have none. */
  numbers: ReadonlyMap<string, number>
  /** Judge formation id to the gate it judges. */
  judgeOf: ReadonlyMap<string, string>
}

const nodeOf = (endpoint: string) => endpoint.split(':')[0]
const portOf = (endpoint: string) => endpoint.split(':').slice(1).join(':')

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

const DECIDERS: Array<[string, GateDecider]> = [['human', 'you'], ['formation', 'judge'], ['code', 'code']]

export function buildFlow(board: Board): FlowModel {
  const connections = board.connections || []
  const missions = board.missions || []
  const gates = board.gates || []
  const tools = board.tools || []
  const formationById = new Map(board.formations.map(node => [node.id, node]))
  const gateById = new Map(gates.map(node => [node.id, node]))
  const toolById = new Map(tools.map(node => [node.id, node]))
  const titleOf = (id: string) => formationById.get(id)?.title ?? gateById.get(id)?.title ?? toolById.get(id)?.title ?? missions.find(node => node.id === id)?.title ?? id

  const judgeOf = new Map<string, string>()
  for (const gate of gates) {
    for (const judge of judgeChain(board, gate.id)) if (!judgeOf.has(judge)) judgeOf.set(judge, gate.id)
  }

  // Work routes: judge sends and verdict returns belong to the gate, not the sequence.
  const workRoutes = (nodeId: string) => connections.filter(connection =>
    nodeOf(connection.from) === nodeId && portOf(connection.from) !== 'judge' && portOf(connection.to) !== 'judge')
  const isStep = (id: string) => !judgeOf.has(id) && (formationById.has(id) || gateById.has(id) || toolById.has(id))

  const numbers = new Map<string, number>()
  const order: string[][] = []
  const walk = (starts: string[]) => {
    const reached: string[] = []
    const queue = [...starts]
    while (queue.length) {
      const id = queue.shift() as string
      if (numbers.has(id) || !isStep(id)) continue
      numbers.set(id, numbers.size + 1)
      reached.push(id)
      const routes = workRoutes(id)
      const onward = [...routes.filter(route => portOf(route.from) !== 'fail'), ...routes.filter(route => portOf(route.from) === 'fail')]
      for (const route of onward) queue.push(nodeOf(route.to))
    }
    return reached
  }
  for (const mission of missions) order.push(walk(workRoutes(mission.id).map(route => nodeOf(route.to))))
  const unreached = [...board.formations, ...gates, ...tools].map(node => node.id).filter(id => isStep(id) && !numbers.has(id))
  order.push(walk(unreached))

  const targets = (fromNumber: number, routes: typeof connections): FlowTarget[] => routes.map(route => {
    const nodeId = nodeOf(route.to)
    const number = numbers.get(nodeId) ?? null
    return { kind: 'step', nodeId, number, title: titleOf(nodeId), back: number !== null && number <= fromNumber }
  })

  const stepOf = (id: string): FlowStep => {
    const number = numbers.get(id) as number
    const gate = gateById.get(id)
    if (gate) {
      const routes = workRoutes(id)
      const pass = targets(number, routes.filter(route => portOf(route.from) === 'pass'))
      const fail = targets(number, routes.filter(route => portOf(route.from) === 'fail'))
      return {
        kind: 'gate', id, number, node: gate,
        deciders: DECIDERS.filter(([kind]) => gate.kinds.includes(kind)).map(([, decider]) => decider),
        judges: judgeChain(board, id).map(judge => formationById.get(judge)).filter((node): node is FormationNode => Boolean(node)),
        pass: pass.length ? pass : [{ kind: 'end' }],
        fail: fail.length ? fail : [{ kind: 'blocks' }],
      }
    }
    const next = targets(number, workRoutes(id))
    const formation = formationById.get(id)
    if (formation) return { kind: 'formation', id, number, node: formation, next: next.length ? next : [{ kind: 'end' }] }
    return { kind: 'tool', id, number, node: toolById.get(id) as ToolNode, next: next.length ? next : [{ kind: 'end' }] }
  }

  const sections: FlowSection[] = missions.map((mission, index) => ({
    mission,
    start: targets(0, workRoutes(mission.id)),
    steps: order[index].map(stepOf),
  }))
  const rest = order[order.length - 1]
  if (rest.length) sections.push({ mission: null, start: [], steps: rest.map(stepOf) })
  return { sections, numbers, judgeOf }
}
