// One in-page terminal: an xterm.js instance plus its seat connection.
//
// Ported from CHROTE dashboard/src/terminal/terminalSession.ts (CHROTE 355ace49,
// which carries chrote-8eyu, chrote-wshh, chrote-te47 and chrote-0k1g),
// re-ported under form-o7p.13.1. The pooling contract (attach, detach,
// reconnect, redialIfDropped), the fixed-grid font fit Peek uses, the
// first-visible-layout activation, the font-ready open, Unicode 11 widths and
// the settled-selection copy are CHROTE's.
//
// Departures from CHROTE, and why:
// - No Bead, path or URL links yet: those are form-o7p.13.2, which ports
//   CHROTE's beadLinks, pathLinks and WebLinksAddon onto this session.
// - No leader-chord key handler: Archon has no chord registry, so every key a
//   focused terminal receives belongs to the seat (ADR-0019: the operator may
//   always type into a seat).
// - No `claim()`: a seat window is pinned at 160x48 (tmux `window-size
//   manual`) and the seat terminal declines CHROTE's claim frame, so there is
//   no sizing seat to take. Seat views always hold a fixed grid, the seat's
//   native one, which is what CHROTE's Peek does for any session.
// - No scrollbar setting: Archon has no settings, and the gutter is dead under
//   tmux, so callers pass `hideScrollbar: true`, CHROTE's default.
//
// The element is owned here, not by React, so a terminal survives being
// detached from the document: a seat tab switched away from in Peek. Detaching
// a plain element neither reloads it nor touches the WebSocket.

import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import { connectTtyd, type TtydConnection } from './ttydProtocol'
import { copyAndAnnounce, type CopyAnnouncer } from '../utils/clipboard'
import type { TerminalTheme } from '../theme/theme'
import '@xterm/xterm/css/xterm.css'
import './terminal.css'

/**
 * `closed` and `dropped` are both "no connection", and the difference is the
 * whole point: `closed` is the seat terminal ending or refusing this
 * terminal, which is what an ended or replaced seat looks like, while `dropped`
 * is the connection being lost with the seat still on the other end, which is
 * what every archond restart does to every open seat terminal.
 */
export type TerminalConnectionState = 'idle' | 'connecting' | 'open' | 'closed' | 'dropped'

export interface TerminalSession {
  /**
   * Attach into a live container, connecting on first attach. An ended seat
   * passes `connect: false` so its last frame can be shown without dialling a
   * session that is no longer there.
   */
  attach(container: HTMLElement, options?: { connect?: boolean }): void
  /** Detach from the document, keeping the connection and the rendered frame. */
  detach(): void
  /**
   * Fit the terminal: the grid to its container, or with a fixed grid, the
   * font to the grid's room. A no-op while detached or hidden.
   */
  fit(): void
  /** Focus the terminal, or, before it has opened, as soon as it does. */
  focus(): void
  /** Put the viewport at the newest output, where an answer arrives. */
  scrollToBottom(): void
  /** The operator's font size; with a fixed grid, the most the fit may use. */
  setFontSize(fontSize: number): void
  /**
   * Hold the grid at this size and fit the font to its room instead, or, with
   * null, go back to fitting the grid. Peek holds the tmux window's own grid,
   * so what it sends down its connection is the size the window already is.
   */
  setFixedGrid(grid: FixedGrid | null): void
  setScrollbarHidden(hidden: boolean): void
  /** Drop the connection and open a new one, without reloading anything. */
  reconnect(): void
  /**
   * Dial again if the last connection was lost rather than ended, and the
   * terminal is on screen. Called when the operator puts the terminal in front
   * of himself; each such moment is worth one attempt and no more. Nothing here
   * retries on its own, and a dial that fails leaves the window's own Refresh
   * as the way back. The dial takes nothing from anyone: it attaches
   * alongside whoever else is watching, without the sizing seat.
   */
  redialIfDropped(): void
  /**
   * Repaint in the host's theme. The palette arrives after the terminal does —
   * GET /api/theme is a fetch, terminals are created from bindings already in
   * local storage — so every live terminal takes it when it lands.
   */
  applyAppearance(terminalTheme: TerminalTheme, fontFamily: string): void
  dispose(): void
}

