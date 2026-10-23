// Ported from CHROTE pathLinks.test.ts; window event replaces Files store.
import { describe, expect, it, vi } from 'vitest'
import { findPaths, pathLinksOnLine } from './pathLinks'
describe('absolute paths as terminal links', () => {
  it('covers exactly the path, in the columns xterm counts', () => {
    const links = pathLinksOnLine('wrote /work/project/docs/journeys.md now', 7)
    expect(links).toHaveLength(1)
    expect(links[0].text).toBe('/work/project/docs/journeys.md')
    expect(links[0].range).toEqual({ start: { x: 7, y: 7 }, end: { x: 36, y: 7 } })
  })

  it.each([
    ['a sentence ends: see /tmp/out.log.', '/tmp/out.log'],
    ['in brackets (/work/project/README.md)', '/work/project/README.md'],
    ['quoted "/home/alice/notes.txt"', '/home/alice/notes.txt'],
    ['with a line: /work/project/dashboard/src/App.tsx:123:5', '/work/project/dashboard/src/App.tsx'],
    ['a directory /work/project/', '/work/project'],
    ['--flag=/etc/hosts', '/etc/hosts'],
  ])('trims what the sentence added: %s', (line, path) => {
    expect(findPaths(line).map(found => found.path)).toEqual([path])
  })

  it.each([
    'see https://example.com/deep/link for the run',
    'file:///work/project is a URL',
    'either/or, 3/4, and/or 2026/09/03',
    'a bare / is nothing',
    '~/relative and ./relative are not absolute',
  ])('offers no link on: %s', line => {
    expect(findPaths(line)).toEqual([])
  })

  it('opens every kind of file without checking the host', () => {
    const receive = vi.fn()
    document.addEventListener('archon-open-path', receive)
    const [link] = pathLinksOnLine('saved /tmp/shot.PNG', 3)
    link.activate(new MouseEvent('click'), link.text)
    expect(receive.mock.calls[0][0].detail.path).toBe('/tmp/shot.PNG')
    document.removeEventListener('archon-open-path', receive)
  })
})
