import { describe, expect, it } from 'vitest'
import { planPlayback, targetLag } from './liveEdge'

describe('planPlayback', () => {
  const fragment = 1 // one-second GOPs

  it('does nothing without buffered video', () => {
    expect(planPlayback([], 0, fragment, 1)).toEqual({ type: 'none' })
  })

  it('jumps into the buffer at start-up', () => {
    // RTSP timestamps rarely start at zero.
    const action = planPlayback([{ start: 1200, end: 1201 }], 0, fragment, 1)
    expect(action).toEqual({ type: 'seek', to: 1200 })
  })

  it('jumps over a gap left by skipped fragments', () => {
    const ranges = [
      { start: 10, end: 14 },
      { start: 16, end: 19 },
    ]
    expect(planPlayback(ranges, 14, fragment, 1)).toEqual({ type: 'seek', to: 19 - targetLag(fragment) })
  })

  it('leaves normal playback alone', () => {
    expect(planPlayback([{ start: 10, end: 20 }], 18.8, fragment, 1)).toEqual({ type: 'none' })
  })

  it('speeds up a little when drifting behind', () => {
    expect(planPlayback([{ start: 10, end: 20 }], 17, fragment, 1)).toEqual({ type: 'rate', rate: 1.1 })
  })

  it('returns to normal speed once caught up', () => {
    expect(planPlayback([{ start: 10, end: 20 }], 18.6, fragment, 1.1)).toEqual({ type: 'rate', rate: 1 })
  })

  it('keeps catching up between the two thresholds', () => {
    expect(planPlayback([{ start: 10, end: 20 }], 18, fragment, 1.1)).toEqual({ type: 'none' })
  })

  it('seeks when far behind, e.g. after the tab was hidden', () => {
    expect(planPlayback([{ start: 0, end: 60 }], 30, fragment, 1)).toEqual({ type: 'seek', to: 60 - targetLag(fragment) })
  })

  it('plans around longer GOPs', () => {
    // With 2 s fragments, 2.5 s behind is normal rather than late.
    expect(planPlayback([{ start: 10, end: 20 }], 17.5, 2, 1)).toEqual({ type: 'none' })
  })
})
