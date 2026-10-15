import { describe, expect, it } from 'vitest'
import { routes } from '../index'
import features from '@/features.json'

// features.json is core.Features (go generate ./core): the web UI keeps parity with the
// other modes through it.
describe('feature registry', () => {
  const names = routes.map((r) => r.name).filter((n): n is string => typeof n === 'string')

  it('has every route a feature lists', () => {
    for (const f of features.features) {
      for (const r of f.web ?? []) expect(names, `${f.name}: route ${r}`).toContain(r)
    }
  })

  it("has every route in a feature, or as the UI's own", () => {
    const listed = new Set(features.features.flatMap((f) => f.web ?? []))
    for (const n of names) {
      expect(listed.has(n) || n in features.routes, `route ${n} is no feature's`).toBe(true)
    }
  })

  it('says why a feature has no route', () => {
    for (const f of features.features) {
      expect((f.web?.length ?? 0) > 0 || !!f.not_applicable, f.name).toBe(true)
    }
  })
})
