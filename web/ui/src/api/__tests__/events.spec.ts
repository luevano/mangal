import { describe, expect, it, vi } from 'vitest'
import { EventStream, parseSSE } from '../events'

describe('parseSSE', () => {
  it('splits events, skips comments, keeps the rest', () => {
    const { events, rest } = parseSSE(': ping\n\nid: 3\ndata: {"a":1}\n\nid: 4\r\ndata: x\r\ndata: y\r\n\r\nid: 5\ndata: part')
    expect(events).toEqual([
      { id: '3', data: '{"a":1}' },
      { id: '4', data: 'x\ny' }
    ])
    expect(rest).toBe('id: 5\ndata: part')
  })
})

// a fetch answering an event stream the test feeds
function fakeFetch() {
  const calls: { url: string; headers: Record<string, string> }[] = []
  let push: (s: string) => void = () => {}
  let end: () => void = () => {}
  let status = 200
  const fn = vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, headers: (init?.headers ?? {}) as Record<string, string> })
    if (status !== 200) return new Response('', { status })
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        const enc = new TextEncoder()
        push = (s) => c.enqueue(enc.encode(s))
        end = () => c.close()
        init?.signal?.addEventListener('abort', () => {
          try {
            c.error(new DOMException('aborted', 'AbortError'))
          } catch {
            // closed
          }
        })
      }
    })
    return new Response(body, { status: 200 })
  })
  return {
    fn,
    calls,
    push: (s: string) => push(s),
    end: () => end(),
    setStatus: (s: number) => (status = s)
  }
}

const tick = () => new Promise((r) => setTimeout(r, 0))
const event = (seq: number, topic: string, kind = 'added') => `id: ${seq}\ndata: ${JSON.stringify({ seq, time: '', topic, kind, data: { id: seq } })}\n\n`

describe('EventStream', () => {
  it('asks for the listened topics, dispatches by topic and keeps the last seq', async () => {
    const f = fakeFetch()
    const s = new EventStream({ fetch: f.fn as unknown as typeof fetch, headers: () => ({ Authorization: 'Bearer t' }), retry: 5 })
    const jobs: number[] = []
    const ops: number[] = []
    s.on('job', (e) => jobs.push(e.seq))
    s.on('op', (e) => ops.push(e.seq))
    s.start(7)
    await tick()
    expect(f.calls[0]!.url).toBe('/api/v1/events?topics=job%2Cop&since=7')
    expect(f.calls[0]!.headers.Authorization).toBe('Bearer t')
    expect(f.calls[0]!.headers['Last-Event-ID']).toBe('7')
    expect(s.state).toBe('open')
    f.push(event(8, 'job') + event(9, 'op'))
    f.push(event(10, 'job').slice(0, 10))
    await tick()
    expect(jobs).toEqual([8])
    expect(ops).toEqual([9])
    expect(s.lastSeq).toBe(9)
    s.stop()
  })

  it('reconnects from the last seq when the stream ends', async () => {
    const f = fakeFetch()
    const s = new EventStream({ fetch: f.fn as unknown as typeof fetch, retry: 5 })
    s.on('watch', () => {})
    s.start()
    await tick()
    expect(f.calls[0]!.url).toBe('/api/v1/events?topics=watch')
    f.push(event(3, 'watch'))
    await tick()
    f.end()
    await tick()
    expect(s.state).toBe('retrying')
    await new Promise((r) => setTimeout(r, 20))
    expect(f.calls[1]!.url).toBe('/api/v1/events?topics=watch&since=3')
    s.stop()
  })

  it('reconnects when the topics change, not when they stay', async () => {
    const f = fakeFetch()
    const s = new EventStream({ fetch: f.fn as unknown as typeof fetch, retry: 5 })
    const off = s.on('job', () => {})
    s.start()
    await tick()
    s.on('job', () => {})
    await tick()
    expect(f.calls.length).toBe(1)
    const offLog = s.on('log', () => {})
    await tick()
    expect(f.calls[1]!.url).toBe('/api/v1/events?topics=job%2Clog')
    offLog()
    off()
    await tick()
    expect(f.calls[2]!.url).toBe('/api/v1/events?topics=job')
    s.stop()
  })

  it("doesn't retry a refused stream", async () => {
    const f = fakeFetch()
    f.setStatus(401)
    const states: string[] = []
    const s = new EventStream({ fetch: f.fn as unknown as typeof fetch, retry: 5, onState: (st) => states.push(st) })
    s.on('op', () => {})
    s.start()
    await new Promise((r) => setTimeout(r, 20))
    expect(f.calls.length).toBe(1)
    expect(states).toContain('refused')
    s.stop()
  })
})
