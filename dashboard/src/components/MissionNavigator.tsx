/* The navigator: every mission with its runs, the runs that need the operator
 * first, and the sessions of open runs. Its look follows CHROTE's session
 * panel (dashboard/src/components/SessionPanel in the CHROTE repository):
 * collapsible groups with a count, 28px rows, the current row marked on its
 * left edge. A run's state is a word; nothing here is a status dot. */
import { useMemo, useRef, useState } from 'react'
import type { BoardSummary, RunStatusProjection } from './formationsTypes'
import { runInputExcerpt, runName, runsInListOrder, shortTime } from './runList'
import './missionNavigator.css'

/** Compact run index entry; event histories belong to selected detail. */
export type ListedRun = RunStatusProjection

const RUNS_SHOWN = 5

const stateWords: Record<string, string> = {
  waiting_human: 'your answer', running: 'running', blocked: 'blocked',
  succeeded: 'succeeded', picked: 'picked', failed: 'failed', canceled: 'canceled',
}

export function runStateWord(status: string): string {
  return stateWords[status] || status
}

/** Start times stay visible even when runs share a Bead or the same input. */
export function runRowLabel(run: RunStatusProjection, now = new Date()): string {
  const time = shortTime(run.startedAt, now)
  return time ? `${time}:${String(new Date(run.startedAt!).getSeconds()).padStart(2, '0')}` : run.runId
}

const needsOperator = (run: RunStatusProjection) => !run.final && (run.status === 'waiting_human' || run.status === 'blocked')

/** The sessions a run still holds, in the order they were created: a seat's cleanup ends its session unless it is kept on call. */
export function runSessions(run: ListedRun): Array<{ sessionName: string; nodeId: string; slotId: string }> {
  return run.sessions || []
}

