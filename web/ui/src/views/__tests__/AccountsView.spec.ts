import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { useStatus } from '@/stores/status'
import type { APIClient, Me } from '@/api/schemas'

const clients: APIClient[] = [
  {
    source: 'anilist',
    name: 'AniList',
    configured: true,
    id: '123',
    secret: true,
    redirect: 'pin',
    redirectUrl: 'https://anilist.co/api/v2/oauth/pin',
    registerUrl: 'https://anilist.co/settings/developer'
  },
  {
    source: 'myanimelist',
    name: 'MyAnimeList',
    configured: false,
    secret: false,
    redirectUrl: 'http://localhost:6969/oauth/mal/callback',
    registerUrl: 'https://myanimelist.net/apiconfig'
  }
]

const admin: Me = { name: 'a', kind: 'user', scopes: ['read', 'progress', 'admin'], admin: true, hide_unavailable: true }
const reader: Me = { name: 'r', kind: 'user', scopes: ['read', 'progress'], admin: false, hide_unavailable: true }

async function render(me: Me) {
  const sent: { url: string; method: string; body: unknown }[] = []
  vi.stubGlobal('fetch', async (input: Request) => {
    const body = input.method === 'GET' ? undefined : await input.text()
    sent.push({ url: input.url, method: input.method, body: body ? JSON.parse(body) : undefined })
    let out: unknown = []
    if (input.url.includes('/v1/accounts/clients')) out = input.method === 'GET' ? clients : clients[0]
    else if (input.url.includes('/v1/config/')) out = {}
    return new Response(JSON.stringify(out), { headers: { 'Content-Type': 'application/json' } })
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
  vi.stubGlobal('confirm', () => true)
  vi.resetModules()
  const { default: AccountsView } = await import('../AccountsView.vue')
  const pinia = createPinia()
  setActivePinia(pinia)
  useStatus().me = me
  const router = createRouter({ history: createMemoryHistory(), routes })
  const w = mount(AccountsView, { global: { plugins: [pinia, router] } })
  await flushPromises()
  return { w, sent }
}

describe('AccountsView', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('sets the server’s API clients for an admin, the secret kept unless typed', async () => {
    const { w, sent } = await render(admin)
    expect(w.find('[data-test=api-clients]').text()).toContain('https://anilist.co/api/v2/oauth/pin')
    await w.find('[data-test=client-anilist]').trigger('click')
    expect((w.find('[data-test=client-id]').element as HTMLInputElement).value).toBe('123')
    await w.find('[data-test=client-save]').trigger('click')
    await flushPromises()
    const put = sent.find((s) => s.method === 'PUT')
    expect(put?.url).toContain('/v1/accounts/clients/anilist')
    expect(put?.body).toEqual({ id: '123' })

    await w.find('[data-test=redirect-anilist]').setValue('local')
    await flushPromises()
    const redirect = sent.find((s) => s.url.includes('/v1/config/metadata.anilist.redirect'))
    expect(redirect?.body).toEqual({ value: 'local' })
  })

  it('has no API clients for a user who isn’t an admin', async () => {
    const { w, sent } = await render(reader)
    expect(w.find('[data-test=api-clients]').exists()).toBe(false)
    expect(sent.some((s) => s.url.includes('/v1/accounts/clients'))).toBe(false)
  })
})
