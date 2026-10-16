import { shortTime, spanLabel } from '../components/runList'
import type { AttemptFile, AttemptOutput, AttemptVerdict, StepAttempt } from '../evidence/runEvidenceApi'

// What the attempts window says about a step's attempts (archon-7963): each
// attempt's outcome in the gate's own words, which attempt the loop settled
// on, and which of an attempt's files belong to which output.

/**
 * How an attempt ended, as its gates judged it: picked as the best (archon-gihe),
 * passed or failed by a gate, still judged or waiting for the operator, still
 * running, stopped before its output, or done with no gate on its output.
 */
export type AttemptOutcome = 'picked' | 'pass' | 'fail' | 'waiting' | 'judging' | 'running' | 'stopped' | 'done'

export const OUTCOME_WORDS: Record<AttemptOutcome, string> = {
  picked: 'picked',
  pass: '✓ pass',
  fail: '✕ fail',
  waiting: 'waiting for you',
  judging: 'judging',
  running: 'running',
  stopped: 'stopped',
  done: 'done',
}

export function attemptOutcome(attempt: StepAttempt, waitingGates: ReadonlySet<string> = new Set()): AttemptOutcome {
  if (attempt.picked) return 'picked'
  if (attempt.status === 'running') return 'running'
  const pending = attempt.verdicts.filter(verdict => verdict.pending)
  if (pending.length) return pending.some(verdict => waitingGates.has(verdict.gateId)) ? 'waiting' : 'judging'
  const decided = attempt.verdicts.filter(verdict => verdict.verdict)
  if (decided.some(verdict => verdict.verdict === 'fail')) return 'fail'
  if (decided.some(verdict => verdict.verdict === 'pass')) return 'pass'
  return attempt.status === 'stopped' ? 'stopped' : 'done'
}

/** The attempt the loop settled on: the one picked as the best, else the latest that passed. */
export function settledAttempt(attempts: readonly StepAttempt[]): number | null {
  const picked = attempts.find(attempt => attempt.picked)
  if (picked) return picked.attempt
  const passed = [...attempts].reverse().find(attempt => attemptOutcome(attempt) === 'pass')
  return passed ? passed.attempt : null
}

/** "10:42 · 4m 12s", or "10:42 · 38s so far" while it runs. */
export function attemptTiming(attempt: StepAttempt, now = new Date()): string {
  const started = attempt.startedAt ? new Date(attempt.startedAt) : null
  if (!started || Number.isNaN(started.getTime())) return ''
  const clock = shortTime(attempt.startedAt, now)
  const ended = attempt.endedAt ? new Date(attempt.endedAt) : null
  if (ended && !Number.isNaN(ended.getTime())) return `${clock} · ${spanLabel(ended.getTime() - started.getTime())}`
  return attempt.status === 'running' ? `${clock} · ${spanLabel(now.getTime() - started.getTime())} so far` : clock
}

/** Who decided a verdict, in the operator's words; a judge or check needs no name beside its gate. */
export function deciderWords(decidedBy: string | undefined): string {
  if (!decidedBy) return ''
  return decidedBy.startsWith('human:') ? 'you' : ''
}

/** Who picked an attempt: you, the judge, or Archon when only one attempt was a candidate. */
export function pickerWords(decidedBy: string): string {
  if (decidedBy.startsWith('human:')) return 'you'
  if (decidedBy === 'archon') return 'the only candidate'
  return decidedBy ? 'the judge' : ''
}

/** A verdict as one line: "Review gate · ✕ fail · you". */
export function verdictHeadline(verdict: AttemptVerdict, waitingGates: ReadonlySet<string> = new Set()): string {
  const gate = verdict.gateTitle || verdict.gateId
  if (verdict.pending) return `${gate} · ${waitingGates.has(verdict.gateId) ? 'waiting for you' : 'judging'}`
  if (verdict.superseded) return `${gate} · evaluated again before your answer`
  const decider = deciderWords(verdict.decidedBy)
  const word = verdict.verdict === 'pass' ? OUTCOME_WORDS.pass : verdict.verdict === 'fail' ? OUTCOME_WORDS.fail : verdict.verdict || ''
  return [gate, word, decider].filter(Boolean).join(' · ')
}

