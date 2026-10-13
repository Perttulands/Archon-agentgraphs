import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { applyTheme, DEFAULT_THEME, loadThemeOnce, type Theme, type ThemeResult } from './theme'
import { readStyle, themeForStyle, writeStyle, type StyleId } from './styles'

interface ThemeValue extends ThemeResult {
  /** The look chosen on this device; `host` draws the host's theme. */
  style: StyleId
  setStyle: (style: StyleId) => void
}

const ThemeContext = createContext<ThemeValue>({ theme: DEFAULT_THEME, style: 'host', setStyle: () => {} })

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [host, setHost] = useState<{ theme: Theme; error?: string; loaded: boolean }>({ theme: DEFAULT_THEME, loaded: false })
  const [style, setStyleState] = useState<StyleId>(readStyle)
  useEffect(() => {
    let disposed = false
    void loadThemeOnce().then(next => {
      if (!disposed) setHost({ ...next, loaded: true })
    })
    return () => { disposed = true }
  }, [])
  const theme = themeForStyle(style, host.theme)
  // First paint already carries the default theme; write again once the host's arrives or a style is chosen.
  useEffect(() => {
    if (host.loaded || style !== 'host') applyTheme(theme)
    document.documentElement.dataset.style = style
  }, [theme, host.loaded, style])
  const setStyle = useCallback((next: StyleId) => { writeStyle(next); setStyleState(next) }, [])
  const value = useMemo(() => ({ theme, error: host.error, style, setStyle }), [theme, host.error, style, setStyle])
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export const useTheme = () => useContext(ThemeContext)
