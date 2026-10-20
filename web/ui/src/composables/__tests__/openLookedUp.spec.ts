import { describe, expect, it, vi } from 'vitest'
import { openLookedUp } from '../openLookedUp'

function fakeTab() {
  return { opener: {}, location: { href: '' }, close: vi.fn() } as unknown as Window
}

describe('openLookedUp', () => {
  it('opens the tab before the lookup, then sends it to the url', async () => {
    const tab = fakeTab()
    const order: string[] = []
    const open = vi.fn(() => {
      order.push('open')
      return tab
    })
    const url = await openLookedUp(async () => {
      order.push('lookup')
      return 'https://mangadex.org/title/md'
    }, open)
    expect(order).toEqual(['open', 'lookup'])
    expect(url).toBe('https://mangadex.org/title/md')
    expect(tab.location.href).toBe(url)
    expect(tab.opener).toBeNull()
  })

  it('closes the tab when the lookup fails', async () => {
    const tab = fakeTab()
    await expect(
      openLookedUp(
        () => Promise.reject(new Error('gone')),
        () => tab
      )
    ).rejects.toThrow('gone')
    expect(tab.close).toHaveBeenCalled()
  })

  it('opens the url itself when the tab was blocked', async () => {
    const open = vi.fn<(u: string, t: string, f?: string) => Window | null>(() => null)
    await openLookedUp(async () => 'https://x.example/', open)
    expect(open).toHaveBeenLastCalledWith('https://x.example/', '_blank', 'noopener')
  })
})
