import { ApiError, registerStream, streamSocketUrl } from '../lib/api'
import { clampFragment, planPlayback, type Range } from './liveEdge'

export type PlayerState =
  | { kind: 'connecting' }
  | { kind: 'live' }
  /** The backend can't pull the stream and will try again at retryAt (epoch ms). */
  | { kind: 'retrying'; message: string; retryAt: number }
  /** We can't reach the backend and will try again at retryAt (epoch ms). */
  | { kind: 'offline'; message: string; retryAt: number }
  /** Retrying won't help, e.g. the browser can't decode the stream. */
  | { kind: 'failed'; message: string }

type ServerMessage =
  | { type: 'status'; state: 'connecting' | 'live' | 'retrying'; error?: string; retryIn?: number }
  | { type: 'init'; mime: string }

/** Keep at most this much already-played video in the buffer, in seconds. */
const KEEP_BEHIND = 6
const MAX_RECONNECT_DELAY = 10
/** WebSocket close code the backend uses to ask for a reconnect. */
const TRY_AGAIN_LATER = 1013

type MediaSourceCtor = typeof MediaSource

function mediaSourceImpl(): MediaSourceCtor | undefined {
  // Safari on iPhone only has ManagedMediaSource (iOS 17.1+).
  const w = window as unknown as { MediaSource?: MediaSourceCtor; ManagedMediaSource?: MediaSourceCtor }
  return w.MediaSource ?? w.ManagedMediaSource
}

/**
 * Plays one stream in a <video> element. It registers the RTSP URL with the
 * backend, opens the stream's WebSocket and appends the fragmented MP4 it
 * receives to a Media Source Extensions buffer. It reconnects on its own and
 * reports what is happening through onState.
 */
export class StreamPlayer {
  private readonly video: HTMLVideoElement
  private readonly rtspUrl: string
  private readonly onState: (state: PlayerState) => void

  private ws: WebSocket | null = null
  private mediaSource: MediaSource | null = null
  private objectUrl: string | null = null
  private buffer: SourceBuffer | null = null
  private queue: ArrayBuffer[] = []
  private running = false
  private reconnectTimer: number | undefined
  private failures = 0
  private live = false
  private lastEnd = 0
  private fragment = 1 // typical fragment duration in seconds, learned as we go

  constructor(video: HTMLVideoElement, rtspUrl: string, onState: (state: PlayerState) => void) {
    this.video = video
    this.rtspUrl = rtspUrl
    this.onState = onState
    video.addEventListener('error', this.onVideoError)
  }

  /** Connects and starts playing. */
  start(): void {
    if (this.running) return
    this.running = true
    void this.connect()
  }

  /**
   * Disconnects. The last frame stays on screen; start() resumes at the
   * live edge.
   */
  stop(): void {
    this.running = false
    window.clearTimeout(this.reconnectTimer)
    this.closeSocket()
    this.queue = []
    this.video.pause()
  }

  /** Disconnects and releases the media pipeline. */
  destroy(): void {
    this.stop()
    this.detachMedia()
    this.video.removeEventListener('error', this.onVideoError)
  }

  private readonly onVideoError = () => {
    if (this.running && this.mediaSource) this.recover('The video decoder reported an error.')
  }

  private async connect(): Promise<void> {
    this.setState({ kind: 'connecting' })
    let id: string
    try {
      id = await registerStream(this.rtspUrl)
    } catch (err) {
      if (!this.running) return
      if (err instanceof ApiError && err.status === 400) {
        this.setState({ kind: 'failed', message: err.message })
        return
      }
      this.scheduleReconnect(err instanceof ApiError ? err.message : 'Can’t reach the stream server.')
      return
    }
    if (!this.running) return

    const ws = new WebSocket(streamSocketUrl(id))
    ws.binaryType = 'arraybuffer'
    this.ws = ws
    ws.onmessage = (event) => {
      if (this.ws !== ws) return
      if (typeof event.data === 'string') {
        this.handleControl(JSON.parse(event.data) as ServerMessage)
      } else {
        this.handleMedia(event.data as ArrayBuffer)
      }
    }
    ws.onclose = (event) => {
      if (this.ws !== ws) return
      this.ws = null
      if (!this.running) return
      if (event.code === TRY_AGAIN_LATER) this.failures = 0
      this.scheduleReconnect('Lost the connection to the stream server.')
    }
  }

  private handleControl(msg: ServerMessage): void {
    if (msg.type === 'init') {
      this.attachMedia(msg.mime)
      return
    }
    switch (msg.state) {
      case 'connecting':
        this.live = false
        this.setState({ kind: 'connecting' })
        break
      case 'retrying':
        this.live = false
        this.setState({
          kind: 'retrying',
          message: msg.error ?? 'The stream stopped.',
          retryAt: Date.now() + (msg.retryIn ?? 1) * 1000,
        })
        break
      case 'live':
        // Reported once video is actually playing; see onAppended.
        this.failures = 0
        break
    }
  }

  private handleMedia(data: ArrayBuffer): void {
    if (!this.mediaSource) return // no init segment yet
    this.queue.push(data)
    this.pump()
  }

