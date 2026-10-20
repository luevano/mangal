import { describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from '../index'

describe('routes', () => {
  it('has the downloads at /downloads, the old /jobs/downloads redirects there', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/jobs/downloads')
    expect(router.currentRoute.value.name).toBe('downloads')
    expect(router.currentRoute.value.path).toBe('/downloads')
  })
})
