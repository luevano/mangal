import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { MetadataResult, MetadataSource } from '@/api/schemas'

const sources: MetadataSource[] = [
  { code: 'al', key: 'anilist', name: 'AniList', url: 'https://anilist.co/manga/', enabled: true },
  { code: 'kt', key: 'kitsu', name: 'Kitsu', url: 'https://kitsu.app/manga/', enabled: false, reason: 'kitsu isn\'t enabled' }
]
const results: MetadataResult[] = [
  { source: 'al', id: '74347', title: 'One-Punch Man', year: 2012, format: 'MANGA', status: 'releasing', url: 'https://anilist.co/manga/74347', score: 1, bad: false },
  { source: 'al', id: '86123', title: 'Mob Psycho 100', url: 'https://anilist.co/manga/86123', score: 0.3, bad: true }
]

describe('MetadataPicker', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('searches the title, shows the matches and picks one', async () => {
    const urls: string[] = []
    vi.stubGlobal('fetch', async (input: Request | string) => {
      const url = typeof input === 'string' ? input : input.url
      urls.push(url)
      const body = url.includes('/sources') ? sources : results
      return new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })
    })
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
    const { default: MetadataPicker } = await import('../MetadataPicker.vue')
    const w = mount(MetadataPicker, { props: { open: true, title: 'Bind', query: 'one punch man' } })
    await flushPromises()
    expect(urls.some((u) => u.includes('/v1/metadata/al/search?q=one+punch+man') || u.includes('/v1/metadata/al/search?q=one%20punch%20man'))).toBe(true)
    const items = w.findAll('[data-test=metadata-result]')
    expect(items).toHaveLength(2)
    expect(items[0]!.text()).toContain('One-Punch Man (2012)')
    expect(items[1]!.text()).toContain('bad match 30%')
    expect(w.find('option[disabled]').text()).toContain('Kitsu')
    await items[0]!.find('[data-test=metadata-pick]').trigger('click')
    const picked = w.emitted('pick')![0] as [MetadataResult, MetadataSource]
    expect(picked[0].id).toBe('74347')
    expect(picked[1].name).toBe('AniList')
  })
})
