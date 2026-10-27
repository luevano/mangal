import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import AppNav from '@/components/AppNav.vue'
import LibraryView from '../LibraryView.vue'
import { routes } from '@/router'
import { useStatus } from '@/stores/status'
import type { Me } from '@/api/schemas'

const reader: Me = { name: 'guest', kind: 'user', scopes: ['read'], admin: false, hide_unavailable: true }
const admin: Me = { name: 'david', kind: 'user', scopes: ['read', 'progress', 'browse', 'download', 'manage', 'tokens', 'admin'], admin: true, hide_unavailable: true }

async function render(component: object, me: Me) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useStatus().me = me
  const router = createRouter({ history: createMemoryHistory(), routes })
  const w = mount(component, { global: { plugins: [pinia, router], stubs: { StatusBadge: true } } })
  await flushPromises()
  return w
}

describe('actions by permission', () => {
  beforeEach(() => vi.stubGlobal('fetch', () => Promise.reject(new Error('offline'))))
  afterEach(() => vi.unstubAllGlobals())

  it("hides the library's actions from a read-only user", async () => {
    expect((await render(LibraryView, reader)).find('[data-test=scan]').exists()).toBe(false)
    expect((await render(LibraryView, admin)).find('[data-test=scan]').exists()).toBe(true)
  })

  it('shows every action with serve.hide_unavailable off', async () => {
    expect((await render(LibraryView, { ...reader, hide_unavailable: false })).find('[data-test=scan]').exists()).toBe(true)
  })

  it('lists Users for an admin and the tokens permission', async () => {
    expect((await render(AppNav, reader)).find('[data-test=nav-users]').exists()).toBe(false)
    expect((await render(AppNav, { ...reader, scopes: ['read', 'tokens'] })).find('[data-test=nav-users]').exists()).toBe(true)
    expect((await render(AppNav, admin)).find('[data-test=nav-users]').exists()).toBe(true)
    expect((await render(AppNav, { name: 'none', kind: 'none', scopes: [], admin: false, hide_unavailable: true })).find('[data-test=nav-users]').exists()).toBe(true)
  })

  it('hides health, logs and accounts from a reader', async () => {
    const nav = await render(AppNav, reader)
    for (const l of ['health', 'logs', 'accounts']) expect(nav.find(`[data-test=nav-${l}]`).exists(), l).toBe(false)
    expect(nav.find('[data-test=nav-library]').exists()).toBe(true)
  })
})
