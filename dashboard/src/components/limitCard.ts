import type { RunLimitUse } from './formationsApi'
import type { BoardDocument, FormationNode, LimitNode, MissionNode } from './formationsTypes'

/**
 * Limit cards (archon-o7p.8): a run has no limits unless its mission holds one.
 * A card covers one step, or the Input card for the whole mission, and caps its
 * rounds (a step's runs, a peer step's journal messages, or every step run of
 * the mission) and its time (archon-o7p.8.2): wall time while the covered work
 * runs, never while it only waits. These are the words the canvas card, its window, the Flow view,
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
 * Whole seconds in words, as the server's durationWords says them: "45 s",
 * "5 min", "1 h 30 min", "1 min 30 s".
 */
export function durationWords(seconds: number): string {
  if (seconds < 60) return `${seconds} s`
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const rest = seconds % 60
  return [hours ? `${hours} h` : '', minutes ? `${minutes} min` : '', rest ? `${rest} s` : ''].filter(Boolean).join(' ')
}

const UNIT_SECONDS: Record<string, number> = { h: 3600, m: 60, min: 60, s: 1 }

/**
 * A typed time in whole seconds: a number of seconds ("90"), or a duration
 * such as "45s", "30m", "1h30m" or "1 h 30 min" as the card says it. Blank is
 * 0, no time; anything else that is not a positive whole number of seconds is
 * null.
 */
export function parseDuration(value: string): number | null {
  const text = value.trim().toLowerCase()
  if (!text) return 0
  const parts = /^\d+$/.test(text) ? [] : /^(?:\s*\d+(?:\.\d+)?\s*(?:h|min|m|s))+\s*$/.test(text) ? [...text.matchAll(/(\d+(?:\.\d+)?)\s*(h|min|m|s)/g)] : null
  if (!parts) return null
  const seconds = parts.length ? parts.reduce((sum, [, amount, unit]) => sum + Number(amount) * UNIT_SECONDS[unit], 0) : Number(text)
  // Fractions must come to whole seconds, as the CLI's --time requires.
  const whole = Math.round(seconds * 1000) / 1000
  return Number.isSafeInteger(whole) && whole > 0 ? whole : null
}

/** Whole seconds as a time the Time field reads back: "45s", "30m", "1h30m". */
export function durationInput(seconds: number | undefined): string {
  if (!seconds) return ''
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const rest = seconds % 60
  return `${hours ? `${hours}h` : ''}${minutes ? `${minutes}m` : ''}${rest ? `${rest}s` : ''}`
}

/** A typed time for the Time field: blank for none, else a positive whole number of seconds. */
export function timeProblem(value: string): string {
  return parseDuration(value) === null ? 'Enter a time such as 45s, 30m or 1h30m (whole seconds), or leave it blank for no time limit.' : ''
}

/** A typed warning: blank for none, else a time shorter than the card's time, which it needs. */
export function warnProblem(value: string, seconds: number | undefined): string {
  const warn = parseDuration(value)
  if (warn === null) return 'Enter a time such as 5m or 30s (whole seconds), or leave it blank for no warning.'
  if (!warn) return ''
  if (!seconds) return 'A warning needs time: give the card time first.'
  if (warn >= seconds) return `Warn with less time left than the card's ${durationWords(seconds)}.`
  return ''
}

/** The rounds knob in words: a step's rounds, a peer step's journal messages, the mission's step runs. */
function roundsWords(board: Board, limit: LimitNode, rounds: number): string {
  const coverage = limitCoverage(board, limit)
  if (coverage.kind === 'step' && coverage.node.type === 'peer') return plural(rounds, 'journal message')
  if (coverage.kind === 'mission') return plural(rounds, 'step run')
  return plural(rounds, 'round')
}

/** "warns at 5 min left", when the card warns and sets time. */
export function limitWarnWords(limit: LimitNode): string {
  return limit.seconds && limit.warnSeconds ? `warns at ${durationWords(limit.warnSeconds)} left` : ''
}

/**
 * The knobs in words for the card itself: "at most 3 rounds", "at most 30 min
 * of work", "at most 3 rounds · 30 min"; a peer step's rounds are its journal
 * messages and the mission's are its step runs.
 */
export function limitKnobWords(board: Board, limit: LimitNode): string {
  if (!limit.rounds && !limit.seconds) return 'sets no limit yet'
  if (!limit.seconds) return `at most ${roundsWords(board, limit, limit.rounds!)}`
  if (!limit.rounds) return `at most ${durationWords(limit.seconds)} of work`
  return `at most ${roundsWords(board, limit, limit.rounds)} · ${durationWords(limit.seconds)}`
}

/** What the card covers, for its face: "Covers Review", "Covers the mission". */
export function limitCoversWords(board: Board, limit: LimitNode): string {
  const coverage = limitCoverage(board, limit)
  switch (coverage.kind) {
    case 'step':
      return `Covers ${coverage.node.title || 'an untitled step'}`
    case 'mission':
      return 'Covers the mission'
    case 'none':
      return 'Wired to nothing yet'
    case 'missing':
      return 'Covers a step that is gone'
  }
}

