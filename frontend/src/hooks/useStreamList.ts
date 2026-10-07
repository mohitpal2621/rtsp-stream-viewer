import { useCallback, useEffect, useState } from 'react'

export interface StreamEntry {
  key: string // client-side identity; the same URL is only added once
  url: string
  name: string
  paused: boolean
}

const STORAGE_KEY = 'rtsp-viewer:streams'
export const MAX_STREAMS = 16

/** Reads the saved list, ignoring anything malformed. */
export function loadStreams(raw: string | null): StreamEntry[] {
  if (!raw) return []
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter(
        (s): s is StreamEntry =>
          typeof s?.key === 'string' && typeof s?.url === 'string' && typeof s?.name === 'string',
      )
      .map((s) => ({ ...s, paused: s.paused === true }))
      .slice(0, MAX_STREAMS)
  } catch {
    return []
  }
}

// crypto.randomUUID only exists on https and localhost pages.
function newKey(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

function readStorage(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

/** The streams on the wall, saved in localStorage so they survive a reload. */
export function useStreamList() {
  const [streams, setStreams] = useState<StreamEntry[]>(() => loadStreams(readStorage()))

  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(streams))
    } catch {
      // Private browsing or storage disabled: the list just won't persist.
    }
  }, [streams])

  const add = useCallback((url: string, name: string) => {
    setStreams((list) => [...list, { key: newKey(), url, name, paused: false }])
  }, [])

  const remove = useCallback((key: string) => {
    setStreams((list) => list.filter((s) => s.key !== key))
  }, [])

  const setPaused = useCallback((key: string, paused: boolean) => {
    setStreams((list) => list.map((s) => (s.key === key ? { ...s, paused } : s)))
  }, [])

  const setAllPaused = useCallback((paused: boolean) => {
    setStreams((list) => list.map((s) => ({ ...s, paused })))
  }, [])

  return { streams, add, remove, setPaused, setAllPaused }
}
