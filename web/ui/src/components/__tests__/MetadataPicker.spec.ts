import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { MetadataResult, MetadataSource } from '@/api/schemas'

const sources: MetadataSource[] = [
  { code: 'al', key: 'anilist', name: 'AniList', url: 'https://anilist.co/manga/', enabled: true },
  { code: 'kt', key: 'kitsu', name: 'Kitsu', url: 'https://kitsu.app/manga/', enabled: false, reason: 'kitsu isn\'t enabled' },
  { code: 'mu', key: 'mangaupdates', name: 'MangaUpdates', url: 'https://www.mangaupdates.com/series/', enabled: true }
]
const results: MetadataResult[] = [
  { source: 'al', id: '74347', title: 'One-Punch Man', year: 2012, format: 'MANGA', status: 'releasing', url: 'https://anilist.co/manga/74347', score: 1, bad: false },
  { source: 'al', id: '86123', title: 'Mob Psycho 100', url: 'https://anilist.co/manga/86123', score: 0.3, bad: true }
]

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

// stubFetch answers each request (a promise answers when it resolves)
function stubFetch(answer: (url: string, init: Request) => Promise<Response> | Response) {
  vi.stubGlobal('fetch', async (input: Request) => answer(input.url, input))
  // openapi-fetch makes a Request of its relative url: jsdom's page has no origin for it
  const NativeRequest = globalThis.Request
  vi.stubGlobal(
    'Request',
    class extends NativeRequest {
      constructor(input: RequestInfo | URL, init?: RequestInit) {
        super(typeof input === 'string' && input.startsWith('/') ? `http://localhost${input}` : input, init)
      }
    }
  )
  vi.resetModules()
}

describe('MetadataPicker', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('gets a partial result whole in its place, keeping its score', async () => {
    const partial: MetadataResult = { source: 'mu', id: '55', title: 'Attack on Titan', url: 'https://www.mangaupdates.com/series/55', score: 0.4, bad: true, partial: true }
    const whole: MetadataResult = { ...partial, status: 'ongoing', authors: ['Isayama'], score: 1, bad: false, partial: undefined }
    const urls: string[] = []
    stubFetch((url) => {
      urls.push(url)
      if (url.includes('/sources')) return json(sources)
      if (url.includes('q=id%3A55') || url.includes('q=id:55')) return json([whole])
      return json(url.includes('/mu/') ? [partial] : [])
    })
    const { default: MetadataPicker } = await import('../MetadataPicker.vue')
    const w = mount(MetadataPicker, { props: { open: true, title: 'Bind', query: 'titan' } })
    await flushPromises()
    expect(urls.some((u) => u.includes('id'))).toBe(false)
    await w.find('[data-test=metadata-details]').trigger('click')
    await flushPromises()
    const row = w.find('[data-test=metadata-result]')
    expect(row.text()).toContain('Isayama')
    expect(row.text()).toContain('bad match 40%')
    expect(w.find('[data-test=metadata-details]').exists()).toBe(false)
  })

  it('searches every source, shows the matches and picks one', async () => {
    const urls: string[] = []
    stubFetch((url) => {
      urls.push(url)
      if (url.includes('/sources')) return json(sources)
      return json(url.includes('/al/') ? results : [])
    })
    const { default: MetadataPicker } = await import('../MetadataPicker.vue')
    const w = mount(MetadataPicker, { props: { open: true, title: 'Bind', query: 'one punch man' } })
    await flushPromises()
    expect(urls.some((u) => u.includes('/v1/metadata/al/search?q=one+punch+man') || u.includes('/v1/metadata/al/search?q=one%20punch%20man'))).toBe(true)
    expect(urls.some((u) => u.includes('/v1/metadata/mu/search'))).toBe(true)
    expect(urls.some((u) => u.includes('/v1/metadata/kt/search'))).toBe(false)
    const items = w.findAll('[data-test=metadata-result]')
    expect(items).toHaveLength(2)
    expect(items[0]!.text()).toContain('One-Punch Man (2012)')
    expect(items[1]!.text()).toContain('bad match 30%')
    expect(w.find('[data-test=metadata-off]').text()).toContain('Kitsu')
    await items[0]!.find('[data-test=metadata-pick]').trigger('click')
    const picked = w.emitted('pick')![0] as [MetadataResult, MetadataSource]
    expect(picked[0].id).toBe('74347')
    expect(picked[1].name).toBe('AniList')
  })

  it('shows each source as it answers, an error only on its source', async () => {
    let answerMU: (r: Response) => void = () => {}
    const mu = new Promise<Response>((resolve) => (answerMU = resolve))
    const twoLive = sources.map((s) => ({ ...s, enabled: true }))
    stubFetch((url) => {
      if (url.includes('/sources')) return json(twoLive)
      if (url.includes('/kt/')) return json({ error: 'kitsu is down' }, 422)
      return url.includes('/mu/') ? mu : json(results)
    })
    const { default: MetadataPicker } = await import('../MetadataPicker.vue')
    const w = mount(MetadataPicker, { props: { open: true, title: 'Bind', query: 'one punch man' } })
    await flushPromises()
    const section = (code: string) => w.find(`[data-test=metadata-source-${code}]`)
    expect(section('al').findAll('[data-test=metadata-result]')).toHaveLength(2)
    expect(section('mu').find('.spinner-border').exists()).toBe(true)
    expect(section('kt').find('[data-test=metadata-error]').exists()).toBe(true)
    expect(section('al').find('[data-test=metadata-error]').exists()).toBe(false)
    expect(w.find('[data-test=metadata-credit]').exists()).toBe(false)

    answerMU(json([{ source: 'mu', id: '1', title: 'One-Punch Man', url: 'https://www.mangaupdates.com/series/1', score: 1, bad: false }]))
    await flushPromises()
    expect(section('mu').find('.spinner-border').exists()).toBe(false)
    expect(section('mu').find('[data-test=metadata-credit]').text()).toContain('MangaUpdates')
    expect(section('al').find('[data-test=metadata-credit]').exists()).toBe(false)
  })

  it('aborts the search when closed', async () => {
    const signals: AbortSignal[] = []
    stubFetch((url, req) => {
      if (url.includes('/sources')) return json(sources)
      signals.push(req.signal)
      return new Promise(() => {})
    })
    const { default: MetadataPicker } = await import('../MetadataPicker.vue')
    const w = mount(MetadataPicker, { props: { open: true, title: 'Bind', query: 'x' } })
    await flushPromises()
    expect(signals).toHaveLength(2)
    await w.setProps({ open: false })
    expect(signals.every((s) => s.aborted)).toBe(true)
  })
})
