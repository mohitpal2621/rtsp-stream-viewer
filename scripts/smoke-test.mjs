// End-to-end check against a running server: the API answers, and a demo
// stream (or the RTSP URL given) delivers an init segment and video
// fragments over the WebSocket. Needs Node 22+ for the built-in WebSocket.
//
//   node scripts/smoke-test.mjs [server-url] [rtsp-url]
//   node scripts/smoke-test.mjs http://localhost:8080

const base = (process.argv[2] ?? 'http://localhost:8080').replace(/\/+$/, '')
let rtspUrl = process.argv[3]
const TIMEOUT_MS = 30_000

function fail(message) {
  console.error(`FAIL: ${message}`)
  process.exit(1)
}

const timer = setTimeout(() => fail(`no video within ${TIMEOUT_MS / 1000}s`), TIMEOUT_MS)

const health = await fetch(`${base}/healthz`).catch((err) => fail(`server unreachable: ${err.message}`))
if (!health.ok) fail(`/healthz returned ${health.status}`)
console.log('ok   /healthz')

if (!rtspUrl) {
  const { demoStreams } = await (await fetch(`${base}/api/config`)).json()
  if (!demoStreams?.length) fail('no demo streams configured; pass an RTSP URL')
  rtspUrl = demoStreams[0].url
}

const res = await fetch(`${base}/api/streams`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ url: rtspUrl }),
})
const body = await res.json()
if (!res.ok) fail(`POST /api/streams returned ${res.status}: ${body.error}`)
console.log(`ok   registered ${rtspUrl} as ${body.id}`)

const started = Date.now()
const ws = new WebSocket(`${base.replace(/^http/, 'ws')}/api/streams/${body.id}/ws`)
ws.binaryType = 'arraybuffer'
let mime = null
let fragments = 0

ws.onmessage = (event) => {
  if (typeof event.data === 'string') {
    const msg = JSON.parse(event.data)
    if (msg.type === 'init') mime = msg.mime
    if (msg.type === 'status') console.log(`     status ${msg.state}${msg.error ? `: ${msg.error}` : ''}`)
    return
  }
  const box = new TextDecoder().decode(new Uint8Array(event.data, 4, 4))
  if (box === 'ftyp') {
    console.log(`ok   init segment (${mime}) after ${Date.now() - started} ms`)
  } else if (box === 'moof' && ++fragments === 3) {
    console.log(`ok   3 fragments after ${Date.now() - started} ms`)
    clearTimeout(timer)
    ws.close()
    process.exit(0)
  }
}
ws.onerror = () => fail('WebSocket error')
ws.onclose = (event) => fail(`WebSocket closed early (code ${event.code})`)
