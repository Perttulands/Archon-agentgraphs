import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import HumanGateAnswerPanel from './HumanGateAnswerPanel'

const questions = '1. Which database?\n2. Who signs off the brief?'

function renderPanel(onDecide = vi.fn(async () => true), upstream: Parameters<typeof HumanGateAnswerPanel>[0]['upstream'] = { state: 'ready', from: 'Question round', text: questions, truncated: false, criterion: '' }) {
  return render(
    <HumanGateAnswerPanel
      runId="run_1"
      gateId="gate_questions"
      requestedSeq={9}
      gateTitle="Operator answers"
      criterion="Answer the open questions"
      upstream={upstream}
      onDecide={onDecide}
    />,
  )
}

describe('HumanGateAnswerPanel', () => {
  afterEach(() => window.localStorage.clear())

  it('shows the upstream questions readably and approves with the typed response', async () => {
    const onDecide = vi.fn(async () => true)
    renderPanel(onDecide)

    expect(screen.getByRole('dialog', { name: 'Answer gate Operator answers' })).toBeInTheDocument()
    expect(screen.getByText('Answer the open questions')).toBeInTheDocument()
    expect(screen.getByText('From Question round')).toBeInTheDocument()
    expect(screen.getByTestId('gate-answer-upstream').querySelector('pre')?.textContent).toBe(questions)
    expect(screen.getByRole('button', { name: 'Send back' })).toBeDisabled()

    const answer = '1. Postgres.\n2. The operator.'
    fireEvent.change(screen.getByLabelText('Your response'), { target: { value: `  ${answer}\n` } })
    expect(window.localStorage.getItem('archon.gateResponse.run_1.9')).toBe(`  ${answer}\n`)
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Approve' })) })

    expect(onDecide).toHaveBeenCalledWith('pass', answer)
    expect(window.localStorage.getItem('archon.gateResponse.run_1.9')).toBeNull()
  })

  it('approves with an empty response and sends back only with text', async () => {
    const onDecide = vi.fn(async () => true)
    renderPanel(onDecide)

    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Approve' })) })
    expect(onDecide).toHaveBeenLastCalledWith('pass', '')

    fireEvent.change(screen.getByLabelText('Your response'), { target: { value: 'Question 2 is unclear' } })
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Send back' })) })
    expect(onDecide).toHaveBeenLastCalledWith('fail', 'Question 2 is unclear')
  })

  it('keeps the draft when recording fails and restores it after a reload', async () => {
    let settle: (recorded: boolean) => void = () => {}
    const onDecide = vi.fn(() => new Promise<boolean>(resolve => { settle = resolve }))
    const { unmount } = renderPanel(onDecide)

    fireEvent.change(screen.getByLabelText('Your response'), { target: { value: 'Use Postgres' } })
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }))
    expect(screen.getByRole('button', { name: 'Approving…' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Send back' })).toBeDisabled()
    await act(async () => { settle(false) })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Approve' })).toBeEnabled())

    unmount()
    renderPanel(onDecide)
    expect(screen.getByLabelText('Your response')).toHaveValue('Use Postgres')
  })

  it('prefers the frozen run criterion and marks truncated input', () => {
    renderPanel(undefined, { state: 'ready', from: 'Question round', text: 'first part', truncated: true, criterion: 'Answer every question' })
    expect(screen.getByText('Answer every question')).toBeInTheDocument()
    expect(screen.queryByText('Answer the open questions')).toBeNull()
    expect(screen.getByText('Showing the start of a long input; the full text is in the run evidence.')).toBeInTheDocument()
  })

  it('says when the gate input is loading or unavailable', () => {
    const { rerender } = renderPanel(undefined, { state: 'loading' })
    expect(screen.getByText('Loading the gate input…')).toBeInTheDocument()
    rerender(
      <HumanGateAnswerPanel
        runId="run_1"
        gateId="gate_questions"
        requestedSeq={9}
        gateTitle="Operator answers"
        criterion=""
        upstream={{ state: 'unavailable', message: 'The gate input could not be read.' }}
        onDecide={vi.fn(async () => true)}
      />,
    )
    expect(screen.getByText('The gate input could not be read.')).toBeInTheDocument()
  })

  it('offers talking with the asked agents beside answering, and names a fallback instead', () => {
    const onTalk = vi.fn()
    const seat = { windowId: 'talk:run_1:5', runId: 'run_1', createdSeq: 5, gateId: 'gate_questions', requestedSeq: 9, nodeId: 'fmn_peers', formationTitle: 'Question peers', slotLabel: 'Peer', agent: 'Delivery Planner', harness: 'claude-code', label: 'Delivery Planner · Claude Code' }
    const panel = (talk: Parameters<typeof HumanGateAnswerPanel>[0]['talk']) => (
      <HumanGateAnswerPanel runId="run_1" gateId="gate_questions" requestedSeq={9} gateTitle="Operator answers" criterion="" talk={talk}
        upstream={{ state: 'loading' }} onDecide={vi.fn(async () => true)} />
    )
    const { rerender } = render(panel({ formationTitle: 'Question peers', seats: [seat, { ...seat, windowId: 'talk:run_1:6', createdSeq: 6 }], fallbackReason: '', onTalk }))
    const talk = screen.getByRole('button', { name: 'Talk with Question peers' })
    fireEvent.click(talk)
    expect(onTalk).toHaveBeenCalledWith(screen.getByRole('dialog', { name: 'Answer gate Operator answers' }))
    expect(screen.getByText('The 2 agents that did the work are waiting in their terminals. They record the decision you confirm, or you answer here.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Approve' })).toBeEnabled()

    rerender(panel({ formationTitle: 'Question peers', seats: [], fallbackReason: '', onTalk }))
    expect(screen.getByRole('button', { name: 'Talk with Question peers' })).toBeDisabled()
    expect(screen.getByText('The question is on its way to the agents. You can answer here meanwhile.')).toBeInTheDocument()

    rerender(panel({ formationTitle: 'Question peers', seats: [], fallbackReason: 'the lab executor keeps no seats', onTalk }))
    expect(screen.getByRole('note')).toHaveTextContent('The agents are not available for this gate: the lab executor keeps no seats. Answer here.')
    expect(screen.queryByRole('button', { name: /Talk with/ })).toBeNull()

    rerender(panel(null))
    expect(screen.queryByRole('note')).toBeNull()
    expect(screen.queryByRole('button', { name: /Talk with/ })).toBeNull()
  })
})
