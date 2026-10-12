import { expect, it } from 'vitest'
import { findBeadIds } from './textLinks'

const ids = (text: string) => findBeadIds(text).map(match => match.id)

it.each(['archon-fh5c', 'archon-n7u.53', 'aqg-0gy.72.2', 'srv-lc1', 'form-e2c', 'archon-jkxq', 'b3d-9aj5'])('links the Bead ID %s whole', id => {
  expect(ids(`See ${id}, then (${id}) and ${id}.`)).toEqual([id, id, id])
})

it('links a dotted ID whole when a label follows it', () => {
  expect(ids('artifact aqg-0gy.72.2-round1 and branch archon-fh5c-fix')).toEqual(['aqg-0gy.72.2', 'archon-fh5c'])
})

it.each([
  'wrist-to-cuff', 'low-poly', 'hard-edged', 'game-framed', 'read-only', 'air-dry', 'state-of-the-art',
  'well-known low-poly-mesh', 'utf-8', 'gpt-4o', 'run_01M4RTEKFWHZ8F6MY11M2H6MN8', 'worktree-agent-ae2658c8', '/srv/archon-n7u/file',
])('links nothing in %s', text => {
  expect(ids(text)).toEqual([])
})

it('never links part of an ID', () => {
  expect(ids('aqg-0gy.72x and archon-fh5cabc and archon-n7u.5.')).toEqual(['archon-n7u.5'])
})
