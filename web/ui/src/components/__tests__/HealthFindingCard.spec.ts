import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import HealthFindingCard from '../HealthFindingCard.vue'
import type { HealthFinding } from '@/api/schemas'

const router = createRouter({
  history: createMemoryHistory(),
  routes: ['/', '/m/:id', '/w/:id', '/trash'].map((path, i) => ({ path, name: ['home', 'library-manga', 'watch', 'trash'][i], component: { template: '<div/>' } }))
})

function finding(over: Partial<HealthFinding>): HealthFinding {
  return { check: 'M6', category: 'metadata', severity: 'warning', subject: { kind: 'manga', id: 5, name: 'One-Punch Man' }, message: 'm', fix: {}, key: 'k', ...over }
}

describe('HealthFindingCard', () => {
  it('asks which manga stays and sends the choice', async () => {
    const f = finding({ fix: { action: { kind: 'merge', mangas: [5, 6] } } })
    const w = mount(HealthFindingCard, { props: { finding: f }, global: { plugins: [router] } })
    const opts = w.findAll('[data-test=fix-choice] option')
    expect(opts.map((o) => o.text())).toEqual(['keep 5 (suggested)', 'keep 6'])
    await w.find('[data-test=fix-choice]').setValue('6')
    await w.find('[data-test=fix]').trigger('click')
    expect(w.emitted('fix')![0]![1]).toBe('6')
  })

  it("lists a summary's items 50 at a time, each with its fix", async () => {
    const items = Array.from({ length: 120 }, (_, i) =>
      finding({ check: 'W10', key: `W10/manga/${i}`, subject: { kind: 'manga', id: i + 1, name: `Manga ${i}` }, fix: { action: { kind: 'watch', provider: 'p' } } })
    )
    const w = mount(HealthFindingCard, { props: { finding: finding({ check: 'W10', subject: { kind: 'library', name: 'library' }, items }) }, global: { plugins: [router] } })
    expect(w.findAll('[data-test=finding-item]')).toHaveLength(0)
    await w.find('[data-test=items]').trigger('click')
    expect(w.findAll('[data-test=finding-item]')).toHaveLength(50)
    expect(w.find('[data-test=pager]').text()).toContain('1–50 of 120')
    await w.find('[data-test=pager-next]').trigger('click')
    expect(w.find('[data-test=finding-item]').text()).toContain('Manga 50')
    await w.find('[data-test=finding-item] [data-test=fix]').trigger('click')
    expect((w.emitted('fix')![0]![0] as HealthFinding).key).toBe('W10/manga/50')
  })
})
