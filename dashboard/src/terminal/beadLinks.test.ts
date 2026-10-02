// Ported from CHROTE beadLinks.test.ts; copy is the approved departure.
import { describe, expect, it, vi } from 'vitest'
import { beadLinksOnLine } from './beadLinks'
vi.mock('../utils/clipboard', () => ({ copyTextToClipboard: vi.fn(() => Promise.resolve(true)) }))
import { copyTextToClipboard } from '../utils/clipboard'
describe('Bead terminal links', () => {
 it('covers the ID in xterm columns', () => {
  const [link] = beadLinksOnLine('working chrote-5grx.15 now', 7)
  expect(link.range).toEqual({ start: { x: 9, y: 7 }, end: { x: 22, y: 7 } })
 })
 it('ignores lines without IDs', () => expect(beadLinksOnLine('npm run test:unit', 1)).toEqual([]))
 it('copies the ID', () => { const [link] = beadLinksOnLine('see ctx-t4ak', 3); link.activate(new MouseEvent('click'), link.text); expect(copyTextToClipboard).toHaveBeenCalledWith('ctx-t4ak') })
})
