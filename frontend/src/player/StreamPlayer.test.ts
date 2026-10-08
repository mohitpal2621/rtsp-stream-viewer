import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { StreamPlayer } from './StreamPlayer'

class FakeSocket {
  static opened: FakeSocket[] = []
  readonly url: string
  binaryType = 'blob'
  closed = false
  onmessage: ((event: { data: unknown }) => void) | null = null
  onclose: ((event: { code: number }) => void) | null = null

  constructor(url: string) {
    this.url = url
    FakeSocket.opened.push(this)
  }

  close() {
    this.closed = true
  }
}

type Listener = () => void

class FakeSourceBuffer {
  appended: number[] = [] // byte lengths, to tell segments apart
  updating = false
  mode = 'segments'
  buffered = { length: 0, start: () => 0, end: () => 0 }
  listeners: Record<string, Listener[]> = {}

  addEventListener(type: string, fn: Listener) {
    ;(this.listeners[type] ??= []).push(fn)
  }

  appendBuffer(data: ArrayBuffer) {
    this.appended.push(data.byteLength)
  }

  finishAppend() {
    this.listeners.updateend?.forEach((fn) => fn())
  }
}

class FakeMediaSource {
  static last: FakeMediaSource
  static isTypeSupported() {
    return true
  }
  readyState = 'closed'
  buffer = new FakeSourceBuffer()
  listeners: Record<string, Listener[]> = {}

  constructor() {
    FakeMediaSource.last = this
  }

  addEventListener(type: string, fn: Listener) {
    ;(this.listeners[type] ??= []).push(fn)
  }

  addSourceBuffer() {
    return this.buffer
  }

  open() {
    this.readyState = 'open'
    this.listeners.sourceopen?.forEach((fn) => fn())
  }
}

function fakeVideo() {
  return {
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    removeAttribute: vi.fn(),
    load: vi.fn(),
    pause: vi.fn(),
  } as unknown as HTMLVideoElement
}

describe('StreamPlayer', () => {
  let resolvers: ((res: Response) => void)[]

  beforeEach(() => {
    FakeSocket.opened = []
    resolvers = []
    vi.stubGlobal('window', {
      location: { href: 'http://localhost/' },
      setTimeout,
      clearTimeout,
      MediaSource: FakeMediaSource,
    })
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:fake')
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    vi.stubGlobal('WebSocket', FakeSocket)
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>((resolve) => resolvers.push(resolve))),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  const registered = () => new Response(JSON.stringify({ id: 'abc' }), { status: 200 })

  it('opens one socket when paused and played again while registering', async () => {
    const player = new StreamPlayer(fakeVideo(), 'rtsp://cam/live', () => {})
    player.start()
    player.stop()
    player.start()
    expect(resolvers).toHaveLength(2)

    // Both registrations come back after the second start().
    resolvers.forEach((resolve) => resolve(registered()))
    await vi.waitFor(() => expect(FakeSocket.opened).toHaveLength(1))
    await new Promise((r) => setTimeout(r, 10))
    expect(FakeSocket.opened).toHaveLength(1)
    expect(FakeSocket.opened[0].url).toBe('ws://localhost/api/streams/abc/ws')
  })

  it('opens no socket when stopped while registering', async () => {
    const player = new StreamPlayer(fakeVideo(), 'rtsp://cam/live', () => {})
    player.start()
    player.stop()
    resolvers[0](registered())
    await new Promise((r) => setTimeout(r, 10))
    expect(FakeSocket.opened).toHaveLength(0)
  })

  it('keeps only the newest fragments while the MediaSource is not open yet', async () => {
    const player = new StreamPlayer(fakeVideo(), 'rtsp://cam/live', () => {})
    player.start()
    resolvers[0](registered())
    await vi.waitFor(() => expect(FakeSocket.opened).toHaveLength(1))
    const socket = FakeSocket.opened[0]

    // Init metadata and segment, then 20 one-keyframe fragments of sizes 1..20,
    // all arriving before the browser opens the MediaSource (a hidden tab).
    socket.onmessage!({ data: JSON.stringify({ type: 'init', mime: 'video/mp4; codecs="avc1.4d401f"' }) })
    socket.onmessage!({ data: new ArrayBuffer(100) })
    for (let size = 1; size <= 20; size++) socket.onmessage!({ data: new ArrayBuffer(size) })

    const source = FakeMediaSource.last
    expect(source.buffer.appended).toEqual([])
    source.open()
    for (let i = 0; i < 10; i++) source.buffer.finishAppend()

    expect(source.buffer.appended).toEqual([100, 17, 18, 19, 20])
  })
})
