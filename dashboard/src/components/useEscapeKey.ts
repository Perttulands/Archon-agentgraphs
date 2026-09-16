/* Escape closes the dialog that registers this hook, unless it is busy. */
import { useEffect, useRef } from 'react'

export function useEscapeKey(active: boolean, onEscape: () => void) {
  const latest = useRef(onEscape)
  useEffect(() => { latest.current = onEscape }, [onEscape])
  useEffect(() => {
    if (!active) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return
      event.preventDefault()
      latest.current()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [active])
}
