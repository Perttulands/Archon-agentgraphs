import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import DismissiblePanel from './DismissiblePanel'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RunBarActions, { DEFAULT_STOP_REASON, stopRunConsequences } from './RunBarActions'
import type { RunStatusProjection } from './formationsTypes'

const run = (extra: Partial<RunStatusProjection> = {}): RunStatusProjection => ({
  runId: 'run_01M3P6BC5ZZY875NVMVM810K49', status: 'waiting_human', final: false, missionSlug: 'runs-gate', inputCardId: 'mis_note', eventCount: 9, beadId: 'archon-3yd.10', ...extra,
})
const titleOf = (nodeId: string) => ({ fmn_draft: 'Draft', gate_review: 'Operator review' } as Record<string, string>)[nodeId] || nodeId

function renderBar(props: Partial<Parameters<typeof RunBarActions>[0]> = {}) {
  const onStop = vi.fn(async () => true)
  const onResume = vi.fn()
  render(
    <div className="fmx">
      <div className="run-banner">
        <RunBarActions run={run()} point={{ kind: 'waiting', nodeId: 'gate_review', gate: true }} pointTitle="Operator review" boardTitle="Runs gate"
          titleOf={titleOf} pendingGate={{ title: 'Operator review', requestedSeq: 9 }} onResume={onResume} onStop={onStop} {...props} />
      </div>
    </div>,
  )
  return { onStop, onResume }
}

