import { describe, expect, it } from 'vitest'
import { referencedFileRequest } from './fileWindowModel'

describe('file requests', () => {
  it('reads a referenced file under the daemon roots and resolves its links beside it', async () => {
    const calls: string[] = []
    const original = globalThis.fetch
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      calls.push(String(input))
      return { ok: true, status: 200, headers: { get: () => null }, json: async () => ({ success: true, data: { file: { path: '/srv/project/docs/rubric.md', name: 'rubric.md', size: 9, modifiedAt: '', kind: 'markdown', text: { text: '# Rubric', bytes: 8 } } } }) } as unknown as Response
    }) as typeof fetch
    try {
      const request = referencedFileRequest('/srv/project/docs/rubric.md', 'Review gate')
      expect(request).toMatchObject({ id: 'file:/srv/project/docs/rubric.md', name: 'rubric.md', context: 'Review gate · /srv/project/docs/rubric.md', path: '/srv/project/docs/rubric.md' })
      expect(request.rawUrl).toBe('/api/formations/files/raw?path=%2Fsrv%2Fproject%2Fdocs%2Frubric.md')
      expect(await request.load()).toEqual({ kind: 'markdown', text: { text: '# Rubric', bytes: 8 } })
      expect(calls).toEqual(['/api/formations/files/preview?path=%2Fsrv%2Fproject%2Fdocs%2Frubric.md'])
      expect(request.link?.('srv/project/docs/scale.md').id).toBe('file:/srv/project/docs/scale.md')
      expect(referencedFileRequest('docs/rubric.md').link?.('docs/scale.md').id).toBe('file:docs/scale.md')
    } finally {
      globalThis.fetch = original
    }
  })
})
