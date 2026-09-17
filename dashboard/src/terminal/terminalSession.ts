// Adapted from CHROTE's terminalSession (dashboard/src/terminal/terminalSession.ts):
// an xterm grid fitted to its window, whose keystrokes and size go to the seat.
// The operator may type into any live seat at any time (ADR-0019).
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import { fontsReady } from '../theme/fonts'
import { TERMINAL_FONT_FAMILY, type TerminalTheme } from '../theme/theme'
import { connectSeatTerminal, type SeatConnection } from './ttydProtocol'
import '@xterm/xterm/css/xterm.css'

export type ConnectionState = 'connecting' | 'open' | 'ended' | 'disconnected' | 'unavailable'

function xtermTheme(theme: TerminalTheme) {
  const [black, red, green, yellow, blue, magenta, cyan, white,
    brightBlack, brightRed, brightGreen, brightYellow, brightBlue, brightMagenta, brightCyan, brightWhite] = theme.ansi
  return { ...theme, black, red, green, yellow, blue, magenta, cyan, white,
    brightBlack, brightRed, brightGreen, brightYellow, brightBlue, brightMagenta, brightCyan, brightWhite }
}

// A hidden or collapsed host measures nothing, and fitting it would send a bogus size to the seat.
const MIN_VISIBLE_PX = 10

export function createTerminalSession(options: {
  url: string; columns: number; rows: number; theme: TerminalTheme
  onStateChange: (state: ConnectionState) => void
}) {
  const terminal = new Terminal({
    cols: options.columns, rows: options.rows, fontSize: 13, fontFamily: TERMINAL_FONT_FAMILY,
    theme: xtermTheme(options.theme), cursorBlink: false, scrollback: 10000,
    allowProposedApi: true,
  })
  const fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  terminal.loadAddon(new Unicode11Addon())
  terminal.unicode.activeVersion = '11'
  const element = document.createElement('div')
  element.className = 'terminal-surface'
  let disposed = false
  let opened = false
  let connection: SeatConnection | undefined
  let connected = false

  // The grid the seat was last told: the handshake's, then each resize sent.
  // A fit made while the socket is still connecting is sent once it opens.
  let sent = { columns: options.columns, rows: options.rows }
  const sendSize = () => {
    if (!connection || !connected || (sent.columns === terminal.cols && sent.rows === terminal.rows)) return
    sent = { columns: terminal.cols, rows: terminal.rows }
    connection.sendResize(terminal.cols, terminal.rows)
  }

  terminal.onData(data => connection?.sendInput(data))
  terminal.onBinary(data => connection?.sendInput(Uint8Array.from(data, char => char.charCodeAt(0) & 0xff)))
  terminal.onResize(sendSize)

  const fit = () => {
    if (!opened || disposed || element.offsetWidth < MIN_VISIBLE_PX || element.offsetHeight < MIN_VISIBLE_PX) return
    fitAddon.fit()
  }

  return {
    attach(host: HTMLElement) {
      host.appendChild(element)
      options.onStateChange('connecting')
      void fontsReady().then(() => {
        if (disposed) return
        terminal.open(element)
        opened = true
        connection = connectSeatTerminal(options.url, { columns: options.columns, rows: options.rows }, terminal,
          () => {
            connected = true
            options.onStateChange('open')
            // The window decides the grid once the seat is attached. The renderer
            // may measure its cell only on the next frame, so fit again then.
            fit()
            sendSize()
            requestAnimationFrame(() => { fit(); sendSize() })
          },
          code => { connected = false; options.onStateChange(code === 1000 ? 'ended' : code === 1008 ? 'unavailable' : 'disconnected') })
      }, () => { if (!disposed) options.onStateChange('unavailable') })
    },
    /** Fit the grid to the window; the new size goes to the seat. */
    fit,
    focus() { terminal.focus() },
    applyTheme(theme: TerminalTheme) { terminal.options.theme = xtermTheme(theme) },
    scrollToBottom() { terminal.scrollToBottom() },
    scrollLines(lines: number) { terminal.scrollLines(lines) },
    selection() { return terminal.getSelection() },
    dispose() {
      disposed = true
      connection?.close()
      terminal.dispose()
      element.remove()
    },
  }
}

export type TerminalSession = ReturnType<typeof createTerminalSession>
