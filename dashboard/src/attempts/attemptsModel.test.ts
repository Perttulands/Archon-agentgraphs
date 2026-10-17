import { describe, expect, it } from 'vitest'
import type { AttemptFile, AttemptVerdict, StepAttempt } from '../evidence/runEvidenceApi'
import {
  attemptFileGroups,
  attemptOutcome,
  attemptTiming,
  comparePair,
  folderSummary,
  inLoopOrder,
  judgeWords,
  openingAttempt,
  pickerWords,
  settledAttempt,
  stepAttempt,
  stepPast,
  verdictHeadline,
} from './attemptsModel'

const text = (value: string) => ({ text: value, bytes: value.length })

function verdict(gateId: string, word?: string, extra: Partial<AttemptVerdict> = {}): AttemptVerdict {
  return { gateId, gateTitle: gateId === 'review' ? 'Review gate' : gateId, evaluationSeq: 1, verdict: word, reason: text(''), evidence: [], ...extra }
}

function attempt(number: number, extra: Partial<StepAttempt> = {}): StepAttempt {
  return { attempt: number, status: 'done', startedSeq: number * 10, outputs: [], verdicts: [], files: [], ...extra }
}

const picked = { gateId: 'review', seq: 40, reason: text('closest'), decidedBy: 'judge', candidates: [1, 2, 3], trigger: 'whenSpent' }

const file = (name: string, kind = 'text', size = 10): AttemptFile => ({ name, path: `/runs/run_1/${name}`, size, modifiedAt: '2026-10-16T10:00:00Z', kind })

describe('attemptOutcome', () => {
  it('reads a pick first, then a running attempt, then pending, failed and passed gates', () => {
    expect(attemptOutcome(attempt(1, { picked, verdicts: [verdict('review', 'fail')] }))).toBe('picked')
    expect(attemptOutcome(attempt(1, { status: 'running' }))).toBe('running')
    expect(attemptOutcome(attempt(1, { verdicts: [verdict('review', undefined, { pending: true })] }))).toBe('judging')
    expect(attemptOutcome(attempt(1, { verdicts: [verdict('review', undefined, { pending: true })] }), new Set(['review']))).toBe('waiting')
    // One gate failing the attempt outweighs another passing it.
    expect(attemptOutcome(attempt(1, { verdicts: [verdict('review', 'pass'), verdict('qa', 'fail')] }))).toBe('fail')
    expect(attemptOutcome(attempt(1, { verdicts: [verdict('review', 'pass')] }))).toBe('pass')
    expect(attemptOutcome(attempt(1, { status: 'stopped' }))).toBe('stopped')
    expect(attemptOutcome(attempt(1))).toBe('done')
  })
})

describe('settledAttempt and openingAttempt', () => {
  const loop = [
    attempt(1, { verdicts: [verdict('review', 'fail')] }),
    attempt(2, { verdicts: [verdict('review', 'pass')] }),
    attempt(3, { verdicts: [verdict('review', 'fail')] }),
  ]

  it('settles on the picked attempt over a passed one, and on the latest pass otherwise', () => {
    expect(settledAttempt(loop)).toBe(2)
    expect(settledAttempt([...loop.slice(0, 2), { ...loop[2], picked }])).toBe(3)
    expect(settledAttempt([loop[0], loop[2]])).toBeNull()
  })

  it('opens on the judge\'s recommendation, then the settled attempt, then the latest', () => {
    expect(openingAttempt(loop, 1)).toBe(1)
    expect(openingAttempt(loop, 9)).toBe(2)
    expect(openingAttempt([loop[0], loop[2]])).toBe(3)
    expect(openingAttempt([])).toBeNull()
  })
})

describe('stepping and pairing', () => {
  const numbers = [1, 2, 3, 4]

  it('steps one attempt along and holds at the ends', () => {
    expect(stepAttempt(numbers, 2, 1)).toBe(3)
    expect(stepAttempt(numbers, 4, 1)).toBe(4)
    expect(stepAttempt(numbers, 1, -1)).toBe(1)
    // From an attempt no longer listed, a step lands on an end.
    expect(stepAttempt(numbers, 9, 1)).toBe(1)
    expect(stepAttempt(numbers, 9, -1)).toBe(4)
    expect(stepAttempt([], 3, 1)).toBe(3)
  })

  it('pairs the focused attempt with the one before it, or after it when it is first', () => {
    expect(comparePair(numbers, 3)).toEqual([2, 3])
    expect(comparePair(numbers, 1)).toEqual([1, 2])
    expect(comparePair([5], 5)).toEqual([5, 5])
  })

  it('pairs any two in the loop\'s order and steps one side past the other', () => {
    expect(inLoopOrder(numbers, 4, 1)).toEqual([1, 4])
    expect(inLoopOrder(numbers, 2, 3)).toEqual([2, 3])
    expect(stepPast(numbers, 3, -1, 2)).toBe(1)
    expect(stepPast(numbers, 3, -1, 1)).toBe(2)
    // Nothing past the other side at the end: the step holds.
    expect(stepPast(numbers, 3, 1, 4)).toBe(3)
    expect(stepPast(numbers, 4, 1)).toBe(4)
  })
})