/** An output and the attempt's files that make it up: the file itself, or everything in the folder. */
export interface OutputFiles {
  output: AttemptOutput
  files: AttemptFile[]
}

/**
 * The attempt's files sorted by what they are: each output's own, and the
 * rest the seat wrote into the attempt's directory.
 */
export function attemptFileGroups(attempt: StepAttempt): { outputs: OutputFiles[]; others: AttemptFile[] } {
  const claimed = new Set<string>()
  const outputs = attempt.outputs.map(output => {
    const artifact = output.artifact
    const files = !artifact ? []
      : output.kind === 'folder' ? attempt.files.filter(file => file.name.startsWith(`${artifact}/`))
        : attempt.files.filter(file => file.name === artifact)
    for (const file of files) claimed.add(file.name)
    return { output, files }
  })
  return { outputs, others: attempt.files.filter(file => !claimed.has(file.name)) }
}

const IMAGE_PATH = /\.(png|jpe?g|gif|webp|avif|bmp)$/i

/** Whether a path names an image the browser can draw; a judge's crops are named so. */
export function isImagePath(path: string): boolean {
  return IMAGE_PATH.test(path)
}

/** The last name in a path, or a file name relative to its folder. */
export function baseName(path: string): string {
  return path.split('/').filter(Boolean).pop() || path
}

/** A file's name relative to a folder, for a listing of the folder. */
export function nameWithin(folder: string, name: string): string {
  return name.startsWith(`${folder}/`) ? name.slice(folder.length + 1) : baseName(name)
}

export interface FolderSummary {
  count: number
  bytes: number
  images: AttemptFile[]
  rest: AttemptFile[]
}

/** A folder's files: how many and how big, its images to preview and the rest to list. */
export function folderSummary(files: readonly AttemptFile[]): FolderSummary {
  const images = files.filter(file => file.kind === 'image')
  return {
    count: files.length,
    bytes: files.reduce((sum, file) => sum + file.size, 0),
    images,
    rest: files.filter(file => file.kind !== 'image'),
  }
}

/** Where a step of the arrow keys lands: one attempt along, held at the ends. */
export function stepAttempt(numbers: readonly number[], current: number, delta: number): number {
  if (!numbers.length) return current
  const at = numbers.indexOf(current)
  const from = at < 0 ? (delta > 0 ? -1 : numbers.length) : at
  return numbers[Math.max(0, Math.min(numbers.length - 1, from + delta))]
}

/** The attempt the window opens on: the judge's pick, then the settled attempt, then the latest. */
export function openingAttempt(attempts: readonly StepAttempt[], recommended?: number): number | null {
  if (recommended && attempts.some(attempt => attempt.attempt === recommended)) return recommended
  return settledAttempt(attempts) ?? attempts[attempts.length - 1]?.attempt ?? null
}

/** Two attempts side by side: the focused one and the one before it, or after it when it is the first. */
export function comparePair(numbers: readonly number[], focus: number): [number, number] {
  const at = Math.max(0, numbers.indexOf(focus))
  if (numbers.length < 2) return [numbers[0] ?? focus, numbers[0] ?? focus]
  return at > 0 ? [numbers[at - 1], numbers[at]] : [numbers[0], numbers[1]]
}

/** The floating window of a step's attempts; one per step. */
export const attemptsWindowId = (nodeId: string) => `attempts:${nodeId}`

/** A pending best-of-N pick (archon-gihe) an attempts window answers. */
export interface AttemptsPick {
  gateId: string
  gateTitle: string
  /** The attempts the pick may choose. */
  candidates: number[]
  recommended?: { attempt: number; reason: string }
  /** Counts the operator's requests to bring the pick up; each takes the keyboard. A run reaching the pick does not. */
  focusRequest: number
  /** Records the choice; resolves to the failure's words, or nothing once recorded. */
  onPick: (attempt: number, reason: string) => Promise<string>
}

/** A text's own byte length, as the daemon counts it. */
export function textBytes(text: string): number {
  return new TextEncoder().encode(text).length
}
