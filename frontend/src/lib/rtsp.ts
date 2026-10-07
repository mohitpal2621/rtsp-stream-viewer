/** Returns an error message for an unusable RTSP URL, or null if it looks fine. */
export function validateRtspUrl(raw: string): string | null {
  const value = raw.trim()
  if (value === '') return 'Enter an RTSP URL.'
  if (/\s/.test(value)) return 'The URL can’t contain spaces.'
  let url: URL
  try {
    url = new URL(value)
  } catch {
    return 'That doesn’t look like a URL. It should start with rtsp://'
  }
  if (url.protocol !== 'rtsp:' && url.protocol !== 'rtsps:') {
    return 'Only rtsp:// and rtsps:// URLs are supported.'
  }
  if (url.hostname === '') return 'The URL is missing a host, as in rtsp://192.168.1.20/stream'
  return null
}

/** Hides the password in a URL so it can be shown on screen. */
export function maskCredentials(raw: string): string {
  try {
    const { password } = new URL(raw)
    // Edit the string rather than the URL object, which would percent-encode the dots.
    return password ? raw.replace(`:${password}@`, ':•••@') : raw
  } catch {
    return raw
  }
}

/** A short label for a stream: the last path segment, or the host. */
export function defaultStreamName(raw: string): string {
  try {
    const url = new URL(raw)
    const segments = url.pathname.split('/').filter(Boolean)
    return decodeURIComponent(segments.at(-1) ?? url.host)
  } catch {
    return raw
  }
}
