import canonicalDefault from '../../../src/internal/api/theme_default.json'

// The interface's one theme, served by the host.
//
// CHROTE keeps a single active theme, authored on the host and served by
// GET /api/theme. The dashboard never picks one: there is no theme setting and
// no picker, so everything below is a read of what the server says, applied
// once. DEFAULT_THEME is the same JSON the server embeds, so first paint from
// theme-colors.css and a failed fetch both land on exactly these values.

/** Chrome colours. Every entry maps to one CSS custom property on :root. */
export interface ThemeUi {
  background: string
  surface: string
  surfaceRaised: string
  divider: string
  text: string
  textSecondary: string
  textDim: string
  accent: string
  error: string
}

/** The xterm palette. `ansi` is exactly 16 entries, black … brightWhite. */
export interface TerminalTheme {
  background: string
  foreground: string
  cursor: string
  selectionBackground: string
  ansi: string[]
}

export interface Theme {
  schema: 1
  name: string
  ui: ThemeUi
  terminal: TerminalTheme
  /**
   * The hues the Library's map draws its shelves in, taken in shelf order.
   * A theme authored before the map had colour carries none, and the built-in
   * palette answers instead.
   */
  shelves: string[]
  /** Per-Unix-user colours, indexed by the server's terminalUsers order. */
  identity: string[]
  /** Art file names served by GET /api/theme/art/{name}. */
  art: string[]
}

/**
 * The one font stack, for chrome and terminal alike. JetBrains Mono carries
 * text and terminal glyphs. Both weights are served from this origin.
 */
export const TERMINAL_FONT_FAMILY = '"JetBrains Mono", monospace'

export const DEFAULT_THEME = canonicalDefault as Theme

const COLOR_PATTERN = /^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$/
const ART_NAME_PATTERN = /^[A-Za-z0-9._-]+$/

function isColor(value: unknown): value is string {
  return typeof value === 'string' && COLOR_PATTERN.test(value)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

const UI_KEYS: (keyof ThemeUi)[] = [
  'background', 'surface', 'surfaceRaised', 'divider',
  'text', 'textSecondary', 'textDim', 'accent', 'error',
]

/**
 * A theme the dashboard can paint, or null. Anything short of the whole
 * contract is nothing: a half-applied palette is worse to look at than the
 * default, and the operator authored this file, so a partial one is a bug he
 * wants to see rather than a gap worth patching.
 */
export function parseTheme(value: unknown): Theme | null {
  if (!isRecord(value) || value.schema !== 1) return null
  if (typeof value.name !== 'string' || value.name === '') return null

  const ui = value.ui
  if (!isRecord(ui) || !UI_KEYS.every(key => isColor(ui[key]))) return null

  const terminal = value.terminal
  if (!isRecord(terminal)) return null
  if (!isColor(terminal.background) || !isColor(terminal.foreground)) return null
  if (!isColor(terminal.cursor) || !isColor(terminal.selectionBackground)) return null
  const ansi = terminal.ansi
  if (!Array.isArray(ansi) || ansi.length !== 16 || !ansi.every(isColor)) return null

  const identity = value.identity
  if (!Array.isArray(identity) || identity.length < 1 || !identity.every(isColor)) return null

  // A theme the operator authored before the map had colour names no shelf
  // hues. That is not a broken theme, so the built-in palette answers for it
  // rather than the map losing its colour.
  const shelves = value.shelves ?? DEFAULT_THEME.shelves
  if (!Array.isArray(shelves) || !shelves.every(isColor)) return null

  const art = value.art ?? []
  if (!Array.isArray(art) || !art.every(name => typeof name === 'string' && ART_NAME_PATTERN.test(name))) return null

  return {
    schema: 1,
    name: value.name,
    ui: UI_KEYS.reduce((acc, key) => {
      acc[key] = ui[key] as string
      return acc
    }, {} as ThemeUi),
    terminal: {
      background: terminal.background,
      foreground: terminal.foreground,
      cursor: terminal.cursor,
      selectionBackground: terminal.selectionBackground,
      ansi: [...ansi],
    },
    shelves: shelves.length > 0 ? [...shelves] as string[] : [...DEFAULT_THEME.shelves],
    identity: [...identity],
    art: [...art] as string[],
  }
}

/**
 * Read the host's theme, once. There is no retry and no poll: the theme is a
 * file on the host that changes when the operator applies a new one, and a
 * dashboard that missed it is one reload away from having it.
 */
export interface ThemeResult { theme: Theme; error?: string }
let themeRequest: Promise<ThemeResult> | undefined

export async function fetchTheme(): Promise<ThemeResult> {
  try {
    const response = await fetch('/api/theme', { signal: AbortSignal.timeout(10000) })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    const theme = parseTheme(await response.json())
    if (!theme) throw new Error('invalid schema-1 theme')
    return { theme }
  } catch (error) {
    const message = `Host theme unavailable; using the default theme. ${error instanceof Error ? error.message : String(error)}`
    console.warn(message)
    return { theme: DEFAULT_THEME, error: message }
  }
}

export function loadThemeOnce(): Promise<ThemeResult> {
  return themeRequest ??= fetchTheme()
}

function rgbChannels(color: string): string {
  const hex = color.slice(1, 7)
  return [0, 2, 4].map(offset => parseInt(hex.slice(offset, offset + 2), 16)).join(', ')
}

/**
 * Write the theme onto :root. These property names are the contract every
 * stylesheet in the dashboard is written against; theme-colors.css holds the
 * same set at DEFAULT_THEME's values so first paint matches.
 */
export function themeProperties(theme: Theme): Record<string, string> {
  const { ui, terminal } = theme
  return {
    '--background': ui.background,
    '--surface-primary': ui.surface,
    '--surface-secondary': ui.surfaceRaised,
    '--divider': ui.divider,
    '--text-primary': ui.text,
    '--text-secondary': ui.textSecondary,
    '--text-dim': ui.textDim,
    '--accent': ui.accent,
    '--accent-light': `color-mix(in srgb, ${ui.accent} 12%, transparent)`,
    '--accent-rgb': rgbChannels(ui.accent),
    '--color-error': ui.error,
    '--color-error-light': `color-mix(in srgb, ${ui.error} 15%, transparent)`,
    '--window-green': terminal.ansi[2],
    '--window-orange': terminal.ansi[3],
    '--bg1': ui.surface,
    '--bg2': ui.surfaceRaised,
    '--font-mono': TERMINAL_FONT_FAMILY,
    '--terminal-background': terminal.background,
    '--terminal-foreground': terminal.foreground,
    ...Object.fromEntries(terminal.ansi.map((color, index) => [`--ansi-${index}`, color])),
  }
}

export function themeCss(theme: Theme): string {
  return `:root{${Object.entries(themeProperties(theme)).map(([key, value]) => `${key}:${value};`).join('')}}`
}

export function applyTheme(theme: Theme, root: HTMLElement = document.documentElement): void {
  Object.entries(themeProperties(theme)).forEach(([key, value]) => root.style.setProperty(key, value))
}
