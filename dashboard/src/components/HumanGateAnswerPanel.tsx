import { createContext, useContext, useEffect, useRef, useState } from 'react'
import type { GateTalkPanel } from '../talk/useGateTalk'
import type { GateRoute } from './formationsApi'
import { gateRouteWords } from './runOutcome'
import './humanGateAnswer.css'

export type GateUpstream =
  | { state: 'loading' }
  | { state: 'ready'; from: string; text: string; truncated: boolean; criterion: string; routes?: GateRoute[] }
  | { state: 'unavailable'; message: string }

export type GateDecision = 'pass' | 'fail'

interface HumanGateAnswerPanelProps {
  runId: string
  gateId: string
  requestedSeq: number
  gateTitle: string
  criterion: string
  upstream: GateUpstream
  onOpenEvidence?: () => void
  /** On a session-channel run: the agents the gate asked, or why it fell back to a notification. */
  talk?: GateTalkPanel | null
  /** Resolves true once the verdict is recorded. */
  onDecide: (verdict: GateDecision, response: string) => Promise<boolean>
  /** A node's title, for a limit that names a step. */
  titleOf?: (nodeId: string) => string
  /** In a floating window, whose title bar already says what needs an answer. */
  framed?: boolean
}

function draftKey(runId: string, requestedSeq: number) {
  return `archon.gateResponse.${runId}.${requestedSeq}`
}

/**
 * Where a framing window learns whether the draft is saved in this browser, so
 * closing it can say truthfully whether the draft comes back.
 */
export const GateDraftSavedContext = createContext<((saved: boolean) => void) | null>(null)

/** Whether the operator has an unsent answer to this request saved in this browser. */
export function hasGateDraft(runId: string, requestedSeq: number): boolean {
  try {
    return Boolean(window.localStorage.getItem(draftKey(runId, requestedSeq))?.trim())
  } catch {
    return false
  }
}

function readDraft(key: string) {
  try {
    const text = window.localStorage.getItem(key) || ''
    return { text, cue: text ? 'Draft restored from this browser. Not submitted.' : '' }
  } catch {
    return { text: '', cue: 'Browser draft unavailable. Your response is not submitted.' }
  }
}

function writeDraft(key: string, text: string) {
  try {
    if (text) window.localStorage.setItem(key, text)
    else window.localStorage.removeItem(key)
    return true
  } catch {
    // A draft is a convenience; the typed response stays in the textarea.
    return false
  }
}

/**
 * Shows what a pending human gate received and records the operator's answer.
 * Approve delivers the response to the next formation with the gate input;
 * Send back returns it to the pushback route as feedback.
 */
