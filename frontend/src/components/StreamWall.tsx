import type { CSSProperties } from 'react'
import type { StreamEntry } from '../hooks/useStreamList'
import { StreamTile } from './StreamTile'

export type Columns = 'auto' | 1 | 2 | 3 | 4

interface Props {
  streams: StreamEntry[]
  columns: Columns
  onPausedChange: (key: string, paused: boolean) => void
  onRemove: (key: string) => void
}

export function StreamWall({ streams, columns, onPausedChange, onRemove }: Props) {
  if (streams.length === 0) {
    return (
      <section className="wall wall--empty" aria-label="Streams">
        <div className="empty">
          <div className="empty__monitor" aria-hidden="true" />
          <h2 className="empty__title">No streams on the wall yet</h2>
          <p className="empty__text">
            Paste an RTSP URL above to start watching. If you don’t have a camera handy, add one of the demo
            streams.
          </p>
        </div>
      </section>
    )
  }

  const style = columns === 'auto' ? undefined : ({ '--columns': columns } as CSSProperties)
  return (
    <section className={`wall ${columns === 'auto' ? 'wall--auto' : 'wall--fixed'}`} style={style} aria-label="Streams">
      {streams.map((stream) => (
        <StreamTile key={stream.key} stream={stream} onPausedChange={onPausedChange} onRemove={onRemove} />
      ))}
    </section>
  )
}
