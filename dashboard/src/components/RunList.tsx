import type { RunStatusProjection } from './formationsTypes'
import { runActorLabel } from './runOutcome'
import { runSpan, shortTime } from './runList'
import { fileAnchor, useFileWindows } from '../files/FileWindows'
import { runMissionFileRequest } from '../files/fileWindowModel'

/** When the shown run started and how long it took or has run. */
export function RunWhen({ run }: { run: RunStatusProjection }) {
  const span = runSpan(run)
  if (!run.startedAt) return run.startedBy ? <span className="run-when">by {runActorLabel(run.startedBy)}</span> : null
  return <span className="run-when" title={`Started ${new Date(run.startedAt).toLocaleString()}${run.startedBy ? ` by ${runActorLabel(run.startedBy)}` : ''}`}>
    started {shortTime(run.startedAt)}{span ? ` · ${run.final ? 'took' : 'for'} ${span}` : ''}{run.startedBy ? ` · ${runActorLabel(run.startedBy)}` : ''}
  </span>
}

/**
 * The canvas shows the mission as it is now. When the shown run ran an
 * earlier revision, this says so and opens the mission as it ran.
 */
export function RunRevisionNote({ run, currentRev, missionTitle }: { run: RunStatusProjection; currentRev: number | undefined; missionTitle: string }) {
  const files = useFileWindows()
  if (!files || !run.missionRev || !currentRev || run.missionRev === currentRev) return null
  return <span className="run-revision" data-file-anchor>
    <button type="button" title={`This run ran revision ${run.missionRev}; the canvas shows revision ${currentRev}. Open the mission as it ran.`}
      onClick={event => files.open(runMissionFileRequest(run.runId, run.missionRev as number, missionTitle), fileAnchor(event.currentTarget))}>
      ran revision {run.missionRev}
    </button>
  </span>
}