function HumanGateAnswerPanel({ runId, gateId, requestedSeq, gateTitle, criterion, upstream, talk, onOpenEvidence, onDecide, titleOf = id => id, framed = false }: HumanGateAnswerPanelProps) {
  const key = draftKey(runId, requestedSeq)
  const [draft, setDraft] = useState(() => readDraft(key))
  const response = draft.text
  const [submitting, setSubmitting] = useState<GateDecision | ''>('')
  const [loadingFile, setLoadingFile] = useState(false)
  const [fileError, setFileError] = useState('')
  const fileInput = useRef<HTMLInputElement>(null)
  const panel = useRef<HTMLElement>(null)
  const trimmed = response.trim()
  // The run's frozen criterion wins over the mission's current draft.
  const shownCriterion = (upstream.state === 'ready' && upstream.criterion) || criterion
  // Where each decision leads, from the run's frozen board (form-n7u.7).
  const routes = upstream.state === 'ready' ? upstream.routes : undefined
  const approve = gateRouteWords('pass', routes?.find(route => route.verdict === 'pass'), titleOf)
  const sendBack = gateRouteWords('fail', routes?.find(route => route.verdict === 'fail'), titleOf)

  const reportSaved = useContext(GateDraftSavedContext)
  // A draft this browser could not even read cannot be kept either.
  const unavailable = draft.cue.startsWith('Browser draft unavailable')
  useEffect(() => { if (unavailable) reportSaved?.(false) }, [reportSaved, unavailable])
  const updateDraft = (text: string) => {
    const saved = writeDraft(key, text)
    reportSaved?.(saved || !text)
    setDraft({ text, cue: saved
      ? text ? 'Draft saved in this browser. Not submitted.' : 'Draft cleared. Not submitted.'
      : 'Draft not saved in this browser. Keep this page open. Not submitted.' })
  }

  // Adapted from CHROTE FilesViewContent's hidden file input and fileService's
  // strict UTF-8 decoding. This reads locally into a draft, with no server upload.
  const loadResponseFile = async (file: File) => {
    setLoadingFile(true)
    setFileError('')
    try {
      const text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(await file.arrayBuffer())
      updateDraft(text)
    } catch {
      setFileError('Could not read this file as UTF-8 text. Your response has not changed.')
    } finally {
      setLoadingFile(false)
    }
  }

  const decide = async (verdict: GateDecision) => {
    if (submitting || loadingFile) return
    setSubmitting(verdict)
    try {
      if (await onDecide(verdict, response)) {
        const cleared = writeDraft(key, '')
        setDraft(current => ({ ...current, cue: cleared ? 'Answer submitted.' : 'Answer submitted. The browser draft could not be cleared.' }))
      }
    } finally {
      setSubmitting('')
    }
  }

  return (
    <section ref={panel} className={`gate-answer${framed ? ' framed' : ''}`} role={framed ? 'region' : 'dialog'} aria-label={framed ? `Your answer to ${gateTitle}` : `Answer gate ${gateTitle}`} data-testid="gate-answer" onPointerDown={event => event.stopPropagation()}>
      <header className="gate-answer-hd">
        {framed ? null : <span className="gate-answer-kicker">Needs your answer</span>}
        {framed ? null : <span className="gate-answer-title">{gateTitle}</span>}
        <span className="gate-answer-id">{gateId} · request #{requestedSeq}</span>
      </header>
      {talk?.fallbackReason ? (
        <p className="gate-answer-fallback" role="note">The agents are not available for this gate: {talk.fallbackReason}. Answer here.</p>
      ) : talk ? (
        <div className="gate-answer-talk">
          <button type="button" className="gate-answer-talk-button" disabled={!talk.seats.length}
            // The terminals open beside the whole panel, so answering here stays in view.
            onClick={event => talk.onTalk(panel.current ?? event.currentTarget)}>Talk with {talk.formationTitle || 'the agents'}</button>
          <span className="gate-answer-talk-note">{talk.seats.length > 1
            ? `The ${talk.seats.length} agents that did the work are waiting in their terminals. They record the decision you confirm, or you answer here.`
            : talk.seats.length
              ? 'The agent that did the work is waiting in its terminal. It records the decision you confirm, or you answer here.'
              : 'The question is on its way to the agents. You can answer here meanwhile.'}</span>
        </div>
      ) : null}
      {shownCriterion ? <p className="gate-answer-criterion">{shownCriterion}</p> : null}

      <div className="gate-answer-upstream" data-testid="gate-answer-upstream">
        {upstream.state === 'loading' ? <div className="gate-answer-empty">Loading the gate input…</div> : null}
        {upstream.state === 'unavailable' ? <div className="gate-answer-empty">{upstream.message}</div> : null}
        {upstream.state === 'ready' ? (
          <>
            <div className="gate-answer-from">From {upstream.from}</div>
            <pre className="gate-answer-text">{upstream.text || 'The upstream step sent no text.'}</pre>
            {upstream.truncated ? <div className="gate-answer-empty">Showing the start of a long input.
              {onOpenEvidence ? <button type="button" className="gate-answer-evidence" onClick={onOpenEvidence}>Open run evidence</button> : null}
            </div> : null}
          </>
        ) : null}
      </div>

      <label className="gate-answer-label" htmlFor={`gate-answer-${gateId}`}>Your response</label>
      <textarea
        id={`gate-answer-${gateId}`}
        className="gate-answer-input"
        value={response}
        placeholder="Answer the questions or explain what to change. Approve passes this on with the gate input; Send back returns it as feedback."
        disabled={submitting !== '' || loadingFile}
        onChange={event => updateDraft(event.target.value)}
      />
      <input ref={fileInput} type="file" hidden aria-label="Response file" accept="text/*,.txt,.md,.json" disabled={submitting !== '' || loadingFile}
        onChange={event => {
          const file = event.currentTarget.files?.[0]
          event.currentTarget.value = ''
          if (file) void loadResponseFile(file)
        }} />
      <button type="button" className="gate-answer-load" disabled={submitting !== '' || loadingFile}
        onClick={() => fileInput.current?.click()}>{loadingFile ? 'Loading response file…' : 'Load response file'}</button>
      <p className="gate-answer-draft">Load a UTF-8 text file from this device to replace your draft. Review or edit it before submitting.</p>
      {fileError ? <p className="gate-answer-file-error" role="alert">{fileError}</p> : null}
      {draft.cue ? <p className="gate-answer-draft" role="status">{draft.cue}</p> : null}
      {/* The consequences sit with the decisions, so they stay in view as the input grows. */}
      <div className="gate-answer-actions">
        {approve.outcome || sendBack.outcome ? (
          <ul className="gate-answer-routes" aria-label="Where your answer leads" data-testid="gate-answer-routes">
            {[approve, sendBack].filter(words => words.outcome).map(words => (
              <li key={words.outcome} className={words.blocks ? 'blocks' : words.last ? 'last' : ''}>{words.outcome}</li>
            ))}
          </ul>
        ) : null}
        <button
          type="button"
          className={`gate-answer-back${sendBack.blocks ? ' blocks' : ''}`}
          disabled={submitting !== '' || loadingFile || !trimmed}
          title={trimmed ? sendBack.outcome || 'Send the response back as feedback' : 'Write what should change before sending back'}
          onClick={() => void decide('fail')}
        >{submitting === 'fail' ? 'Sending…' : sendBack.button}</button>
        <button
          type="button"
          className={`gate-answer-approve${approve.blocks ? ' blocks' : ''}`}
          disabled={submitting !== '' || loadingFile}
          title={approve.outcome || undefined}
          onClick={() => void decide('pass')}
        >{submitting === 'pass' ? 'Approving…' : approve.button}</button>
      </div>
    </section>
  )
}

export default HumanGateAnswerPanel
