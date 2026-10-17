import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import AttemptColumn from './AttemptColumn'
import type { StepAttempt } from '../evidence/runEvidenceApi'

afterEach(cleanup)

it('shows an output movie and the judge movie in the attempt without opening another surface', () => {
  const path = '/work/judge videos/review.webm'
  const text = (value: string) => ({ text: value, bytes: value.length })
  const attempt: StepAttempt = {
    attempt: 1, status: 'done', startedSeq: 2, files: [], filesTruncated: true, filesOmitted: 8215,
    directory: { name: 'draw/1', path: '/work/draw/1' },
    outputs: [{ portId: 'movie', label: 'Turntable', kind: 'file', fileKind: 'video', artifact: 'draw/1/turntable.mp4', text: text('') }],
    verdicts: [{ gateId: 'review', gateTitle: 'Review', evaluationSeq: 3, verdict: 'pass', reason: text('Watched the result.'),
      evidence: [{ kind: 'formation', text: text(path) }] }],
  }
  render(<AttemptColumn attempt={attempt} marks={{ focused: true, settled: false }} onFocus={() => {}}
    view={{ runId: 'run_1', stepTitle: 'Draw', waitingGates: new Set(), zoom: null, onZoom: () => {}, compare: false, now: new Date() }} />)
  expect(screen.getByText('8215 more files not listed.')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Browse all files' })).toBeVisible()
  const output = within(screen.getByTestId('attempt-1-output-movie')).getByLabelText('turntable.mp4')
  const judge = within(screen.getByTestId('attempt-1-verdict-review')).getByLabelText('review.webm')
  expect(output).toHaveAttribute('src', '/api/runs/run_1/artifacts/draw/1/turntable.mp4')
  expect(judge).toHaveAttribute('src', `/api/files/raw?path=${encodeURIComponent(path)}`)
  for (const video of [output, judge]) {
    expect(video.tagName).toBe('VIDEO')
    expect(video).toHaveAttribute('controls')
    expect(video).not.toHaveAttribute('autoplay')
  }
})
