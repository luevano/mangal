import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

describe('MultiSearch', () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => vi.unstubAllGlobals())

  it('fills in each provider as it answers, remembers the ticked ones', async () => {
    let slow: (r: Response) => void = () => {}
    const searched: string[] = []
    vi.stubGlobal('fetch', async (input: Request) => {
      const url = new URL(input.url)
      if (url.pathname === '/api/v1/providers') return Response.json([{ id: 'a', name: 'Alpha' }, { id: 'b', name: 'Beta' }, { id: 'c', name: 'Gamma' }, { id: 'own', name: 'Own' }])
      const provider = url.pathname.split('/')[4]!
      searched.push(`${provider}: ${url.searchParams.get('q')}`)
      if (provider === 'a') return new Promise<Response>((resolve) => (slow = resolve))
      if (provider === 'b') return Response.json({ title: 'down' }, { status: 422 })
      return Response.json([{ id: 'c1', title: 'Frieren' }])
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
    const { default: MultiSearch } = await import('../MultiSearch.vue')
    const w = mount(MultiSearch, { props: { storageKey: 'k', query: 'frieren', exclude: 'own' } })
    await flushPromises()
    // every one but the excluded, searched at once
    expect(w.findAll('[data-test=multi-providers] label').map((l) => l.text())).toEqual(['Alpha', 'Beta', 'Gamma'])
    expect(searched.sort()).toEqual(['a: frieren', 'b: frieren', 'c: frieren'])
    const sections = () => w.findAll('[data-test=multi-section]')
    expect(sections()[0]!.text()).toContain('searching…')
    expect(sections()[0]!.findAll('[data-test=multi-placeholder]').length).toBe(3)
    expect(sections()[1]!.find('[data-test=multi-error]').exists()).toBe(true)
    expect(sections()[2]!.text()).toContain('Frieren')
    await sections()[2]!.find('[data-test=multi-result]').trigger('click')
    expect(w.emitted('pick')?.[0]).toEqual(['c', { id: 'c1', title: 'Frieren' }])

    slow(Response.json([{ id: 'a1', title: 'Frieren A' }]))
    await flushPromises()
    expect(sections()[0]!.text()).toContain('Frieren A')
    expect(sections()[0]!.text()).not.toContain('searching…')

    // unticked: remembered, not searched
    await w.find('[data-test=tick-b]').setValue(false)
    expect(JSON.parse(localStorage.getItem('k')!)).toEqual(['a', 'c'])
    searched.length = 0
    await w.find('[data-test=multi-submit]').trigger('click')
    await flushPromises()
    expect(searched.sort()).toEqual(['a: frieren', 'c: frieren'])
    expect(w.emitted('search')?.length).toBe(2)

    const again = mount(MultiSearch, { props: { storageKey: 'k', exclude: 'own' } })
    await flushPromises()
    expect(again.findAll('input[type=checkbox]').map((i) => (i.element as HTMLInputElement).checked)).toEqual([true, false, true])
  })
})
