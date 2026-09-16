import { describe, expect, it } from 'vitest'
import type { BoardDocument } from '../components/formationsTypes'
import { nodeFileRefs } from './referencedFiles'

const port = (id: string) => [{ id, label: id }]

const board: BoardDocument = {
  id: 'brd_refs', slug: 'refs', title: 'Refs', rev: 1, etag: 'e',
  missions: [{ id: 'mis_a', title: 'Launch', goal: '', beadId: '', files: ['docs/sketch.md'] }],
  formations: [
    { id: 'fmn_work', type: 'solo', title: 'Work', inputs: port('in'), outputs: port('out'), slots: [], brief: { goal: 'Do it', files: ['docs/design.md'] } },
    { id: 'fmn_first', type: 'solo', title: 'First judge', inputs: port('in1'), outputs: port('out1'), slots: [], brief: { files: ['rubrics/review.md', 'rubrics/first.md'] } },
    { id: 'fmn_second', type: 'solo', title: 'Second judge', inputs: port('in2'), outputs: port('out2'), slots: [], brief: { files: ['rubrics/second.md'] } },
  ],
  gates: [
    { id: 'gate_review', title: 'Review', kinds: ['formation'], criterion: '', files: ['rubrics/review.md'] },
    { id: 'gate_plain', title: 'Plain', kinds: ['human'], criterion: '' },
  ],
  connections: [
    { id: 'e1', from: 'gate_review:judge', to: 'fmn_first:in1' },
    { id: 'e2', from: 'fmn_first:out1', to: 'fmn_second:in2' },
    { id: 'e3', from: 'fmn_second:out2', to: 'gate_review:judge' },
  ],
}

describe('referenced files of a node', () => {
  it('lists a mission\'s files and a formation\'s brief files, and nothing for a node without any', () => {
    expect(nodeFileRefs(board, 'mis_a')).toEqual([{ ref: 'docs/sketch.md', owner: 'Launch' }])
    expect(nodeFileRefs(board, 'fmn_work')).toEqual([{ ref: 'docs/design.md', owner: 'Work' }])
    expect(nodeFileRefs(board, 'gate_plain')).toEqual([])
    expect(nodeFileRefs(board, 'fmn_missing')).toEqual([])
    expect(nodeFileRefs(null, 'mis_a')).toEqual([])
  })

  it('shows on a gate its own files, then each judge\'s brief files in chain order, once each', () => {
    expect(nodeFileRefs(board, 'gate_review')).toEqual([
      { ref: 'rubrics/review.md', owner: 'Review' },
      { ref: 'rubrics/first.md', owner: 'First judge', judge: true },
      { ref: 'rubrics/second.md', owner: 'Second judge', judge: true },
    ])
  })
})
