import { describe, expect, it } from 'vitest'
import { inverseUndoStep } from './formationsRedo'
import { UndoHistory, boardStep, undoOutcomeMessage, type UndoRunner, type UndoStep } from './formationsUndo'
import type { BoardDocument, LayoutDocument } from './formationsTypes'
const board: BoardDocument = {id:'mission',slug:'mission',title:'Mission',rev:1,etag:'a',formations:[{id:'worker',title:'Worker',type:'solo',slots:[],inputs:[{id:'in',label:'Input'}],outputs:[]}],connections:[{id:'same-edge',from:'entry:out',to:'worker:in'}]}
const layout: LayoutDocument = {missionId:'mission',missionRev:1,etag:'l',nodes:[{id:'worker',x:123,y:456}],edges:[]}
describe('redo',()=>{
 it('names redo failures without rewriting the original card title or reason', () => {
  const entry = { board: 'mission', label: 'the brief undo policy', steps: [boardStep({ title: 'undo policy' })] }
  const reason = 'undo policy changed elsewhere'
  expect(undoOutcomeMessage({ status: 'failed', entry, message: reason, done: [], remaining: entry.steps }, 'redo')).toBe('Could not redo the brief undo policy: undo policy changed elsewhere. It was removed from the redo history.')
  expect(undoOutcomeMessage({ status: 'busy', entry, done: [], remaining: entry.steps }, 'redo')).toContain('press Ctrl+Shift+Z')
 })
 it('captures the explicit retained solo slot for redo after restoring several staffed slots', () => {
  const solo: BoardDocument = { ...board, formations: [{ ...board.formations[0], slots: [{ id: 'kept', label: 'Kept', controller: false, agentId: 'role', harness: 'openai-codex', effort: 'medium' }] }] }
  expect(inverseUndoStep(solo, layout, boardStep({ setFormationType: { id: 'worker', type: 'peer', slots: [] } }))).toEqual([
   boardStep({ setFormationType: { id: 'worker', type: 'solo', slots: solo.formations[0].slots, keepSlotId: 'kept' } }),
  ])
 })
 it('leaves queued Undo and Redo intact when their view becomes read only before writes settle', async () => {
  const history = new UndoHistory(); history.setBoard('mission')
  let editable = true
  let value = 1
  let release: () => void = () => undefined
  const idle = new Promise<void>(resolve => { release = resolve })
  const apply = async (step: UndoStep) => { const before = value; value = Number('board' in step ? step.board.value : 0); return [boardStep({ value: before })] }
  const runner: UndoRunner = { board: () => 'mission', idle: () => idle, canEdit: () => editable, apply, isConflict: () => false, reload: async () => undefined }
  history.record({ board: 'mission', label: 'edit', steps: [boardStep({ value: 0 })] })
  const pending = history.undo(runner)
  editable = false; release()
  expect(await pending).toEqual({ status: 'empty' }); expect(value).toBe(1); expect(history.size).toBe(1)
  editable = true; await history.undo(runner)
  editable = false; expect(await history.redo(runner)).toEqual({ status: 'empty' }); expect(history.redoSize).toBe(1)
  editable = true; await history.redo(runner); expect(value).toBe(1)
 })
 it('restores the removed wire identity and deleted node placement',()=>{
  expect(inverseUndoStep(board,layout,boardStep({unwireConnection:{from:'entry:out',to:'worker:in'}}))).toEqual([boardStep({wireConnection:{id:'same-edge',from:'entry:out',to:'worker:in'}})])
  const inverse=inverseUndoStep(board,layout,boardStep({deleteFormation:{id:'worker'}}))[0]
  expect(inverse).toEqual(boardStep({restoreNode:{formation:board.formations[0],connections:board.connections,index:0,x:123,y:456}}))
 })
 it('captures only the overwritten fields and respects current values',()=>{
  expect(inverseUndoStep(board,layout,boardStep({updateFormation:{id:'worker',title:'Old'}}))).toEqual([boardStep({updateFormation:{id:'worker',title:'Worker'}})])
 })
 it('reverses a multi-write gesture in order, then clears redo for a new edit',async()=>{
  const history=new UndoHistory();history.setBoard('mission');let value=2
  const apply=async(step:UndoStep)=>{const previous=value;value=Number(('board' in step ? step.board : {}).value);return [boardStep({value:previous})]}
  const runner:UndoRunner={board:()=> 'mission',idle:async()=>undefined,apply,isConflict:()=>false,reload:async()=>undefined}
  history.record({board:'mission',label:'gesture',steps:[boardStep({value:1}),boardStep({value:0})]})
  await history.undo(runner);expect(value).toBe(0);expect(history.redoSize).toBe(1)
  await history.redo(runner);expect(value).toBe(2);expect(history.size).toBe(1)
  await history.undo(runner);history.record({board:'mission',label:'new',steps:[boardStep({value:3})]});expect(history.redoSize).toBe(0)
  history.setBoard('other');expect(history.size).toBe(0);expect(history.redoSize).toBe(0)
 })
})
