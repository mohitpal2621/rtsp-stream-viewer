import { useId, useState, type FormEvent } from 'react'
import type { DemoStream } from '../lib/api'
import { defaultStreamName, validateRtspUrl } from '../lib/rtsp'

interface Props {
  existingUrls: string[]
  demoStreams: DemoStream[]
  full: boolean
  onAdd: (url: string, name: string) => void
}

export function AddStreamForm({ existingUrls, demoStreams, full, onAdd }: Props) {
  const [url, setUrl] = useState('')
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const errorId = useId()

  function submit(event: FormEvent) {
    event.preventDefault()
    const trimmed = url.trim()
    const problem =
      validateRtspUrl(trimmed) ??
      (existingUrls.includes(trimmed) ? 'That stream is already on the wall.' : null)
    if (problem) {
      setError(problem)
      return
    }
    onAdd(trimmed, name.trim() || defaultStreamName(trimmed))
    setUrl('')
    setName('')
    setError(null)
  }

  return (
    <div className="add-stream">
      <form className="add-stream__form" onSubmit={submit} noValidate>
        <label className="add-stream__field add-stream__field--url">
          <span className="visually-hidden">RTSP URL</span>
          <input
            type="url"
            inputMode="url"
            autoComplete="off"
            spellCheck={false}
            placeholder="rtsp://camera.local:554/stream"
            value={url}
            onChange={(e) => {
              setUrl(e.target.value)
              if (error) setError(null)
            }}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? errorId : undefined}
          />
        </label>
        <label className="add-stream__field add-stream__field--name">
          <span className="visually-hidden">Name (optional)</span>
          <input
            type="text"
            autoComplete="off"
            placeholder="Name (optional)"
            maxLength={40}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <button type="submit" className="button button--primary" disabled={full}>
          Add stream
        </button>
      </form>

      {error && (
        <p className="add-stream__error" id={errorId} role="alert">
          {error}
        </p>
      )}
      {full && !error && <p className="add-stream__note">The wall is full. Remove a stream to add another.</p>}

      {demoStreams.length > 0 && (
        <div className="demo-streams">
          <span className="demo-streams__label">Demo streams from this server:</span>
          {demoStreams.map((demo) => {
            const added = existingUrls.includes(demo.url)
            return (
              <button
                key={demo.url}
                type="button"
                className="chip"
                disabled={added || full}
                title={added ? 'Already on the wall' : demo.url}
                onClick={() => onAdd(demo.url, demo.name)}
              >
                {demo.name}
                {added && <span className="visually-hidden"> (already on the wall)</span>}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
