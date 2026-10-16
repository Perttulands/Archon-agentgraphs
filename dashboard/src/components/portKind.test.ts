import { describe, expect, it, vi } from 'vitest'
import { portKindMenuItems, portKindOf } from './portKind'

describe('portKindMenuItems', () => {
  it('heads with what the port carries and offers the other kinds', () => {
    const onSet = vi.fn()
    const items = portKindMenuItems({ id: 'out', label: 'Result' }, onSet)
    expect(items.map(item => [item.label, Boolean(item.head)])).toEqual([
      ['Carries text', true], ['Carry a file instead', false], ['Carry a folder instead', false],
    ])
    items[2].action?.()
    expect(onSet).toHaveBeenCalledWith('folder')
  })

  it('offers text back to a file port', () => {
    const items = portKindMenuItems({ id: 'out', label: 'Report', kind: 'file' }, vi.fn())
    expect(items.map(item => item.label)).toEqual(['Carries a file', 'Carry text instead', 'Carry a folder instead'])
  })

  it('reads a port without a kind as text', () => {
    expect(portKindOf({})).toBe('text')
    expect(portKindOf({ kind: 'folder' })).toBe('folder')
  })
})
