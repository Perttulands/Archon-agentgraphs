import type { BoardConnection, BoardDocument, FormationPortDirection, LayoutEdge, LayoutNode } from './formationsTypes'

/**
 * One write that reverses part of an edit: a board patch or a layout patch.
 * `what` names the part for the operator when an undo has several steps.
 */
export type UndoStep = (
  | { board: Record<string, unknown> }
  | { layout: { nodes?: LayoutNode[]; edges?: LayoutEdge[] } }
) & { what?: string }

/**
 * One canvas edit's undo. The steps run in order and are fixed when the edit
 * succeeds, so undo never needs the board as it is later. `board` is the slug
 * of the board it was recorded on; `label` names the edit for the operator,
 * as in "Could not undo the delete of formation “Plan”".
 */
export interface CockpitUndo {
  board: string
  label: string
  steps: UndoStep[]
}

/** An entry before the cockpit stamps it with the board its edit wrote to. */
export type UndoDraft = Omit<CockpitUndo, 'board'>

export type UndoOutcome =
  | { status: 'empty' }
  | { status: 'undone'; entry: CockpitUndo }
  /** The board no longer allows it: dropped. `done` steps were applied first. */
  | { status: 'failed'; entry: CockpitUndo; message: string; done: UndoStep[]; remaining: UndoStep[] }
  /** Another write kept winning after one reload: the remaining steps stay on the history. */
  | { status: 'busy'; entry: CockpitUndo; done: UndoStep[]; remaining: UndoStep[] }

/** What an undo needs from the cockpit. */
export interface UndoRunner {
  /** The slug of the board on screen; entries of any other board are discarded. */
  board(): string
  /** Resolves once no edit is still being saved, so undo sees its entry and its board revision. */
  idle(): Promise<void>
  apply(step: UndoStep): Promise<void>
  /** A stale board revision or ETag: the step was not applied and may succeed after a reload. */
  isConflict(err: unknown): boolean
  reload(): Promise<void>
}

/**
 * The cockpit's undo history. Undos run one at a time, newest first, after
 * every edit in flight has been saved. A stale-revision conflict reloads the
 * board and retries once; if it still conflicts, what was not undone stays on
 * the history. Any other failure is reported once and the entry is dropped, so
 * older entries stay reachable.
 */
export class UndoHistory {
  private entries: CockpitUndo[] = []
  private queue: Promise<unknown> = Promise.resolve()
  private currentBoard = ''

  /** Switching boards empties the history: undo belongs to the board it was recorded on. */
  setBoard(slug: string) {
    if (slug === this.currentBoard) return
    this.currentBoard = slug
    this.entries = []
  }

  /** Keeps an entry only if it belongs to the current board, so a late edit cannot land on another. */
  record(entry: CockpitUndo | null | undefined) {
    if (entry && entry.steps.length && entry.board === this.currentBoard) this.entries.push(entry)
  }

  clear() {
    this.entries = []
  }

  get size() {
    return this.entries.length
  }

  undo(runner: UndoRunner): Promise<UndoOutcome> {
    const run = this.queue.then(async (): Promise<UndoOutcome> => {
      await runner.idle()
      let entry = this.entries.pop()
      while (entry && (entry.board !== this.currentBoard || entry.board !== runner.board())) entry = this.entries.pop()
      if (!entry) return { status: 'empty' }
      let index = 0
      let reloaded = false
      while (index < entry.steps.length) {
        try {
          await runner.apply(entry.steps[index])
          index++
        } catch (err) {
          const done = entry.steps.slice(0, index)
          const remaining = entry.steps.slice(index)
          if (runner.isConflict(err)) {
            if (!reloaded) {
              reloaded = true
              await runner.reload().catch(() => undefined)
              continue
            }
            this.entries.push({ ...entry, steps: remaining })
            return { status: 'busy', entry, done, remaining }
          }
          return { status: 'failed', entry, message: err instanceof Error ? err.message : String(err), done, remaining }
        }
      }
      return { status: 'undone', entry }
    })
    this.queue = run.catch(() => undefined)
    return run
  }
}

/**
 * Counts board and layout writes still in flight. idle() also waits a task
 * after the last one settles, so the edit that made a write has recorded its
 * undo entry, and an edit's follow-up write has started, before undo runs.
 */
export class WriteTracker {
  private pending = new Set<Promise<unknown>>()

  track<T>(write: Promise<T>): Promise<T> {
    this.pending.add(write)
    const settle = () => { this.pending.delete(write) }
    write.then(settle, settle)
    return write
  }

  get busy() {
    return this.pending.size > 0
  }

  async idle(): Promise<void> {
    for (;;) {
      while (this.pending.size) await Promise.allSettled([...this.pending])
      await new Promise(resolve => setTimeout(resolve, 0))
      if (!this.pending.size) return
    }
  }
}

function describeSteps(steps: UndoStep[], total: number): string {
  const named = steps.map(step => step.what).filter((what): what is string => Boolean(what))
  if (named.length === steps.length) return named.length > 1 ? `${named.slice(0, -1).join(', ')} and ${named[named.length - 1]}` : named[0]
  return `${steps.length} of ${total} steps`
}

