// Adapted from CHROTE dashboard/src/components/TerminalPool.tsx (CHROTE
// 355ace49) under form-o7p.13.1: one terminal per seat, outliving the view
// that shows it, created unconnected and dialled only when first shown, and
// woken when the operator comes back to the page.
//
// Departures from CHROTE, and why: CHROTE keeps one app-wide pool keyed by the
// sessions its workspace tiles are bound to. Archon's seat views are floating
// windows, and one xterm element can be on screen in only one place, so each
// window holds its own pool, keyed by the seat's terminal URL (its run and
// attempt), for as long as it is open. A seat that is no longer live in the
// window's last projection leaves the pool; a new attempt has a new URL and so
// a new terminal. Every terminal holds its seat's native grid, because a seat
// window is pinned and the view fits its font instead (CHROTE's Peek).
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createTerminalSession, type FixedGridBox, type TerminalConnectionState, type TerminalSession } from './terminalSession'
import { useTheme } from '../theme/ThemeContext'
import { TERMINAL_FONT_FAMILY } from '../theme/theme'
import type { CopyAnnouncer } from '../utils/clipboard'

/** The most a seat view's font may be; the fit shrinks it to its room, down to 11px. */
export const SEAT_TERMINAL_FONT_SIZE = 14

/** A seat a pool can hold: its terminal URL and its native grid. */
export interface PooledSeat {
  url: string
  columns: number
  rows: number
}

export interface SeatTerminalPool {
  terminals: ReadonlyMap<string, TerminalSession>
  states: ReadonlyMap<string, TerminalConnectionState>
  /** The box each terminal's grid took at its last font fit. */
  boxes: ReadonlyMap<string, FixedGridBox>
}

export function useSeatTerminalPool(seats: readonly PooledSeat[], announce: CopyAnnouncer): SeatTerminalPool {
  const { theme } = useTheme()
  const themeRef = useRef(theme)
  themeRef.current = theme
  const announceRef = useRef(announce)
  announceRef.current = announce
  const poolRef = useRef<Map<string, TerminalSession>>(new Map())
  const [terminals, setTerminals] = useState<ReadonlyMap<string, TerminalSession>>(poolRef.current)
  const [states, setStates] = useState<ReadonlyMap<string, TerminalConnectionState>>(new Map())
  const [boxes, setBoxes] = useState<ReadonlyMap<string, FixedGridBox>>(new Map())

  // Keyed on what identifies a terminal, so a fresh projection of the same
  // seats changes nothing.
  const key = seats.map(seat => `${seat.url} ${seat.columns}x${seat.rows}`).join('\n')
  const wanted = useMemo(() => new Map(seats.map(seat => [seat.url, seat])), [key]) // eslint-disable-line react-hooks/exhaustive-deps

  // Reconcile the pool with the seats. A terminal is created unconnected; it
  // dials only when a view first attaches it.
  useEffect(() => {
    const pool = poolRef.current
    let changed = false
    Array.from(pool.keys()).forEach(url => {
      if (wanted.has(url)) return
      pool.get(url)?.dispose()
      pool.delete(url)
      changed = true
    })
    wanted.forEach((seat, url) => {
      if (pool.has(url)) return
      pool.set(url, createTerminalSession({
        url,
        fontSize: SEAT_TERMINAL_FONT_SIZE,
        hideScrollbar: true,
        terminalTheme: themeRef.current.terminal,
        fontFamily: TERMINAL_FONT_FAMILY,
        // The seat's native grid from the first handshake on. The room is the
        // view's, given once it is on screen.
        fixedGrid: { cols: seat.columns, rows: seat.rows, room: { width: Infinity, height: Infinity } },
        onFixedGridFit: box => setBoxes(previous => new Map(previous).set(url, box)),
        onStateChange: state => setStates(previous => new Map(previous).set(url, state)),
        announce: (message, severity) => announceRef.current(message, severity),
      }))
      changed = true
    })
    if (!changed) return
    setTerminals(new Map(pool))
    const prune = <T,>(previous: ReadonlyMap<string, T>) => {
      const next = new Map(previous)
      Array.from(next.keys()).forEach(url => { if (!pool.has(url)) next.delete(url) })
      return next
    }
    setStates(prune)
    setBoxes(prune)
  }, [wanted])

  useEffect(() => () => {
    poolRef.current.forEach(terminal => terminal.dispose())
    poolRef.current.clear()
  }, [])

  useEffect(() => {
    terminals.forEach(terminal => terminal.applyAppearance(theme.terminal, TERMINAL_FONT_FAMILY))
  }, [theme, terminals])

  // A page hidden across an archond restart comes back with no connections.
  // That is answered when the operator returns, because that is when he is
  // looking. redialIfDropped() and fit() ignore terminals that are detached or
  // not on screen, and neither retries.
  useEffect(() => {
    const wake = () => {
      if (document.visibilityState !== 'visible') return
      terminals.forEach(terminal => {
        terminal.redialIfDropped()
        terminal.fit()
      })
    }
    document.addEventListener('visibilitychange', wake)
    return () => document.removeEventListener('visibilitychange', wake)
  }, [terminals])

  return useMemo(() => ({ terminals, states, boxes }), [terminals, states, boxes])
}

/** A copy's confirmation or failure, shown for a few seconds where the window says so. */
export function useNotice(ms = 4000): [string, CopyAnnouncer] {
  const [notice, setNotice] = useState('')
  const timer = useRef<ReturnType<typeof setTimeout>>()
  const announce = useCallback<CopyAnnouncer>(message => {
    clearTimeout(timer.current)
    setNotice(message)
    timer.current = setTimeout(() => setNotice(''), ms)
  }, [ms])
  useEffect(() => () => clearTimeout(timer.current), [])
  return [notice, announce]
}
