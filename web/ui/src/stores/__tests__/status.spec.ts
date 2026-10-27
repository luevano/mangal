import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useStatus, type Scope } from '../status'
import type { Me } from '@/api/schemas'
import opScopes from '@/api/scopes.json'

const all: Scope[] = ['read', 'progress', 'browse', 'download', 'manage', 'tokens', 'admin']
const me = (kind: Me['kind'], scopes: string[], hide = true): Me => ({ name: kind, kind, scopes, admin: scopes.includes('admin'), hide_unavailable: hide })

describe('status store permissions', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it.each([
    ['auth none', me('none', [...all]), all, true],
    ['local', me('local', [...all]), all, true],
    ['admin user', me('user', [...all]), all, true],
    ['read-only user', me('user', ['read']), ['read'], false],
    ['progress token', me('token', ['read', 'progress']), ['read', 'progress'], false],
    ['manager', me('user', ['read', 'progress', 'browse', 'download', 'manage']), ['read', 'progress', 'browse', 'download', 'manage'], false]
  ] as [string, Me, Scope[], boolean][])('%s', (_, m, allowed, admin) => {
    const st = useStatus()
    st.me = m
    for (const s of all) expect(st.can(s), s).toBe(allowed.includes(s))
    expect(st.canAdmin()).toBe(admin)
    expect(st.allows('GET /v1/users')).toBe(admin)
    expect(st.allows('POST /v1/library/scan')).toBe(allowed.includes('manage'))
    expect(st.allows('POST /v1/library/rename')).toBe(admin)
    expect(st.allows('GET /v1/providers/{provider}/search')).toBe(allowed.includes('browse'))
    expect(st.allows('PUT /v1/reader/position')).toBe(allowed.includes('progress'))
    expect(st.allows('POST /v1/auth/logout')).toBe(true)
  })

  it('treats auth none as everything, whatever its scopes say', () => {
    const st = useStatus()
    st.me = { name: 'none', kind: 'none', scopes: [], admin: false, hide_unavailable: true }
    expect(st.can('manage') && st.canAdmin()).toBe(true)
  })

  it('shows everything with serve.hide_unavailable off', () => {
    const st = useStatus()
    st.me = me('user', ['read'], false)
    expect(all.every((s) => st.can(s))).toBe(true)
    expect(st.canAdmin() && st.allows('POST /v1/users')).toBe(true)
  })
})

// every API call of the UI is an operation of api/scopes.json (go generate ./web), so
// allows() knows what it needs
describe('operation scopes', () => {
  const norm = (p: string) => p.replace(/\{[^}]+\}|\$\{[^}]+\}/g, '{}')
  const known = new Set(Object.keys(opScopes).map(norm))
  function files(dir: string): string[] {
    return readdirSync(dir).flatMap((f) => {
      const p = join(dir, f)
      if (statSync(p).isDirectory()) return f === '__tests__' ? [] : files(p)
      return /\.(vue|ts)$/.test(f) ? [p] : []
    })
  }

  it('knows every operation the UI calls', () => {
    const src = join(__dirname, '../..')
    let calls = 0
    for (const f of files(src)) {
      const text = readFileSync(f, 'utf8')
      const found = [
        ...[...text.matchAll(/client\.(GET|POST|PUT|PATCH|DELETE)\('([^']+)'/g)].map((m) => `${m[1]} ${m[2]}`),
        ...[...text.matchAll(/change\('(POST|PUT|DELETE)', [`']([^`']+)[`']/g)].map((m) => `${m[1]} /v1${m[2]}`),
        ...[...text.matchAll(/allows\('([^']+)'\)/g)].map((m) => m[1]!)
      ]
      for (const op of found) expect(known.has(norm(op)), `${f}: ${op}`).toBe(true)
      calls += found.length
    }
    expect(calls).toBeGreaterThan(50)
  })
})
