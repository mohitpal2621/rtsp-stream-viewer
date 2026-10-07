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
    vi.stubGlobal('window', { location: { href: 'http://localhost/' }, setTimeout, clearTimeout })
    vi.stubGlobal('WebSocket', FakeSocket)
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise<Response>((resolve) => resolvers.push(resolve))),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
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
})
