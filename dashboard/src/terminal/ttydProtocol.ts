// Adapted from CHROTE's ttydProtocol (dashboard/src/terminal/ttydProtocol.ts):
// output framing and flow control, plus the operator's input and resize frames
// (ADR-0019: typing into a seat is never blocked). Frames are binary with a
// one-byte ASCII prefix. The client sends `0` input, `1` resize as JSON and
// `2`/`3` flow control, after an unprefixed JSON grid as the handshake; the
// server sends `0` output.
export interface OutputSink {
  write(data: Uint8Array, onDrained?: () => void): void
}

export interface SeatConnection {
  /** Strings are sent as UTF-8; byte arrays (xterm's binary input) are sent raw. */
  sendInput(data: string | Uint8Array): void
  sendResize(columns: number, rows: number): void
  /** Close without reporting the close; the caller already knows. */
  close(): void
}

const CLIENT_INPUT = 0x30 // '0'
const CLIENT_RESIZE = '1'
const CLIENT_PAUSE = '2'
const CLIENT_RESUME = '3'
const SERVER_OUTPUT = 0x30 // '0'

export function connectSeatTerminal(
  url: string,
  grid: { columns: number; rows: number },
  sink: OutputSink,
  onOpen: () => void,
  onClose: (code: number) => void,
): SeatConnection {
  const socket = new WebSocket(url, ['tty'])
  socket.binaryType = 'arraybuffer'
  const encoder = new TextEncoder()
  let closed = false
  const open = () => !closed && socket.readyState === WebSocket.OPEN
  const send = (data: string) => {
    if (open()) socket.send(encoder.encode(data))
  }
  let written = 0
  let pending = 0
  socket.onopen = () => {
    send(JSON.stringify(grid))
    onOpen()
  }
  socket.onerror = () => socket.close()
  socket.onclose = event => { if (!closed) onClose(event.code) }
  socket.onmessage = (event: MessageEvent<ArrayBuffer>) => {
    if (closed || !(event.data instanceof ArrayBuffer)) return
    const frame = new Uint8Array(event.data)
    if (!frame.length || frame[0] !== SERVER_OUTPUT) return
    const data = frame.subarray(1)
    written += data.length
    if (written <= 100_000) { sink.write(data); return }
    written = 0
    pending += 1
    if (pending > 10) send(CLIENT_PAUSE)
    sink.write(data, () => {
      pending = Math.max(0, pending - 1)
      if (pending < 4) send(CLIENT_RESUME)
    })
  }
  return {
    sendInput(data) {
      if (!open()) return
      const bytes = typeof data === 'string' ? encoder.encode(data) : data
      const frame = new Uint8Array(bytes.length + 1)
      frame[0] = CLIENT_INPUT
      frame.set(bytes, 1)
      socket.send(frame)
    },
    sendResize(columns, rows) {
      send(CLIENT_RESIZE + JSON.stringify({ columns, rows }))
    },
    close() {
      closed = true
      socket.onopen = socket.onmessage = socket.onclose = socket.onerror = null
      socket.close()
    },
  }
}
