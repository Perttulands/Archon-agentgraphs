import { describe, expect, it } from 'vitest'
import { UndoHistory, WriteTracker, boardStep, combineUndo, nodeDeleteUndo, portRemoveUndo, restoreBlocker, undoOutcomeMessage, type UndoRunner, type UndoStep } from './formationsUndo'
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

class Conflict extends Error {}

/** A runner on board 'b' that applies steps through `apply` and counts reloads. */
function runner(apply: (step: UndoStep) => Promise<void>, overrides: Partial<UndoRunner> = {}) {
  const calls = { reloads: 0 }
  const value: UndoRunner = {
    board: () => 'b',
    idle: () => Promise.resolve(),
    apply,
    isConflict: err => err instanceof Conflict,
    reload: async () => { calls.reloads++ },
    ...overrides,
  }
  return { value, calls }
}

function historyOn(slug = 'b') {
  const history = new UndoHistory()
  history.setBoard(slug)
  return history
}

describe('UndoHistory', () => {
  it('undoes newest first and runs one entry at a time', async () => {
    const history = historyOn()
    history.record({ board: 'b', label: 'first', steps: [boardStep({ a: 1 })] })
    history.record({ board: 'b', label: 'second', steps: [boardStep({ b: 1 }), boardStep({ b: 2 })] })
    history.record({ board: 'b', label: 'nothing', steps: [] })
    expect(history.size).toBe(2)
    const applied: UndoStep[] = []
    let release: () => void = () => undefined
    const gate = new Promise<void>(resolve => { release = resolve })
    const { value } = runner(async step => {
      applied.push(step)
      await gate
    })
    const one = history.undo(value)
    const two = history.undo(value)
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(applied).toEqual([boardStep({ b: 1 })])
    release()
    expect(await one).toEqual(expect.objectContaining({ status: 'undone', entry: expect.objectContaining({ label: 'second' }) }))
    expect(await two).toEqual(expect.objectContaining({ status: 'undone', entry: expect.objectContaining({ label: 'first' }) }))
    expect(applied).toEqual([boardStep({ b: 1 }), boardStep({ b: 2 }), boardStep({ a: 1 })])
    expect(await history.undo(value)).toEqual({ status: 'empty' })
  })

  it('drops an entry the board refuses, so older history stays reachable', async () => {
    const history = historyOn()
    history.record({ board: 'b', label: 'the rename', steps: [boardStep({ rename: true })] })
    history.record({ board: 'b', label: 'the delete of gate “Tests”', steps: [boardStep({ restore: true })] })
    const failed = await history.undo(runner(async () => { throw new Error('what it was wired to (fmn) is no longer on the board') }).value)
    expect(undoOutcomeMessage(failed)).toBe('Could not undo the delete of gate “Tests”: what it was wired to (fmn) is no longer on the board. It was removed from the undo history.')
    const applied: UndoStep[] = []
    expect((await history.undo(runner(async step => { applied.push(step) }).value)).status).toBe('undone')
    expect(applied).toEqual([boardStep({ rename: true })])
    expect(history.size).toBe(0)
  })

  it('reloads the board once on a stale-revision conflict and retries the same step', async () => {
    const history = historyOn()
    history.record({ board: 'b', label: 'the rename', steps: [boardStep({ rename: true })] })
    let attempts = 0
    const { value, calls } = runner(async () => { if (attempts++ === 0) throw new Conflict('Formation definition changed; reload and retry') })
    expect((await history.undo(value)).status).toBe('undone')
    expect(attempts).toBe(2)
    expect(calls.reloads).toBe(1)
    expect(history.size).toBe(0)
  })

  it('keeps what was not undone when the board keeps changing, and says so without dropping it', async () => {
    const history = historyOn()
    history.record({ board: 'b', label: 'the reconnection', steps: [boardStep({ one: 1 }, 'the new wire'), boardStep({ two: 2 }, 'the old wire')] })
    let attempts = 0
    const { value, calls } = runner(async step => { attempts++; if ('board' in step && step.board.two) throw new Conflict('stale') })
    const outcome = await history.undo(value)
    expect(outcome.status).toBe('busy')
    expect(calls.reloads).toBe(1)
    expect(attempts).toBe(3)
    expect(undoOutcomeMessage(outcome)).toBe('The board kept changing while undoing the reconnection. The new wire was undone. The rest is still on the undo history; press Ctrl+Z to try again.')
    const applied: UndoStep[] = []
    expect((await history.undo(runner(async step => { applied.push(step) }).value)).status).toBe('undone')
    expect(applied).toEqual([boardStep({ two: 2 }, 'the old wire')])
  })

  it('says what a multi-step undo did and did not undo when it fails midway', async () => {
    const history = historyOn()
    history.record({ board: 'b', label: 'the judge detach from “Review”', steps: [boardStep({ fields: 1 }, 'the gate settings'), boardStep({ chain: 1 }, 'the judge')] })
    const outcome = await history.undo(runner(async step => { if ('board' in step && step.board.chain) throw new Error('formation fmn_judge no longer exists') }).value)
    expect(undoOutcomeMessage(outcome)).toBe('Partly undid the judge detach from “Review”: the gate settings was undone, but the judge could not be: formation fmn_judge no longer exists. It was removed from the undo history.')
    history.record({ board: 'b', label: 'the edit', steps: [boardStep({ a: 1 }), boardStep({ b: 1 }), boardStep({ c: 1 })] })
    const unnamed = await history.undo(runner(async step => { if ('board' in step && step.board.c) throw new Error('nope') }).value)
    expect(undoOutcomeMessage(unnamed)).toBe('Partly undid the edit: 2 of 3 steps were undone, but 1 of 3 steps could not be: nope. It was removed from the undo history.')
  })

  it('keeps only entries of the board on screen, at record time and at undo time', async () => {
    const history = historyOn('a')
    history.record({ board: 'a', label: 'on a', steps: [boardStep({ a: 1 })] })
    history.setBoard('b')
    expect(history.size).toBe(0)
    history.record({ board: 'a', label: 'late edit from a', steps: [boardStep({ late: 1 })] })
    expect(history.size).toBe(0)
    history.record({ board: 'b', label: 'on b', steps: [boardStep({ b: 1 })] })
    const applied: UndoStep[] = []
    const stillLoading = runner(async step => { applied.push(step) }, { board: () => 'a' })
    expect(await history.undo(stillLoading.value)).toEqual({ status: 'empty' })
    expect(applied).toEqual([])
  })

  it('waits for edits in flight, and for the entry they record, before undoing', async () => {
    const writes = new WriteTracker()
    const history = historyOn()
    history.record({ board: 'b', label: 'older', steps: [boardStep({ older: 1 })] })
    let finish: () => void = () => undefined
    const write = writes.track(new Promise<void>(resolve => { finish = resolve }))
    // The edit records its undo in the continuation of its write, as the cockpit does.
    void write.then(() => history.record({ board: 'b', label: 'in flight', steps: [boardStep({ newest: 1 })] }))
    const applied: UndoStep[] = []
    const pending = history.undo(runner(async step => { applied.push(step) }, { idle: () => writes.idle() }).value)
    await new Promise(resolve => setTimeout(resolve, 5))
    expect(applied).toEqual([])
    finish()
    expect(await pending).toEqual(expect.objectContaining({ status: 'undone', entry: expect.objectContaining({ label: 'in flight' }) }))
    expect(applied).toEqual([boardStep({ newest: 1 })])
  })

  it('combines one gesture into one entry that undoes the last edit first', () => {
    const combined = combineUndo('the judge', { label: '', steps: [boardStep({ first: true })] }, null, { label: '', steps: [boardStep({ second: 1 }), boardStep({ second: 2 })] })
    expect(combined).toEqual({ label: 'the judge', steps: [boardStep({ second: 1 }), boardStep({ second: 2 }), boardStep({ first: true })] })
  })
})

