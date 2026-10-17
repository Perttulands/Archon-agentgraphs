import { describe, expect, it } from 'vitest'
import { citedPaths, resolveCitation } from './evidenceCitations'

describe('evidence file citations', () => {
  it('reads the relative render names used by real judge reports', () => {
    expect(citedPaths('Evidence: `renders/sh_face_hd.png`, `renders/sh_face_zoom.png` (1.2 m).')).toEqual(['renders/sh_face_hd.png', 'renders/sh_face_zoom.png'])
  })
  it('uses absolute cited evidence before an artifact list with several attempts', () => {
    const absolute = '/state/.archon/artifacts/run_1/judge/3/renders/sh_face_hd.png'
    expect(resolveCitation('renders/sh_face_hd.png', ['judge/2/renders/sh_face_hd.png', 'judge/3/renders/sh_face_hd.png'], [absolute])).toBe(absolute)
  })
  it('finds a unique relative artifact and declines an ambiguous attempt', () => {
    expect(resolveCitation('renders/contact.png', ['judge/3/renders/contact.png'])).toBe('judge/3/renders/contact.png')
    expect(resolveCitation('renders/contact.png', ['judge/2/renders/contact.png', 'judge/3/renders/contact.png'])).toBeNull()
  })
  it('scopes repeated render names to the cited report directory', () => {
    expect(resolveCitation('renders/contact.png', ['judge/2/renders/contact.png', 'judge/3/renders/contact.png'], ['/state/judge/3/report.md'])).toBe('judge/3/renders/contact.png')
  })
  it('resolves beside a cited report when the bounded artifact listing omitted its image', () => {
    expect(resolveCitation('renders/sh_face_hd.png', [], ['/state/judge/3/report.md'])).toBe('/state/judge/3/renders/sh_face_hd.png')
  })
})
