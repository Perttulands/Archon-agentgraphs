import type { BoardConnection, BoardDocument, FormationPortDirection, LayoutEdge, LayoutNode } from './formationsTypes'

/** One write that reverses part of an edit: a board patch or a layout patch. */
export type UndoStep =
  | { board: Record<string, unknown> }
  | { layout: { nodes?: LayoutNode[]; edges?: LayoutEdge[] } }

/**
 * One canvas edit's undo. The steps run in order and are fixed when the edit
 * succeeds, so undo never needs the board as it is later. `label` names the
 * edit for the operator, as in "Undid delete formation “Plan”".
 */
export interface CockpitUndo {
  label: string
  steps: UndoStep[]
}

export type UndoOutcome =
  | { status: 'empty' }
  | { status: 'undone'; entry: CockpitUndo }
  | { status: 'failed'; entry: CockpitUndo; message: string }

/**
 * The cockpit's undo history. Undos run one at a time, newest first. An entry
 * is taken off the history before it runs and is never put back: a failed undo
 * is reported once and older entries stay reachable.
 */
export class UndoHistory {
  private entries: CockpitUndo[] = []
  private queue: Promise<unknown> = Promise.resolve()

  record(entry: CockpitUndo | null | undefined) {
    if (entry && entry.steps.length) this.entries.push(entry)
  }

  clear() {
    this.entries = []
  }

  get size() {
    return this.entries.length
  }

  undo(apply: (step: UndoStep) => Promise<void>): Promise<UndoOutcome> {
    const run = this.queue.then(async (): Promise<UndoOutcome> => {
      const entry = this.entries.pop()
      if (!entry) return { status: 'empty' }
      try {
        for (const step of entry.steps) await apply(step)
        return { status: 'undone', entry }
      } catch (err) {
        return { status: 'failed', entry, message: err instanceof Error ? err.message : String(err) }
      }
    })
    this.queue = run.catch(() => undefined)
    return run
  }
}

/** The status line after an undo, or '' when there was nothing to undo. */
export function undoOutcomeMessage(outcome: UndoOutcome): string {
  if (outcome.status === 'failed') return `Could not undo ${outcome.entry.label}: ${outcome.message}. It was removed from the undo history.`
  return ''
}

/** Combines the undos of one gesture's edits into one entry, the last edit undone first. */
export function combineUndo(label: string, ...parts: Array<CockpitUndo | null | undefined>): CockpitUndo {
  return { label, steps: parts.filter((part): part is CockpitUndo => Boolean(part)).reverse().flatMap(part => part.steps) }
}

export const boardStep = (patch: Record<string, unknown>): UndoStep => ({ board: patch })

type NodeKind = 'mission' | 'formation' | 'gate'

function nodeOf(board: BoardDocument, id: string): { kind: NodeKind; node: object; title: string } | null {
  const mission = board.missions?.find(item => item.id === id)
  if (mission) return { kind: 'mission', node: mission, title: mission.title }
  const formation = board.formations.find(item => item.id === id)
  if (formation) return { kind: 'formation', node: formation, title: formation.title }
  const gate = board.gates?.find(item => item.id === id)
  if (gate) return { kind: 'gate', node: gate, title: gate.title }
  return null
}

function touching(connections: BoardConnection[], matches: (endpoint: string) => boolean): BoardConnection[] {
  return connections.filter(connection => matches(connection.from) || matches(connection.to)).map(({ id, from, to }) => ({ id, from, to }))
}

export function quoted(title: string, fallback: string) {
  return title ? `“${title}”` : fallback
}

/**
 * The undo of deleting a node, captured from the board before the delete: the
 * node as the server sent it, every connection touching it, and where it sat.
 */
export function nodeDeleteUndo(board: BoardDocument, nodeId: string, position: { x: number; y: number }): CockpitUndo | null {
  const found = nodeOf(board, nodeId)
  if (!found) return null
  const connections = touching(board.connections || [], endpoint => endpoint.split(':')[0] === nodeId)
  return {
    label: `delete ${found.kind} ${quoted(found.title, 'untitled')}`,
    steps: [boardStep({ restoreNode: { [found.kind]: found.node, connections, x: Math.round(position.x), y: Math.round(position.y) } })],
  }
}

/** The undo of removing a formation port: the port back in its place with its connections. */
export function portRemoveUndo(board: BoardDocument, formationId: string, portId: string): CockpitUndo | null {
  const formation = board.formations.find(item => item.id === formationId)
  if (!formation) return null
  const inputIndex = formation.inputs.findIndex(port => port.id === portId)
  const outputIndex = formation.outputs.findIndex(port => port.id === portId)
  const direction: FormationPortDirection | null = inputIndex >= 0 ? 'input' : outputIndex >= 0 ? 'output' : null
  if (!direction) return null
  const index = direction === 'input' ? inputIndex : outputIndex
  const port = direction === 'input' ? formation.inputs[index] : formation.outputs[index]
  const endpoint = `${formationId}:${portId}`
  const connections = touching(board.connections || [], candidate => candidate === endpoint)
  return {
    label: `remove ${direction} “${port.label || port.id}” from ${quoted(formation.title, 'the formation')}`,
    steps: [boardStep({ restorePort: { formationId, direction, port: { id: port.id, label: port.label }, index, connections } })],
  }
}
