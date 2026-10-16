import { beforeEach, describe, expect, it } from 'vitest'
import { clampPage, keyAction, loadSettings, neighbors, nextFit, preload, saveSettings, step, zoneAction, loadPosition, savePosition, newer } from '../reader'

describe('reader', () => {
  beforeEach(() => localStorage.clear())

  it('maps the arrows to the direction', () => {
    expect(keyAction('ArrowRight', 'ltr')).toBe('next')
    expect(keyAction('ArrowRight', 'rtl')).toBe('prev')
    expect(keyAction('ArrowLeft', 'rtl')).toBe('next')
    expect(keyAction('ArrowDown', 'ltr')).toBe('next')
    expect(keyAction('ArrowDown', 'webtoon')).toBeUndefined()
    expect(keyAction(' ', 'webtoon')).toBeUndefined()
    expect(keyAction('w', 'ltr')).toBe('mode-webtoon')
    expect(keyAction('N', 'rtl')).toBe('next-chapter')
    expect(keyAction('x', 'ltr')).toBeUndefined()
  })

  it('maps the tap zones', () => {
    expect(zoneAction(10, 300, 'ltr')).toBe('prev')
    expect(zoneAction(290, 300, 'ltr')).toBe('next')
    expect(zoneAction(10, 300, 'rtl')).toBe('next')
    expect(zoneAction(150, 300, 'rtl')).toBe('menu')
  })

  it('steps and stops at the ends', () => {
    expect(step(0, 5, 'next')).toEqual({ index: 1 })
    expect(step(4, 5, 'next')).toEqual({ index: 4, edge: 'end' })
    expect(step(0, 5, 'prev')).toEqual({ index: 0, edge: 'start' })
    expect(step(2, 5, 'last')).toEqual({ index: 4 })
    expect(step(2, 5, 'first')).toEqual({ index: 0 })
  })

  it('preloads ±2, nearest first', () => {
    expect(preload(0, 10)).toEqual([1, 2])
    expect(preload(5, 10)).toEqual([6, 4, 7, 3])
    expect(preload(9, 10)).toEqual([8, 7])
  })

  it('finds the neighbors, clamps a jump, cycles the fit', () => {
    expect(neighbors(['a', 'b', 'c'], 1)).toEqual({ prev: 'a', next: 'c' })
    expect(neighbors(['a', 'b', 'c'], 0)).toEqual({ prev: undefined, next: 'b' })
    expect(neighbors(['a'], -1)).toEqual({})
    expect(clampPage(0, 10)).toBe(0)
    expect(clampPage(12, 10)).toBe(9)
    expect(clampPage(3, 10)).toBe(2)
    expect(nextFit('original')).toBe('screen')
  })

  it('remembers the settings per manga, the last as the default', () => {
    expect(loadSettings('lib:1').mode).toBe('ltr')
    saveSettings('lib:1', { mode: 'webtoon', fit: 'width', background: 'gray' })
    expect(loadSettings('lib:1')).toEqual({ mode: 'webtoon', fit: 'width', background: 'gray' })
    expect(loadSettings('lib:2').mode).toBe('webtoon')
    saveSettings('lib:2', { mode: 'rtl', fit: 'screen', background: 'black' })
    expect(loadSettings('lib:1').mode).toBe('webtoon')
    localStorage.setItem('mangal.reader.manga.lib:3', '{"mode":"nope"}')
    expect(loadSettings('lib:3').mode).toBe('rtl')
    savePosition('c', 4, 100)
    expect(loadPosition('c')).toEqual({ page: 4, at: 100 })
    expect(loadPosition('d')).toEqual({ page: 0, at: 0 })
    // saved by an older version: a plain number
    localStorage.setItem('mangal.reader.pos.e', '7')
    expect(loadPosition('e')).toEqual({ page: 7, at: 0 })
    expect(newer({ page: 4, at: 100 }, { page: 9, at: 200 })).toEqual({ page: 9, at: 200 })
    expect(newer({ page: 4, at: 300 }, { page: 9, at: 200 })).toEqual({ page: 4, at: 300 })
    expect(newer({ page: 4, at: 300 })).toEqual({ page: 4, at: 300 })
  })
})
