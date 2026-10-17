import { beforeEach, expect, it, vi } from 'vitest'
import { readBoardView, writeBoardView } from './boardView'

beforeEach(() => localStorage.clear())

it('migrates the initially opened mission once and removes the old map', () => {
  localStorage.setItem('archon.missionView.v1', JSON.stringify({ first: 'flow' }))
  expect(readBoardView('first')).toBe('flow')
  expect(localStorage.getItem('archon.missionView.v1')).toBeNull()
  expect(readBoardView('second')).toBe('flow')
  writeBoardView('canvas')
  expect(readBoardView('first')).toBe('canvas')
})

it.each(['{', 'null', '[]', '{"other":"flow"}'])('defaults safely for old preference %s', old => {
  localStorage.setItem('archon.missionView.v1', old)
  expect(readBoardView('first')).toBe('canvas')
  expect(localStorage.getItem('archon.missionView.v1')).toBeNull()
})

it('keeps the new preference if an old map remains', () => {
  writeBoardView('canvas')
  localStorage.setItem('archon.missionView.v1', '{"first":"flow"}')
  expect(readBoardView('first')).toBe('canvas')
  expect(localStorage.getItem('archon.missionView.v1')).toBeNull()
})

it('works when storage is unavailable', () => {
  const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('unavailable') })
  expect(readBoardView('first')).toBe('canvas')
  get.mockRestore()
})
