import { describe, expect, it } from 'vitest'
import type { JobKind } from '@/api/schemas'
import { changes, formOf, kindState, nextLabel } from '../jobKinds'

const urls: JobKind = {
  kind: 'urls',
  title: 'Source addresses',
  about: '',
  enabled: true,
  paused: false,
  manual: false,
  schedule: '168h',
  schedule_key: 'jobs.urls.interval',
  no_next: 'paused',
  providers: [
    { provider: 'mango-mangadex', name: 'MangaDex', on: true, items: 3, missing: 1 },
    { provider: 'mango-mangaplus', name: 'MangaPlus', on: false, default: 'off', items: 0, missing: 0 }
  ]
}

describe('job kinds', () => {
  it('says the state and the next run', () => {
    expect(kindState(urls)).toBe('on')
    expect(kindState({ ...urls, paused: true })).toBe('paused')
    expect(kindState({ ...urls, manual: true })).toBe('manual')
    expect(nextLabel(urls)).toBe('paused')
  })

  it('sends only what changed', () => {
    const was = formOf(urls, '1s-3s')
    expect(was.providers).toEqual({ 'mango-mangadex': '', 'mango-mangaplus': 'off' })
    const f = structuredClone(was)
    expect(changes(urls, was, f)).toEqual({})
    f.providers['mango-mangaplus'] = 'On'
    f.providers['mango-mangadex'] = '336h'
    f.schedule = '72h'
    f.delay = '2s'
    f.enabled = false
    expect(changes(urls, was, f)).toEqual({
      enabled: false,
      schedule: '72h',
      delay_min: '2s',
      delay_max: '2s',
      providers: { 'mango-mangaplus': 'on', 'mango-mangadex': '336h' }
    })
  })
})
