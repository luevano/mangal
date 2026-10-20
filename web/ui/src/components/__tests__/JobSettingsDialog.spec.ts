import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { JobKind } from '@/api/schemas'

const urls: JobKind = {
  kind: 'urls',
  title: 'Source addresses',
  about: '',
  enabled: true,
  paused: false,
  manual: false,
  schedule: '168h',
  schedule_key: 'jobs.urls.interval',
  providers_key: 'jobs.urls.providers',
  delay: '1s-3s',
  providers: [
    { provider: 'mango-mangadex', name: 'MangaDex', on: true, items: 3, missing: 1 },
    { provider: 'mango-mangaplus', name: 'MangaPlus', on: false, default: 'off', items: 0, missing: 0 }
  ]
}

describe('JobSettingsDialog', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('puts only what changed', async () => {
    setActivePinia(createPinia())
    const sent: { url: string; method: string; body: unknown }[] = []
    vi.stubGlobal('fetch', async (input: Request) => {
      sent.push({ url: input.url, method: input.method, body: await input.json() })
      return new Response(JSON.stringify(urls), { headers: { 'Content-Type': 'application/json' } })
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
    const { default: JobSettingsDialog } = await import('../JobSettingsDialog.vue')
    const w = mount(JobSettingsDialog, { props: { open: true, kind: urls } })
    expect((w.find('[data-test=job-p-mango-mangaplus]').element as HTMLInputElement).value).toBe('off')
    await w.find('[data-test=job-p-mango-mangaplus]').setValue('on')
    await w.find('[data-test=job-delay]').setValue('2s-4s')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(sent).toHaveLength(1)
    expect(sent[0]!.method).toBe('PUT')
    expect(sent[0]!.url).toContain('/v1/jobs/kinds/urls/settings')
    expect(sent[0]!.body).toEqual({ delay_min: '2s', delay_max: '4s', providers: { 'mango-mangaplus': 'on' } })
    expect(w.emitted('done')).toBeTruthy()
  })
})
