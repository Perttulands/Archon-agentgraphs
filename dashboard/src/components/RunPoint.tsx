import { useEffect, useState } from 'react'
import { fetchRunProblems, isHumanVerdictPause } from '../evidence/runEvidenceApi'
import { runPointPhrase, type RunPoint as RunPointModel } from './formationsRunState'
import '../styles/formations-run.css'

// The run bar's current point: the node a run waits at, runs or stopped on,
// with the recorded block reason. The projection is sanitized, so the reason
// comes from the run's problems evidence (ADR-0017), which also serves blocks
// that name no node.

interface BlockExplanation {
  key: string
  reason: string
  pause: boolean
}

export default function RunPoint({ runId, point, title, onLocate, action = 'Show it on the canvas' }: {
  runId: string
  point: RunPointModel | null
  title: string
  onLocate: (nodeId: string) => void
  /** What a click does, for the tooltip. */
  action?: string
}) {
  const [explanation, setExplanation] = useState<BlockExplanation | null>(null)
  const blockSeq = point?.kind === 'blocked' ? point.blockSeq || 0 : 0
  // The key names the run and block, so a later poll of the same block keeps its reason.
  const blockKey = blockSeq ? `${runId}/${blockSeq}` : ''

  useEffect(() => {
    if (!blockKey) return
    let current = true
    fetchRunProblems(runId).then(problems => {
      const problem = problems.find(item => item.seq === blockSeq)
      if (current) setExplanation({ key: blockKey, reason: problem?.reason.text || '', pause: problem ? isHumanVerdictPause(problem) : false })
    }, () => {
      if (current) setExplanation({ key: blockKey, reason: '', pause: false })
    })
    return () => { current = false }
  }, [blockKey, runId, blockSeq])

  if (!point) return null
  const known = explanation && explanation.key === blockKey ? explanation : null
  const phrase = runPointPhrase(point, title, known?.reason, known?.pause)
  if (!phrase || phrase === point.kind) return null
  return (
    <button
      type="button"
      className={`run-point ${known?.pause ? 'paused' : point.kind}`}
      data-testid="run-point"
      title={point.nodeId ? `${phrase}. ${action}.` : phrase}
      disabled={!point.nodeId}
      onClick={() => onLocate(point.nodeId)}
    >{phrase}</button>
  )
}
