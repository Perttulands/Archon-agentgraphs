import { describe, expect, it } from 'vitest'
import { arrangedLayoutNodes } from './formationsArrange'
import { displayLayoutFor } from './formationsCanvas'
import type { BoardDocument } from './formationsTypes'

const board: BoardDocument = {
  id: 'mission', slug: 'mission', title: 'Delivery', rev: 7, etag: 'rev-7',
  inputCards: [{ id: 'input', title: 'Input', goal: '' }],
  // Deliberately not in execution order, as in hand-authored TOML.
  formations: ['judge', 'work', 'finish'].map(id => ({ id, title: id, type: 'solo', slots: [], inputs: [], outputs: [] })),
  gates: [{ id: 'gate', title: 'Review', kinds: ['formation'], criterion: '' }],
  limits: [{ id: 'limit', title: 'Rounds', target: 'work', rounds: 3 }],
  connections: [
    { id: '1', from: 'input:out', to: 'work:in' },
    { id: '2', from: 'work:out', to: 'gate:in' },
    { id: '3', from: 'gate:judge', to: 'judge:in' },
    { id: '4', from: 'judge:out', to: 'gate:judge' },
    { id: '5', from: 'gate:fail', to: 'work:in' },
    { id: '6', from: 'gate:pass', to: 'finish:in' },
  ],
}

describe('display arrangement', () => {
  it('follows the path, stacking judges and limits with their owners without editing', () => {
    const before = structuredClone(board)
    const positions = arrangedLayoutNodes(board)
    expect(positions).toEqual([
      { id: 'input', x: 112, y: 112 },
      { id: 'work', x: 476, y: 112 },
      { id: 'limit', x: 476, y: 672 },
      { id: 'gate', x: 896, y: 112 },
      { id: 'judge', x: 896, y: 476 },
      { id: 'finish', x: 1316, y: 112 },
    ])
    expect(displayLayoutFor(board, new Map())).toEqual(new Map(positions.map(node => [node.id, node])))
    expect(board).toEqual(before)
    const reordered = { ...board, formations: [...board.formations].reverse(), connections: [...board.connections].reverse() }
    expect(arrangedLayoutNodes(reordered)).toEqual(positions)
  })

  it('keeps saved coordinates while filling each missing position from Arrange', () => {
    const saved = new Map([['work', { id: 'work', x: 31, y: 47 }]])
    const resolved = displayLayoutFor(board, saved)
    expect(resolved.get('work')).toEqual(saved.get('work'))
    expect(resolved.get('gate')).toEqual({ id: 'gate', x: 896, y: 112 })
    expect(saved.size).toBe(1)
  })

  it('handles forward joins, fail-only paths, disconnected cycles and referenced files', () => {
    const graph = structuredClone(board)
    graph.formations.push(...['branch', 'rework', 'cycle-a', 'cycle-b'].map(id => ({ id, title: id, type: 'solo' as const, slots: [], inputs: [], outputs: [] })))
    graph.formations[0].brief = { goal: '', files: ['/rubric.md'] }
    graph.connections.push(
      { id: '7', from: 'input:out', to: 'branch:in' },
      { id: '8', from: 'branch:out', to: 'work:in' },
      { id: '9', from: 'gate:fail', to: 'rework:in' },
      { id: '10', from: 'cycle-a:out', to: 'cycle-b:in' },
      { id: '11', from: 'cycle-b:out', to: 'cycle-a:in' },
    )
    const layout = displayLayoutFor(graph, new Map())
    const at = (id: string) => layout.get(id)!
    expect(layout.size).toBe(10)
    expect(at('work').x).toBeGreaterThan(at('branch').x)
    expect(at('rework').x).toBeGreaterThan(at('gate').x)
    expect(at('cycle-a').x).toBeGreaterThan(at('finish').x)
    expect(at('cycle-b').x).toBeGreaterThan(at('cycle-a').x)
    expect(at('judge').y).toBe(504)
  })
})
