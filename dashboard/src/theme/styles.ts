import { DEFAULT_THEME, type Theme } from './theme'

// The looks the operator can choose between on this device. `host` is the
// theme the host serves and stays the default; the others are built in.
// Chosen in the app bar and remembered per browser (archon-fh5c.8).

export type StyleId = 'host' | 'paper'

const STORAGE_KEY = 'archon.style.v1'

/** Paper: a light look. Ink on warm white, with colours dark enough to read on it. */
export const PAPER_THEME: Theme = {
  ...DEFAULT_THEME,
  name: 'paper',
  ui: {
    background: '#f3f1ec',
    surface: '#fbfaf7',
    surfaceRaised: '#eeebe4',
    divider: '#cbc5b8',
    text: '#1d1c1a',
    textSecondary: '#55524c',
    textDim: '#827d74',
    accent: '#3257d6',
    error: '#cf3030',
  },
  terminal: {
    background: '#fbfaf7',
    foreground: '#1d1c1a',
    cursor: '#1d1c1a',
    selectionBackground: '#3257d640',
    ansi: [
      '#1d1c1a', '#cf3030', '#2c8a3f', '#b27408', '#3257d6', '#8a3fc4', '#12808a', '#55524c',
      '#827d74', '#e04545', '#3aa350', '#c98a12', '#4a70ec', '#a35ad8', '#1a9aa6', '#1d1c1a',
    ],
  },
}

export const STYLES: ReadonlyArray<{ id: StyleId; label: string }> = [
  { id: 'host', label: 'Console' },
  { id: 'paper', label: 'Paper' },
]

export function readStyle(): StyleId {
  try {
    return window.localStorage.getItem(STORAGE_KEY) === 'paper' ? 'paper' : 'host'
  } catch {
    return 'host'
  }
}

export function writeStyle(style: StyleId): void {
  try {
    if (style === 'host') window.localStorage.removeItem(STORAGE_KEY)
    else window.localStorage.setItem(STORAGE_KEY, style)
  } catch {
    // Private mode must not make the choice unusable for this visit.
  }
}

/** The theme a style draws: the host's own, or a built-in one. */
export function themeForStyle(style: StyleId, host: Theme): Theme {
  return style === 'paper' ? PAPER_THEME : host
}
