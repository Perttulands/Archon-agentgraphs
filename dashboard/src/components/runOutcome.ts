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
    case 'operator:standalone':
      return 'Archon'
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

/** Why a run ended and who ended it: "failed at Execution: … · ended by Archon". */
export function runEndPhrase(kind: 'failed' | 'canceled', where: string, end?: Pick<EvidenceProblem, 'reason' | 'actor' | 'code'>): string {
  const reason = end?.reason.text.trim() || end?.code || ''
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
  if (route.endsRun) return { button: `${verb} and end the run`, outcome: `${verb} ends the run.`, blocks: false, last: false }
  if (route.unwired || !route.targets.length) {
    return verdict === 'fail'
      ? { button: verb, outcome: 'Send back blocks the run: this gate has no send-back route.', blocks: true, last: false }
      : { button: verb, outcome: '', blocks: false, last: false }
  }
  const to = titles(route.targets)
  const button = verdict === 'pass' ? `${verb} → ${to}` : `${verb} to ${to}`
  const notes: string[] = []
  let attemptNote = ''
  let last = false
  for (const target of route.targets) {
    if (!target.attempt || !target.maxAttempts || target.attempt < 2) continue
    const lastAttempt = target.attempt >= target.maxAttempts
    last ||= lastAttempt
    const attempt = `attempt ${target.attempt} of ${target.maxAttempts}${lastAttempt ? ', its last' : ''}`
    // One destination carries its attempt in brackets; several name theirs.
    if (route.targets.length === 1) attemptNote = ` (${attempt})`
    else notes.push(`${target.title || target.nodeId} starts ${attempt}`)
  }
  const formations = route.targets.filter(target => target.kind === 'formation').length
  if (route.dispatches && route.dispatches.max > 0) {
    const left = route.dispatches.max - route.dispatches.used
    if (left <= formations) {
      last = true
      notes.push(`the run has ${left} of ${route.dispatches.max} dispatches left`)
    }
  }
  const verbed = verdict === 'pass' ? `${to} ${route.targets.length > 1 ? 'run' : 'runs'} next` : `${to} ${route.targets.length > 1 ? 'run' : 'runs'} again with your response`
  return { button, outcome: `${verb}: ${verbed}${attemptNote}${notes.length ? `; ${notes.join('; ')}` : ''}.`, blocks: false, last }
}
