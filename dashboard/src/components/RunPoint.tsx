import { useEffect, useState } from 'react'
import { fetchRunProblems, type RunProblem } from '../evidence/runEvidenceApi'
import { runPointPhrase, type RunPoint as RunPointModel } from './formationsRunState'
import { runEndPhrase, runEndProblem, runLimitPhrase } from './runOutcome'
import type { RunLimitUse } from './formationsApi'
import '../styles/formations-run.css'

// One of the run bar's current points: a node a run waits at, runs or stopped
// on, with the recorded block reason, or why a final run failed or was
// canceled and who ended it. The projection is sanitized, so the reason comes
// from the run's problems evidence (ADR-0017), which also serves blocks that
// name no node.

interface Explanation {
  key: string
  reason: string
  /** The limit the block exhausted. */
  limit?: RunLimitUse
  /** The run end, for a failed or canceled run. */
  end?: RunProblem
}

export default function RunPoint({ runId, point, title, onLocate, titleOf, action = 'Show it on the canvas' }: {
  runId: string
  point: RunPointModel | null
  title: string
  onLocate: (nodeId: string) => void
  /** A node's title on the board, for a limit that names another node. */
  titleOf?: (nodeId: string) => string
  /** What a click does, for the tooltip. */
  action?: string
}) {
  const [explanation, setExplanation] = useState<Explanation | null>(null)
  const blockSeq = point?.kind === 'blocked' ? point.blockSeq || 0 : 0
  const ended = point?.kind === 'failed' || point?.kind === 'canceled'
  // The key names the run and block, so a later poll of the same block keeps its reason.
  const key = blockSeq ? `${runId}/${blockSeq}` : ended ? `${runId}/${point.kind}` : ''

  useEffect(() => {
    if (!key) return
    let current = true
    fetchRunProblems(runId).then(problems => {
      if (!current) return
      if (!blockSeq) {
        setExplanation({ key, reason: '', end: runEndProblem(problems) })
        return
      }
      const problem = problems.find(item => item.seq === blockSeq)
      setExplanation({ key, reason: problem?.reason.text || '', limit: problem?.limit })
    }, () => {
      if (current) setExplanation({ key, reason: '' })
    })
    return () => { current = false }
  }, [key, runId, blockSeq])

  if (!point) return null
  const known = explanation && explanation.key === key ? explanation : null
  const where = title || point.nodeId
  const named = (id: string) => titleOf?.(id) || (id === point.nodeId ? title : '') || id
  const limit = known?.limit ? runLimitPhrase(known.limit, named) : ''
  const phrase = ended && known?.end
    ? runEndPhrase(point.kind as 'failed' | 'canceled', where, known.end)
    : runPointPhrase(point, title, limit || known?.reason)
  if (!phrase || phrase === point.kind) return null
  return (
    <button
      type="button"
      className={`run-point ${point.kind}`}
      data-testid="run-point"
      title={point.nodeId ? `${phrase}. ${action}.` : phrase}
      disabled={!point.nodeId}
      onClick={() => onLocate(point.nodeId)}
    >{phrase}</button>
  )
}
