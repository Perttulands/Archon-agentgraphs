import { beforeEach, describe, expect, it } from 'vitest'
import { DEFAULT_THEME } from './theme'
import { PAPER_THEME, readStyle, themeForStyle, writeStyle } from './styles'

describe('cockpit styles', () => {
  beforeEach(() => window.localStorage.clear())

  it('keeps the host theme until the operator chooses a style, and remembers the choice', () => {
    expect(readStyle()).toBe('host')
    expect(themeForStyle(readStyle(), DEFAULT_THEME)).toBe(DEFAULT_THEME)
    writeStyle('paper')
    expect(readStyle()).toBe('paper')
    expect(themeForStyle(readStyle(), DEFAULT_THEME)).toBe(PAPER_THEME)
  })

  it('falls back to the host theme on a stored style it does not know', () => {
    window.localStorage.setItem('archon.style.v1', 'neon')
    expect(readStyle()).toBe('host')
  })
})
