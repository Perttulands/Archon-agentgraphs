import { endPathWords } from './endNode'
import type { GateRoute, GateRouteTarget, RunLimitUse } from './formationsApi'
import { grantWords, leftWords, limitUsePhrase, spentAllowance } from './limitCard'
import type { EvidenceProblem } from '../evidence/runEvidenceApi'

// The words for how a run stopped and where a gate's answer leads
// (archon-n7u.5, .6, .7). The daemon supplies the facts: who ended a run and why,
// the Limit card a block found spent (archon-o7p.8), and each verdict's
// destinations on the run's frozen mission. These functions only phrase them.

/** Who an actor recorded in the ledger is, in the operator's words. */
export function runActorLabel(actor = ''): string {
  switch (actor) {
    case '':
      return ''
    case 'human:ui':
      return 'the operator in the cockpit'
    case 'agent:archon':
    case 'operator:archon':
      return 'the archon CLI'
    case 'archond':
      return 'Archon'
    // The coordinator's own identity, which later events inherit from the
    // start: it says the coordinator recorded the event, not who asked.
    case 'operator:standalone':
      return 'the coordinator'
    default:
      return actor
  }
}

/**
 * A spent Limit card, as the block reason says it: "Draft used 3 of 3 rounds",
 * or "the mission used 20 of 20 rounds, 1 of them granted" for the card on the
 * Input card.
 */
export function runLimitPhrase(limit: RunLimitUse, titleOf: (nodeId: string) => string, inputCard: (nodeId: string) => boolean = () => false): string {
  const who = inputCard(limit.nodeId) ? 'the mission' : titleOf(limit.nodeId) || limit.nodeId || 'the step'
  return limitUsePhrase(limit, who)
}

/** The end problem of a final run: its run_failed or run_canceled, the latest. */
export function runEndProblem<T extends EvidenceProblem>(problems: readonly T[]): T | undefined {
  return [...problems].reverse().find(problem => problem.type === 'run_failed' || problem.type === 'run_canceled')
}

/** The reason a cancel records when the operator gives none; it adds nothing to "canceled by …". */
export const DEFAULT_STOP_REASON = 'operator stop'

/** Why a run ended and who ended it: "failed at Execution: … · ended by Archon". */
export function runEndPhrase(kind: 'failed' | 'canceled', where: string, end?: Pick<EvidenceProblem, 'reason' | 'actor' | 'code'>): string {
  const recorded = end?.reason.text.trim() || end?.code || ''
  const reason = kind === 'canceled' && recorded === DEFAULT_STOP_REASON ? '' : recorded
  const actor = runActorLabel(end?.actor)
  if (kind === 'canceled') {
    return `canceled${where ? ` at ${where}` : ''}${actor ? ` by ${actor}` : ''}${reason ? `: ${reason}` : ''}`
  }
  return `failed${where ? ` at ${where}` : ''}${reason ? `: ${reason}` : ''}${actor ? ` · ended by ${actor}` : ''}`
}

/**
 * A problem's line in run evidence. A run end names who ended it; a block names
 * its limit, and one the run resumed past says so, so an earlier block is not
 * read as what stopped the run.
 */
export function problemHeadline(problem: EvidenceProblem, names: { node: (nodeId: string) => string; inputCard?: (nodeId: string) => boolean }): string {
  const parts = [`#${problem.seq}`]
  const actor = runActorLabel(problem.actor)
  switch (problem.type) {
    case 'run_failed':
      parts.push(`run failed${actor ? ` · ended by ${actor}` : ''}`)
      break
    case 'run_canceled':
      parts.push(`run canceled${actor ? ` by ${actor}` : ''}`)
      break
    case 'run_blocked':
      parts.push('blocked')
      break
    default:
      parts.push(problem.type)
  }
  if (problem.code) parts.push(problem.code)
  if (problem.limit) parts.push(runLimitPhrase(problem.limit, names.node, names.inputCard))
  if (problem.resumedSeq) parts.push(`resumed at #${problem.resumedSeq}`)
  else if (problem.type === 'run_blocked' && problem.resumeAllowed !== undefined) parts.push(problem.resumeAllowed ? 'resumable' : 'not resumable')
  return parts.join(' · ')
}

export interface GateRouteWords {
  /** The button's label: "Approve → Publish", "Send back to Draft". */
  button: string
  /** The line under the answer that says what the verdict does. */
  outcome: string
  /** The verdict blocks the run instead of continuing it. */
  blocks: boolean
  /** The verdict takes the last of a limit. */
  last: boolean
}

