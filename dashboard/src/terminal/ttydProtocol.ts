// Ported from CHROTE dashboard/src/terminal/ttydProtocol.ts (CHROTE 355ace49,
// re-ported under archon-o7p.13.1): ttyd's browser protocol as CHROTE speaks it.
//
// Frames are binary with a one-byte ASCII command prefix. The client sends `0`
// input, `1` resize as JSON, `2`/`3` flow control and `4` claim, after an
// unprefixed JSON object as the opening handshake; the seat terminal attaches
// no pty until that handshake arrives. The server sends `0` output.
//
// Departures from CHROTE: the socket URL is the run seat's own terminal route
// (seatApi.seatSocketUrl), not CHROTE's session-name `terminalSocketUrl` with a
// viewing mode, because a seat is addressed by its run and attempt and proven
// by its recorded tmux identity on the server. Archon's server declines the
// `4` claim (seat windows are pinned), so nothing in the cockpit sends one;
// the frame stays here so the protocol matches CHROTE's byte for byte.

const CLIENT_INPUT = 0x30 // '0'
const CLIENT_RESIZE = '1'
const CLIENT_PAUSE = '2'
const CLIENT_RESUME = '3'
// CHROTE's own addition to the protocol: take the tmux sizing seat for this
// session. ttyd never defined a `4` frame.
const CLIENT_CLAIM = '4'

const SERVER_OUTPUT = 0x30 // '0'

// ttyd's own flow-control thresholds. Above the byte limit, output is written
// with a drain callback and the server is told to stop reading the pty until the
// renderer catches up, so a firehose cannot grow an unbounded write queue.
const FLOW_BYTE_LIMIT = 100_000
const FLOW_HIGH_WATER = 10
const FLOW_LOW_WATER = 4

export interface TerminalOutputSink {
  /** Write pty output. `onDrained` fires once the renderer has consumed it. */
  write(data: Uint8Array, onDrained?: () => void): void
}

export interface TtydConnection {
  /** Strings are sent as UTF-8; byte arrays (xterm's `onBinary`) are sent raw. */
  sendInput(data: string | Uint8Array): void
  sendResize(cols: number, rows: number): void
  /**
   * Ask to become this session's one sizing client. Archon's seat terminal
   * declines it: a seat window is pinned.
   */
  claimSizing(): void
  /** Close without reporting `onClose`; the caller already knows. */
  close(): void
}

// The close code the seat terminal sends when the terminal ended or the attach
// was refused. Anything else reaching the browser (1001 from a daemon shutting
// down, 1006 from a lost connection) is a connection nobody closed for good.
const NORMAL_CLOSURE = 1000

export interface TtydClose {
  /**
   * The server said the terminal ended, by closing with its own 1000 close
   * frame. Every other close is the connection being lost with the seat still on
   * the other end: the daemon restarting, the network dropping, the device
   * sleeping. The seat is untouched by those, so they are worth dialling again
   * and this is not.
   */
  terminalEnded: boolean
}

export interface TtydConnectionEvents {
  onOpen?: () => void
  onClose?: (close: TtydClose) => void
}

export function connectTtyd(
  url: string,
  initialSize: { cols: number; rows: number },
  sink: TerminalOutputSink,
  events: TtydConnectionEvents = {},
): TtydConnection {
  const socket = new WebSocket(url, ['tty'])
  socket.binaryType = 'arraybuffer'
  const encoder = new TextEncoder()
  const send = (payload: string) => {
    if (socket.readyState === WebSocket.OPEN) socket.send(encoder.encode(payload))
  }

  let writtenSinceLimit = 0
  let pendingWrites = 0

  socket.onopen = () => {
    // The token is ttyd's; neither CHROTE nor Archon checks it.
    send(JSON.stringify({ AuthToken: '', columns: initialSize.cols, rows: initialSize.rows }))
    events.onOpen?.()
  }
  socket.onerror = () => socket.close()
  socket.onclose = event => events.onClose?.({ terminalEnded: event.code === NORMAL_CLOSURE })
  socket.onmessage = (event: MessageEvent<ArrayBuffer>) => {
    const frame = new Uint8Array(event.data)
    // Window title and preferences are not consumed.
    if (frame.length === 0 || frame[0] !== SERVER_OUTPUT) return
    const data = frame.subarray(1)

    writtenSinceLimit += data.length
    if (writtenSinceLimit <= FLOW_BYTE_LIMIT) {
      sink.write(data)
      return
    }
    writtenSinceLimit = 0
    pendingWrites += 1
    sink.write(data, () => {
      pendingWrites = Math.max(pendingWrites - 1, 0)
      if (pendingWrites < FLOW_LOW_WATER) send(CLIENT_RESUME)
    })
    if (pendingWrites > FLOW_HIGH_WATER) send(CLIENT_PAUSE)
  }

  return {
    sendInput: data => {
      if (socket.readyState !== WebSocket.OPEN) return
      const bytes = typeof data === 'string' ? encoder.encode(data) : data
      const frame = new Uint8Array(bytes.length + 1)
      frame[0] = CLIENT_INPUT
      frame.set(bytes, 1)
      socket.send(frame)
    },
    sendResize: (cols, rows) => send(CLIENT_RESIZE + JSON.stringify({ columns: cols, rows })),
    claimSizing: () => send(CLIENT_CLAIM),
    close: () => {
      socket.onclose = null
      socket.close()
    },
  }
}
