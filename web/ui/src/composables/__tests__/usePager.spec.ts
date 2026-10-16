import { describe, expect, it } from 'vitest'
import { nextTick, ref } from 'vue'
import { usePager } from '../usePager'

describe('usePager', () => {
  it('pages the list, back to the first page when it shrinks', async () => {
    const list = ref<number[] | undefined>(Array.from({ length: 250 }, (_, i) => i))
    const p = usePager(list, 100)
    expect(p.items.value).toHaveLength(100)
    p.offset.value = 200
    expect(p.items.value).toEqual(Array.from({ length: 50 }, (_, i) => 200 + i))
    list.value = [1, 2, 3]
    await nextTick()
    expect(p.offset.value).toBe(0)
    expect(p.items.value).toEqual([1, 2, 3])
    list.value = undefined
    expect(p.total.value).toBe(0)
  })
})
