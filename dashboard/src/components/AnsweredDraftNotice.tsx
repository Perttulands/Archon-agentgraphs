import { useEffect, useState } from 'react'
import { fetchNodeEvidence, type EvidenceHumanRequest } from '../evidence/runEvidenceApi'
import { readGateDraft } from './HumanGateAnswerPanel'
import { runActorLabel } from './runOutcome'

/** An answer arriving elsewhere never consumes this browser's unsubmitted words. */
export function AnsweredDraftNotice({ runId, gateId, requestedSeq, title, onEvidence }: {
  runId: string; gateId: string; requestedSeq: number; title: string; onEvidence: () => void
}) {
  const [dismissed, setDismissed] = useState(false)
  const [decision, setDecision] = useState<EvidenceHumanRequest['decision']>()
  const [draft] = useState(() => readGateDraft(runId, requestedSeq))
  useEffect(() => {
    let current = true
    void fetchNodeEvidence(runId, gateId).then(evidence => {
      const request = evidence.evaluations?.flatMap(evaluation => evaluation.humanRequests || []).find(request => request.seq === requestedSeq)
      if (current) setDecision(request?.decision)
    }, () => {})
    return () => { current = false }
  }, [runId, gateId, requestedSeq])
  if (dismissed || !draft) return null
  return <aside className="answered-draft" role="status">
    <p>{title} was answered elsewhere{decision ? ` by ${runActorLabel(decision.decidedBy)}: ${decision.verdict === 'pass' ? 'approved' : 'sent back'}` : '.'}</p>
    {decision?.response.text && <blockquote>{decision.response.text}</blockquote>}
    <p>Your unsubmitted draft is kept in this browser.</p>
    <textarea aria-label={`Kept draft for ${title}`} readOnly value={draft} />
    <button type="button" onClick={onEvidence}>Open answer evidence</button>
    <button type="button" onClick={() => setDismissed(true)}>Dismiss</button>
  </aside>
}