export interface FixedGrid {
  cols: number
  rows: number
  /**
   * The most the terminal may take, in CSS pixels. The font is fitted to this
   * and never to the container, so a container shrunk to the answer cannot
   * change the answer.
   */
  room: { width: number; height: number }
}

/**
 * A fixed grid after its font fit: the box, in CSS pixels, that holds exactly
 * that grid at the font it was fitted to, and the grid, room and font ceiling
 * it was fitted for. Past the readability floor the box is larger than the
 * room, and what does not fit is the caller's to clip.
 */
export interface FixedGridBox extends FixedGrid {
  maxFontSize: number
  width: number
  height: number
}

export interface TerminalSessionOptions {
  url: string
  fontSize: number
  hideScrollbar: boolean
  /** The theme's terminal object: background, foreground, cursor, selection, 16 ansi. */
  terminalTheme: TerminalTheme
  fontFamily: string
  /** A grid to hold from the start, so the first handshake already carries it. */
  fixedGrid?: FixedGrid | null
  /** Told the box a fixed grid needs, after every fit of one. */
  onFixedGridFit?: (box: FixedGridBox) => void
  onStateChange?: (state: TerminalConnectionState) => void
  /** Where a painted selection reports whether it reached the clipboard. */
  announce: CopyAnnouncer
}

// xterm names the 16 ansi entries; the theme carries them as an ordered array,
// because that is the order every palette in the world is written in.
function xtermTheme(theme: TerminalTheme) {
  const [
    black, red, green, yellow, blue, magenta, cyan, white,
    brightBlack, brightRed, brightGreen, brightYellow, brightBlue, brightMagenta, brightCyan, brightWhite,
  ] = theme.ansi
  return {
    background: theme.background,
    foreground: theme.foreground,
    cursor: theme.cursor,
    selectionBackground: theme.selectionBackground,
    black, red, green, yellow, blue, magenta, cyan, white,
    brightBlack, brightRed, brightGreen, brightYellow, brightBlue, brightMagenta, brightCyan, brightWhite,
  }
}

// A grid is only meaningful once the container has real layout. Detached and
// display:none terminals report zero, and fitting them would push a bogus size
// to the shared tmux window.
const MIN_VISIBLE_PX = 10

// The font fit moves in half pixels, and stops where reading stops: a grid
// that does not fit at the floor is shown clipped rather than as dots.
const FONT_FIT_STEP = 0.5
const FONT_FIT_FLOOR = 11
// What the fit addon reserves for xterm's scrollbar when no overview ruler
// says otherwise (its ViewportConstants.DEFAULT_SCROLL_BAR_WIDTH).
const FIT_ADDON_SCROLLBAR_PX = 14

/**
 * The largest font, no larger than the operator's, at which a fixed grid fits
 * its room, as `fits` answers for a candidate size, and never below 11px or
 * the operator's own size if that is smaller. A grid that fits at one size
 * fits at every smaller one, so it is searched by halving rather than tried
 * size by size: every try is a real measurement of the cell.
 */
export function fitFontSize(maxFontSize: number, fits: (fontSize: number) => boolean): number {
  if (maxFontSize <= FONT_FIT_FLOOR || fits(maxFontSize)) return maxFontSize
  let low = FONT_FIT_FLOOR
  let high = maxFontSize
  while (high - low > FONT_FIT_STEP) {
    const middle = Math.round(low + high) / 2
    if (fits(middle)) low = middle
    else high = middle
  }
  return low
}

