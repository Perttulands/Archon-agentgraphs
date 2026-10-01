import { endPathWords } from './endNode'
import type { GateRoute, GateRouteTarget, RunLimitUse } from './formationsApi'
import type { EvidenceProblem } from '../evidence/runEvidenceApi'

// The words for how a run stopped and where a gate's answer leads
// (form-n7u.5, .6, .7). The daemon supplies the facts: who ended a run and why,
// the limit a block exhausted, and each verdict's destinations on the run's
// frozen board. These functions only phrase them.

/** Who an actor recorded in the ledger is, in the operator's words. */
export function runActorLabel(actor = ''): string {
  switch (actor) {
    case '':
      return ''
    case 'agent:ui':
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

/** "Draft used 3 of 3 attempts" or "the run used 8 of 8 dispatches". */
export function runLimitPhrase(limit: RunLimitUse, titleOf: (nodeId: string) => string): string {
  if (limit.kind === 'dispatches') return `the run used ${limit.used} of ${limit.max} dispatches`
  const title = limit.nodeId ? titleOf(limit.nodeId) || limit.nodeId : 'the step'
  return `${title} used ${limit.used} of ${limit.max} attempts`
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
export function problemHeadline(problem: EvidenceProblem, names: { node: (nodeId: string) => string }): string {
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
  if (problem.limit) parts.push(runLimitPhrase(problem.limit, names.node))
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
 * daemon that does not report routes, the plain verb and no outcome line.
 */
export function gateRouteWords(verdict: 'pass' | 'fail', route: GateRoute | undefined, titleOf: (nodeId: string) => string): GateRouteWords {
  const verb = verdict === 'pass' ? 'Approve' : 'Send back'
  if (!route) return { button: verb, outcome: '', blocks: false, last: false }
  if (route.limit) {
    const to = titles(route.targets)
    // The route names its steps as the run's frozen board titled them.
    const named = (nodeId: string) => route.targets.find(target => target.nodeId === nodeId)?.title || titleOf(nodeId)
    return {
      button: verdict === 'pass' ? `${verb} → ${to}` : `${verb} to ${to}`,
      outcome: `${verb} blocks the run: ${runLimitPhrase(route.limit, named)}. It cannot resume.`,
      blocks: true,
      last: false,
    }
  }
  if (!route.targets.length) return { button: verb, outcome: '', blocks: false, last: false }
  // Every route leads somewhere (form-o7p.10); an End node target ends this path.
  const steps = route.targets.filter(target => target.kind !== 'end')
  const ends = route.targets.filter(target => target.kind === 'end').map(target => endPathWords(target.outcome))
  if (!steps.length) {
    const ended = ends.join(' and ')
    if (route.endsRun && route.runFails) return { button: `${verb} and fail the run`, outcome: `${verb}: ${ended}, and with nothing else to run, the run fails.`, blocks: false, last: false }
    if (route.endsRun) return { button: `${verb} and end the run`, outcome: `${verb}: ${ended}, and with nothing else to run, the run succeeds.`, blocks: false, last: false }
    if (route.runFails) return { button: verb, outcome: `${verb}: ${ended}, so the run fails once its other open work ends.`, blocks: false, last: false }
    return { button: verb, outcome: `${verb}: ${ended}; the run goes on with its other work.`, blocks: false, last: false }
  }
  const to = titles(steps)
  const button = verdict === 'pass' ? `${verb} → ${to}` : `${verb} to ${to}`
  let last = false
  // What each destination does with the answer, with its attempt when it has run before.
  const clauses = steps.map(target => {
    const name = target.title || target.nodeId
    let attempt = ''
    if (target.attempt && target.maxAttempts && target.attempt >= 2) {
      const lastAttempt = target.attempt >= target.maxAttempts
      last ||= lastAttempt
      attempt = ` (attempt ${target.attempt} of ${target.maxAttempts}${lastAttempt ? ', its last' : ''})`
    }
    if (target.kind !== 'formation') return `${name} receives ${verdict === 'pass' ? 'it' : 'your response'} next`
    if (target.waitsForInputs) return `${name} receives ${verdict === 'pass' ? 'this' : 'your response'} and waits for its other inputs${attempt}`
    if (verdict === 'pass') return `${name} runs next${attempt}`
    // A step that never ran runs for the first time, not again.
    return `${name} runs ${target.attempt && target.attempt > 1 ? 'again ' : ''}with your response${attempt}`
  })
  const notes: string[] = []
  const needed = route.dispatchesNeeded ?? route.targets.filter(target => target.kind === 'formation').length
  if (route.dispatches && route.dispatches.max > 0 && needed > 0) {
    const left = route.dispatches.max - route.dispatches.used
    if (left <= needed) {
      last = true
      notes.push(`the run has ${left} of ${route.dispatches.max} dispatches left`)
    }
  }
  // A rejected path already ended, or this route ends one: the run fails once its work ends.
  if (route.runFails) notes.push('the run fails once its other open work ends')
  return { button, outcome: `${verb}: ${[...clauses, ...ends, ...notes].join('; ')}.`, blocks: false, last }
}