/** The status line after an undo, or '' when it fully worked or there was nothing to undo. */
export function undoOutcomeMessage(outcome: UndoOutcome): string {
  if (outcome.status === 'failed') {
    const total = outcome.entry.steps.length
    if (!outcome.done.length) return `Could not undo ${outcome.entry.label}: ${outcome.message}. It was removed from the undo history.`
    return `Partly undid ${outcome.entry.label}: ${describeSteps(outcome.done, total)} ${outcome.done.length === 1 ? 'was' : 'were'} undone, but ${describeSteps(outcome.remaining, total)} could not be: ${outcome.message}. It was removed from the undo history.`
  }
  if (outcome.status === 'busy') {
    const total = outcome.entry.steps.length
    const done = describeSteps(outcome.done, total)
    const partly = outcome.done.length ? ` ${done.charAt(0).toUpperCase()}${done.slice(1)} ${outcome.done.length === 1 ? 'was' : 'were'} undone.` : ''
    return `The mission kept changing while undoing ${outcome.entry.label}.${partly} The rest is still on the undo history; press Ctrl+Z to try again.`
  }
  return ''
}

/** Combines the undos of one gesture's edits into one entry, the last edit undone first. */
export function combineUndo(label: string, ...parts: Array<UndoDraft | null | undefined>): UndoDraft {
  return { label, steps: parts.filter((part): part is UndoDraft => Boolean(part)).reverse().flatMap(part => part.steps) }
}

export const boardStep = (patch: Record<string, unknown>, what?: string): UndoStep => (what ? { board: patch, what } : { board: patch })

type NodeKind = 'mission' | 'formation' | 'gate'

/** What undo messages call each node kind; the mission node is the Input card. */
const KIND_WORD: Record<NodeKind, string> = { mission: 'Input card', formation: 'formation', gate: 'gate' }

function nodeOf(board: BoardDocument, id: string): { kind: NodeKind; node: object; title: string; index: number } | null {
  const lists: Array<[NodeKind, Array<{ id: string; title: string }>]> = [['mission', board.missions || []], ['formation', board.formations], ['gate', board.gates || []]]
  for (const [kind, nodes] of lists) {
    const index = nodes.findIndex(item => item.id === id)
    if (index >= 0) return { kind, node: nodes[index], title: nodes[index].title, index }
  }
  return null
}

function touching(connections: BoardConnection[], matches: (endpoint: string) => boolean): BoardConnection[] {
  return connections.filter(connection => matches(connection.from) || matches(connection.to)).map(({ id, from, to }) => ({ id, from, to }))
}

export function quoted(title: string, fallback: string) {
  return title ? `“${title}”` : fallback
}

const FORMATION_TYPES = ['solo', 'peer', 'orchestrated']
const LEGACY_SCRIPT_FIELDS = ['command', 'commandArgv', 'commandCwd', 'commandShell', 'legacyScriptMigration']

/**
 * Why restoring this node after a delete would be refused, or null when it
 * can be undone. It mirrors the store's restore rules: retired authoring
 * (inline verification, a retired formation type, a legacy script gate) and
 * gate fields that creating or editing a gate would not accept.
 */
export function restoreBlocker(board: BoardDocument, nodeId: string): string | null {
  const formation = board.formations.find(item => item.id === nodeId)
  if (formation) {
    if (formation.verification) return 'it still carries retired inline verification'
    if (!FORMATION_TYPES.includes(formation.type)) return `its type “${formation.type}” is retired`
    return null
  }
  const gate = board.gates?.find(item => item.id === nodeId) as (Record<string, unknown> & { kinds?: string[]; check?: string; checkVersion?: string; checkValue?: string }) | undefined
  if (gate) {
    if (LEGACY_SCRIPT_FIELDS.some(field => gate[field] !== undefined && gate[field] !== '')) return 'it is a legacy script gate'
    if (!gate.kinds?.length) return 'it names no gate kind'
    if (!gate.kinds.includes('code') && `${gate.check || ''}${gate.checkVersion || ''}${gate.checkValue || ''}`.trim()) return 'it has a code check without the code kind'
  }
  return null
}

/**
 * The undo of deleting a node, captured from the board before the delete: the
 * node as the server sent it, every connection touching it, where it sat on the
 * canvas and its place among the board's nodes of its kind.
 */
export function nodeDeleteUndo(board: BoardDocument, nodeId: string, position: { x: number; y: number }): UndoDraft | null {
  const found = nodeOf(board, nodeId)
  if (!found) return null
  const connections = touching(board.connections || [], endpoint => endpoint.split(':')[0] === nodeId)
  return {
    label: `the delete of ${KIND_WORD[found.kind]} ${quoted(found.title, 'untitled')}`,
    steps: [boardStep({ restoreNode: { [found.kind]: found.node, connections, index: found.index, x: Math.round(position.x), y: Math.round(position.y) } })],
  }
}

/** The undo of removing a formation port: the port back in its place with its connections. */
export function portRemoveUndo(board: BoardDocument, formationId: string, portId: string): UndoDraft | null {
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
    label: `the removal of ${direction} “${port.label || port.id}” from ${quoted(formation.title, 'the formation')}`,
    steps: [boardStep({ restorePort: { formationId, direction, port: { id: port.id, label: port.label }, index, connections } })],
  }
}
