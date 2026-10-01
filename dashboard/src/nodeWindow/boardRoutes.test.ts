import { describe, expect, it } from 'vitest'
import { judgeChain, nodeRoutes, stepNumbers } from './boardRoutes'

const port = (id: string, label = id) => ({ id, label })
const formation = (id: string, title: string) => ({ id, title, type: 'solo', inputs: [port('in', 'Input')], outputs: [port('out', 'Output')], slots: [] })
const gate = (id: string, title: string, kinds = ['human']) => ({ id, title, kinds, criterion: '' })
const wire = (from: string, to: string) => ({ id: `${from}->${to}`, from, to })

// The Wayfinding shape: map, framing review, questions, answers, draft, an
// adversarial review judged by a critic, and a sign-off whose pass ends its path
// at a Done End node.
const board = {
  missions: [{ id: 'mission', title: 'Wayfinding', goal: '', beadId: '' }],
  formations: [formation('map', 'Map the territory'), formation('questions', 'Question peers'), formation('draft', 'Draft the brief'), formation('critic', 'Brief critic')],
  gates: [gate('framing', 'Framing review'), gate('answers', 'Answer questions'), gate('review', 'Adversarial review', ['formation']), gate('signoff', 'Brief sign-off')],
  ends: [{ id: 'done', title: 'Done', outcome: 'done' as const }],
  connections: [
    wire('mission:out', 'map:in'),
    wire('map:out', 'framing:in'),
    wire('framing:pass', 'questions:in'),
    wire('framing:fail', 'map:in'),
    wire('questions:out', 'answers:in'),
    wire('answers:pass', 'draft:in'),
    wire('answers:fail', 'questions:in'),
    wire('draft:out', 'review:in'),
    wire('review:judge', 'critic:in'),
    wire('critic:out', 'review:judge'),
    wire('review:pass', 'signoff:in'),
    wire('review:fail', 'draft:in'),
    wire('signoff:fail', 'draft:in'),
    wire('signoff:pass', 'done:in'),
  ],
}

describe('board routes', () => {
  it('numbers steps as the Flow view does: missions and judges carry no number', () => {
    expect([...stepNumbers(board).entries()]).toEqual([
      ['map', 1], ['framing', 2], ['questions', 3], ['answers', 4],
      ['draft', 5], ['review', 6], ['signoff', 7],
    ])
    expect(judgeChain(board, 'review')).toEqual(['critic'])
    expect(judgeChain(board, 'framing')).toEqual([])
  })

  it('states a gate in words, with a loop back and a pass that ends its path', () => {
    expect(nodeRoutes(board, 'review').map(route => route.text)).toEqual([
      'Fed by 5 Draft the brief',
      'Judged by Brief critic',
      'Pass → 7 Brief sign-off',
      'Fail ↺ back to 5 Draft the brief',
    ])
    expect(nodeRoutes(board, 'signoff').map(route => route.text)).toEqual([
      'Fed by 6 Adversarial review',
      'Pass → this path ends (done)',
      'Fail ↺ back to 5 Draft the brief',
    ])
    expect(nodeRoutes(board, 'signoff').find(route => route.kind === 'pass')?.nodeId).toBe('done')
  })

  it('names a renamed End node and says when a route leads nowhere yet', () => {
    const renamed = { ...board, ends: [{ id: 'done', title: 'Shipped', outcome: 'done' as const }] }
    expect(nodeRoutes(renamed, 'signoff').find(route => route.kind === 'pass')?.text).toBe('Pass → this path ends (done) · Shipped')
    const dangling = { ...board, connections: board.connections.filter(connection => connection.from !== 'signoff:pass' && connection.from !== 'signoff:fail') }
    expect(nodeRoutes(dangling, 'signoff').map(route => route.text)).toEqual([
      'Fed by 6 Adversarial review',
      'Pass → leads nowhere: wire it to a step or an End node',
      'Fail → leads nowhere: wire it to a step or an End node',
    ])
  })

  it('states an End node by the routes that end there', () => {
    const shared = { ...board, ends: [...board.ends, { id: 'no', title: 'Rejected', outcome: 'rejected' as const }], connections: [...board.connections, wire('framing:fail', 'no:in'), wire('draft:out', 'no:in')] }
    expect(nodeRoutes(shared, 'no').map(route => route.text)).toEqual([
      "Ends 2 Framing review's fail route",
      'Ends 5 Draft the brief',
    ])
    expect(nodeRoutes(shared, 'no').map(route => route.kind)).toEqual(['ended-by', 'ended-by'])
    expect(nodeRoutes({ ...shared, connections: [] }, 'no').map(route => route.text)).toEqual(['No route leads here yet'])
  })

  it('states formations, judges and missions in words', () => {
    expect(nodeRoutes(board, 'draft').map(route => route.text)).toEqual([
      'Fed by 4 Answer questions',
      'Sent back by 6 Adversarial review',
      'Sent back by 7 Brief sign-off',
      'Feeds → 6 Adversarial review',
    ])
    expect(nodeRoutes(board, 'critic').map(route => route.text)).toEqual(['Judges 6 Adversarial review'])
    expect(nodeRoutes(board, 'mission')).toEqual([{ kind: 'starts', nodeId: 'map', text: 'Starts → 1 Map the territory' }])
    expect(nodeRoutes({ ...board, connections: [] }, 'map').map(route => route.text)).toEqual(['Feeds → leads nowhere: wire it to a step or an End node'])
  })
})
