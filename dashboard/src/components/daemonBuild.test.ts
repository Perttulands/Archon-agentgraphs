import { afterEach, describe, expect, it, vi } from 'vitest'
import { buildLabel, fetchDaemonBuild } from './daemonBuild'

describe('the daemon build', () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it('reads the version and commit /healthz reports', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      expect(String(input)).toBe('/healthz')
      return Promise.resolve({
        ok: true, status: 200, headers: { get: () => null },
        json: () => Promise.resolve({ success: true, data: { status: 'ok', version: '0.9.0', commit: '0123456789abcdef0123456789abcdef01234567' } }),
      } as unknown as Response)
    }) as unknown as typeof fetch)
    const build = await fetchDaemonBuild()
    expect(build).toEqual({ version: '0.9.0', commit: '0123456789abcdef0123456789abcdef01234567' })
    expect(buildLabel(build!)).toBe('0.9.0 · 0123456')
  })

  it('labels a development build by what it reports', () => {
    expect(buildLabel({ version: 'dev', commit: 'unknown' })).toBe('dev · unknown')
  })
})
