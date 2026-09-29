import { describe, expect, it } from 'vitest'
import { UndoHistory, boardStep, combineUndo, nodeDeleteUndo, portRemoveUndo, undoOutcomeMessage, type UndoStep } from './formationsUndo'
import type { BoardDocument } from './formationsTypes'

const board: BoardDocument = {
  id: 'brd', slug: 'board', title: 'Board', rev: 3, etag: 'e3',
  missions: [{ id: 'mis', title: 'Ship', goal: 'Ship it', beadId: '' }],
  formations: [{
    id: 'fmn', type: 'solo', title: 'Plan', brief: { goal: 'Plan it' },
    inputs: [{ id: 'in_a', label: 'Input' }, { id: 'in_b', label: 'Rework' }],
    outputs: [{ id: 'out', label: 'Output' }],
    slots: [{ id: 'slot', label: 'Agent', agentId: 'codex', harness: 'openai-codex', controller: false }],
  }],
  gates: [{ id: 'gate', title: '', kinds: ['human'], criterion: 'Looks right' }],
  connections: [
    { id: 'e1', from: 'mis:out', to: 'fmn:in_a' },
    { id: 'e2', from: 'fmn:out', to: 'gate:in' },
    { id: 'e3', from: 'gate:fail', to: 'fmn:in_b' },
  ],
}

describe('UndoHistory', () => {
  it('undoes newest first and runs one entry at a time', async () => {
    const history = new UndoHistory()
    history.record({ label: 'first', steps: [boardStep({ a: 1 })] })
    history.record({ label: 'second', steps: [boardStep({ b: 1 }), boardStep({ b: 2 })] })
    history.record({ label: 'nothing', steps: [] })
    expect(history.size).toBe(2)
    const applied: UndoStep[] = []
    let release: () => void = () => undefined
    const gate = new Promise<void>(resolve => { release = resolve })
    const apply = async (step: UndoStep) => {
      applied.push(step)
      await gate
    }
    const one = history.undo(apply)
    const two = history.undo(apply)
    await Promise.resolve()
    expect(applied).toEqual([boardStep({ b: 1 })])
    release()
    expect(await one).toEqual(expect.objectContaining({ status: 'undone', entry: expect.objectContaining({ label: 'second' }) }))
    expect(await two).toEqual(expect.objectContaining({ status: 'undone', entry: expect.objectContaining({ label: 'first' }) }))
    expect(applied).toEqual([boardStep({ b: 1 }), boardStep({ b: 2 }), boardStep({ a: 1 })])
    expect(await history.undo(apply)).toEqual({ status: 'empty' })
  })

  it('drops a failed entry so older history stays reachable', async () => {
    const history = new UndoHistory()
    history.record({ label: 'the rename', steps: [boardStep({ rename: true })] })
    history.record({ label: 'delete gate “Tests”', steps: [boardStep({ restore: true })] })
    const failed = await history.undo(async () => { throw new Error('Formation resource not found') })
    expect(undoOutcomeMessage(failed)).toBe('Could not undo delete gate “Tests”: Formation resource not found. It was removed from the undo history.')
    const applied: UndoStep[] = []
    expect((await history.undo(async step => { applied.push(step) })).status).toBe('undone')
    expect(applied).toEqual([boardStep({ rename: true })])
    expect(history.size).toBe(0)
  })

  it('combines one gesture into one entry that undoes the last edit first', () => {
    const combined = combineUndo('the judge', { label: '', steps: [boardStep({ first: true })] }, null, { label: '', steps: [boardStep({ second: 1 }), boardStep({ second: 2 })] })
    expect(combined).toEqual({ label: 'the judge', steps: [boardStep({ second: 1 }), boardStep({ second: 2 }), boardStep({ first: true })] })
  })
})

describe('delete and port undo capture', () => {
  it('captures a node as the server sent it, with every touching connection and its place', () => {
    expect(nodeDeleteUndo(board, 'fmn', { x: 420.4, y: 96 })).toEqual({
      label: 'delete formation “Plan”',
      steps: [boardStep({ restoreNode: { formation: board.formations[0], connections: board.connections, index: 0, x: 420, y: 96 } })],
    })
    expect(nodeDeleteUndo(board, 'gate', { x: 1, y: 2 })?.label).toBe('delete gate untitled')
    expect(nodeDeleteUndo(board, 'mis', { x: 1, y: 2 })?.steps).toEqual([
      boardStep({ restoreNode: { mission: board.missions![0], connections: [board.connections[0]], index: 0, x: 1, y: 2 } }),
    ])
    expect(nodeDeleteUndo(board, 'missing', { x: 0, y: 0 })).toBeNull()
  })

  it('captures a removed port with its direction, place and connections', () => {
    expect(portRemoveUndo(board, 'fmn', 'in_b')).toEqual({
      label: 'remove input “Rework” from “Plan”',
      steps: [boardStep({ restorePort: { formationId: 'fmn', direction: 'input', port: { id: 'in_b', label: 'Rework' }, index: 1, connections: [board.connections[2]] } })],
    })
    expect(portRemoveUndo(board, 'fmn', 'out')?.steps).toEqual([
      boardStep({ restorePort: { formationId: 'fmn', direction: 'output', port: { id: 'out', label: 'Output' }, index: 0, connections: [board.connections[1]] } }),
    ])
    expect(portRemoveUndo(board, 'fmn', 'nope')).toBeNull()
  })
})
