import { describe, expect, it } from 'vitest'
import { ApiError, mangaGone } from '../errors'

describe('mangaGone', () => {
  it('is a 404 of the manga, not of the provider or another error', () => {
    expect(mangaGone(new ApiError(404, { error: 'no manga with id "x" in mango-mangadex', code: 'not_found' }))).toBe(true)
    expect(mangaGone(new ApiError(404, { error: 'provider "nope": not found', code: 'not_found' }))).toBe(false)
    expect(mangaGone(new ApiError(422, { error: 'site down', code: 'unprocessable' }))).toBe(false)
    expect(mangaGone(new Error('boom'))).toBe(false)
  })
})
