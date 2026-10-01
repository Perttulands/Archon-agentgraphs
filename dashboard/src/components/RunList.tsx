import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { RunStatusProjection } from './formationsTypes'
import { runStatusLabel } from './formationsRunDiscovery'
import { runInputExcerpt, runSpan, runsInListOrder, shortTime } from './runList'
import { fileAnchor, useFileWindows } from '../files/FileWindows'
import { runMissionFileRequest } from '../files/fileWindowModel'
import './runList.css'

/** When the shown run started and how long it took or has run. */
export function RunWhen({ run }: { run: RunStatusProjection }) {
  const span = runSpan(run)
  if (!run.startedAt) return null
  return <span className="run-when" title={`Started ${new Date(run.startedAt).toLocaleString()}${run.startedBy ? ` by ${run.startedBy}` : ''}`}>
    started {shortTime(run.startedAt)}{span ? ` · ${run.final ? 'took' : 'for'} ${span}` : ''}{run.startedBy ? ` · ${run.startedBy}` : ''}
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

/**
 * The mission's runs, one click from its run bar (archon-o7p.2). Each row says
 * when the run started, how long it took or has run, its status, the start of
 * what it was given and who drives it, so no run needs its ID to be found.
 * Choosing a row shows that run; a finished run shown can be put away.
 */
export function RunList({ runs, shown, missionTitle, onChoose, onPutAway }: {
  runs: RunStatusProjection[]
  shown: RunStatusProjection | null
  missionTitle: string
  onChoose: (runId: string) => void
  onPutAway: () => void
}) {
  const [menu, setMenu] = useState<{ left: number; top: number } | null>(null)
  const buttonRef = useRef<HTMLButtonElement | null>(null)
  const listRef = useRef<HTMLDivElement | null>(null)
  const ordered = runsInListOrder(runs, shown)
  const now = new Date()

  useEffect(() => {
    if (!menu) return
    const close = (event: Event) => {
      if (event instanceof KeyboardEvent) {
        if (event.key !== 'Escape') return
        buttonRef.current?.focus()
      } else if (listRef.current?.contains(event.target as Node) || buttonRef.current?.contains(event.target as Node)) {
        return
      }
      setMenu(null)
    }
    document.addEventListener('pointerdown', close, true)
    document.addEventListener('keydown', close)
    return () => {
      document.removeEventListener('pointerdown', close, true)
      document.removeEventListener('keydown', close)
    }
  }, [menu])
  const open = Boolean(menu)
  useEffect(() => {
    if (open) listRef.current?.querySelector<HTMLElement>('[aria-current="true"], .run-row')?.focus({ preventScroll: true })
  }, [open])

  if (!ordered.length) return null
  return (
    <>
      <button ref={buttonRef} type="button" className="run-list-button" aria-haspopup="dialog" aria-expanded={open}
        title={`Every run of ${missionTitle}`}
        onClick={event => {
          const rect = event.currentTarget.getBoundingClientRect()
          setMenu(menu ? null : { left: Math.max(8, Math.min(rect.left, window.innerWidth - 640)), top: rect.bottom + 4 })
        }}>Runs ({ordered.length})</button>
      {/* Drawn at the page's top level, like the produced menu, to open above every window. */}
      {menu ? createPortal((
        <div ref={listRef} className="run-list" role="dialog" aria-label={`Runs of ${missionTitle}`} style={{ left: menu.left, top: menu.top }}
          onKeyDown={event => {
            const rows = [...(listRef.current?.querySelectorAll<HTMLElement>('.run-row') || [])]
            const at = rows.indexOf(document.activeElement as HTMLElement)
            const step = { ArrowDown: 1, ArrowUp: -1 }[event.key]
            if (step && rows.length) {
              event.preventDefault()
              rows[(at + step + rows.length) % rows.length]?.focus()
            }
          }}>
          <ol className="run-rows">
            {ordered.map(run => {
              const current = run.runId === shown?.runId
              const excerpt = runInputExcerpt(run)
              const span = runSpan(run, now)
              return (
                <li key={run.runId}>
                  <button type="button" className={`run-row${current ? ' current' : ''}`} aria-current={current || undefined}
                    title={`${runStatusLabel(run.status)}${run.startedAt ? ` · started ${new Date(run.startedAt).toLocaleString()}` : ''} · ${run.runId}`}
                    onClick={() => { setMenu(null); onChoose(run.runId) }}>
                    <span className={`run-row-status ${run.status}`}>{runStatusLabel(run.status)}</span>
                    <span className="run-row-when">{shortTime(run.startedAt, now) || 'start unknown'}{span ? ` · ${run.final ? 'took' : 'for'} ${span}` : ''}</span>
                    <span className={`run-row-input${excerpt ? '' : ' none'}`}>{excerpt || 'no inputs recorded'}</span>
                    <span className="run-row-driver">{run.startedBy || ''}</span>
                  </button>
                </li>
              )
            })}
          </ol>
          {shown?.final ? (
            <div className="run-list-foot">
              <button type="button" onClick={() => { setMenu(null); onPutAway() }}>Put this run away</button>
            </div>
          ) : null}
        </div>
      ), document.body) : null}
    </>
  )
}
