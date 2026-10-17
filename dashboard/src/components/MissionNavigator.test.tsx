import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import MissionNavigator, { runRowLabel } from './MissionNavigator'
import { RunWhen } from './RunList'
import type { RunStatusProjection } from './formationsTypes'

const now = new Date(2026, 9, 17, 12, 0)
const run = (runId: string, second: number, extra: Partial<RunStatusProjection> = {}): RunStatusProjection => ({
  runId, missionSlug: 'character', inputCardId: 'input', eventCount: 4, status: 'succeeded', final: true,
  startedAt: new Date(2026, 9, 17, 11, 45, second).toISOString(), ...extra,
})

describe('run identity', () => {
  it('keeps start times distinct for repeated work within the same minute', () => {
    expect(runRowLabel(run('run_a', 1, { beadId: 'archon-task' }), now)).toBe('11:45:01')
    expect(runRowLabel(run('run_b', 34, { beadId: 'archon-task' }), now)).toBe('11:45:34')
    expect(runRowLabel(run('run_c', 0, { startedAt: undefined }), now)).toBe('run_c')
  })

  it('shows a start and status above the Bead or input, and preserves both in the title and filter', () => {
    const runs = [run('run_a', 1, { beadId: 'archon-task', inputs: [{ name: 'subject', kind: 'text', value: 'Repeated character request' }] }),
      run('run_b', 34, { status: 'failed', inputs: [{ name: 'subject', kind: 'text', value: 'Repeated character request' }] })]
    render(<MissionNavigator missions={[{ id: 'mission', slug: 'character', title: 'Character', rev: 1, etag: 'mission-1' }]} loaded runs={runs} selectedSlug="character" shownRunId=""
      busy={false} readOnly={false} stepTitle={() => ''} onEdit={vi.fn()} onRun={vi.fn()} onSession={vi.fn()} onNew={vi.fn()} onRename={vi.fn()} onDelete={vi.fn()} />)
    const a = screen.getByTestId('nav-run-run_a')
    expect(a.querySelector('.nav-row-name')).toHaveTextContent('11:45:01')
    expect(a.querySelector('.nav-row-sub')).toHaveTextContent('archon-task')
    expect(a).toHaveAttribute('title', expect.stringContaining('Repeated character request'))
    const b = screen.getByTestId('nav-run-run_b')
    expect(within(b).getByText('failed')).toBeInTheDocument()
    expect(b.querySelector('.nav-row-sub')).toHaveTextContent('Repeated character request')
    fireEvent.change(screen.getByRole('searchbox', { name: 'Filter missions and runs' }), { target: { value: 'archon-task' } })
    expect(screen.getByTestId('nav-run-run_a')).toBeInTheDocument()
    expect(screen.queryByTestId('nav-run-run_b')).toBeNull()
  })

  it('keeps the driver visible even when a projection has no start time', () => {
    render(<RunWhen run={run('run_a', 1, { startedAt: undefined, startedBy: 'agent:identity' })} />)
    expect(screen.getByText('by agent:identity')).toBeInTheDocument()
  })
})
