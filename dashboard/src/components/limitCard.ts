import type { RunLimitUse } from './formationsApi'
import type { BoardDocument, FormationNode, LimitNode, MissionNode } from './formationsTypes'

/**
 * Limit cards (archon-o7p.8): a run has no limits unless its mission holds one.
 * A card covers one step, or the Input card for the whole mission, and caps its
 * rounds: a step's runs, a peer step's journal messages, or every step run of
 * the mission. These are the words the canvas card, its window, the Flow view,
 * the gate answer panel and the run bar share, as internal/formations
 * limit_node.go and the CLI's run wait say them.
 */

type Board = Pick<BoardDocument, 'formations'> & Partial<Pick<BoardDocument, 'inputCards' | 'limits'>>

/** What a card covers on the mission as it stands. */
export type LimitCoverage =
  | { kind: 'step'; node: FormationNode }
  | { kind: 'mission'; node: MissionNode }
  /** Wired to nothing yet: a draft. */
  | { kind: 'none' }
  /** A target that is not a step or the Input card, as after its step was deleted. */
  | { kind: 'missing'; target: string }

/** The room a Limit card takes when placing it, as the server's arrange assumes. */
export const LIMIT_ROOM = { width: 220, height: 72 }

export function limitCoverage(board: Board, limit: LimitNode): LimitCoverage {
  if (!limit.target) return { kind: 'none' }
  const formation = board.formations.find(node => node.id === limit.target)
  if (formation) return { kind: 'step', node: formation }
  const mission = board.inputCards?.find(node => node.id === limit.target)
  if (mission) return { kind: 'mission', node: mission }
  return { kind: 'missing', target: limit.target }
}

/** The cards that cover a step or the Input card; a second one is a finding. */
export function limitsCovering(board: Board, nodeId: string): LimitNode[] {
  return (board.limits || []).filter(limit => limit.target === nodeId)
}

function plural(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? '' : 's'}`
}

/**
 * The knob in words for the card itself: "at most 3 rounds"; a peer step's
 * rounds are its journal messages and the mission's are its step runs.
 */
export function limitKnobWords(board: Board, limit: LimitNode): string {
  if (!limit.rounds) return 'sets no limit yet'
  const coverage = limitCoverage(board, limit)
  if (coverage.kind === 'step' && coverage.node.type === 'peer') return `at most ${plural(limit.rounds, 'journal message')}`
  if (coverage.kind === 'mission') return `at most ${plural(limit.rounds, 'step run')}`
  return `at most ${plural(limit.rounds, 'round')}`
}

/** What the card covers, for its face: "Covers Review", "Covers the whole mission". */
export function limitCoversWords(board: Board, limit: LimitNode): string {
  const coverage = limitCoverage(board, limit)
  switch (coverage.kind) {
    case 'step':
      return `Covers ${coverage.node.title || 'an untitled step'}`
    case 'mission':
      return 'Covers the whole mission'
    case 'none':
      return 'Wired to nothing yet'
    case 'missing':
      return 'Covers a step that is gone'
  }
}

/**
 * The limit as the node it covers states it: "Cap: at most 3 rounds", "Cap:
 * the whole mission may make at most 20 step runs". Flow puts it under its
 * Limit label; node windows say limitLine.
 */
export function limitSummary(board: Board, limit: LimitNode): string {
  const name = limit.title || 'Limit'
  if (!limit.rounds) return `${name}: sets no limit yet`
  if (limitCoverage(board, limit).kind === 'mission') return `${name}: the whole mission may make at most ${plural(limit.rounds, 'step run')}`
  return `${name}: ${limitKnobWords(board, limit)}`
}

/** "Limit Cap: at most 3 rounds", for a node window's connections. */
export function limitLine(board: Board, limit: LimitNode): string {
  return `Limit ${limitSummary(board, limit)}`
}

/** What the card does to a run, for its window. */
export function limitMeaning(board: Board, limit: LimitNode): string {
  const coverage = limitCoverage(board, limit)
  if (coverage.kind === 'none') return 'Wired to nothing: drag its handle onto a step, or onto the Input card for the whole mission.'
  if (coverage.kind === 'missing') return 'It covers a step that is no longer in the mission: wire it to a step, or to the Input card for the whole mission.'
  if (!limit.rounds) return 'It sets no limit yet: give it rounds, or delete it.'
  if (coverage.kind === 'mission') {
    return `The whole mission may make at most ${plural(limit.rounds, 'step run')}, judges included. When they are spent the run blocks before the next step starts, until you grant one more round.`
  }
  const step = coverage.node.title || 'The step'
  if (coverage.node.type === 'peer') {
    return `${step} may hold at most ${plural(limit.rounds, 'journal message')} over all its attempts. At the cap its conversation stops and the run blocks until you grant one more round.`
  }
  return `${step} may run at most ${limit.rounds === 1 ? 'once' : `${limit.rounds} times`}, send-backs and resumed re-runs included. When its rounds are spent the run blocks before it starts again, until you grant one more round.`
}

/** A typed rounds value: blank for none, else a positive whole number. */
export function roundsProblem(value: string): string {
  if (!value || (/^\d+$/.test(value) && Number.isSafeInteger(Number(value)) && Number(value) > 0)) return ''
  return 'Enter a positive whole number of rounds, or leave it blank for no limit.'
}

/** "Review used 3 of 3 rounds", "The mission used 20 of 20 rounds, 1 of them granted". */
export function limitUsePhrase(limit: RunLimitUse, who: string): string {
  const granted = limit.granted ? `, ${limit.granted} of them granted` : ''
  return `${who} used ${limit.used} of ${plural(limit.max, 'round')}${granted}`
}

/** A spent card's allowance: "its only round", "all 3 of its rounds". */
export function spentAllowance(limit: RunLimitUse): string {
  return limit.max === 1 ? 'its only round' : `all ${limit.max} of its rounds`
}

export interface Rect { x: number; y: number; width: number; height: number }

/** Where the line from a rectangle's centre toward a point leaves the rectangle. */
function edgeToward(rect: Rect, toward: { x: number; y: number }): { x: number; y: number } {
  const cx = rect.x + rect.width / 2
  const cy = rect.y + rect.height / 2
  const dx = toward.x - cx
  const dy = toward.y - cy
  if (!dx && !dy) return { x: cx, y: cy }
  const scale = Math.min(dx ? (rect.width / 2) / Math.abs(dx) : Infinity, dy ? (rect.height / 2) / Math.abs(dy) : Infinity)
  return { x: cx + dx * Math.min(scale, 1), y: cy + dy * Math.min(scale, 1) }
}

/**
 * The tether from a Limit card to what it covers: a straight line between the
 * two cards' edges, along the line joining their centres. It is not a wire and
 * carries nothing; it only shows what the card covers.
 */
export function tetherLine(card: Rect, target: Rect): { x1: number; y1: number; x2: number; y2: number } {
  const from = edgeToward(card, { x: target.x + target.width / 2, y: target.y + target.height / 2 })
  const to = edgeToward(target, { x: card.x + card.width / 2, y: card.y + card.height / 2 })
  return { x1: Math.round(from.x), y1: Math.round(from.y), x2: Math.round(to.x), y2: Math.round(to.y) }
}
