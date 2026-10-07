// Talks to the Go backend. In development Vite proxies /api to it; in a
// split deployment VITE_API_URL points at it.
const API_BASE = (import.meta.env.VITE_API_URL ?? '').replace(/\/+$/, '')

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export interface DemoStream {
  name: string
  url: string
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(API_BASE + path, init)
  } catch {
    throw new ApiError(0, 'The server didn’t respond. It may be restarting or offline.')
  }
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    // A proxy or host in front of the backend may answer without our JSON body.
    const fallback = res.status >= 500 ? 'The server isn’t responding right now.' : `Request failed (HTTP ${res.status}).`
    throw new ApiError(res.status, body.error ?? fallback)
  }
  return body as T
}

export async function fetchDemoStreams(): Promise<DemoStream[]> {
  const body = await request<{ demoStreams: DemoStream[] }>('/api/config')
  return body.demoStreams
}

/** Registers an RTSP URL with the backend and returns its stream ID. */
export async function registerStream(url: string): Promise<string> {
  const body = await request<{ id: string }>('/api/streams', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url }),
  })
  return body.id
}

export function streamSocketUrl(id: string): string {
  const url = new URL(`${API_BASE}/api/streams/${encodeURIComponent(id)}/ws`, window.location.href)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  return url.toString()
}
