import { describe, expect, it } from 'vitest'
import { loadStreams, MAX_STREAMS } from './useStreamList'

describe('loadStreams', () => {
  it('returns an empty list for missing or corrupt data', () => {
    expect(loadStreams(null)).toEqual([])
    expect(loadStreams('not json')).toEqual([])
    expect(loadStreams('{"key":"a"}')).toEqual([])
  })

  it('keeps valid entries and drops malformed ones', () => {
    const raw = JSON.stringify([
      { key: 'a', url: 'rtsp://cam/1', name: 'One', paused: true },
      { key: 'b', url: 'rtsp://cam/2', name: 'Two' },
      { key: 'c', url: 42, name: 'Bad' },
      null,
    ])
    expect(loadStreams(raw)).toEqual([
      { key: 'a', url: 'rtsp://cam/1', name: 'One', paused: true },
      { key: 'b', url: 'rtsp://cam/2', name: 'Two', paused: false },
    ])
  })

  it('caps the list at the maximum', () => {
    const many = Array.from({ length: MAX_STREAMS + 5 }, (_, i) => ({ key: `${i}`, url: `rtsp://cam/${i}`, name: `${i}` }))
    expect(loadStreams(JSON.stringify(many))).toHaveLength(MAX_STREAMS)
  })
})
