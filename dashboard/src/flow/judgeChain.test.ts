import { describe, expect, it } from 'vitest'
import type { BoardDocument } from '../components/formationsTypes'
import vectors from '../../../src/internal/formations/testdata/judge_chains.json'
import { judgeChain } from './flowModel'

// The engine's judgeChainForGate reads the same vectors, so the cockpit and
// the engine agree on what a judge chain holds (archon-n7u.51).

describe('judgeChain', () => {
  it.each(vectors.map(vector => [vector.name, vector] as const))('%s', (_name, vector) => {
    const board = {
      formations: vector.formations.map(id => ({ id })),
      connections: vector.connections.map(([from, to], index) => ({ id: `edge_${index}`, from, to })),
    } as unknown as Pick<BoardDocument, 'connections' | 'formations'>
    expect(judgeChain(board, 'gate_g')).toEqual(vector.chain)
  })
})
