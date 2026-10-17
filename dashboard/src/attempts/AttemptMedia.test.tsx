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
  expect(screen.getByText('at least 8215 more files not listed.')).toBeVisible()
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

it('previews folder videos beyond the first eight rows', () => {
  const files = [...Array.from({ length: 180 }, (_, i) => `cache-${i}.txt`), 'turntable.mp4', 'review.webm']
    .map(name => ({ name: `draw/1/${name}`, path: `/work/draw/1/${name}`, kind: 'binary', size: 1, modifiedAt: '' }))
  const attempt: StepAttempt = {
    attempt: 1, status: 'done', startedSeq: 2, files, verdicts: [],
    outputs: [{ portId: 'folder', kind: 'folder', artifact: 'draw/1', text: { text: '', bytes: 0 } }],
  }
  const { container } = render(<AttemptColumn attempt={attempt} marks={{ focused: true, settled: false }} onFocus={() => {}}
    view={{ runId: 'run_1', stepTitle: 'Draw', waitingGates: new Set(), zoom: null, onZoom: () => {}, compare: false, now: new Date() }} />)
  for (const name of ['turntable.mp4', 'review.webm']) {
    const video = screen.getByLabelText(name)
    expect(video).toHaveAttribute('controls')
    expect(video.compareDocumentPosition(container.querySelector('.attempt-listing')!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  }
  expect(container.querySelectorAll('.attempt-listing li')).toHaveLength(8)
  expect(screen.getByText('and 172 more listed files')).toBeVisible()
})
