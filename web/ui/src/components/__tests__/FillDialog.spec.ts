import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

describe('FillDialog', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('plans, then starts the job', async () => {
    setActivePinia(createPinia())
    const posted: { url: string; body: unknown }[] = []
    const plan = { provider: 'mango-mangadex', manga_id: 'md', manga_title: 'Dark Gathering', from: 'mango-mangapill', from_manga_title: 'Dark Gathering', folder: '/lib/Dark Gathering', found: [{ number: 1, title: 'Lie Low' }], not_found: [], downloaded: [], lines: ['get chapter 1 "Lie Low" from mango-mangapill ("Chapter 1")'] }
    vi.stubGlobal('fetch', async (input: Request) => {
      if (input.method === 'GET') return Response.json([{ id: 'mango-mangadex', name: 'MangaDex' }, { id: 'mango-mangapill', name: 'Mangapill' }])
      posted.push({ url: input.url, body: await input.json() })
      const job = input.url.includes('apply=true') ? { job: { id: 3, title: 'Dark Gathering: chapter 1 from Mangapill' } } : {}
      return Response.json({ ...plan, ...job })
    })
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
    const { default: FillDialog } = await import('../FillDialog.vue')
    const w = mount(FillDialog, { props: { open: false, provider: 'mango-mangadex', mangaId: 'md', title: 'Dark Gathering' } })
    await w.setProps({ open: true })
    await flushPromises()
    // not the manga's own provider
    expect(w.findAll('[data-test=fill-from] option').map((o) => o.text())).toEqual(['Mangapill'])
    await w.find('[data-test=fill-from]').setValue('mango-mangapill')
    await w.find('input[placeholder="every unavailable one, or 1-2, 10-"]').setValue('1-2')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(posted[0]!.url).toContain('/v1/providers/mango-mangadex/fill')
    expect(posted[0]!.url).not.toContain('apply')
    expect(posted[0]!.body).toEqual({ manga_id: 'md', from: 'mango-mangapill', chapters: '1-2' })
    expect(w.find('[data-test=fill-plan]').text()).toContain('Lie Low')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(posted[1]!.url).toContain('apply=true')
    expect(w.emitted('queued')?.[0]).toEqual([{ id: 3, title: 'Dark Gathering: chapter 1 from Mangapill' }])
  })
})