  /** Starts a fresh MediaSource for a new init segment. */
  private attachMedia(mime: string): void {
    const MS = mediaSourceImpl()
    if (!MS) {
      this.failHard('This browser doesn’t support Media Source Extensions, which the player needs.')
      return
    }
    if (!MS.isTypeSupported(mime)) {
      this.failHard(`This browser can’t decode this stream (${mime}).`)
      return
    }

    this.detachMedia()
    this.queue = []
    this.live = false
    this.lastEnd = 0
    const ms = new MS()
    this.mediaSource = ms
    // Required for ManagedMediaSource, harmless elsewhere.
    this.video.disableRemotePlayback = true
    this.objectUrl = URL.createObjectURL(ms)
    this.video.src = this.objectUrl
    ms.addEventListener('sourceopen', () => {
      if (this.mediaSource !== ms) return
      let buffer: SourceBuffer
      try {
        buffer = ms.addSourceBuffer(mime)
      } catch {
        this.failHard(`This browser can’t decode this stream (${mime}).`)
        return
      }
      buffer.mode = 'segments'
      buffer.addEventListener('updateend', () => this.onAppended())
      buffer.addEventListener('error', () => this.recover('The video decoder reported an error.'))
      this.buffer = buffer
      this.pump()
    })
  }

  private detachMedia(): void {
    this.buffer = null
    this.mediaSource = null
    if (this.objectUrl) {
      URL.revokeObjectURL(this.objectUrl)
      this.objectUrl = null
      this.video.removeAttribute('src')
      this.video.load()
    }
  }

  /** Feeds the next queued segment to the SourceBuffer, trimming old video first if needed. */
  private pump(): void {
    const buffer = this.buffer
    if (!buffer || buffer.updating || this.mediaSource?.readyState !== 'open') return

    const ranges = buffer.buffered
    if (ranges.length > 0) {
      const removeEnd = this.video.currentTime - KEEP_BEHIND
      if (removeEnd - ranges.start(0) > 2) {
        buffer.remove(0, removeEnd) // triggers updateend, which pumps again
        return
      }
    }

    const next = this.queue.shift()
    if (!next) return
    try {
      buffer.appendBuffer(next)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'QuotaExceededError') {
        // The buffer is full, most likely because playback stalled. Jump to
        // the newest video and free what is behind it. If there is nothing
        // to free, drop this fragment: the next one starts on a keyframe.
        this.jumpToLive()
        const cut = this.video.currentTime - 1
        if (ranges.length > 0 && cut > ranges.start(0)) {
          this.queue.unshift(next)
          buffer.remove(0, cut)
        }
        return
      }
      this.recover('The stream sent data the browser couldn’t use.')
    }
  }

  private onAppended(): void {
    const ranges = this.bufferedRanges()
    if (ranges.length > 0) {
      const end = ranges[ranges.length - 1].end
      if (this.lastEnd > 0 && end > this.lastEnd) {
        // Smooth the observed fragment length to plan latency around it.
        this.fragment = 0.8 * this.fragment + 0.2 * clampFragment(end - this.lastEnd)
      }
      this.lastEnd = end
      this.keepUp(ranges)
      if (!this.live) {
        this.live = true
        void this.video.play().catch(() => {})
        this.setState({ kind: 'live' })
      }
    }
    this.pump()
  }

  private keepUp(ranges: Range[]): void {
    const action = planPlayback(ranges, this.video.currentTime, this.fragment, this.video.playbackRate)
    if (action.type === 'seek') this.video.currentTime = action.to
    if (action.type === 'rate') this.video.playbackRate = action.rate
  }

  private jumpToLive(): void {
    const ranges = this.bufferedRanges()
    if (ranges.length > 0) this.video.currentTime = ranges[ranges.length - 1].end - 0.1
  }

  private bufferedRanges(): Range[] {
    const out: Range[] = []
    const buffered = this.buffer?.buffered
    if (!buffered) return out
    for (let i = 0; i < buffered.length; i++) {
      out.push({ start: buffered.start(i), end: buffered.end(i) })
    }
    return out
  }

  /** Starts over with a fresh connection, which brings a new init segment. */
  private recover(message: string): void {
    this.closeSocket()
    this.detachMedia()
    this.scheduleReconnect(message)
  }

  private failHard(message: string): void {
    this.running = false
    this.closeSocket()
    this.setState({ kind: 'failed', message })
  }

  private scheduleReconnect(message: string): void {
    this.live = false
    const delay = Math.min(MAX_RECONNECT_DELAY, 2 ** this.failures)
    this.failures++
    this.setState({ kind: 'offline', message, retryAt: Date.now() + delay * 1000 })
    window.clearTimeout(this.reconnectTimer)
    this.reconnectTimer = window.setTimeout(() => {
      if (this.running) void this.connect()
    }, delay * 1000)
  }

  private closeSocket(): void {
    const ws = this.ws
    this.ws = null
    ws?.close()
  }

  private setState(state: PlayerState): void {
    if (this.running || state.kind === 'failed') this.onState(state)
  }
}
