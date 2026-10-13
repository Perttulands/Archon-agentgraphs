import { useEffect, useState } from 'react'
import { fetchNodeEvidence, type NodeEvidence } from '../evidence/runEvidenceApi'

const POLL_MS = 5000

/**
 * What the run on the canvas did at this gate, first in the gate's window: the
 * latest verdict with its reason, or why there is none and the verdict the
 * judge returned. Earlier evaluations stay in the run evidence.
 */
export function GateRunFacts({ runId, gateId, onSettle }: {
  runId: string
  gateId: string
  /** Moves the run past a rejected judge verdict: false asks the judge again, true takes the verdict as a fail. */
  onSettle?: (takeFail: boolean) => Promise<void>
}) {
  const [evidence, setEvidence] = useState<NodeEvidence | null>(null)
  const [settling, setSettling] = useState(false)
  useEffect(() => {
    let current = true
    const load = () => fetchNodeEvidence(runId, gateId).then(result => { if (current) setEvidence(result) }, () => {})
    void load()
    const timer = window.setInterval(load, POLL_MS)
    return () => { current = false; window.clearInterval(timer) }
  }, [runId, gateId])
  const settle = (takeFail: boolean) => {
    if (!onSettle) return
    setSettling(true)
    void onSettle(takeFail).finally(() => setSettling(false))
  }
  if (!evidence || evidence.runId !== runId || evidence.nodeId !== gateId) return null
  const evaluation = evidence.evaluations?.[evidence.evaluations.length - 1]
  const block = [...(evidence.problems || [])].reverse().find(problem => !problem.resumedSeq)
  const failure = evaluation && !evaluation.verdict ? evaluation.judgeFailures?.[evaluation.judgeFailures.length - 1] : undefined
  const verdict = evaluation?.verdict
  const why = block?.reason.text.trim() || failure?.reason.text.trim() || ''
  if (!verdict && !why) return null
  return (
    <section className="ngate-run" data-testid={`gate-run-${gateId}`} aria-label="This run">
      {verdict ? (
        <div className={`ngate-verdict ${verdict.verdict === 'fail' ? 'fail' : 'pass'}`}>
          <strong>{verdict.verdict || 'no verdict'}</strong>{evaluation?.attempt ? ` · round ${evaluation.attempt}` : ''}
          {verdict.reason.text.trim() ? <p>{verdict.reason.text.trim()}</p> : null}
        </div>
      ) : null}
      {why && (!verdict || block) ? <div className="ngate-why">{why}</div> : null}
      {failure?.returned?.text ? <pre className="ngate-returned" aria-label="The judge returned">{failure.returned.text}</pre> : null}
      {onSettle && block?.code === 'invalid_judge_result' && block.resumeAllowed ? (
        <div className="ngate-actions">
          <button type="button" disabled={settling} onClick={() => settle(false)}>Ask the judge again</button>
          <button type="button" disabled={settling} onClick={() => settle(true)}>Take it as a fail</button>
        </div>
      ) : null}
    </section>
  )
}