describe('attemptFileGroups and folderSummary', () => {
  it('gives each output its own files and keeps the rest apart', () => {
    const groups = attemptFileGroups(attempt(1, {
      outputs: [
        { portId: 'out', kind: 'file', text: text('x'), artifact: 'execution/1/outputs/out/portrait.png' },
        { portId: 'meshes', kind: 'folder', text: text('x'), artifact: 'execution/1/outputs/meshes' },
        { portId: 'notes', kind: 'text', text: text('notes') },
      ],
      files: [
        file('execution/1/outputs/out/portrait.png', 'image'),
        file('execution/1/outputs/meshes/a.glb', 'binary'),
        file('execution/1/outputs/meshes/textures/a.png', 'image'),
        file('execution/1/outputs/meshes-old/b.glb', 'binary'),
        file('execution/1/prompt.txt'),
      ],
    }))
    expect(groups.outputs.map(group => group.files.map(entry => entry.name))).toEqual([
      ['execution/1/outputs/out/portrait.png'],
      ['execution/1/outputs/meshes/a.glb', 'execution/1/outputs/meshes/textures/a.png'],
      [],
    ])
    // A sibling folder whose name starts the same is not the output's.
    expect(groups.others.map(entry => entry.name)).toEqual(['execution/1/outputs/meshes-old/b.glb', 'execution/1/prompt.txt'])
  })

  it('puts cited images and contact sheets ahead of the thumbnail cap, without dropping other files', () => {
    const frames = Array.from({ length: 20 }, (_, i) => file(`frames/frame-${i}.png`, 'image', 1))
    const images = [...frames, ...['contact-sheet.jpg', 'final.png', 'sheet.png', 'judge-crop.png'].map(name => file(`proof/${name}`, 'image', 1))]
    const summary = folderSummary(images, ['Compare the face in /work/proof/judge-crop.png.'])
    expect(summary.images.slice(0, 4).map(image => image.name)).toEqual(['proof/judge-crop.png', 'proof/contact-sheet.jpg', 'proof/sheet.png', 'proof/final.png'])
    expect(summary.count).toBe(24)
    expect(summary.images.slice(4)).toEqual(frames)
  })

  it('counts a folder and parts its images from the rest', () => {
    const summary = folderSummary([file('m/a.glb', 'binary', 100), file('m/a.png', 'image', 20), file('m/b.glb', 'binary', 100)])
    expect(summary).toMatchObject({ count: 3, bytes: 220 })
    expect(summary.images.map(entry => entry.name)).toEqual(['m/a.png'])
    expect(summary.models.map(entry => entry.name)).toEqual(['m/a.glb', 'm/b.glb'])
    expect(summary.rest).toEqual([])
  })
})

describe('words', () => {
  it('names the gate, the verdict and, for the operator, who decided', () => {
    expect(verdictHeadline(verdict('review', 'fail'))).toBe('Review gate · ✕ fail')
    expect(verdictHeadline(verdict('review', 'pass', { decidedBy: 'human:ui' }))).toBe('Review gate · ✓ pass · you')
    expect(verdictHeadline(verdict('review', undefined, { pending: true }), new Set(['review']))).toBe('Review gate · waiting for you')
    expect(verdictHeadline(verdict('review', undefined, { superseded: true }))).toBe('Review gate · evaluated again before your answer')
    // A request the gate replaced decides nothing about the attempt.
    expect(attemptOutcome(attempt(1, { verdicts: [verdict('review', undefined, { superseded: true }), verdict('review', 'pass')] }))).toBe('pass')
  })

  it('says who picked an attempt, naming a judge by its step\'s title', () => {
    const titleOf = (id: string) => (id === 'fmn_judge' ? 'Likeness judge' : id)
    expect(pickerWords('human:ui', titleOf)).toBe('you')
    expect(pickerWords('fmn_judge', titleOf)).toBe('Likeness judge')
    expect(pickerWords('archon', titleOf)).toBe('the only candidate')
    // A judge the board does not name never shows its ID.
    expect(pickerWords('fmn_gone', titleOf)).toBe('the judge')
    expect(pickerWords('fmn_judge')).toBe('the judge')
    expect(judgeWords('fmn_judge', titleOf)).toBe('Likeness judge')
    expect(judgeWords(undefined, titleOf)).toBe('the judge')
  })

  it('times a finished attempt by its span and a running one so far', () => {
    const now = new Date('2026-10-16T10:10:00Z')
    expect(attemptTiming(attempt(1, { startedAt: '2026-10-16T10:01:00Z', endedAt: '2026-10-16T10:05:12Z' }), now)).toMatch(/ · 4m 12s$/)
    expect(attemptTiming(attempt(1, { status: 'running', startedAt: '2026-10-16T10:09:22Z' }), now)).toMatch(/ · 38s so far$/)
    expect(attemptTiming(attempt(1), now)).toBe('')
  })
})

it('does not confuse image suffixes and promotes only three final images behind contact sheets', () => {
  const frames = [file('frame.png', 'image', 1), file('crop.png', 'image', 1)]
  const finals = Array.from({ length: 20 }, (_, i) => file(`final_${i}.png`, 'image', 1))
  const images = [...frames, ...finals, file('contact-sheet.png', 'image', 1), file('other-crop.png', 'image', 1)]
  const summary = folderSummary(images, ['/work/other-crop.png'])
  expect(summary.images.slice(0, 7).map(image => image.name)).toEqual([
    'other-crop.png', 'contact-sheet.png', 'final_0.png', 'final_1.png', 'final_2.png', 'frame.png', 'crop.png',
  ])
})
