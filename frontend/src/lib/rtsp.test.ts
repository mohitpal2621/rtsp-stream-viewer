import { describe, expect, it } from 'vitest'
import { defaultStreamName, maskCredentials, validateRtspUrl } from './rtsp'

describe('validateRtspUrl', () => {
  it.each([
    'rtsp://localhost:8554/testsrc',
    'rtsps://camera.example.com/live',
    'rtsp://admin:p%40ss@192.168.1.20:554/Streaming/Channels/101',
  ])('accepts %s', (url) => {
    expect(validateRtspUrl(url)).toBeNull()
  })

  it.each([
    ['', 'Enter an RTSP URL'],
    ['http://camera.local/stream', 'Only rtsp://'],
    ['rtsp://camera local/stream', 'spaces'],
    ['camera.local/stream', 'start with rtsp://'],
    ['rtsp:///stream', 'missing a host'],
  ])('rejects %j', (url, message) => {
    expect(validateRtspUrl(url)).toContain(message)
  })
})

describe('maskCredentials', () => {
  it('hides the password but keeps the user', () => {
    expect(maskCredentials('rtsp://admin:secret@cam.local/live')).toBe('rtsp://admin:•••@cam.local/live')
  })

  it('leaves URLs without a password alone', () => {
    expect(maskCredentials('rtsp://cam.local/live')).toBe('rtsp://cam.local/live')
  })
})

describe('defaultStreamName', () => {
  it('uses the last path segment', () => {
    expect(defaultStreamName('rtsp://cam.local:554/front/door')).toBe('door')
  })

  it('falls back to the host', () => {
    expect(defaultStreamName('rtsp://cam.local:554')).toBe('cam.local:554')
  })
})