export function createTerminalSession(options: TerminalSessionOptions): TerminalSession {
  const element = document.createElement('div')
  element.className = 'terminal-surface'

  let fontSize = options.fontSize
  let fixedGrid = options.fixedGrid ?? null

  const terminal = new Terminal({
    fontSize,
    ...(fixedGrid ? { cols: fixedGrid.cols, rows: fixedGrid.rows } : {}),
    fontFamily: options.fontFamily,
    theme: xtermTheme(options.terminalTheme),
    // xterm files its Unicode version handling as proposed API, so the width
    // table below cannot be swapped without this. ttyd's client set it too.
    allowProposedApi: true,
  })
  const fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)

  // tmux lays the pane out with its own width table, so the browser has to
  // measure characters the same way or every character after an emoji sits a
  // cell left of where tmux put it — closing box-drawing that does not close,
  // and status lines that overlap, on the agent sessions the operator watches
  // most. Measured on a scratch tmux 3.6a socket: printing U+1F680 at column 0
  // leaves tmux's cursor at column 2. xterm defaults to a Unicode 6 provider,
  // under which every emoji is one column. ttyd's client ran Unicode 11, which
  // is why this only started drifting when ttyd left.
  terminal.loadAddon(new Unicode11Addon())
  terminal.unicode.activeVersion = '11'

  let connection: TtydConnection | null = null
  let opened = false
  let disposed = false
  let attachment = 0
  let fontReady: Promise<FontFace[]> | null = null
  let pendingAttach: (() => void) | null = null
  // The last connection was lost rather than ended, so dialling again reaches
  // the same terminal instead of taking a session from whoever holds it.
  let dropped = false
  // Focus was asked for before the terminal had a textarea to take it.
  let focusOnOpen = false

  const setState = (state: TerminalConnectionState) => {
    if (!disposed) options.onStateChange?.(state)
  }

  terminal.onData(data => connection?.sendInput(data))
  terminal.onBinary(data => connection?.sendInput(Uint8Array.from(data, char => char.charCodeAt(0) & 0xff)))
  terminal.onResize(({ cols, rows }) => connection?.sendResize(cols, rows))

  // Painting a selection puts it on the clipboard. This was ttyd's client
  // doing `document.execCommand('copy')` on every selection change, not
  // anything xterm does, so it left with the iframe; the operator asked for it
  // back, including the part where painting overwrites the system clipboard
  // without asking. Under tmux mouse mode the gesture that paints is Shift and
  // left-drag, which is what xterm's force-selection escape hatch listens for.
  //
  // The copy waits for the drag to settle rather than following
  // onSelectionChange, which fires once per mousemove. The mouseup that ends a
  // drag can land anywhere, so it is watched on the document, and only while a
  // press that began in this terminal is in flight. The press-time selection is
  // remembered so that a plain click on a terminal that still holds an older
  // selection does not silently put it back over whatever the operator copied
  // since. Reading it needs the capture phase: xterm stops propagation of the
  // very mousedown that forces a selection under mouse mode.
  let selectionAtPress = ''
  const copySettledSelection = () => {
    document.removeEventListener('mouseup', copySettledSelection)
    const painted = terminal.getSelection()
    if (painted && painted !== selectionAtPress) void copyAndAnnounce(painted, 'selection', options.announce)
  }
  const watchSelectionDrag = () => {
    selectionAtPress = terminal.getSelection()
    document.addEventListener('mouseup', copySettledSelection)
  }
  element.addEventListener('mousedown', watchSelectionDrag, true)

  const isMeasurable = () => element.offsetWidth >= MIN_VISIBLE_PX && element.offsetHeight >= MIN_VISIBLE_PX

  // A fixed grid keeps its columns and rows whatever the room, so the font is
  // what moves: each candidate size is set for real and the grid it draws
  // measured against the room, because only xterm knows what its cell costs.
  // The container is never asked, so the answer does not depend on where it
  // is shown. The renderer paints once, at the size that is left.
  const fit = () => {
    if (!isMeasurable()) return
    // A font may finish loading after the tile was hidden. Its first visible
    // fit completes that attachment at real dimensions, without a timer.
    if (pendingAttach) {
      const finish = pendingAttach
      pendingAttach = null
      finish()
      return
    }
    if (!opened) return
    const grid = fixedGrid
    if (!grid) {
      fitAddon.fit()
      return
    }
    terminal.options.fontSize = fitFontSize(fontSize, candidate => {
      terminal.options.fontSize = candidate
      const box = fixedGridBox()
      return box !== null && box.width <= grid.room.width && box.height <= grid.room.height
    })
    const box = fixedGridBox()
    if (box) options.onFixedGridFit?.({ ...grid, maxFontSize: fontSize, ...box })
  }

  // The box the grid takes at the font now set, counted as the fit addon
  // counts it: the grid as drawn, the terminal element's padding, and the
  // scrollbar width it reserves whenever there is scrollback.
  const fixedGridBox = () => {
    const screen = terminal.element?.querySelector('.xterm-screen')?.getBoundingClientRect()
    if (!terminal.element || !screen || screen.width < MIN_VISIBLE_PX || screen.height < MIN_VISIBLE_PX) return null
    const style = window.getComputedStyle(terminal.element)
    const padding = (side: string) => parseInt(style.getPropertyValue(`padding-${side}`)) || 0
    const scrollbar = terminal.options.scrollback === 0
      ? 0
      : (terminal.options.overviewRuler?.width || FIT_ADDON_SCROLLBAR_PX)
    return {
      width: screen.width + padding('left') + padding('right') + scrollbar,
      height: screen.height + padding('top') + padding('bottom'),
    }
  }

  // xterm measures its cell when open() runs. A swapped web font arriving
  // later leaves that measurement stale until a resize, so the first fit uses
  // the fallback cell and the resize itself only then teaches xterm the real
  // one. Open after the terminal font is available instead. The browser's
  // FontFaceSet is the completion event; no timer or repeated fit is needed.
  const afterFontReady = (ready: () => void) => {
    const fonts = document.fonts
    const font = `${terminal.options.fontSize}px ${terminal.options.fontFamily}`
    if (!fonts || fonts.check(font)) {
      ready()
      return
    }
    fontReady ??= fonts.load(font)
    void fontReady.then(ready, ready)
  }

  const connect = () => {
    dropped = false
    setState('connecting')
    connection = connectTtyd(options.url, { cols: terminal.cols, rows: terminal.rows }, terminal, {
      onOpen: () => setState('open'),
      onClose: ({ terminalEnded }) => {
        connection = null
        dropped = !terminalEnded
        setState(terminalEnded ? 'closed' : 'dropped')
      },
    })
  }

  const setScrollbarHidden = (hidden: boolean) => {
    element.classList.toggle('terminal-surface--no-scrollbar', hidden)
  }
  setScrollbarHidden(options.hideScrollbar)

  return {
    attach(container, attachOptions) {
      if (disposed) return
      const thisAttachment = ++attachment
      container.appendChild(element)
      const finishAttach = () => {
        if (disposed || thisAttachment !== attachment || !element.parentElement) return
        if (!isMeasurable()) {
          pendingAttach = finishAttach
          return
        }
        pendingAttach = null
        if (!opened) {
          terminal.open(element)
          opened = true
        }
        fit()
        if (focusOnOpen) {
          focusOnOpen = false
          terminal.focus()
        }
        if (!connection && attachOptions?.connect !== false) connect()
      }
      if (opened) finishAttach()
      else afterFontReady(finishAttach)
    },
    detach() {
      attachment += 1
      pendingAttach = null
      element.remove()
    },
    fit,
    focus() {
      if (opened) terminal.focus()
      else focusOnOpen = true
    },
    scrollToBottom() {
      terminal.scrollToBottom()
    },
    setFontSize(size) {
      if (fontSize === size) return
      fontSize = size
      if (!fixedGrid) terminal.options.fontSize = size
      fit()
    },
    setFixedGrid(grid) {
      fixedGrid = grid
      if (grid && (terminal.cols !== grid.cols || terminal.rows !== grid.rows)) terminal.resize(grid.cols, grid.rows)
      if (!grid) terminal.options.fontSize = fontSize
      fit()
    },
    setScrollbarHidden,
    reconnect() {
      if (disposed) return
      connection?.close()
      connection = null
      terminal.reset()
      connect()
    },
    redialIfDropped() {
      // Off screen is not a moment worth an attempt: a terminal nobody is
      // looking at has nothing to show for the connection it would open.
      if (disposed || connection || !dropped || !isMeasurable()) return
      connect()
    },
    applyAppearance(terminalTheme, fontFamily) {
      if (disposed) return
      terminal.options.theme = xtermTheme(terminalTheme)
      terminal.options.fontFamily = fontFamily
      fit()
    },
    dispose() {
      disposed = true
      pendingAttach = null
      element.removeEventListener('mousedown', watchSelectionDrag, true)
      document.removeEventListener('mouseup', copySettledSelection)
      connection?.close()
      connection = null
      terminal.dispose()
      element.remove()
    },
  }
}
