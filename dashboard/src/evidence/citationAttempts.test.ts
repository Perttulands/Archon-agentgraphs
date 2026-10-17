import { describe, expect, it, vi } from 'vitest'
import { evaluationCitationScope, fetchCitationAttempts, withJudgingAttempts } from './citationAttempts'
import { resolveCitation } from './evidenceCitations'
import type { NodeEvidence, StepAttempt, StepAttempts } from './runEvidenceApi'
import * as api from './runEvidenceApi'

const text = (text: string) => ({ text, bytes: text.length })
const report = '/state/.archon/artifacts/run_1/fmn_judge_clean/3/report.md'
const paths = ['sheets/cu2_flankL.jpg', 'renders/S3eq_tex_convFace.png']
const attempt = (node: string, n: number, files: string[]): StepAttempt => ({ attempt: n, startedSeq: n * 10, status: 'done', directory: { name: `${node}/${n}`, path: `/state/.archon/artifacts/run_1/${node}/${n}` }, outputs: [], verdicts: [], files: files.map(file => ({ name: `${node}/${n}/${file}`, path: `/state/.archon/artifacts/run_1/${node}/${n}/${file}`, kind: 'image', size: 1, modifiedAt: '' })) })
const judged = attempt('S3', 3, [])
judged.verdicts = [{ gateId: 'gate', evaluationSeq: 50, seq: 60, verdict: 'fail', reason: text(`See ${paths.join(' and ')}`), evidence: [{ text: text(report) }] }]
const judges: StepAttempts[] = [{ nodeId: 'fmn_judge_clean', title: 'Judge', outputs: [], attempts: [attempt('fmn_judge_clean', 2, paths), attempt('fmn_judge_clean', 3, [...paths, 'report.md'])] }]

describe('judge attempt citations', () => {
  it('includes the cited judging attempt files and folder in attempt and gate scopes', () => {
    const evaluation = { seq: 50, kinds: ['formation'], criterion: text('Check'), kindResults: [], verdict: { seq: 60, verdict: 'fail', reason: judged.verdicts[0].reason, evidence: judged.verdicts[0].evidence } }
    const steps = [{ nodeId: 'S3', title: 'S3', outputs: [], attempts: [judged] }, ...judges]
    for (const scope of [withJudgingAttempts(judged, judges), evaluationCitationScope(evaluation, steps)]) {
      expect(scope.directories).toContain('fmn_judge_clean/3')
      expect(scope.directories).not.toContain('fmn_judge_clean/2')
      for (const path of paths) expect(resolveCitation(path, scope.artifacts, scope.paths, scope.directories)).toBe(`fmn_judge_clean/3/${path}`)
    }
  })
  it('falls through the producing attempt directory to an explicitly cited report without borrowing other rounds', () => {
    const known = judges[0].attempts.flatMap(attempt => attempt.files.map(file => file.name))
    expect(resolveCitation(paths[0], known, [report], ['S3/3'])).toBe(`fmn_judge_clean/3/${paths[0]}`)
    expect(resolveCitation(paths[0], [known[0]], [report], ['S3/3'])).toBeNull()
  })
  it('fetches only the formation and the judge named by its report', async () => {
    const fetch = vi.spyOn(api, 'fetchRunAttempts').mockImplementation(async (runId, nodeId) => ({ runId, steps: nodeId === 'S3' ? [{ nodeId: 'S3', title: 'S3', outputs: [], attempts: [judged] }] : judges }))
    try {
      const evidence: NodeEvidence = { runId: 'run_1', nodeId: 'S3', kind: 'formation' }
      expect(await fetchCitationAttempts('run_1', evidence)).toHaveLength(2)
      expect(fetch.mock.calls).toEqual([['run_1', 'S3'], ['run_1', 'fmn_judge_clean']])
    } finally { fetch.mockRestore() }
  })
})
