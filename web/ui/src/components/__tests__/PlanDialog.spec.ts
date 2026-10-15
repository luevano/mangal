import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import PlanDialog from '../PlanDialog.vue'
import { usePlan } from '@/stores/plan'
import type { ChangeResponse } from '@/api/schemas'

const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }, { path: '/ops', name: 'ops', component: { template: '<div/>' } }] })

describe('PlanDialog', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('shows the plan; a blocked one applies only forced', async () => {
    const p = usePlan()
    await p.open({
      title: 'Undo merge #3',
      run: async (apply): Promise<ChangeResponse> =>
        apply
          ? { applied: true, result: { kind: 'undo', op: 4, summary: 'undone' }, undo: 'mangal library undo 4' }
          : { applied: false, plan: { kind: 'undo', summary: 'undo merge', lines: ['restore 5'], warnings: ['careful'], blocked: ['op 5 came after'], forceable: true, hash: 'h' } }
    })
    const w = mount(PlanDialog, { global: { plugins: [router] } })
    expect(w.find('[data-test=plan-summary]').text()).toBe('undo merge')
    expect(w.find('[data-test=plan-lines]').text()).toContain('restore 5')
    expect(w.text()).toContain('careful')
    expect(w.text()).toContain('op 5 came after')
    const apply = w.find('[data-test=plan-apply]')
    expect(apply.attributes('disabled')).toBeDefined()
    await w.find('#plan-force').setValue(true)
    expect(w.find('[data-test=plan-apply]').attributes('disabled')).toBeUndefined()
    await w.find('[data-test=plan-apply]').trigger('click')
    await new Promise((r) => setTimeout(r, 0))
    expect(w.find('[data-test=plan-done]').text()).toContain('undone')
    expect(w.find('[data-test=plan-undo]').text()).toContain('mangal library undo 4')
  })

  it('is hidden when idle', () => {
    const w = mount(PlanDialog, { global: { plugins: [router] } })
    expect(w.find('[data-test=plan-dialog]').exists()).toBe(false)
  })
})
