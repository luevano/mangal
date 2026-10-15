import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { usePlan } from '../plan'
import { useStatus } from '../status'
import { useChanges } from '../changes'
import { ApiError } from '@/api/errors'
import type { ApplyFields, ChangeResponse, Plan } from '@/api/schemas'

const plan = (hash: string, extra: Partial<Plan> = {}): Plan => ({ kind: 'merge', lines: ['merge 6 into 5'], hash, ...extra })

describe('plan dialog flow', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('plans, applies with the hash, counts the change', async () => {
    const calls: [boolean, ApplyFields][] = []
    const run = vi.fn(async (apply: boolean, fields: ApplyFields): Promise<ChangeResponse> => {
      calls.push([apply, fields])
      return apply ? { applied: true, result: { kind: 'merge', op: 42, summary: 'merged' }, undo: 'POST /api/v1/ops/42/undo' } : { applied: false, plan: plan('h1') }
    })
    const onApplied = vi.fn()
    const p = usePlan()
    await p.open({ title: 'Merge', run, onApplied })
    expect(p.step).toBe('review')
    expect(p.plan?.lines).toEqual(['merge 6 into 5'])
    await p.apply()
    expect(calls).toEqual([
      [false, {}],
      [true, { expect: 'h1' }]
    ])
    expect(p.step).toBe('done')
    expect(p.result?.undo).toContain('ops/42')
    expect(onApplied).toHaveBeenCalled()
    expect(useChanges().version).toBe(1)
  })

  it('shows a changed plan again, applies the new hash', async () => {
    let n = 0
    const run = vi.fn(async (apply: boolean, fields: ApplyFields): Promise<ChangeResponse> => {
      if (!apply) return { applied: false, plan: plan('h1') }
      if (n++ === 0) throw new ApiError(409, { error: 'the plan changed', code: 'plan_changed', plan: plan('h2', { lines: ['merge 6 into 5', 'and 7'] }) })
      expect(fields.expect).toBe('h2')
      return { applied: true }
    })
    const p = usePlan()
    await p.open({ title: 'Merge', run })
    await p.apply()
    expect(p.step).toBe('review')
    expect(p.plan?.hash).toBe('h2')
    expect(p.notice).toMatch(/changed/)
    await p.apply()
    expect(p.step).toBe('done')
  })

  it('asks for a confirm when the server wants one', async () => {
    const run = vi.fn(async (apply: boolean, fields: ApplyFields): Promise<ChangeResponse> => {
      if (!apply) return { applied: false, plan: plan('h1', { irreversible: true }) }
      if (fields.confirm !== 'empty-trash') throw new ApiError(428, { error: 'confirm it', code: 'confirm_required', confirm: 'empty-trash' })
      return { applied: true }
    })
    const p = usePlan()
    await p.open({ title: 'Empty', run })
    await p.apply()
    expect(p.step).toBe('confirm')
    expect(p.confirmWord).toBe('empty-trash')
    p.confirmed = true
    await p.apply()
    expect(p.step).toBe('done')
  })

  it('needs force for a forceable blocked plan', async () => {
    const run = vi.fn(async (apply: boolean, fields: ApplyFields): Promise<ChangeResponse> => {
      if (!apply) return { applied: false, plan: plan('h1', { blocked: ['op 3 is newer'], forceable: true }) }
      expect(fields.force).toBe(true)
      return { applied: true }
    })
    const p = usePlan()
    await p.open({ title: 'Undo', run })
    p.force = true
    await p.apply()
    expect(p.step).toBe('done')
  })

  it('asks for a token and tries again', async () => {
    let n = 0
    const run = vi.fn(async (apply: boolean): Promise<ChangeResponse> => {
      if (!apply) return { applied: false, plan: plan('h1') }
      if (n++ === 0) throw new ApiError(401, { error: 'this needs a token with the write scope', code: 'unauthorized' })
      return { applied: true }
    })
    const st = useStatus()
    st.askToken = vi.fn(async () => true)
    const p = usePlan()
    await p.open({ title: 'Merge', run })
    await p.apply()
    expect(st.askToken).toHaveBeenCalled()
    expect(p.step).toBe('done')
  })

  it('shows an error', async () => {
    const run = vi.fn(async (): Promise<ChangeResponse> => {
      throw new ApiError(409, { error: 'refused', code: 'refused', reasons: ['the folder is gone'] })
    })
    const p = usePlan()
    await p.open({ title: 'Merge', run })
    expect(p.step).toBe('error')
    expect(p.error).toBe('refused: the folder is gone')
  })
})