/**
 * The limit as the node it covers states it: "Cap: at most 3 rounds and 30 min
 * of work, warns at 5 min left", "Cap: the whole mission may make at most 20
 * step runs and work at most 2 h". Flow puts it under its Limit label; node
 * windows say limitLine.
 */
export function limitSummary(board: Board, limit: LimitNode): string {
  const name = limit.title || 'Limit'
  if (!limit.rounds && !limit.seconds) return `${name}: sets no limit yet`
  const warn = limitWarnWords(limit)
  const tail = warn ? `, ${warn}` : ''
  if (limitCoverage(board, limit).kind === 'mission') {
    const knobs = [
      limit.rounds ? `make at most ${plural(limit.rounds, 'step run')}` : '',
      limit.seconds ? `work at most ${durationWords(limit.seconds)}` : '',
    ].filter(Boolean).join(' and ')
    return `${name}: the whole mission may ${knobs}${tail}`
  }
  if (!limit.seconds) return `${name}: ${limitKnobWords(board, limit)}`
  const rounds = limit.rounds ? `${roundsWords(board, limit, limit.rounds)} and ` : ''
  return `${name}: at most ${rounds}${durationWords(limit.seconds)} of work${tail}`
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
  if (!limit.rounds && !limit.seconds) return 'It sets no limit yet: give it rounds or time, or delete it.'
  const sentences: string[] = []
  const step = coverage.kind === 'step' ? coverage.node.title || 'The step' : ''
  if (limit.rounds) {
    if (coverage.kind === 'mission') {
      sentences.push(`The whole mission may make at most ${plural(limit.rounds, 'step run')}, judges included. When they are spent the run blocks before the next step starts, until you grant one more round.`)
    } else if (coverage.kind === 'step' && coverage.node.type === 'peer') {
      sentences.push(`${step} may hold at most ${plural(limit.rounds, 'journal message')} over all its attempts. At the cap its conversation stops and the run blocks until you grant one more round.`)
    } else {
      sentences.push(`${step} may run at most ${limit.rounds === 1 ? 'once' : `${limit.rounds} times`}, send-backs and resumed re-runs included. When its rounds are spent the run blocks before it starts again, until you grant one more round.`)
    }
  }
  if (limit.seconds) {
    const time = durationWords(limit.seconds)
    if (coverage.kind === 'mission') {
      sentences.push(`The whole mission may work at most ${time}, counted while any step runs, judges included. Waiting on a human gate does not count while no other step runs, nor does a blocked run. When the time runs out the running step stops and the run blocks until you grant ${time} more.`)
    } else {
      sentences.push(`${step} may work at most ${time} over all its attempts, counted only while one runs. Waiting on a human gate does not count, so a send-back resumes it with the time it has left. When the time runs out the step stops and the run blocks until you grant ${time} more.`)
    }
    if (limit.warnSeconds) {
      sentences.push(coverage.kind === 'mission'
        ? `With ${durationWords(limit.warnSeconds)} left, Archon pastes a warning into the seats working then.`
        : `With ${durationWords(limit.warnSeconds)} left, Archon pastes a warning into its seats.`)
    }
  }
  return sentences.join(' ')
}

/** A typed rounds value: blank for none, else a positive whole number. */
export function roundsProblem(value: string): string {
  if (!value || (/^\d+$/.test(value) && Number.isSafeInteger(Number(value)) && Number(value) > 0)) return ''
  return 'Enter a positive whole number of rounds, or leave it blank for no limit.'
}

/**
 * "Review used 3 of 3 rounds", "The mission used 20 of 20 rounds, 1 of them
 * granted", "Review used 30 min of 30 min", as the engine's block reason.
 */
export function limitUsePhrase(limit: RunLimitUse, who: string): string {
  if (limit.kind === 'time') {
    const granted = limit.granted ? `, ${durationWords(limit.granted)} of it granted` : ''
    return `${who} used ${durationWords(limit.used)} of ${durationWords(limit.max)}${granted}`
  }
  const granted = limit.granted ? `, ${limit.granted} of them granted` : ''
  return `${who} used ${limit.used} of ${plural(limit.max, 'round')}${granted}`
}

/** A spent card's allowance: "its only round", "all 3 of its rounds", "all 30 min of its time". */
export function spentAllowance(limit: RunLimitUse): string {
  if (limit.kind === 'time') return `all ${durationWords(limit.max)} of its time`
  return limit.max === 1 ? 'its only round' : `all ${limit.max} of its rounds`
}

/** What a grant gives a spent card: "one more round", or the card's time again, "30 min more". */
export function grantWords(limit: RunLimitUse | undefined): string {
  if (limit?.kind === 'time') return `${durationWords(limit.max - (limit.granted || 0))} more`
  return 'one more round'
}

/** What a time card has left: "25 min of 30 min left". */
export function leftWords(limit: RunLimitUse): string {
  return `${durationWords(Math.max(limit.max - limit.used, 0))} of ${durationWords(limit.max)} left`
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
