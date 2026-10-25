import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '@/router'
import { useStatus } from '@/stores/status'
import type { ConfigChange, ConfigEntry, Me } from '@/api/schemas'

const entries: ConfigEntry[] = [
  {
    key: 'download.skip_if_exists',
    value: true,
    default: true,
    description: 'skip',
    secret: false,
    applies: 'now',
    stored: 'db',
    source: { kind: 'default' },
    kind: 'bool'
  },
  {
    key: 'log.level',
    value: 'info',
    default: 'info',
    description: 'level',
    secret: false,
    applies: 'restart',
    stored: 'file',
    source: { kind: 'default' },
    kind: 'string',
    values: ['debug', 'info']
  },
  {
    key: 'providers.mango-mangaplus.quality',
    value: 'high',
    default: 'super_high',
    description: 'quality',
    secret: false,
    applies: 'reload',
    stored: 'db',
    source: { kind: 'db' },
    kind: 'string',
    provider: 'mango-mangaplus'
  }
]
const history: ConfigChange[] = [
  {
    id: 7,
    key: 'providers.mango-mangaplus.quality',
    new: '"high"',
    who: 'tui',
    at: '2026-10-25T10:00:00Z'
  }
]

const admin: Me = {
  name: 'a',
  kind: 'user',
  scopes: ['read', 'watch', 'progress', 'write'],
  admin: true,
  hide_unavailable: true
}
const writer: Me = {
  name: 'w',
  kind: 'user',
  scopes: ['read', 'write'],
  admin: false,
  hide_unavailable: true
}
const reader: Me = {
  name: 'r',
  kind: 'user',
  scopes: ['read'],
  admin: false,
  hide_unavailable: true
}

async function render(me: Me) {
  const sent: { url: string; method: string; body: unknown }[] = []
  vi.stubGlobal('fetch', async (input: Request) => {
    const body = input.method === 'GET' ? undefined : await input.text()
    sent.push({ url: input.url, method: input.method, body: body ? JSON.parse(body) : undefined })
    let out: unknown = entries
    if (input.url.includes('/v1/config/history'))
      out = input.method === 'POST' ? entries[2] : history
    else if (input.method === 'PUT') out = { ...entries[0], value: false }
    else if (input.url.includes('/v1/status')) out = {}
    return new Response(JSON.stringify(out), { headers: { 'Content-Type': 'application/json' } })
  })
  const NativeRequest = globalThis.Request
  vi.stubGlobal(
    'Request',
    class extends NativeRequest {
      constructor(input: RequestInfo | URL, init?: RequestInit) {
        super(
          typeof input === 'string' && input.startsWith('/') ? `http://localhost${input}` : input,
          init
        )
      }
    }
  )
  vi.resetModules()
  const { default: ConfigView } = await import('../ConfigView.vue')
  const pinia = createPinia()
  setActivePinia(pinia)
  useStatus().me = me
  const router = createRouter({ history: createMemoryHistory(), routes })
  const w = mount(ConfigView, { global: { plugins: [pinia, router] } })
  await flushPromises()
  return { w, sent }
}

describe('ConfigView', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('groups a provider’s settings and says where each is stored', async () => {
    const { w } = await render(writer)
    expect(w.text()).toContain('providers.mango-mangaplus')
    expect(w.find('[data-test="key-log.level"]').text()).toContain('mangal.toml')
    expect(w.find('[data-test="key-download.skip_if_exists"]').text()).toContain('database')
  })

  it('edits for an admin, read only for a writer and a reader', async () => {
    const { w, sent } = await render(admin)
    await w.find('[data-test="edit-download.skip_if_exists"]').trigger('change')
    await flushPromises()
    const put = sent.find((s) => s.method === 'PUT')
    expect(put?.url).toContain('/v1/config/download.skip_if_exists')
    expect(put?.body).toEqual({ value: false })

    for (const me of [writer, reader]) {
      const r = await render(me)
      expect(r.w.find('[data-test="key-download.skip_if_exists"]').text()).toContain('true')
      expect(r.w.find('[data-test^="edit-"]').exists()).toBe(false)
      expect(r.w.find('select').exists()).toBe(false)
      expect(r.w.text()).not.toContain('reset to default')
      expect(r.w.find('[data-test=history] button').exists()).toBe(false)
    }
  })

  it('undoes a change from the history', async () => {
    const { w, sent } = await render(admin)
    const button = w.find('[data-test=history] button')
    expect(button.exists()).toBe(true)
    await button.trigger('click')
    await flushPromises()
    expect(
      sent.some((s) => s.method === 'POST' && s.url.includes('/v1/config/history/7/undo'))
    ).toBe(true)
  })
})
