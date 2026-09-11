import { describe, expect, it, vi, afterEach } from 'vitest'
import canonicalDefault from '../../../src/internal/api/theme_default.json'
import { DEFAULT_THEME, applyTheme, fetchTheme, parseTheme, themeCss, themeProperties } from './theme'

afterEach(() => vi.unstubAllGlobals())

describe('host theme', () => {
  it('uses the server default for first paint, runtime fallback and every semantic alias', () => {
    expect(DEFAULT_THEME).toEqual(canonicalDefault)
    expect(parseTheme(canonicalDefault)).toEqual(DEFAULT_THEME)
    const root = document.createElement('div')
    applyTheme(DEFAULT_THEME, root)
    for (const [key, value] of Object.entries(themeProperties(DEFAULT_THEME))) {
      expect(themeCss(DEFAULT_THEME)).toContain(`${key}:${value};`)
      expect(root.style.getPropertyValue(key)).toBe(value)
    }
    expect(root.style.getPropertyValue('--window-green')).toBe(DEFAULT_THEME.terminal.ansi[2])
    expect(root.style.getPropertyValue('--window-orange')).toBe(DEFAULT_THEME.terminal.ansi[3])
    expect(root.style.getPropertyValue('--bg1')).toBe(DEFAULT_THEME.ui.surface)
    expect(root.style.getPropertyValue('--bg2')).toBe(DEFAULT_THEME.ui.surfaceRaised)
  })

  it('accepts the raw host response and applies a complete custom palette', async () => {
    const host = { ...DEFAULT_THEME, ui: { ...DEFAULT_THEME.ui, accent: '#abcdef' } }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => host }))
    expect(await fetchTheme()).toEqual({ theme: host })
    expect(themeProperties(host)['--accent-rgb']).toBe('171, 205, 239')
  })

  it.each([
    { ok: false, status: 500 },
    { ok: true, json: async () => ({ ...DEFAULT_THEME, terminal: { ansi: [] } }) },
    { ok: true, json: async () => ({ success: true, data: DEFAULT_THEME }) },
  ])('reports failure and retains the whole fallback for invalid responses', async response => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response))
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const result = await fetchTheme()
    expect(result.theme).toEqual(DEFAULT_THEME)
    expect(result.error).toContain('using the default theme')
    warning.mockRestore()
  })
})
