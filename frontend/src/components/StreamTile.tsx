import { useEffect, useRef, useState } from 'react'
import type { StreamEntry } from '../hooks/useStreamList'
import { maskCredentials } from '../lib/rtsp'
import { StreamPlayer, type PlayerState } from '../player/StreamPlayer'
import { CloseIcon, FullscreenIcon, PauseIcon, PlayIcon } from './icons'

interface Props {
  stream: StreamEntry
  onPausedChange: (key: string, paused: boolean) => void
  onRemove: (key: string) => void
}

type Tally = 'live' | 'standby' | 'off' | 'fault'

/** One monitor on the wall: the video, a status overlay and a label strip with controls. */
export function StreamTile({ stream, onPausedChange, onRemove }: Props) {
  const tileRef = useRef<HTMLElement>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const playerRef = useRef<StreamPlayer | null>(null)
  const [state, setState] = useState<PlayerState>({ kind: 'connecting' })
  const [resolution, setResolution] = useState<string | null>(null)

  useEffect(() => {
    const player = new StreamPlayer(videoRef.current!, stream.url, setState)
    playerRef.current = player
    return () => {
      player.destroy()
      playerRef.current = null
    }
  }, [stream.url])

  useEffect(() => {
    const player = playerRef.current
    if (!player) return
    if (stream.paused) {
      player.stop()
    } else {
      player.start()
    }
  }, [stream.paused, stream.url])

  const toggleFullscreen = () => {
    if (document.fullscreenElement) {
      void document.exitFullscreen()
    } else {
      void tileRef.current?.requestFullscreen()
    }
  }

  const { tally, label } = describe(state, stream.paused)

  return (
    <article ref={tileRef} className="tile" aria-label={stream.name}>
      <div className="tile__screen" onDoubleClick={toggleFullscreen}>
        <video
          ref={videoRef}
          className="tile__video"
          muted
          playsInline
          onResize={(e) => {
            const v = e.currentTarget
            setResolution(v.videoWidth ? `${v.videoWidth}×${v.videoHeight}` : null)
          }}
        />
        <Overlay state={state} paused={stream.paused} />
      </div>

      <footer className="tile__strip">
        <span className={`tally tally--${tally}`} aria-hidden="true" />
        <div className="tile__identity">
          <h2 className="tile__name">{stream.name}</h2>
          <p className="tile__url" title={maskCredentials(stream.url)}>
            {maskCredentials(stream.url)}
          </p>
        </div>
        <p className="tile__status" aria-live="polite">
          {label}
          {state.kind === 'live' && !stream.paused && resolution && (
            <span className="tile__resolution">{resolution}</span>
          )}
        </p>
        <div className="tile__controls">
          <button
            type="button"
            className="icon-button"
            onClick={() => onPausedChange(stream.key, !stream.paused)}
            aria-label={stream.paused ? `Play ${stream.name}` : `Pause ${stream.name}`}
            title={stream.paused ? 'Play' : 'Pause'}
          >
            {stream.paused ? <PlayIcon /> : <PauseIcon />}
          </button>
          {document.fullscreenEnabled && (
            <button
              type="button"
              className="icon-button"
              onClick={toggleFullscreen}
              aria-label={`Show ${stream.name} full screen`}
              title="Full screen"
            >
              <FullscreenIcon />
            </button>
          )}
          <button
            type="button"
            className="icon-button icon-button--remove"
            onClick={() => onRemove(stream.key)}
            aria-label={`Remove ${stream.name}`}
            title="Remove"
          >
            <CloseIcon />
          </button>
        </div>
      </footer>
    </article>
  )
}

function describe(state: PlayerState, paused: boolean): { tally: Tally; label: string } {
  if (paused) return { tally: 'off', label: 'Paused' }
  switch (state.kind) {
    case 'live':
      return { tally: 'live', label: 'Live' }
    case 'connecting':
      return { tally: 'standby', label: 'Connecting' }
    case 'retrying':
      return { tally: 'standby', label: 'No signal' }
    case 'reconnecting':
      return { tally: 'standby', label: 'Reconnecting' }
    case 'failed':
      return { tally: 'fault', label: 'Can’t play' }
  }
}

function Overlay({ state, paused }: { state: PlayerState; paused: boolean }) {

  if (paused) {
    return (
      <div className="overlay overlay--quiet">
        <p className="overlay__title">Paused</p>
        <p className="overlay__detail">Press play to pick up the live picture again.</p>
      </div>
    )
  }
  switch (state.kind) {
    case 'live':
      return null
    case 'connecting':
      return (
        <div className="overlay">
          <p className="overlay__title">Connecting…</p>
        </div>
      )
    case 'retrying':
      return (
        <div className="overlay overlay--problem" role="status">
          <p className="overlay__title">The camera isn’t sending video</p>
          <p className="overlay__detail">{state.message}</p>
          <RetryCountdown key={state.retryAt} retryAt={state.retryAt} />
        </div>
      )
    case 'reconnecting':
      return (
        <div className="overlay overlay--problem" role="status">
          <p className="overlay__title">Reconnecting</p>
          <p className="overlay__detail">{state.message}</p>
          <RetryCountdown key={state.retryAt} retryAt={state.retryAt} />
        </div>
      )
    case 'failed':
      return (
        <div className="overlay overlay--problem" role="alert">
          <p className="overlay__title">This stream can’t be played</p>
          <p className="overlay__detail">{state.message}</p>
        </div>
      )
  }
}

/** "Trying again in N s", ticking down to retryAt (epoch ms). Keyed by retryAt, so each retry starts fresh. */
function RetryCountdown({ retryAt }: { retryAt: number }) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 500)
    return () => window.clearInterval(timer)
  }, [])
  const seconds = Math.ceil((retryAt - now) / 1000)
  return <p className="overlay__retry">{seconds > 0 ? `Trying again in ${seconds} s` : 'Trying again…'}</p>
}
