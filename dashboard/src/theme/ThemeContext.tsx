import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { applyTheme, DEFAULT_THEME, loadThemeOnce, type ThemeResult } from './theme'

const ThemeContext = createContext<ThemeResult>({ theme: DEFAULT_THEME })

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [result, setResult] = useState<ThemeResult>({ theme: DEFAULT_THEME })
  useEffect(() => {
    let disposed = false
    void loadThemeOnce().then(next => {
      if (disposed) return
      applyTheme(next.theme)
      setResult(next)
    })
    return () => { disposed = true }
  }, [])
  return <ThemeContext.Provider value={result}>{children}</ThemeContext.Provider>
}

export const useTheme = () => useContext(ThemeContext)
