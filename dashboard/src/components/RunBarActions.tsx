import { useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { RunStatusProjection } from './formationsTypes'
import type { RunPoint } from './formationsRunState'
import { useEscapeKey } from './useEscapeKey'
import { hasGateDraft } from './HumanGateAnswerPanel'
import '../styles/formations-run.css'

// The run bar's recoveries: Resume only when resuming can make progress, a
// plain statement when a block cannot resume (form-n7u.6), and Stop behind a
// confirmation that names the run and what ends with it (form-n7u.8).

/** The reason a cancel records when the operator gives none. */
export const DEFAULT_STOP_REASON = 'operator stop'

export interface RunBarActionsProps {
  run: RunStatusProjection
  point: RunPoint | null
  /** The point's node title. */
  pointTitle: string
  boardTitle: string
  titleOf: (nodeId: string) => string
  /** The human request the run waits on, if any. */
  pendingGate: { title: string; requestedSeq: number } | null
  /** The block is the pause after the operator's answer, which resumes on its own. */
  paused?: boolean
  onResume: () => void
  /** Resolves once the run is canceled, false when the stop failed. */
  onStop: (reason: string) => Promise<boolean>
}

export default function RunBarActions({ run, point, pointTitle, boardTitle, titleOf, pendingGate, paused = false, onResume, onStop }: RunBarActionsProps) {
  const [confirming, setConfirming] = useState(false)
  const stopButton = useRef<HTMLButtonElement>(null)
  if (run.final) return null
  const cannotResume = run.status === 'blocked' && !run.resumeAllowed && !paused
  return (
    <>
      {run.resumeAllowed ? <button type="button" onClick={onResume}>Resume run</button> : null}
      {cannotResume ? (
        <span className="run-note" role="note" data-testid="run-not-resumable" title="Resuming cannot make progress from this block. Stop the run and start a new one.">Can’t resume. Start a new run.</span>
      ) : null}
      <button type="button" ref={stopButton} className="run-stop" aria-haspopup="dialog" onClick={() => setConfirming(true)}>Stop run</button>
      {confirming ? createPortal(
        <StopRunDialog run={run} point={point} pointTitle={pointTitle} boardTitle={boardTitle} titleOf={titleOf} pendingGate={pendingGate}
          onStop={onStop}
          onClose={() => {
            setConfirming(false)
            stopButton.current?.focus()
          }} />,
        // Out of the run bar's stacking context, so the dialog sits above floating windows.
        stopButton.current?.closest('.fmx') || document.body,
      ) : null}
    </>
  )
}

/** What stopping this run ends, in the order the operator should read it. */
export function stopRunConsequences(run: RunStatusProjection, point: RunPoint | null, pointTitle: string, titleOf: (nodeId: string) => string, pendingGate: RunBarActionsProps['pendingGate']): string[] {
  const lines: string[] = []
  if (point?.kind === 'running') lines.push(`The agents working on ${pointTitle || point.nodeId} are interrupted.`)
  if (pendingGate) {
    lines.push(hasGateDraft(run.runId, pendingGate.requestedSeq)
      ? `${pendingGate.title} stops waiting for you, and your unsent answer is not sent.`
      : `${pendingGate.title} stops waiting for you.`)
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

function StopRunDialog({ run, point, pointTitle, boardTitle, titleOf, pendingGate, onStop, onClose }: Omit<RunBarActionsProps, 'onResume' | 'paused'> & { onClose: () => void }) {
  const [reason, setReason] = useState('')
  const [stopping, setStopping] = useState(false)
  const dialogRef = useRef<HTMLDivElement>(null)
  const keepButton = useRef<HTMLButtonElement>(null)
  // Keep running is the safe default, so Enter on an opened dialog stops nothing.
  useLayoutEffect(() => { keepButton.current?.focus() }, [])
  useEscapeKey(!stopping, onClose)
  const runName = `…${run.runId.slice(-6)}`
  const consequences = stopRunConsequences(run, point, pointTitle, titleOf, pendingGate)
  return (
    <div ref={dialogRef} className="pop stop-run" role="alertdialog" aria-modal="true" aria-labelledby="stop-run-title" aria-describedby="stop-run-consequences"
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
          <strong>{boardTitle || run.boardSlug}</strong>{run.beadId ? ` · ${run.beadId}` : ''} · run <span title={run.runId}>{runName}</span>
          {pointTitle && point ? <>, {point.kind === 'waiting' ? 'waiting for you at' : point.kind === 'running' ? 'running' : 'stopped at'} <strong>{pointTitle}</strong></> : null}
        </p>
        <ul className="stop-run-consequences" id="stop-run-consequences">
          {consequences.map(line => <li key={line}>{line}</li>)}
        </ul>
        <label htmlFor="stop-run-reason">Why (optional)</label>
        <textarea id="stop-run-reason" value={reason} disabled={stopping} placeholder={DEFAULT_STOP_REASON}
          aria-describedby="stop-run-reason-help" onChange={event => setReason(event.target.value)} />
        <p id="stop-run-reason-help" className="field-note">Recorded with the cancel and shown with the run.</p>
        <div className="pop-actions">
          <button ref={keepButton} className="cancel" type="button" disabled={stopping} onClick={onClose}>Keep running</button>
          <button className="retire" type="submit" disabled={stopping}>{stopping ? 'Stopping…' : 'Stop run'}</button>
        </div>
      </form>
    </div>
  )
}