export default function MissionNavigator({ missions, loaded, runs, selectedSlug, shownRunId, busy, readOnly, stepTitle, onEdit, onRun, onSession, onNew, onRename, onDelete }: {
  missions: BoardSummary[]
  loaded: boolean
  runs: ListedRun[]
  selectedSlug: string
  shownRunId: string
  /** A mission dialog is open; mission actions wait for it. */
  busy: boolean
  /** The mission has not opened yet, so it cannot be renamed or deleted. */
  readOnly: boolean
  /** A step's title when its mission is the one on the canvas; empty otherwise. */
  stepTitle: (slug: string, nodeId: string) => string
  onEdit: (slug: string) => void
  onRun: (slug: string, runId: string) => void
  onSession: (slug: string, runId: string, nodeId: string) => void
  onNew: (trigger: HTMLElement) => void
  onRename: (trigger: HTMLElement) => void
  onDelete: (trigger: HTMLElement) => void
}) {
  const [filter, setFilter] = useState('')
  const [opened, setOpened] = useState<Record<string, boolean>>({})
  const [allRuns, setAllRuns] = useState<Record<string, boolean>>({})
  const now = new Date()
  const needle = filter.trim().toLowerCase()

  const bySlug = useMemo(() => {
    const map = new Map<string, ListedRun[]>()
    for (const run of runs) map.set(run.missionSlug, [...(map.get(run.missionSlug) || []), run])
    return map
  }, [runs])
  const live = useMemo(() => new Set(missions.map(mission => mission.slug)), [missions])
  const titleOf = (slug: string) => missions.find(mission => mission.slug === slug)?.title || slug
  const matches = (run: ListedRun, title: string) => !needle || [title, runName(run), runRowLabel(run, now), run.beadId || '', runInputExcerpt(run), runStateWord(run.status)].some(text => text.toLowerCase().includes(needle))

  const needing = runsInListOrder(runs.filter(run => live.has(run.missionSlug) && needsOperator(run)), null).filter(run => matches(run, titleOf(run.missionSlug)))
  const sessions = runs.filter(run => live.has(run.missionSlug) && !run.final).flatMap(run => runSessions(run).map(session => ({ ...session, run })))
    .filter(session => !needle || [session.sessionName, titleOf(session.run.missionSlug)].some(text => text.toLowerCase().includes(needle)))
  const initialOpen = useRef(new Map<string, boolean>())
  const isOpen = (key: string, fallback: boolean) => {
    if (!initialOpen.current.has(key)) initialOpen.current.set(key, fallback)
    return needle ? true : opened[key] ?? initialOpen.current.get(key)!
  }
  const toggle = (key: string, fallback: boolean) => setOpened(current => ({ ...current, [key]: !(current[key] ?? initialOpen.current.get(key) ?? fallback) }))

  const group = (key: string, title: string, count: number, fallback: boolean, body: () => React.ReactNode, actions?: React.ReactNode, problem?: string) => {
    const open = isOpen(key, fallback)
    return (
      <section className="nav-group" key={key} data-testid={`nav-group-${key}`}>
        <div className="nav-group-hd">
          <button type="button" className="nav-group-toggle" aria-expanded={open} disabled={Boolean(problem)} title={problem || title} onClick={() => toggle(key, fallback)}>
            <span className="nav-caret" aria-hidden="true">{open ? '▼' : '▶'}</span>
            <span className="nav-group-name">{title}</span>
          </button>
          {actions}
          <span className="nav-count">{count}</span>
        </div>
        {problem ? <details className="nav-source-problem" data-testid={`source-problem-${key}`}><summary>Source details</summary><p>{problem}</p></details> : null}
        {open && !problem ? <div className="nav-rows">{body()}</div> : null}
      </section>
    )
  }

  const runRow = (run: ListedRun, secondary: string, id = 'nav-run') => (
    <button type="button" key={run.runId} className={`nav-row${run.runId === shownRunId ? ' current' : ''}`} aria-current={run.runId === shownRunId || undefined}
      data-testid={`${id}-${run.runId}`} title={[run.runId, run.startedAt ? `started ${new Date(run.startedAt).toLocaleString()}` : '', runStateWord(run.status), run.beadId, runInputExcerpt(run)].filter(Boolean).join(' · ')}
      onClick={() => onRun(run.missionSlug, run.runId)}>
      <span className="nav-row-line">
        <span className="nav-row-name"><span className="nav-run-number">{runName(run)}</span> <span className="nav-run-time">{runRowLabel(run, now)}</span></span>
        <span className={`nav-state ${run.status}`}>{runStateWord(run.status)}</span>
      </span>
      {secondary ? <span className="nav-row-sub">{secondary}</span> : null}
    </button>
  )
  const workOf = (run: ListedRun) => run.beadId || runInputExcerpt(run)

  return (
    <aside className="mission-nav" data-testid="mission-nav" aria-label="Missions and runs">
      <div className="mission-nav-hd">
        <span className="mission-nav-title">Missions</span>
        <button type="button" className="mission-nav-add" data-testid="new-board" aria-label="New mission" title="New mission" disabled={busy} onClick={event => onNew(event.currentTarget)}>+</button>
      </div>
      <div className="mission-nav-filter">
        <input type="search" aria-label="Filter missions and runs" placeholder="filter missions and runs" value={filter} onChange={event => setFilter(event.target.value)} />
      </div>
      <div className="mission-nav-list">
        {!loaded ? <div className="mission-nav-empty" role="status">Loading missions…</div> : missions.length === 0 ? <div className="mission-nav-empty">No missions yet</div> : null}
        {needing.length ? group('needs-you', 'Needs you', needing.length, true, () => needing.map(run => runRow(run, [titleOf(run.missionSlug), workOf(run)].filter(Boolean).join(' · '), 'nav-needs'))) : null}
        {missions.map(mission => {
          const own = runsInListOrder(bySlug.get(mission.slug) || [], null)
          const shown = own.filter(run => matches(run, mission.title))
          if (needle && !shown.length && !mission.title.toLowerCase().includes(needle)) return null
          const selected = mission.slug === selectedSlug
          const limit = allRuns[mission.slug] || needle ? shown.length : RUNS_SHOWN
          const visible = shown.slice(0, limit)
          // The shown run stays listed even when it is older than the first few.
          const current = shown.find(run => run.runId === shownRunId)
          if (current && !visible.includes(current)) visible.push(current)
          const actions = selected ? (
            <span className="nav-group-actions">
              <button type="button" aria-label="Rename mission" disabled={readOnly || busy} onClick={event => onRename(event.currentTarget)}>Rename</button>
              <button type="button" aria-label="Delete mission" className="danger" disabled={readOnly || busy} onClick={event => onDelete(event.currentTarget)}>Delete</button>
            </span>
          ) : null
          return group(mission.slug, mission.broken ? `${mission.slug} · cannot be read` : mission.title || mission.slug, own.length, selected, () => (
            <>
              <button type="button" className={`nav-row nav-edit${selected && !shownRunId ? ' current' : ''}`} aria-current={(selected && !shownRunId) || undefined}
                data-testid={`nav-mission-${mission.slug}`} onClick={() => onEdit(mission.slug)}>
                <span className="nav-row-line">
                  <span className="nav-row-name">Edit mission</span>
                </span>
              </button>
              {visible.length ? <div className="nav-runs-heading"><span>Runs</span><span>State</span></div> : null}
              {visible.map(run => runRow(run, workOf(run)))}
              {shown.length > visible.length ? (
                <button type="button" className="nav-row nav-more" onClick={() => setAllRuns(current => ({ ...current, [mission.slug]: true }))}>
                  {shown.length - visible.length} earlier {shown.length - visible.length === 1 ? 'run' : 'runs'}
                </button>
              ) : null}
            </>
          ), actions, mission.broken)
        })}
        {sessions.length ? group('sessions', 'Sessions', sessions.length, true, () => sessions.map(session => (
          <button type="button" key={session.sessionName} className="nav-row" title={`${session.sessionName} · ${titleOf(session.run.missionSlug)}`}
            data-testid={`nav-session-${session.sessionName}`} onClick={() => onSession(session.run.missionSlug, session.run.runId, session.nodeId)}>
            <span className="nav-row-line">
              <span className="nav-row-name">{stepTitle(session.run.missionSlug, session.nodeId) || titleOf(session.run.missionSlug)}</span>
              <span className="nav-state">{runName(session.run)}</span>
            </span>
            <span className="nav-row-sub">{session.sessionName}</span>
          </button>
        ))) : null}
      </div>
    </aside>
  )
}
