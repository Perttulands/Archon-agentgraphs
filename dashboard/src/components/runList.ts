/* A mission's runs, each identifiable without its ID (archon-o7p.2): when it
 * started, how long it took or has run, its status, the start of what it was
 * given and who drives it. */
import type { RunStatusProjection } from './formationsTypes'
import { openRunsByAttention } from './formationsRunDiscovery'

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const pad = (n: number) => String(n).padStart(2, '0')

/** A local time: 14:02 today, otherwise 1 Oct 14:02. */
export function shortTime(iso: string | undefined, now = new Date()): string {
  if (!iso) return ''
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return ''
  const clock = `${pad(at.getHours())}:${pad(at.getMinutes())}`
  if (at.toDateString() === now.toDateString()) return clock
  return `${at.getDate()} ${MONTHS[at.getMonth()]}${at.getFullYear() === now.getFullYear() ? '' : ` ${at.getFullYear()}`} ${clock}`
}

/** A span of time in its two largest units: 45s, 3m 12s, 2h 5m, 3d 4h. */
export function spanLabel(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60
  if (days) return hours ? `${days}d ${hours}h` : `${days}d`
  if (hours) return minutes ? `${hours}h ${minutes}m` : `${hours}h`
  if (minutes) return seconds ? `${minutes}m ${seconds}s` : `${minutes}m`
  return `${seconds}s`
}

/** How long a run took, or has run so far. */
export function runSpan(run: RunStatusProjection, now = new Date()): string {
  if (!run.startedAt) return ''
  const start = new Date(run.startedAt).getTime()
  const end = run.final && run.updatedAt ? new Date(run.updatedAt).getTime() : now.getTime()
  if (Number.isNaN(start) || Number.isNaN(end)) return ''
  return spanLabel(end - start)
}

/** The start of what the run was given, on one line. */
export function runInputExcerpt(run: RunStatusProjection, max = 90): string {
  const inputs = run.inputs || []
  const first = inputs.find(input => input.kind === 'text') || inputs[0]
  if (!first) return ''
  const text = first.value.replace(/\s+/g, ' ').trim()
  return text.length > max ? `${text.slice(0, max - 1)}…` : text
}

/** Open runs by attention, then finished runs newest first. The shown run is always listed. */
export function runsInListOrder(runs: RunStatusProjection[], shown: RunStatusProjection | null): RunStatusProjection[] {
  const all = shown ? [...runs.filter(run => run.runId !== shown.runId), shown] : runs
  const finished = all.filter(run => run.final).sort((a, b) => (a.runId < b.runId ? 1 : a.runId > b.runId ? -1 : 0))
  return [...openRunsByAttention(all), ...finished]
}
