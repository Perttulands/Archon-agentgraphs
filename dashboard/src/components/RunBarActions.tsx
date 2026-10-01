import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import type { RunStatusProjection } from './formationsTypes'
import type { RunPoint } from './formationsRunState'
import { hasGateDraft } from './HumanGateAnswerPanel'
import { fetchRunProblems } from '../evidence/runEvidenceApi'
import { DEFAULT_STOP_REASON, runLimitPhrase } from './runOutcome'
import '../styles/formations-run.css'

// The run bar's recoveries: Resume only when resuming can make progress, a
// plain statement of why a block cannot resume (archon-n7u.6), and Stop behind a
// modal confirmation that names the run and what ends with it (archon-n7u.8).

export { DEFAULT_STOP_REASON }

export interface RunBarActionsProps {
  run: RunStatusProjection
  /** Every gate waiting and step running or stopped now, the first most needing the operator. */
  points: RunPoint[]
  boardTitle: string
  titleOf: (nodeId: string) => string
  /** The human requests the run waits on; several can wait at once (archon-o7p.11). */
  waitingGates: { title: string; requestedSeq: number }[]
  onResume: () => void
  /** Resolves once the run is canceled, false when the stop failed. */
  onStop: (reason: string) => Promise<boolean>
}

/** The recorded fact of the run's latest block: its limit, else its reason. */
function useBlockFact(runId: string, active: boolean, titleOf: (nodeId: string) => string): string {
  const [fact, setFact] = useState<{ runId: string; limit?: Parameters<typeof runLimitPhrase>[0]; reason: string } | null>(null)
  useEffect(() => {
    if (!active) return
    let current = true
    fetchRunProblems(runId).then(problems => {
      const block = [...problems].reverse().find(problem => problem.type === 'run_blocked')
      if (current) setFact({ runId, limit: block?.limit, reason: block?.reason.text.trim() || '' })
    }, () => { if (current) setFact({ runId, reason: '' }) })
    return () => { current = false }
  }, [active, runId])
  if (!active || fact?.runId !== runId) return ''
  return fact.limit ? runLimitPhrase(fact.limit, titleOf) : fact.reason
}

export default function RunBarActions({ run, points, boardTitle, titleOf, waitingGates, onResume, onStop }: RunBarActionsProps) {
  const [confirming, setConfirming] = useState(false)
  const stopButton = useRef<HTMLButtonElement>(null)
  const returnFocus = useRef(false)
  const cannotResume = !run.final && run.status === 'blocked' && !run.resumeAllowed
  const fact = useBlockFact(run.runId, cannotResume, titleOf)
  // Focus goes back to Stop once the dialog is gone and the page is no longer inert.
  useEffect(() => {
    if (confirming || !returnFocus.current) return
    returnFocus.current = false
    stopButton.current?.focus()
  }, [confirming])
  if (run.final) return null
  return (
    <>
      {run.resumeAllowed ? <button type="button" onClick={onResume}>Resume run</button> : null}
      {cannotResume ? (
        <span className="run-note" role="note" data-testid="run-not-resumable" title={fact ? `This block cannot be resumed: ${fact}.` : 'This block cannot be resumed.'}>
          {fact ? `Can’t resume: ${fact}.` : 'Can’t resume.'}
        </span>
      ) : null}
      <button type="button" ref={stopButton} className="run-stop" aria-haspopup="dialog" onClick={() => setConfirming(true)}>Stop run</button>
      {confirming ? (
        <StopRunDialog run={run} points={points} boardTitle={boardTitle} titleOf={titleOf} waitingGates={waitingGates}
          onStop={onStop}
          onClose={() => {
            returnFocus.current = true
            setConfirming(false)
          }} />
      ) : null}
    </>
  )
}

/** Names in a sentence: "A", "A and B", "A, B and C". */
function andList(names: string[]): string {
  return names.length > 1 ? `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}` : names[0] || ''
}

/** What stopping this run ends, in the order the operator should read it. */
export function stopRunConsequences(run: RunStatusProjection, points: RunPoint[], titleOf: (nodeId: string) => string, waitingGates: RunBarActionsProps['waitingGates']): string[] {
  const lines: string[] = []
  const working = points.filter(point => point.kind === 'running').map(point => titleOf(point.nodeId) || point.nodeId)
  if (working.length) lines.push(`The agents working on ${andList(working)} are interrupted.`)
  for (const gate of waitingGates) {
    lines.push(hasGateDraft(run.runId, gate.requestedSeq)
      ? `${gate.title} stops waiting for you, and your unsent answer is not sent.`
      : `${gate.title} stops waiting for you.`)
  }
  const seats = run.onCallSeats || []
  if (seats.length) {
    const names = seats.map(seat => `${titleOf(seat.nodeId) || seat.nodeId} (${seat.slotId})`)
    lines.push(`${seats.length === 1 ? 'The agent seat' : `The ${seats.length} agent seats`} kept on call end${seats.length === 1 ? 's' : ''}: ${names.join(', ')}. Archon waits up to a minute for each agent to go idle, then closes its terminal.`)
  } else {
    lines.push('No agent seats are kept on call for this run.')
  }
  lines.push('The run ends as canceled and cannot be resumed. Its workspace and outputs stay.')
  return lines
}