describe('delete and port undo capture', () => {
  it('captures a node as the server sent it, with every touching connection and its place', () => {
    expect(nodeDeleteUndo(board, 'fmn', { x: 420.4, y: 96 })).toEqual({
      label: 'the delete of formation “Plan”',
      steps: [boardStep({ restoreNode: { formation: board.formations[0], connections: board.connections, index: 0, x: 420, y: 96 } })],
    })
    expect(nodeDeleteUndo(board, 'gate', { x: 1, y: 2 })?.label).toBe('the delete of gate untitled')
    expect(nodeDeleteUndo(board, 'mis', { x: 1, y: 2 })?.steps).toEqual([
      boardStep({ restoreNode: { mission: board.missions![0], connections: [board.connections[0]], index: 0, x: 1, y: 2 } }),
    ])
    expect(nodeDeleteUndo(board, 'missing', { x: 0, y: 0 })).toBeNull()
  })

  it('captures a removed port with its direction, place and connections', () => {
    expect(portRemoveUndo(board, 'fmn', 'in_b')).toEqual({
      label: 'the removal of input “Rework” from “Plan”',
      steps: [boardStep({ restorePort: { formationId: 'fmn', direction: 'input', port: { id: 'in_b', label: 'Rework' }, index: 1, connections: [board.connections[2]] } })],
    })
    expect(portRemoveUndo(board, 'fmn', 'out')?.steps).toEqual([
      boardStep({ restorePort: { formationId: 'fmn', direction: 'output', port: { id: 'out', label: 'Output' }, index: 0, connections: [board.connections[1]] } }),
    ])
    expect(portRemoveUndo(board, 'fmn', 'nope')).toBeNull()
  })

  it('names why a delete could not be undone, mirroring the store restore rules', () => {
    expect(restoreBlocker(board, 'fmn')).toBeNull()
    expect(restoreBlocker(board, 'gate')).toBeNull()
    expect(restoreBlocker(board, 'mis')).toBeNull()
    const legacy = structuredClone(board) as BoardDocument
    Object.assign(legacy.formations[0], { verification: { id: 'v', kinds: ['code'], criterion: 'Tests pass', onFail: 'block' } })
    expect(restoreBlocker(legacy, 'fmn')).toBe('it still carries retired inline verification')
    legacy.formations[0] = { ...board.formations[0], type: 'flow' as never }
    expect(restoreBlocker(legacy, 'fmn')).toBe('its type “flow” is retired')
    Object.assign(legacy.gates![0], { command: 'make test' })
    expect(restoreBlocker(legacy, 'gate')).toBe('it is a legacy script gate')
    legacy.gates![0] = { ...board.gates![0], kinds: [] }
    expect(restoreBlocker(legacy, 'gate')).toBe('it names no gate kind')
    legacy.gates![0] = { ...board.gates![0], checkValue: 'PASS' }
    expect(restoreBlocker(legacy, 'gate')).toBe('it has a code check without the code kind')
  })
})
