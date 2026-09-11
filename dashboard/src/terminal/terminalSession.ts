// Reduced from CHROTE's terminalSession. The observer has a fixed native grid;
// mounting, moving and closing its renderer cannot affect the runtime seat.
import { Terminal } from '@xterm/xterm'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import { fontsReady } from '../theme/fonts'
import { TERMINAL_FONT_FAMILY, type TerminalTheme } from '../theme/theme'
import { observeTerminal } from './ttydProtocol'
import '@xterm/xterm/css/xterm.css'

export type ConnectionState = 'connecting' | 'open' | 'ended' | 'disconnected' | 'unavailable'

function xtermTheme(theme: TerminalTheme) {
  const [black, red, green, yellow, blue, magenta, cyan, white,
    brightBlack, brightRed, brightGreen, brightYellow, brightBlue, brightMagenta, brightCyan, brightWhite] = theme.ansi
  return { ...theme, black, red, green, yellow, blue, magenta, cyan, white,
    brightBlack, brightRed, brightGreen, brightYellow, brightBlue, brightMagenta, brightCyan, brightWhite }
}

export function createTerminalSession(options: {
  url: string; columns: number; rows: number; theme: TerminalTheme
  onStateChange: (state: ConnectionState) => void
}) {
  const terminal = new Terminal({
    cols: options.columns, rows: options.rows, fontSize: 13, fontFamily: TERMINAL_FONT_FAMILY,
    theme: xtermTheme(options.theme), disableStdin: true, cursorBlink: false, scrollback: 10000,
    allowProposedApi: true,
  })
  terminal.loadAddon(new Unicode11Addon())
  terminal.unicode.activeVersion = '11'
  const element = document.createElement('div')
  element.className = 'terminal-surface'
  let disposed = false
  let connection: ReturnType<typeof observeTerminal> | undefined
  return {
    attach(host: HTMLElement) {
      host.appendChild(element)
      options.onStateChange('connecting')
      void fontsReady().then(() => {
        if (disposed) return
        terminal.open(element)
        // xterm measures the native grid; the surrounding panel scrolls it.
        const screen = element.querySelector<HTMLElement>('.xterm-screen')
        if (screen) element.style.width = `${screen.offsetWidth + 16}px`
        connection = observeTerminal(options.url, { columns: options.columns, rows: options.rows }, terminal,
          () => options.onStateChange('open'),
          code => options.onStateChange(code === 1000 ? 'ended' : code === 1008 ? 'unavailable' : 'disconnected'))
      }, () => { if (!disposed) options.onStateChange('unavailable') })
    },
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
