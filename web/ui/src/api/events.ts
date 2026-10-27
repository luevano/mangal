import type { Event } from './schemas'

export type Topic = 'watch' | 'job' | 'op' | 'health' | 'log' | 'library' | 'history' | 'settings' | 'watches'
export type Listener = (e: Event) => void
export type StreamState = 'idle' | 'connecting' | 'open' | 'retrying' | 'refused'

export interface StreamOptions {
  url?: string
  headers?: () => Record<string, string>
  fetch?: typeof fetch
  // retry is the wait before reconnecting, in ms
  retry?: number
  onState?: (s: StreamState, error?: string) => void
}

// parseSSE splits a chunk of an event stream into events (what a blank line ends) and
// the rest, still incomplete.
export function parseSSE(buffer: string): { events: { id?: string; data: string }[]; rest: string } {
  const events: { id?: string; data: string }[] = []
  const text = buffer.replace(/\r\n?/g, '\n')
  const blocks = text.split('\n\n')
  const rest = blocks.pop() ?? ''
  for (const block of blocks) {
    let id: string | undefined
    const data: string[] = []
    for (const line of block.split('\n')) {
      if (line === '' || line.startsWith(':')) continue
      const i = line.indexOf(':')
      const field = i < 0 ? line : line.slice(0, i)
      let value = i < 0 ? '' : line.slice(i + 1)
      if (value.startsWith(' ')) value = value.slice(1)
      if (field === 'id') id = value
      else if (field === 'data') data.push(value)
    }
    if (data.length) events.push({ id, data: data.join('\n') })
  }
  return { events, rest }
}

// EventStream is the one connection to /api/v1/events: listeners subscribe to topics, the
// stream asks for the topics someone listens to, reconnects (from the last seq) when it
// drops or the topics change. fetch, not EventSource: headers (Last-Event-ID) and the refusals
// can be told apart.
export class EventStream {
  lastSeq = 0
  state: StreamState = 'idle'
  private listeners = new Map<Topic, Set<Listener>>()
  private abort?: AbortController
  private timer?: ReturnType<typeof setTimeout>
  private topicsKey = ''
  private stopped = true
  private opts: Required<Omit<StreamOptions, 'onState'>> & Pick<StreamOptions, 'onState'>

  constructor(opts: StreamOptions = {}) {
    this.opts = {
      url: opts.url ?? '/api/v1/events',
      headers: opts.headers ?? (() => ({})),
      fetch: opts.fetch ?? ((...a) => fetch(...a)),
      retry: opts.retry ?? 2000,
      onState: opts.onState
    }
  }

  topics(): Topic[] {
    return [...this.listeners.entries()]
      .filter(([, set]) => set.size > 0)
      .map(([t]) => t)
      .sort()
  }

  // on listens to the topic's events, until the returned function is called.
  on(topic: Topic, fn: Listener): () => void {
    let set = this.listeners.get(topic)
    if (!set) this.listeners.set(topic, (set = new Set()))
    set.add(fn)
    this.topicsChanged()
    return () => {
      set.delete(fn)
      this.topicsChanged()
    }
  }

  // start connects, from since (the status's event_seq) the first time.
  start(since?: number) {
    if (since !== undefined && since > this.lastSeq) this.lastSeq = since
    this.stopped = false
    this.connect()
  }

  stop() {
    this.stopped = true
    clearTimeout(this.timer)
    this.abort?.abort()
    this.abort = undefined
    this.setState('idle')
  }

  // reconnect drops the connection and connects again now (a new login).
  reconnect() {
    if (this.stopped) return
    clearTimeout(this.timer)
    this.abort?.abort()
    this.connect()
  }

  private topicsChanged() {
    const key = this.topics().join(',')
    if (key === this.topicsKey) return
    if (!this.stopped) this.reconnect()
  }

  private setState(s: StreamState, error?: string) {
    this.state = s
    this.opts.onState?.(s, error)
  }

  private dispatch(e: Event) {
    if (e.seq > this.lastSeq) this.lastSeq = e.seq
    for (const fn of this.listeners.get(e.topic as Topic) ?? []) {
      try {
        fn(e)
      } catch (err) {
        console.error('event listener', err)
      }
    }
  }

  private connect() {
    const topics = this.topics()
    this.topicsKey = topics.join(',')
    this.abort?.abort()
    if (topics.length === 0) {
      this.setState('idle')
      return
    }
    const abort = new AbortController()
    this.abort = abort
    const q = new URLSearchParams({ topics: this.topicsKey })
    if (this.lastSeq > 0) q.set('since', String(this.lastSeq))
    const headers: Record<string, string> = { Accept: 'text/event-stream', ...this.opts.headers() }
    if (this.lastSeq > 0) headers['Last-Event-ID'] = String(this.lastSeq)
    this.setState('connecting')
    this.run(`${this.opts.url}?${q}`, headers, abort).catch(() => {})
  }

  private async run(url: string, headers: Record<string, string>, abort: AbortController) {
    let error = ''
    try {
      const res = await this.opts.fetch(url, { headers, signal: abort.signal })
      if (!res.ok || !res.body) {
        if (res.status >= 400 && res.status < 500) {
          // not logged in or a bad request: retrying won't help until the login changes
          this.setState('refused', `HTTP ${res.status}`)
          return
        }
        throw new Error(`HTTP ${res.status}`)
      }
      this.setState('open')
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      for (;;) {
        const { value, done } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const parsed = parseSSE(buffer)
        buffer = parsed.rest
        for (const raw of parsed.events) {
          try {
            this.dispatch(JSON.parse(raw.data) as Event)
          } catch {
            // not an event
          }
        }
      }
    } catch (e) {
      if (abort.signal.aborted) return
      error = e instanceof Error ? e.message : String(e)
    }
    if (abort.signal.aborted || this.stopped) return
    this.setState('retrying', error || 'disconnected')
    clearTimeout(this.timer)
    this.timer = setTimeout(() => this.connect(), this.opts.retry)
  }
}
