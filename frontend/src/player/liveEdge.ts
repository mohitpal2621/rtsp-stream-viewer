// Keeps a live MSE stream close to real time without stuttering.
//
// The backend sends one fragment per keyframe interval (GOP), so the newest
// buffered video grows in steps of a GOP. Playing right at the buffered end
// would stall between steps; playing too far behind adds latency. We aim to
// stay about 1.5 fragments behind the end and correct drift by playing
// slightly faster, or by jumping when we are far behind or stuck in a gap.

export interface Range {
  start: number
  end: number
}

export type PlaybackAction =
  | { type: 'none' }
  | { type: 'seek'; to: number }
  | { type: 'rate'; rate: number }

/** Smallest and largest fragment duration we plan around, in seconds. */
const MIN_FRAGMENT = 0.25
const MAX_FRAGMENT = 6

export function clampFragment(seconds: number): number {
  return Math.min(MAX_FRAGMENT, Math.max(MIN_FRAGMENT, seconds))
}

/** How far behind the buffered end playback should sit. */
export function targetLag(fragment: number): number {
  return Math.max(0.8, clampFragment(fragment) * 1.5)
}

/**
 * Decides how to adjust playback given the buffered ranges, the playhead and
 * the typical fragment duration.
 */
export function planPlayback(ranges: Range[], currentTime: number, fragment: number, rate: number): PlaybackAction {
  if (ranges.length === 0) return { type: 'none' }
  const newest = ranges[ranges.length - 1]
  const target = targetLag(fragment)
  const lag = newest.end - currentTime

  // Before the newest range means we're at the start, or stuck behind a gap
  // left by skipped fragments. Each fragment starts on a keyframe, so jumping
  // into the newest range is safe.
  if (currentTime < newest.start) {
    return { type: 'seek', to: Math.max(newest.start, newest.end - target) }
  }
  // Far behind, for example after the tab was in the background: jump.
  if (lag > target + 3 * clampFragment(fragment)) {
    return { type: 'seek', to: newest.end - target }
  }
  // A little behind: catch up gradually. Back to normal once close enough.
  const wanted = lag > target + clampFragment(fragment) ? 1.1 : 1
  if (wanted !== rate && (wanted > 1 || lag <= target)) {
    return { type: 'rate', rate: wanted }
  }
  return { type: 'none' }
}
