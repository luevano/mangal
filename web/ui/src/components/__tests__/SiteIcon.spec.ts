import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SiteIcon from '../SiteIcon.vue'

describe('SiteIcon', () => {
  it("shows the provider's or source's icon, nothing once it fails", async () => {
    const w = mount(SiteIcon, { props: { provider: 'mango-mangadex', title: 'MangaDex' } })
    expect(w.find('img').attributes()).toMatchObject({ src: '/api/v1/providers/mango-mangadex/icon', alt: '', title: 'MangaDex', loading: 'lazy' })
    await w.find('img').trigger('error')
    expect(w.find('img').exists()).toBe(false)
    // another site gets its own try
    await w.setProps({ provider: undefined, source: 'al' })
    expect(w.find('img').attributes('src')).toBe('/api/v1/metadata/al/icon')
  })
})
