import { useEffect, useState } from 'react'
import { AddStreamForm } from './components/AddStreamForm'
import { StreamWall, type Columns } from './components/StreamWall'
import { Toolbar } from './components/Toolbar'
import { MAX_STREAMS, useStreamList } from './hooks/useStreamList'
import { fetchDemoStreams, type DemoStream } from './lib/api'

const COLUMNS_KEY = 'rtsp-viewer:columns'

function loadColumns(): Columns {
  try {
    const n = Number(localStorage.getItem(COLUMNS_KEY))
    return n === 1 || n === 2 || n === 3 || n === 4 ? n : 'auto'
  } catch {
    return 'auto'
  }
}

export default function App() {
  const { streams, add, remove, setPaused, setAllPaused } = useStreamList()
  const [columns, setColumns] = useState<Columns>(loadColumns)
  const [demoStreams, setDemoStreams] = useState<DemoStream[]>([])
  const [serverDown, setServerDown] = useState(false)

  // Loading the demo list doubles as a check that the backend is up. A free
  // hosting tier may be asleep, so keep trying with backoff.
  useEffect(() => {
    let cancelled = false
    let timer: number | undefined
    const load = (retryDelay: number) => {
      fetchDemoStreams()
        .then((demos) => {
          if (cancelled) return
          setDemoStreams(demos)
          setServerDown(false)
        })
        .catch(() => {
          if (cancelled) return
          setServerDown(true)
          timer = window.setTimeout(() => load(Math.min(retryDelay * 2, 30)), retryDelay * 1000)
        })
    }
    load(2)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [])

  const changeColumns = (value: Columns) => {
    setColumns(value)
    try {
      localStorage.setItem(COLUMNS_KEY, String(value))
    } catch {
      // Storage unavailable: the choice lasts until reload.
    }
  }

  const pausedCount = streams.filter((s) => s.paused).length

  return (
    <div className="app">
      <header className="masthead">
        <div className="masthead__title">
          <h1>RTSP Stream Viewer</h1>
          <p>Watch live camera feeds side by side in your browser.</p>
        </div>
        <AddStreamForm
          existingUrls={streams.map((s) => s.url)}
          demoStreams={demoStreams}
          full={streams.length >= MAX_STREAMS}
          onAdd={add}
        />
      </header>

      {serverDown && (
        <p className="banner" role="status">
          Can’t reach the stream server. If it was asleep it can take up to a minute to start. This page keeps
          trying.
        </p>
      )}

      <main className="main">
        {streams.length > 0 && (
          <Toolbar
            count={streams.length}
            pausedCount={pausedCount}
            columns={columns}
            onColumnsChange={changeColumns}
            onPauseAll={() => setAllPaused(true)}
            onPlayAll={() => setAllPaused(false)}
          />
        )}
        <StreamWall streams={streams} columns={columns} onPausedChange={setPaused} onRemove={remove} />
      </main>
    </div>
  )
}
