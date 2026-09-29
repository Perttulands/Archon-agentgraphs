// Adapted from CHROTE dashboard/src/components/TerminalPool.test.tsx (355ace49)
// under form-o7p.13.1: a window's pool of seat terminals instead of CHROTE's
// app-wide pool of bound sessions.
import { act, render } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useSeatTerminalPool, type PooledSeat, type SeatTerminalPool } from './seatTerminalPool'
import { DEFAULT_THEME, TERMINAL_FONT_FAMILY } from '../theme/theme'
import type { FixedGrid, FixedGridBox, TerminalConnectionState } from './terminalSession'

interface CreatedSession {
  url: string
  fontSize: number
  hideScrollbar: boolean
  fixedGrid: FixedGrid | null | undefined
  terminalBackground: string
  fontFamily: string
  disposed: boolean
  redials: number
  report: (state: TerminalConnectionState) => void
  fitted: (box: FixedGridBox) => void
}

const created = vi.hoisted(() => [] as CreatedSession[])

vi.mock('./terminalSession', () => ({
  createTerminalSession: (options: {
    url: string
    fontSize: number
    hideScrollbar: boolean
    fixedGrid?: FixedGrid | null
    terminalTheme: { background: string }
    fontFamily: string
    onStateChange?: (state: TerminalConnectionState) => void
    onFixedGridFit?: (box: FixedGridBox) => void
  }) => {
    const record: CreatedSession = {
      url: options.url,
      fontSize: options.fontSize,
      hideScrollbar: options.hideScrollbar,
      fixedGrid: options.fixedGrid,
      terminalBackground: options.terminalTheme.background,
      fontFamily: options.fontFamily,
      disposed: false,
      redials: 0,
      report: state => options.onStateChange?.(state),
      fitted: box => options.onFixedGridFit?.(box),
    }
    created.push(record)
    return {
      attach: vi.fn(),
      detach: vi.fn(),
      fit: vi.fn(),
      focus: vi.fn(),
      redialIfDropped: () => { record.redials += 1 },
      applyAppearance: (theme: { background: string }, fontFamily: string) => {
        record.terminalBackground = theme.background
        record.fontFamily = fontFamily
      },
      dispose: () => { record.disposed = true },
    }
  },
}))

// The host's theme lands after the window opens.
const themeState = vi.hoisted(() => ({ current: null as unknown }))
vi.mock('../theme/ThemeContext', () => ({ useTheme: () => ({ theme: themeState.current }) }))

const seat = (seq: number): PooledSeat => ({ url: `ws://host/api/formations/runs/run_1/seats/${seq}/terminal`, columns: 160, rows: 49 })

let pool: SeatTerminalPool

function Probe({ seats }: { seats: PooledSeat[] }) {
  pool = useSeatTerminalPool(seats, () => {})
  return null
}

describe('seat terminal pool', () => {
  beforeEach(() => {
    created.length = 0
    themeState.current = DEFAULT_THEME
  })

  it('holds one unconnected terminal per live seat, at the seat grid, in the one font stack', () => {
    render(<Probe seats={[seat(7), seat(8)]} />)

    expect(Array.from(pool.terminals.keys())).toEqual([seat(7).url, seat(8).url])
    expect(created.map(session => session.url)).toEqual([seat(7).url, seat(8).url])
    expect(created[0].fixedGrid).toEqual({ cols: 160, rows: 49, room: { width: Infinity, height: Infinity } })
    expect(created.map(session => [session.fontSize, session.hideScrollbar, session.fontFamily]))
      .toEqual([[14, true, TERMINAL_FONT_FAMILY], [14, true, TERMINAL_FONT_FAMILY]])
  })

  it('keeps a terminal across a new projection of the same seats, and disposes one whose seat left', () => {
    const { rerender } = render(<Probe seats={[seat(7), seat(8)]} />)
    const [seven, eight] = created

    rerender(<Probe seats={[{ ...seat(7) }]} />)

    expect(seven.disposed).toBe(false)
    expect(eight.disposed).toBe(true)
    expect(created).toHaveLength(2)
    expect(Array.from(pool.terminals.keys())).toEqual([seat(7).url])
  })

  it('gives a new attempt a new terminal', () => {
    const { rerender } = render(<Probe seats={[seat(8)]} />)

    rerender(<Probe seats={[seat(28)]} />)

    expect(created.map(session => [session.url, session.disposed])).toEqual([[seat(8).url, true], [seat(28).url, false]])
  })

  it('hands a theme that lands after the window opened to terminals that already exist', () => {
    const { rerender } = render(<Probe seats={[seat(7)]} />)

    themeState.current = { ...DEFAULT_THEME, terminal: { ...DEFAULT_THEME.terminal, background: '#123456' } }
    rerender(<Probe seats={[seat(7)]} />)

    expect(created.map(session => session.terminalBackground)).toEqual(['#123456'])
    expect(created.every(session => !session.disposed)).toBe(true)
  })

  it('publishes each terminal connection state and fitted box', () => {
    render(<Probe seats={[seat(7)]} />)
    expect(pool.states.get(seat(7).url)).toBeUndefined()

    act(() => { created[0].report('open') })
    expect(pool.states.get(seat(7).url)).toBe('open')
    act(() => { created[0].report('dropped') })
    expect(pool.states.get(seat(7).url)).toBe('dropped')

    const box = { cols: 160, rows: 49, room: { width: 1200, height: 800 }, maxFontSize: 14, width: 1150, height: 760 }
    act(() => { created[0].fitted(box) })
    expect(pool.boxes.get(seat(7).url)).toEqual(box)
  })

  it('offers every terminal one redial when the operator comes back to the page, and none while it is hidden', () => {
    render(<Probe seats={[seat(7), seat(8)]} />)
    const visibility = vi.spyOn(document, 'visibilityState', 'get')

    visibility.mockReturnValue('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(created.map(session => session.redials)).toEqual([0, 0])

    visibility.mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(created.map(session => session.redials)).toEqual([1, 1])
    visibility.mockRestore()
  })

  it('disposes every terminal when the window closes', () => {
    const { unmount } = render(<Probe seats={[seat(7), seat(8)]} />)

    unmount()

    expect(created.every(session => session.disposed)).toBe(true)
  })
})