describe('RunBarActions', () => {
  afterEach(() => {
    cleanup()
    localStorage.clear()
    vi.unstubAllGlobals()
  })

  it('closes only itself on Escape, above a menu and window listeners beneath it', () => {
    const menuDismissed = vi.fn()
    const windowEscape = vi.fn()
    window.addEventListener('keydown', windowEscape)
    render(<DismissiblePanel panelPosition="fixed" onDismiss={menuDismissed}><div>menu</div></DismissiblePanel>)
    const { onStop } = renderBar()
    fireEvent.click(screen.getByRole('button', { name: 'Stop run' }))
    fireEvent.keyDown(screen.getByRole('button', { name: 'Keep running' }), { key: 'Escape' })
    expect(screen.queryByRole('alertdialog')).toBeNull()
    expect(menuDismissed).not.toHaveBeenCalled()
    expect(windowEscape).not.toHaveBeenCalled()
    expect(onStop).not.toHaveBeenCalled()
    window.removeEventListener('keydown', windowEscape)
  })

  it('is modal: a backdrop covers the page and everything behind it is inert until it closes', () => {
    renderBar()
    const stop = screen.getByRole('button', { name: 'Stop run' })
    fireEvent.click(stop)
    const layer = screen.getByTestId('stop-run-layer')
    expect(layer.querySelector('.stop-run-backdrop')).not.toBeNull()
    const behind = [...document.body.children].filter(element => element !== layer)
    expect(behind.length).toBeGreaterThan(0)
    for (const element of behind) expect(element).toHaveAttribute('inert')
    fireEvent.click(screen.getByRole('button', { name: 'Keep running' }))
    for (const element of behind) expect(element).not.toHaveAttribute('inert')
    expect(stop).toHaveFocus()
  })

  it('asks before stopping, naming the run, where it is and what ends', () => {
    localStorage.setItem('archon.gateResponse.run_01M3P6BC5ZZY875NVMVM810K49.9', 'half an answer')
    const { onStop } = renderBar()
    fireEvent.click(screen.getByRole('button', { name: 'Stop run' }))
    expect(onStop).not.toHaveBeenCalled()
    const dialog = screen.getByRole('alertdialog', { name: 'Stop run …810K49?' })
    expect(dialog).toHaveTextContent('Runs gate · archon-3yd.10 · run …810K49, waiting for you at Operator review')
    expect(dialog).toHaveTextContent('Operator review stops waiting for you, and your unsent answer is not sent.')
    expect(dialog).toHaveTextContent('No agent seats are kept on call for this run.')
    expect(dialog).toHaveTextContent('The run ends as canceled and cannot be resumed. Its workspace and outputs stay.')
    // Keep running is the safe default.
    expect(screen.getByRole('button', { name: 'Keep running' })).toHaveFocus()
  })

  it('cancels the stop on Escape and gives focus back to Stop', () => {
    const { onStop } = renderBar()
    const stop = screen.getByRole('button', { name: 'Stop run' })
    fireEvent.click(stop)
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('alertdialog')).toBeNull()
    expect(onStop).not.toHaveBeenCalled()
    expect(stop).toHaveFocus()
  })

  it('stops with the operator\'s reason, or the default one', async () => {
    const { onStop } = renderBar()
    fireEvent.click(screen.getByRole('button', { name: 'Stop run' }))
    fireEvent.change(screen.getByLabelText('Why (optional)'), { target: { value: '  the brief was wrong ' } })
    await act(async () => { fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Stop run' })) })
    expect(onStop).toHaveBeenCalledWith('the brief was wrong')
    expect(screen.queryByRole('alertdialog')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Stop run' }))
    await act(async () => { fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Stop run' })) })
    expect(onStop).toHaveBeenLastCalledWith(DEFAULT_STOP_REASON)
  })

  it('keeps the dialog open when the stop fails', async () => {
    const onStop = vi.fn(async () => false)
    renderBar({ onStop })
    fireEvent.click(screen.getByRole('button', { name: 'Stop run' }))
    await act(async () => { fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Stop run' })) })
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
  })

  it('names the kept seats that end and the agents interrupted', () => {
    const lines = stopRunConsequences(
      run({ status: 'running', onCallSeats: [{ nodeId: 'fmn_draft', slotId: 'writer', createdSeq: 4, keptSeq: 6, waitingOn: [] }] }),
      { kind: 'running', nodeId: 'fmn_publish', gate: false }, 'Publish', titleOf, null,
    )
    expect(lines).toEqual([
      'The agents working on Publish are interrupted.',
      'The agent seat kept on call ends: Draft (writer). Archon waits up to a minute for each agent to go idle, then closes its terminal.',
      'The run ends as canceled and cannot be resumed. Its workspace and outputs stay.',
    ])
  })

  it('offers Resume only when the run can resume, and says why it cannot', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
      ok: true, status: 200, headers: { get: () => '' },
      json: () => Promise.resolve({ success: true, data: { problems: [
        { seq: 20, type: 'run_blocked', code: 'resume_attempts_exhausted', nodeIds: ['fmn_draft'], reason: { text: 'resume attempts exhausted', bytes: 25 }, resumeAllowed: false, limit: { kind: 'attempts', nodeId: 'fmn_draft', used: 3, max: 3 } },
      ] } }),
    } as unknown as Response)))
    const { onResume } = renderBar({ run: run({ status: 'blocked', resumeAllowed: true }), pendingGate: null })
    fireEvent.click(screen.getByRole('button', { name: 'Resume run' }))
    expect(onResume).toHaveBeenCalled()
    expect(screen.queryByTestId('run-not-resumable')).toBeNull()
    cleanup()

    renderBar({ run: run({ status: 'blocked', resumeAllowed: false }), pendingGate: null })
    expect(screen.queryByRole('button', { name: 'Resume run' })).toBeNull()
    await waitFor(() => expect(screen.getByTestId('run-not-resumable')).toHaveTextContent('Can’t resume: Draft used 3 of 3 attempts.'))
    expect(screen.getByTestId('run-not-resumable')).not.toHaveTextContent(/new run/)
    expect(screen.getByRole('button', { name: 'Stop run' })).toBeInTheDocument()
  })

  it('shows nothing for a finished run', () => {
    renderBar({ run: run({ status: 'canceled', final: true }) })
    expect(screen.queryByRole('button', { name: 'Stop run' })).toBeNull()
  })
})
