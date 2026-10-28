import { describe, expect, it } from 'vitest'
import { sourceURL } from '../format'

describe('sourceURL', () => {
  it('links MangaUpdates by its pages base 36 id', () => {
    expect(sourceURL('mu', '55099564912')).toBe('https://www.mangaupdates.com/series/pb8uwds')
    expect(sourceURL('mu', 'pb8uwds')).toBe('')
    expect(sourceURL('al', '30013')).toBe('https://anilist.co/manga/30013')
  })
})