const titles = (targets: readonly GateRouteTarget[]) => targets.map(target => target.title || target.nodeId).join(' and ')

/**
 * What Approve or Send back does, from its route. Without a route, as from a
 * daemon that does not report routes, the plain verb and no outcome line. A
 * step a Limit card covers says its round, and a route that would find a card
 * spent says the run blocks until a grant, as the CLI's run wait does.
 */
export function gateRouteWords(verdict: 'pass' | 'fail', route: GateRoute | undefined, titleOf: (nodeId: string) => string): GateRouteWords {
  const verb = verdict === 'pass' ? 'Approve' : 'Send back'
  if (!route || !route.targets.length) return { button: verb, outcome: '', blocks: false, last: false }
  // Every route leads somewhere (archon-o7p.10); an End node target ends this path.
  const steps = route.targets.filter(target => target.kind !== 'end')
  const ends = route.targets.filter(target => target.kind === 'end').map(target => endPathWords(target.outcome))
  let last = false
  if (!steps.length && !route.limit) {
    const ended = ends.join(' and ')
    if (route.endsRun && route.runFails) return { button: `${verb} and fail the run`, outcome: `${verb}: ${ended}, and with nothing else to run, the run fails.`, blocks: false, last: false }
    if (route.endsRun) return { button: `${verb} and end the run`, outcome: `${verb}: ${ended}, and with nothing else to run, the run succeeds.`, blocks: false, last: false }
    if (route.runFails) return { button: verb, outcome: `${verb}: ${ended}, so the run fails once its other open work ends.`, blocks: false, last: false }
    return { button: verb, outcome: `${verb}: ${ended}; the run goes on with its other work.`, blocks: false, last: false }
  }
  const to = titles(steps)
  const button = !steps.length ? verb : verdict === 'pass' ? `${verb} → ${to}` : `${verb} to ${to}`
  // What each destination does with the answer, with its round and time left
  // while its Limit card has some: "(round 2 of 3, 25 min of 30 min left)".
  const clauses = steps.map(target => {
    const name = target.title || target.nodeId
    const left: string[] = []
    const rounds = target.rounds
    if (rounds && rounds.used < rounds.max) {
      last ||= rounds.used + 1 === rounds.max
      left.push(`round ${rounds.used + 1} of ${rounds.max}`)
    }
    if (target.time && target.time.used < target.time.max) left.push(leftWords(target.time))
    const round = left.length ? ` (${left.join(', ')})` : ''
    if (target.kind !== 'formation') return `${name} receives ${verdict === 'pass' ? 'it' : 'your response'} next`
    if (target.waitsForInputs) return `${name} receives ${verdict === 'pass' ? 'this' : 'your response'} and waits for its other inputs${round}`
    if (verdict === 'pass') return `${name} runs next${round}`
    // A step that never ran runs for the first time, not again.
    return `${name} runs ${target.attempt && target.attempt > 1 ? 'again ' : ''}with your response${round}`
  })
  const notes: string[] = []
  const mission = route.missionRounds
  if (route.limit) {
    // The card the route needs is spent: the run blocks before the step starts.
    const limit = route.limit
    const missionCard = mission || route.missionTime
    const who = missionCard && missionCard.limitId === limit.limitId
      ? 'the mission'
      : route.targets.find(target => target.nodeId === limit.nodeId)?.title || titleOf(limit.nodeId) || limit.nodeId
    const where = [...clauses, ...ends].join('; ')
    return {
      button,
      outcome: `${verb}: ${where}, but ${who} has used ${spentAllowance(limit)}, so the run blocks instead until you grant ${grantWords(limit)}.`,
      blocks: true,
      last: false,
    }
  }
  if (mission && route.roundsNeeded) {
    const left = mission.max - mission.used
    if (left <= route.roundsNeeded) {
      last = true
      notes.push(`the mission has ${left} of ${mission.max} rounds left`)
    }
  }
  // The mission's working time left, while the route starts a step.
  if (route.missionTime && steps.length) notes.push(`the mission has ${leftWords(route.missionTime).replace(/ left$/, ' of working time left')}`)
  // A rejected path already ended, or this route ends one: the run fails once its work ends.
  if (route.runFails) notes.push('the run fails once its other open work ends')
  return { button, outcome: `${verb}: ${[...clauses, ...ends, ...notes].join('; ')}.`, blocks: false, last }
}
