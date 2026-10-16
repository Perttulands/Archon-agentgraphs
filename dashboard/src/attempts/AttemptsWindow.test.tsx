import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from 'vitest'
import type { ComponentProps } from 'react'
import type { StepAttempt } from '../evidence/runEvidenceApi'
import { WindowManagerProvider, useWindowManager } from '../windows/WindowManager'
import AttemptsWindow, { attemptsWindowSize } from './AttemptsWindow'

vi.mock('../evidence/runEvidenceApi', async importOriginal => ({
  ...await importOriginal<typeof import('../evidence/runEvidenceApi')>(),
  fetchRunAttempts: vi.fn(),
}))
const { fetchRunAttempts } = await import('../evidence/runEvidenceApi')
const fetchAttempts = fetchRunAttempts as Mock

const text = (value: string) => ({ text: value, bytes: value.length })

function attempt(number: number, verdict: 'pass' | 'fail' | 'running', extra: Partial<StepAttempt> = {}): StepAttempt {
  return {
    attempt: number, status: verdict === 'running' ? 'running' : 'done', startedSeq: number * 10,
    outputs: verdict === 'running' ? [] : [{ portId: 'notes', label: 'Notes', kind: 'text', text: text(`notes of attempt ${number}`) }],
    verdicts: verdict === 'running' ? [] : [{ gateId: 'review', gateTitle: 'Review gate', evaluationSeq: number * 10 + 2, verdict, reason: text(`reason ${number}`), evidence: [] }],
    files: [], ...extra,
  }
}

let attempts: StepAttempt[] = []

function Window(props: Partial<ComponentProps<typeof AttemptsWindow>>) {
  const stack = useWindowManager()
  return (
    <WindowManagerProvider stack={stack}>
      <AttemptsWindow runId="run_1" nodeId="draw" stepTitle="Draw Ahti" count={attempts.length} version="1" live={false}
        waitingGates={new Set()} onClose={() => {}} {...props} />
    </WindowManagerProvider>
  )
}

const focused = () => document.querySelector('[data-attempt][aria-current="true"]')?.getAttribute('data-attempt')
const press = (key: string) => fireEvent.keyDown(screen.getByTestId('attempts-window'), { key })

