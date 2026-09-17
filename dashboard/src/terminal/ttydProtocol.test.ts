import { afterEach, describe, expect, it, vi } from 'vitest'
import { connectSeatTerminal } from './ttydProtocol'

class Socket {
  static OPEN = 1
  static last: Socket
  readyState = 1
  binaryType = ''
  sent: Uint8Array[] = []
  onopen: (() => void) | null = null
  onclose: ((event: { code: number }) => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: { data: ArrayBuffer }) => void) | null = null
  constructor(readonly url: string, readonly protocols: string[]) { Socket.last = this }
  send(data: Uint8Array) { this.sent.push(data) }
  close() {}
}
afterEach(() => vi.unstubAllGlobals())
const decode = (bytes: Uint8Array) => new TextDecoder().decode(bytes)

describe('seat tty transport', () => {
  it('sends the grid handshake and bounded flow control, and nothing after disposal', () => {
    vi.stubGlobal('WebSocket', Socket)
    const drained: (() => void)[] = []
    const write = vi.fn((_data, done?: () => void) => { if (done) drained.push(done) })
    const connection = connectSeatTerminal('ws://localhost/seat', { columns: 123, rows: 41 }, { write }, vi.fn(), vi.fn())
    const socket = Socket.last
    socket.onopen!()
    expect(socket.protocols).toEqual(['tty'])
    expect(JSON.parse(decode(socket.sent[0]))).toEqual({ columns: 123, rows: 41 })
    for (let i = 0; i < 12; i++) {
      const frame = new Uint8Array(100002); frame[0] = 0x30
      socket.onmessage!({ data: frame.buffer })
    }
    expect(socket.sent.slice(1).map(decode)).toContain('2')
    drained.splice(0, 10).forEach(done => done())
    expect(socket.sent.slice(1).map(decode)).toContain('3')
    const count = socket.sent.length
    connection.close()
    drained.forEach(done => done())
    connection.sendInput('late')
    connection.sendResize(90, 30)
    expect(socket.sent).toHaveLength(count)
  })

  it('sends typed input as 0 frames, binary input raw, and resize as a 1 frame', () => {
    vi.stubGlobal('WebSocket', Socket)
    const connection = connectSeatTerminal('ws://localhost/seat', { columns: 80, rows: 24 }, { write: vi.fn() }, vi.fn(), vi.fn())
    const socket = Socket.last
    socket.readyState = 0
    connection.sendInput('before open')
    expect(socket.sent).toHaveLength(0)
    socket.readyState = 1
    socket.onopen!()
    connection.sendInput('yes, ship it\r')
    connection.sendInput(Uint8Array.of(0x1b, 0x5b, 0x41))
    connection.sendResize(132, 40)
    expect(socket.sent.slice(1).map(decode)).toEqual(['0yes, ship it\r', '0\x1b[A', '1{"columns":132,"rows":40}'])
  })

  it('passes terminal end, shutdown and rejection codes without reconnecting', () => {
    vi.stubGlobal('WebSocket', Socket)
    const onClose = vi.fn()
    connectSeatTerminal('ws://localhost/seat', { columns: 80, rows: 24 }, { write: vi.fn() }, vi.fn(), onClose)
    for (const code of [1000, 1001, 1008, 1006]) Socket.last.onclose!({ code })
    expect(onClose.mock.calls).toEqual([[1000], [1001], [1008], [1006]])
    expect(Socket.last.sent).toHaveLength(0)
  })
})
