import type { RunStatusProjection } from './formationsTypes'
import { runSpan, shortTime } from './runList'

/** When the shown run started and how long it took or has run. */
export function RunWhen({ run }: { run: RunStatusProjection }) {
  const span = runSpan(run)
  if (!run.startedAt) return run.startedBy ? <span className="run-when">by {run.startedBy}</span> : null
  return <span className="run-when" title={`Started ${new Date(run.startedAt).toLocaleString()}${run.startedBy ? ` by ${run.startedBy}` : ''}`}>
    started {shortTime(run.startedAt)}{span ? ` · ${run.final ? 'took' : 'for'} ${span}` : ''}{run.startedBy ? ` · ${run.startedBy}` : ''}
  </span>
}
