import { describe, expect, it } from 'vitest'
import { citedPaths, resolveCitation } from './evidenceCitations'

describe('evidence file citations', () => {
  it('excludes scheme-bearing tokens without linking their URL tails', () => {
    expect(citedPaths('https://docs/x.html ftp://host/a.png file:///tmp/a.png custom+v1:renders/a.png `renders/real.png`')).toEqual(['renders/real.png'])
    expect(resolveCitation('https://docs/x.html', ['docs/x.html'])).toBeNull()
  })
  it('requires a known artifact even for absolute paths and fraction-like text', () => {
    expect(resolveCitation('/tmp/missing.png', [], ['/tmp/report.md'])).toBeNull()
    expect(citedPaths('3/4.5').map(path => resolveCitation(path, []))).toEqual([null])
    expect(resolveCitation('/state/judge/3/render.png', ['judge/3/render.png'])).toBe('judge/3/render.png')
  })
  it('reads the relative render names used by real judge reports', () => {
    expect(citedPaths('Evidence: `renders/sh_face_hd.png`, `renders/sh_face_zoom.png` (1.2 m).')).toEqual(['renders/sh_face_hd.png', 'renders/sh_face_zoom.png'])
  })
  it('uses absolute cited evidence before an artifact list with several attempts', () => {
    const absolute = '/state/.archon/artifacts/run_1/judge/3/renders/sh_face_hd.png'
    expect(resolveCitation('renders/sh_face_hd.png', ['judge/2/renders/sh_face_hd.png', 'judge/3/renders/sh_face_hd.png'], [absolute])).toBe('judge/3/renders/sh_face_hd.png')
  })
  it('finds a unique relative artifact and declines an ambiguous attempt', () => {
    expect(resolveCitation('renders/contact.png', ['judge/3/renders/contact.png'])).toBe('judge/3/renders/contact.png')
    expect(resolveCitation('renders/contact.png', ['judge/2/renders/contact.png', 'judge/3/renders/contact.png'])).toBeNull()
  })
  it('scopes repeated render names to the cited report directory', () => {
    expect(resolveCitation('renders/contact.png', ['judge/2/renders/contact.png', 'judge/3/renders/contact.png'], ['/state/judge/3/report.md'])).toBe('judge/3/renders/contact.png')
  })
  it('does not invent a sibling missing from the artifact list', () => {
    expect(resolveCitation('renders/sh_face_hd.png', [], ['/state/judge/3/report.md'])).toBeNull()
  })
  it('resolves against an attempt directory and output folder without borrowing another attempt', () => {
    const files = ['replay/2/replay/checkpoints/09.jpg', 'replay/3/replay/checkpoints/09.jpg', 'replay/3/replay/input-preflight.json']
    expect(resolveCitation('replay/checkpoints/09.jpg', files, [], ['replay/3', 'replay/3/replay'])).toBe(files[1])
    expect(resolveCitation('checkpoints/09.jpg', files, [], ['replay/3', 'replay/3/replay'])).toBe(files[1])
    expect(resolveCitation('replay/3/replay/input-preflight.json', files, [], ['replay/3'])).toBe(files[2])
    expect(resolveCitation('replay/checkpoints/09.jpg', [files[0]], [], ['replay/3'])).toBeNull()
  })

  it('resolves real replay citations inside snapshotted output folders', () => {
    const image = 'fmn_replay/1/outputs/port_replay/replay/checkpoints/09.jpg'
    const json = 'fmn_replay/1/outputs/port_replay/replay/input-preflight.json'
    const directories = ['fmn_replay/1', 'fmn_replay/1/outputs/port_replay/replay']
    const earlier = image.replace('/1/', '/2/')
    expect(resolveCitation('replay/checkpoints/09.jpg', [earlier, image], [], directories)).toBe(image)
    expect(resolveCitation('replay/input-preflight.json', [json], [], directories)).toBe(json)
    expect(resolveCitation('replay/checkpoints/09.jpg', [earlier], [], directories)).toBeNull()
    expect(resolveCitation('replay/checkpoints/09.jpg', [image, 'fmn_replay/1/other/replay/checkpoints/09.jpg'], [], directories)).toBeNull()
  })

})