/**
 * A modal layer: a backdrop over the page, with everything else in the
 * document inert while it is open, so nothing behind it takes a click, the
 * keyboard or a screen reader's attention.
 */
function ModalLayer({ onEscape, children }: { onEscape: () => void; children: ReactNode }) {
  const layer = useRef<HTMLDivElement>(null)
  const escape = useRef(onEscape)
  escape.current = onEscape
  useLayoutEffect(() => {
    const own = layer.current
    const made = [...document.body.children].filter(element => element !== own && !element.hasAttribute('inert'))
    for (const element of made) element.setAttribute('inert', '')
    return () => { for (const element of made) element.removeAttribute('inert') }
  }, [])
  // Escape closes this layer alone: it is handled before any other listener
  // (window capture) and goes no further, so a menu or window beneath it
  // does not close with it.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      event.stopPropagation()
      escape.current()
    }
    window.addEventListener('keydown', onKeyDown, true)
    return () => window.removeEventListener('keydown', onKeyDown, true)
  }, [])
  // .fmx gives the dialog the cockpit's theme; the layer itself only covers the page.
  return createPortal(
    <div ref={layer} className="fmx stop-run-layer" data-testid="stop-run-layer">
      <div className="stop-run-backdrop" aria-hidden="true" />
      {children}
    </div>,
    document.body,
  )
}

function StopRunDialog({ run, points, boardTitle, titleOf, waitingGates, onStop, onClose }: Omit<RunBarActionsProps, 'onResume'> & { onClose: () => void }) {
  const [reason, setReason] = useState('')
  const [stopping, setStopping] = useState(false)
  const keepButton = useRef<HTMLButtonElement>(null)
  // Keep running is the safe default, so Enter on an opened dialog stops nothing.
  useLayoutEffect(() => { keepButton.current?.focus() }, [])
  const runName = `…${run.runId.slice(-6)}`
  const consequences = stopRunConsequences(run, points, titleOf, waitingGates)
  const where = points.map(point => {
    const title = titleOf(point.nodeId) || point.nodeId
    return title ? `${point.kind === 'waiting' ? 'waiting for you at' : point.kind === 'running' ? 'running' : 'stopped at'} ${title}` : ''
  }).filter(Boolean).join(' · ')
  return (
    <ModalLayer onEscape={() => { if (!stopping) onClose() }}>
      <div className="pop stop-run" role="alertdialog" aria-modal="true" aria-labelledby="stop-run-title" aria-describedby="stop-run-consequences"
        data-testid="stop-run-dialog" onPointerDown={event => event.stopPropagation()}
        onKeyDown={event => {
          if (event.key !== 'Tab') return
          const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('button:not(:disabled), textarea:not(:disabled)'))
          const first = controls[0]
          const last = controls[controls.length - 1]
          if (event.shiftKey ? document.activeElement === first : document.activeElement === last) {
            event.preventDefault()
            ;(event.shiftKey ? last : first)?.focus()
          }
        }}>
        <div className="pop-head">
          <span className="pt" id="stop-run-title">Stop run {runName}?</span>
          <button className="x" type="button" aria-label="Keep the run going" disabled={stopping} onClick={onClose}>x</button>
        </div>
        <form className="pop-body" onSubmit={async event => {
          event.preventDefault()
          setStopping(true)
          const stopped = await onStop(reason.trim() || DEFAULT_STOP_REASON)
          setStopping(false)
          if (stopped) onClose()
        }}>
          <p className="stop-run-what">
            <strong>{boardTitle || run.missionSlug}</strong>{run.beadId ? ` · ${run.beadId}` : ''} · run <span title={run.runId}>{runName}</span>
            {where ? <>, <strong>{where}</strong></> : null}
          </p>
          <ul className="stop-run-consequences" id="stop-run-consequences">
            {consequences.map(line => <li key={line}>{line}</li>)}
          </ul>
          <label htmlFor="stop-run-reason">Why (optional)</label>
          <textarea id="stop-run-reason" value={reason} disabled={stopping} placeholder="Say why, so the run records it"
            aria-describedby="stop-run-reason-help" onChange={event => setReason(event.target.value)} />
          <p id="stop-run-reason-help" className="field-note">Recorded with the cancel and shown with the run.</p>
          <div className="pop-actions">
            <button ref={keepButton} className="cancel" type="button" disabled={stopping} onClick={onClose}>Keep running</button>
            <button className="retire" type="submit" disabled={stopping}>{stopping ? 'Stopping…' : 'Stop run'}</button>
          </div>
        </form>
      </div>
    </ModalLayer>
  )
}