describe('AttemptsWindow', () => {
  beforeEach(() => {
    attempts = [attempt(1, 'fail'), attempt(2, 'fail'), attempt(3, 'pass'), attempt(4, 'fail')]
    fetchAttempts.mockImplementation(async () => ({ runId: 'run_1', steps: [{ nodeId: 'draw', title: 'Draw Ahti', outputs: [], attempts }] }))
  })
  afterEach(() => { cleanup(); vi.clearAllMocks() })

  it('lays every attempt out in order with its gate\'s words, opens on the one that passed and steps with the keys', async () => {
    render(<Window />)
    await screen.findByTestId('attempt-4')
    expect(fetchAttempts).toHaveBeenCalledWith('run_1', 'draw')
    expect([...document.querySelectorAll('[data-attempt]')].map(column => column.getAttribute('data-attempt'))).toEqual(['1', '2', '3', '4'])
    expect(screen.getByTestId('attempt-2-verdict-review')).toHaveTextContent('Review gate · ✕ fail')
    expect(screen.getByTestId('attempt-2-verdict-review')).toHaveTextContent('reason 2')
    expect(screen.getByTestId('attempt-3')).toHaveClass('settled')
    expect(focused()).toBe('3')

    press('ArrowLeft')
    expect(focused()).toBe('2')
    press('End')
    expect(focused()).toBe('4')
    press('ArrowRight')
    expect(focused()).toBe('4')
    press('1')
    expect(focused()).toBe('1')
    fireEvent.click(screen.getByTestId('attempts-pip-3'))
    expect(focused()).toBe('3')
  })

  it('puts two attempts side by side and steps the focused side', async () => {
    render(<Window />)
    await screen.findByTestId('attempt-4')
    press('c')
    expect([...document.querySelectorAll('[data-attempt]')].map(column => column.getAttribute('data-attempt'))).toEqual(['2', '3'])
    expect(screen.getByTestId('attempts-compare')).toHaveAttribute('aria-pressed', 'true')
    press('ArrowRight')
    expect([...document.querySelectorAll('[data-attempt]')].map(column => column.getAttribute('data-attempt'))).toEqual(['2', '4'])
    fireEvent.pointerDown(screen.getByTestId('attempt-2'))
    press('ArrowLeft')
    expect([...document.querySelectorAll('[data-attempt]')].map(column => column.getAttribute('data-attempt'))).toEqual(['1', '4'])
    // Back to the strip, on the attempt last chosen.
    press('C')
    expect(document.querySelectorAll('[data-attempt]')).toHaveLength(4)
    expect(focused()).toBe('1')
  })

  it('follows a new attempt as it starts, unless the operator stepped back', async () => {
    attempts = [attempt(1, 'fail'), attempt(2, 'running')]
    const view = render(<Window live version="1" />)
    await screen.findByTestId('attempt-2')
    expect(focused()).toBe('2')
    expect(screen.getByTestId('attempt-2')).toHaveTextContent('No output yet.')

    attempts = [attempt(1, 'fail'), attempt(2, 'fail'), attempt(3, 'running')]
    view.rerender(<Window live version="2" />)
    await screen.findByTestId('attempt-3')
    await waitFor(() => expect(focused()).toBe('3'))

    press('ArrowLeft')
    attempts = [...attempts.slice(0, 2), attempt(3, 'fail'), attempt(4, 'running')]
    view.rerender(<Window live version="3" />)
    await screen.findByTestId('attempt-4')
    expect(focused()).toBe('2')
  })

  it('answers a pending pick: the judge\'s choice comes first and one button records it', async () => {
    attempts = attempts.map(entry => ({ ...entry, verdicts: entry.verdicts.map(verdict => ({ ...verdict, verdict: 'fail' })) }))
    const onPick = vi.fn(async () => 'this pick request changed')
    render(<Window pick={{ gateId: 'review', gateTitle: 'Review gate', candidates: [1, 2, 3], recommended: { attempt: 3, reason: 'closest to the reference' }, focusRequest: 1, onPick }} />)
    await screen.findByTestId('attempt-4')
    expect(focused()).toBe('3')
    expect(screen.getByTestId('attempt-3-recommended')).toHaveTextContent('closest to the reference')
    expect(screen.getByTestId('attempt-4')).toHaveClass('excluded')
    expect(screen.getByTestId('attempts-pick-go')).toHaveTextContent('Pick attempt 3')

    press('ArrowRight')
    expect(screen.getByTestId('attempts-pick-go')).toHaveTextContent('Attempt 4 is not a candidate')
    expect(screen.getByTestId('attempts-pick-go')).toBeDisabled()
    press('ArrowLeft')
    press('ArrowLeft')

    fireEvent.change(screen.getByRole('textbox', { name: 'Why this attempt' }), { target: { value: ' sharper jaw ' } })
    await act(async () => { fireEvent.click(screen.getByTestId('attempts-pick-go')) })
    expect(onPick).toHaveBeenCalledWith(2, 'sharper jaw')
    expect(screen.getByRole('alert')).toHaveTextContent('this pick request changed')
    expect(screen.getByTestId('attempts-pick-go')).toBeEnabled()
  })

  it('sizes the window for its attempts, up to most of the screen', () => {
    expect(attemptsWindowSize(1, { width: 1920, height: 1080 })).toEqual({ width: 480, height: 929 })
    expect(attemptsWindowSize(3, { width: 1920, height: 1080 })).toEqual({ width: 1240, height: 929 })
    expect(attemptsWindowSize(9, { width: 1920, height: 1080 }).width).toBe(1805)
  })
})
