// Adapted from CHROTE's ttydProtocol: keep output framing and flow control,
// expose only observer disposal. There is no input, resize or claim operation.
export interface OutputSink {
  write(data: Uint8Array, onDrained?: () => void): void
}

export function observeTerminal(
  url: string,
  grid: { columns: number; rows: number },
  sink: OutputSink,
  onOpen: () => void,
  onClose: (code: number) => void,
): { close(): void } {
  const socket = new WebSocket(url, ['tty'])
  socket.binaryType = 'arraybuffer'
  const encoder = new TextEncoder()
  let closed = false
  const send = (data: string) => {
    if (!closed && socket.readyState === WebSocket.OPEN) socket.send(encoder.encode(data))
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
    if (!frame.length || frame[0] !== 0x30) return
    const data = frame.subarray(1)
    written += data.length
    if (written <= 100_000) { sink.write(data); return }
    written = 0
    pending += 1
    if (pending > 10) send('2')
    sink.write(data, () => {
      pending = Math.max(0, pending - 1)
      if (pending < 4) send('3')
    })
  }
  return { close() {
    closed = true
    socket.onopen = socket.onmessage = socket.onclose = socket.onerror = null
    socket.close()
  } }
}
