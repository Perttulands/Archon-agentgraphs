import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RunList } from './RunList'
import { runInputExcerpt, runSpan, runsInListOrder, shortTime, spanLabel } from './runList'
import type { RunStatusProjection } from './formationsTypes'

const at = (day: number, hours: number, minutes: number, seconds = 0) => new Date(2026, 9, day, hours, minutes, seconds).toISOString()
const run = (runId: string, status: string, final: boolean, extra: Partial<RunStatusProjection> = {}): RunStatusProjection => ({
  runId, status, final, missionSlug: 'scouting', inputCardId: 'inp', eventCount: 3, ...extra,
})

describe('run list model', () => {
  it('says when a run started, in local time', () => {
    const now = new Date(2026, 9, 1, 18, 0)
    expect(shortTime(at(1, 14, 2), now)).toBe('14:02')
    expect(shortTime(at(30, 9, 5), new Date(2026, 9, 31, 1, 0))).toBe('30 Oct 09:05')
    expect(shortTime(new Date(2025, 11, 31, 23, 59).toISOString(), now)).toBe('31 Dec 2025 23:59')
    expect(shortTime(undefined, now)).toBe('')
    expect(shortTime('not a time', now)).toBe('')
  })

  it('says how long a run took, or has run, in its two largest units', () => {
    expect([45, 192, 3600, 7500, 3 * 86400 + 4 * 3600, 2 * 86400].map(seconds => spanLabel(seconds * 1000))).toEqual(['45s', '3m 12s', '1h', '2h 5m', '3d 4h', '2d'])
    const now = new Date(2026, 9, 1, 14, 10)
    expect(runSpan(run('run_a', 'succeeded', true, { startedAt: at(1, 14, 0), updatedAt: at(1, 14, 3, 12) }), now)).toBe('3m 12s')
    expect(runSpan(run('run_b', 'running', false, { startedAt: at(1, 14, 0), updatedAt: at(1, 14, 1) }), now)).toBe('10m')
    expect(runSpan(run('run_c', 'running', false), now)).toBe('')
  })

  it('takes the first text input, on one line, as the excerpt', () => {
    expect(runInputExcerpt(run('run_a', 'running', false, { inputs: [{ name: 'repo', kind: 'folder', value: '/work' }, { name: 'topic', kind: 'text', value: '  Captions\n for long   videos ' }] }))).toBe('Captions for long videos')
    expect(runInputExcerpt(run('run_b', 'running', false, { inputs: [{ name: 'repo', kind: 'folder', value: '/work' }] }))).toBe('/work')
    expect(runInputExcerpt(run('run_c', 'running', false, { inputs: [{ name: 'brief', kind: 'text', value: 'x'.repeat(200) }] }), 10)).toBe(`${'x'.repeat(9)}…`)
    expect(runInputExcerpt(run('run_d', 'running', false))).toBe('')
  })

  it('lists open runs by attention, then finished runs newest first, with the shown run current', () => {
    const runs = [run('run_01A', 'succeeded', true), run('run_01B', 'running', false), run('run_01C', 'failed', true), run('run_01D', 'waiting_human', false)]
    expect(runsInListOrder(runs, null).map(item => item.runId)).toEqual(['run_01D', 'run_01B', 'run_01C', 'run_01A'])
    // The shown run keeps its own latest status: here it finished since the list was read.
    const shown = { ...runs[1], status: 'succeeded', final: true }
    expect(runsInListOrder(runs, shown).map(item => `${item.runId}:${item.status}`)).toEqual(['run_01D:waiting_human', 'run_01C:failed', 'run_01B:succeeded', 'run_01A:succeeded'])
  })
})

describe('RunList', () => {
  afterEach(cleanup)

  const runs = [
    run('run_01OLD', 'failed', true, { startedAt: at(1, 9, 0), updatedAt: at(1, 9, 4), startedBy: 'agent:archon', inputs: [{ name: 'brief', kind: 'text', value: 'Draft the importer' }] }),
    run('run_01WAIT', 'waiting_human', false, { startedAt: at(1, 10, 0), updatedAt: at(1, 10, 1), startedBy: 'human:ui', inputs: [{ name: 'topic', kind: 'text', value: 'Speaker labels' }] }),
    run('run_01BARE', 'succeeded', true, { startedAt: at(1, 8, 0), updatedAt: at(1, 8, 0, 30) }),
  ]

  it('opens on one click and identifies every run without its ID', () => {
    const onChoose = vi.fn()
    render(<RunList runs={runs} shown={runs[1]} missionTitle="Scouting" onChoose={onChoose} onPutAway={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Runs (3)' }))
    const list = screen.getByRole('dialog', { name: 'Runs of Scouting' })
    const rows = within(list).getAllByRole('button').filter(button => button.classList.contains('run-row'))
    const cells = rows.map(row => [...row.querySelectorAll('span')].map(span => span.textContent))
    // An open run's time so far keeps growing; its cell starts with when it started.
    expect(cells[0][1]).toMatch(new RegExp(`^${shortTime(at(1, 10, 0))} · for \\d`))
    cells[0][1] = 'open'
    expect(cells).toEqual([
      ['Waiting for your answer', 'open', 'Speaker labels', 'the operator in the cockpit'],
      ['Failed', `${shortTime(at(1, 9, 0))} · took 4m`, 'Draft the importer', 'the archon CLI'],
      ['Succeeded', `${shortTime(at(1, 8, 0))} · took 30s`, 'no inputs recorded', ''],
    ])
    expect(rows[0]).toHaveAttribute('aria-current', 'true')
    expect(rows[0]).toHaveFocus()
    expect(within(list).queryByRole('button', { name: 'Put this run away' })).toBeNull()
    fireEvent.click(rows[1])
    expect(onChoose).toHaveBeenCalledWith('run_01OLD')
    expect(screen.queryByRole('dialog', { name: 'Runs of Scouting' })).toBeNull()
  })

  it('puts a finished run away, and closes on Escape', () => {
    const onPutAway = vi.fn()
    render(<RunList runs={runs} shown={runs[0]} missionTitle="Scouting" onChoose={vi.fn()} onPutAway={onPutAway} />)
    fireEvent.click(screen.getByRole('button', { name: 'Runs (3)' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: 'Runs of Scouting' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Runs (3)' }))
    fireEvent.click(screen.getByRole('button', { name: 'Put this run away' }))
    expect(onPutAway).toHaveBeenCalledTimes(1)
  })

  it('shows nothing for a mission without runs', () => {
    const { container } = render(<RunList runs={[]} shown={null} missionTitle="Scouting" onChoose={vi.fn()} onPutAway={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()
  })
})
