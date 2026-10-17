import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

describe('HoldDialog', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('suggests the AniList status mark and posts it', async () => {
    setActivePinia(createPinia())
    const posted: { url: string; body: unknown }[] = []
    vi.stubGlobal('fetch', async (input: Request) => {
      posted.push({ url: input.url, body: await input.json() })
      return new Response(JSON.stringify({ watches: [], check: { new_chapters: 2 } }), { headers: { 'Content-Type': 'application/json' } })
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
    const { default: HoldDialog } = await import('../HoldDialog.vue')
    const w = mount(HoldDialog, { props: { open: false, title: 'Kingdom', watch: 21, status: 'HIATUS' } })
    await w.setProps({ open: true })
    expect(w.find('[data-test=hold-suggested]').text()).toContain('hiatus on AniList')
    expect((w.find('#hold-hiatus').element as HTMLInputElement).checked).toBe(true)
    // nothing to clear
    expect((w.find('#hold-').element as HTMLInputElement).disabled).toBe(true)
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(posted).toHaveLength(1)
    expect(posted[0]!.url).toContain('/v1/watches/21/hold')
    expect(posted[0]!.body).toEqual({ mark: 'hiatus' })
    expect(w.emitted('done')).toBeTruthy()
  })

  it('suggests another provider from the coverage, with it as the reason', async () => {
    setActivePinia(createPinia())
    vi.resetModules()
    const { default: HoldDialog } = await import('../HoldDialog.vue')
    const w = mount(HoldDialog, { props: { open: false, title: 'Lost', manga: 4, coverage: 'the provider has 3 of the library\'s 20 chapters', status: 'FINISHED' } })
    await w.setProps({ open: true })
    expect((w.find('#hold-provider').element as HTMLInputElement).checked).toBe(true)
    expect((w.find('input.form-control').element as HTMLInputElement).value).toContain('3 of the library')
  })
})
