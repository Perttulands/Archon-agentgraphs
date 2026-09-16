import { useState } from 'react'
import './humanGateAnswer.css'

export type GateUpstream =
  | { state: 'loading' }
  | { state: 'ready'; from: string; text: string; truncated: boolean; criterion: string }
  | { state: 'unavailable'; message: string }

export type GateDecision = 'pass' | 'fail'

interface HumanGateAnswerPanelProps {
  runId: string
  gateId: string
  requestedSeq: number
  gateTitle: string
  criterion: string
  upstream: GateUpstream
  /** Resolves true once the verdict is recorded. */
  onDecide: (verdict: GateDecision, response: string) => Promise<boolean>
}

function draftKey(runId: string, requestedSeq: number) {
  return `archon.gateResponse.${runId}.${requestedSeq}`
}

function readDraft(key: string) {
  try {
    return window.localStorage.getItem(key) || ''
  } catch {
    return ''
  }
}

function writeDraft(key: string, text: string) {
  try {
    if (text) window.localStorage.setItem(key, text)
    else window.localStorage.removeItem(key)
  } catch {
    // A draft is a convenience; the typed response stays in the textarea.
  }
}

/**
 * Shows what a pending human gate received and records the operator's answer.
 * Approve delivers the response to the next formation with the gate input;
 * Send back returns it to the pushback route as feedback.
 */
function HumanGateAnswerPanel({ runId, gateId, requestedSeq, gateTitle, criterion, upstream, onDecide }: HumanGateAnswerPanelProps) {
  const key = draftKey(runId, requestedSeq)
  const [response, setResponse] = useState(() => readDraft(key))
  const [submitting, setSubmitting] = useState<GateDecision | ''>('')
  const trimmed = response.trim()
  // The run's frozen criterion wins over the board's current draft.
  const shownCriterion = (upstream.state === 'ready' && upstream.criterion) || criterion

  const decide = async (verdict: GateDecision) => {
    if (submitting) return
    setSubmitting(verdict)
    try {
      if (await onDecide(verdict, trimmed)) writeDraft(key, '')
    } finally {
      setSubmitting('')
    }
  }

  return (
    <section className="gate-answer" role="dialog" aria-label={`Answer gate ${gateTitle}`} data-testid="gate-answer" onPointerDown={event => event.stopPropagation()}>
      <header className="gate-answer-hd">
        <span className="gate-answer-kicker">Needs your answer</span>
        <span className="gate-answer-title">{gateTitle}</span>
        <span className="gate-answer-id">{gateId} · request #{requestedSeq}</span>
      </header>
      {shownCriterion ? <p className="gate-answer-criterion">{shownCriterion}</p> : null}

      <div className="gate-answer-upstream" data-testid="gate-answer-upstream">
        {upstream.state === 'loading' ? <div className="gate-answer-empty">Loading the gate input…</div> : null}
        {upstream.state === 'unavailable' ? <div className="gate-answer-empty">{upstream.message}</div> : null}
        {upstream.state === 'ready' ? (
          <>
            <div className="gate-answer-from">From {upstream.from}</div>
            <pre className="gate-answer-text">{upstream.text || 'The upstream step sent no text.'}</pre>
            {upstream.truncated ? <div className="gate-answer-empty">Showing the start of a long input; the full text is in the run evidence.</div> : null}
          </>
        ) : null}
      </div>

      <label className="gate-answer-label" htmlFor={`gate-answer-${gateId}`}>Your response</label>
      <textarea
        id={`gate-answer-${gateId}`}
        className="gate-answer-input"
        value={response}
        placeholder="Answer the questions or explain what to change. Approve sends this to the next step."
        disabled={submitting !== ''}
        onChange={event => {
          setResponse(event.target.value)
          writeDraft(key, event.target.value)
        }}
      />
      <div className="gate-answer-actions">
        <button
          type="button"
          className="gate-answer-back"
          disabled={submitting !== '' || !trimmed}
          title={trimmed ? 'Send the response back as feedback' : 'Write what should change before sending back'}
          onClick={() => void decide('fail')}
        >{submitting === 'fail' ? 'Sending…' : 'Send back'}</button>
        <button
          type="button"
          className="gate-answer-approve"
          disabled={submitting !== ''}
          onClick={() => void decide('pass')}
        >{submitting === 'pass' ? 'Approving…' : 'Approve'}</button>
      </div>
    </section>
  )
}

export default HumanGateAnswerPanel
