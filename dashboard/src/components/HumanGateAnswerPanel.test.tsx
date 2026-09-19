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
  afterEach(() => { vi.restoreAllMocks(); window.localStorage.clear() })

  it.each([['Approve', 'pass'], ['Send back', 'fail']])('loads a long Unicode file into an editable draft and preserves the %s response', async (button, verdict) => {
    const onDecide = vi.fn(async () => true)
    renderPanel(onDecide)
    const answer = `  ${'Vastaus: ääkköset 日本語 🧭\twith space.\n'.repeat(200)}\n`
    const file = new File([answer], 'answer.txt', { type: 'text/plain' })
    Object.defineProperty(file, 'arrayBuffer', { value: async () => new TextEncoder().encode(answer).buffer })
    fireEvent.change(screen.getByLabelText('Response file'), { target: { files: [file] } })
    await waitFor(() => expect(screen.getByLabelText('Your response')).toHaveValue(answer))
    expect(onDecide).not.toHaveBeenCalled()
    const edited = `${answer}An additional answer.  \n`
    fireEvent.change(screen.getByLabelText('Your response'), { target: { value: edited } })
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: button })) })
    expect(onDecide).toHaveBeenCalledWith(verdict, edited)
  })

  it.each(['read failure', 'invalid UTF-8'])('preserves the current draft on %s', async failure => {
    const onDecide = vi.fn(async () => true)
    renderPanel(onDecide)
    fireEvent.change(screen.getByLabelText('Your response'), { target: { value: 'Keep this answer.  \n' } })
    const file = new File([], 'broken.txt')
    Object.defineProperty(file, 'arrayBuffer', { value: async () => {
      if (failure === 'read failure') throw new Error('Read failed')
      return new Uint8Array([0xff]).buffer
    } })
    fireEvent.change(screen.getByLabelText('Response file'), { target: { files: [file] } })
    expect(await screen.findByRole('alert')).toHaveTextContent('Your response has not changed.')
    expect(screen.getByLabelText('Your response')).toHaveValue('Keep this answer.  \n')
    expect(window.localStorage.getItem('archon.gateResponse.run_1.9')).toBe('Keep this answer.  \n')
    expect(onDecide).not.toHaveBeenCalled()
  })

  it('preserves the UTF-8 BOM and CRLF bytes as decoded text when an imported answer is not edited', async () => {
    const onDecide = vi.fn(async () => true)
    renderPanel(onDecide)
    const answer = '\uFEFF  First line.\r\nSecond line.  \r\n'
    const file = new File([], 'answer.txt')
    Object.defineProperty(file, 'arrayBuffer', { value: async () => new TextEncoder().encode(answer).buffer })
    fireEvent.change(screen.getByLabelText('Response file'), { target: { files: [file] } })
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Draft saved'))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Approve' })) })
    expect(onDecide).toHaveBeenCalledWith('pass', answer)
  })

  it('blocks editing and submission until the file finishes reading', async () => {
    const onDecide = vi.fn(async () => true)
    renderPanel(onDecide)
    let finish!: (value: ArrayBuffer) => void
    const file = new File([], 'answer.txt')
    Object.defineProperty(file, 'arrayBuffer', { value: () => new Promise<ArrayBuffer>(resolve => { finish = resolve }) })
    fireEvent.change(screen.getByLabelText('Response file'), { target: { files: [file] } })
    expect(screen.getByLabelText('Your response')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Approve' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Send back' })).toBeDisabled()
    await act(async () => { finish(new TextEncoder().encode('Loaded').buffer) })
    expect(screen.getByLabelText('Your response')).toHaveValue('Loaded')
    expect(screen.getByRole('button', { name: 'Approve' })).toBeEnabled()
    expect(onDecide).not.toHaveBeenCalled()
  })

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
    expect(screen.getByRole('status')).toHaveTextContent('Draft saved in this browser. Not submitted.')
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Approve' })) })

    expect(onDecide).toHaveBeenCalledWith('pass', `  ${answer}\n`)
    expect(window.localStorage.getItem('archon.gateResponse.run_1.9')).toBeNull()
    expect(screen.getByRole('status')).toHaveTextContent('Answer submitted.')
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
    expect(screen.getByRole('status')).toHaveTextContent('Draft restored from this browser. Not submitted.')
  })

  it('prefers the frozen run criterion and marks truncated input', () => {
    renderPanel(undefined, { state: 'ready', from: 'Question round', text: 'first part', truncated: true, criterion: 'Answer every question' })
    expect(screen.getByText('Answer every question')).toBeInTheDocument()
    expect(screen.queryByText('Answer the open questions')).toBeNull()
    expect(screen.getByText('Showing the start of a long input.')).toBeInTheDocument()
  })


  it('offers evidence only when an opener is available and preserves the response', () => {
    const onOpenEvidence = vi.fn()
    const props = { runId: 'run_1', gateId: 'gate_questions', requestedSeq: 9, gateTitle: 'Questions', criterion: '',
      upstream: { state: 'ready' as const, from: 'Peers', text: 'Part', truncated: true, criterion: '' }, onDecide: vi.fn(async () => true) }
    const { rerender } = render(<HumanGateAnswerPanel {...props} onOpenEvidence={onOpenEvidence} />)
    fireEvent.change(screen.getByLabelText('Your response'), { target: { value: 'Keep my answer' } })
    fireEvent.click(screen.getByRole('button', { name: 'Open run evidence' }))
    expect(onOpenEvidence).toHaveBeenCalledOnce()
    expect(screen.getByLabelText('Your response')).toHaveValue('Keep my answer')
    expect(props.onDecide).not.toHaveBeenCalled()
    rerender(<HumanGateAnswerPanel {...props} />)
    expect(screen.queryByRole('button', { name: 'Open run evidence' })).toBeNull()
  })

  it('does not claim a saved draft when storage fails and keeps the typed answer', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('quota') })
    renderPanel()
    fireEvent.change(screen.getByLabelText('Your response'), { target: { value: 'Still here' } })
    expect(screen.getByRole('status')).toHaveTextContent('Draft not saved in this browser. Keep this page open. Not submitted.')
    expect(screen.getByLabelText('Your response')).toHaveValue('Still here')
    expect(window.localStorage.getItem('archon.gateResponse.run_1.9')).toBeNull()
  })

  it('distinguishes a submitted answer from a browser draft that could not be cleared', async () => {
    renderPanel()
    fireEvent.change(screen.getByLabelText('Your response'), { target: { value: 'Use Postgres' } })
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('storage unavailable') })
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Approve' })) })
    expect(screen.getByRole('status')).toHaveTextContent('Answer submitted. The browser draft could not be cleared.')
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
