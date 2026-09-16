import { useEffect, useState } from 'react'
import { fetchNodeEvidence, isHumanVerdictPause } from '../evidence/runEvidenceApi'
import { runPointPhrase, type RunPoint as RunPointModel } from './formationsRunState'
import '../styles/formations-run.css'

// The run bar's current point: the node a run waits at, runs or stopped on,
// with the recorded block reason. The projection is sanitized, so the reason
// comes from the blocked node's evidence (ADR-0017).

interface BlockExplanation {
  key: string
  reason: string
  pause: boolean
}

export default function RunPoint({ runId, point, title, onLocate }: {
  runId: string
  point: RunPointModel | null
  title: string
  onLocate: (nodeId: string) => void
}) {
  const [explanation, setExplanation] = useState<BlockExplanation | null>(null)
  const blockNodeId = point?.kind === 'blocked' ? point.nodeId : ''
  const blockSeq = point?.kind === 'blocked' ? point.blockSeq || 0 : 0
  // The key names the run, node and block, so a later poll of the same block keeps its reason.
  const blockKey = blockNodeId && blockSeq ? `${runId}/${blockNodeId}/${blockSeq}` : ''

  useEffect(() => {
    if (!blockKey) return
    let current = true
    fetchNodeEvidence(runId, blockNodeId).then(evidence => {
      const problem = evidence.problems?.find(item => item.seq === blockSeq)
      if (current) setExplanation({ key: blockKey, reason: problem?.reason.text || '', pause: problem ? isHumanVerdictPause(problem) : false })
    }, () => {
      if (current) setExplanation({ key: blockKey, reason: '', pause: false })
    })
    return () => { current = false }
  }, [blockKey, runId, blockNodeId, blockSeq])

  if (!point) return null
  const known = explanation && explanation.key === blockKey ? explanation : null
  const phrase = runPointPhrase(point, title, known?.reason, known?.pause)
  if (!phrase || phrase === point.kind) return null
  return (
    <button
      type="button"
      className={`run-point ${known?.pause ? 'paused' : point.kind}`}
      data-testid="run-point"
      title={point.nodeId ? `${phrase}. Show it on the canvas.` : phrase}
      disabled={!point.nodeId}
      onClick={() => onLocate(point.nodeId)}
    >{phrase}</button>
  )
}
