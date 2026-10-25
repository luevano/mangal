import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { readableMs, toastMax, toastMin, toastPerChar, useToasts } from '../toasts'
import NotificationBell from '@/components/NotificationBell.vue'

describe('toasts', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
  })
  afterEach(() => vi.useRealTimers())

  it('stays the time it takes to read, longer for warnings and errors', () => {
    expect(readableMs('info', 'Saved')).toBe(toastMin + 5 * toastPerChar)
    expect(readableMs('warning', 'Saved')).toBe((toastMin + 5 * toastPerChar) * 1.5)
    expect(readableMs('danger', 'x'.repeat(2000))).toBe(toastMax)
  })

  it('takes a longer asked time, never a shorter one; 0 stays', () => {
    const t = useToasts()
    const short = t.push('info', 'Saved', 1000)
    const long = t.push('info', 'Saved', 60000)
    const sticky = t.push('info', 'Looking…', 0)
    vi.advanceTimersByTime(readableMs('info', 'Saved'))
    expect(t.list.map((x) => x.id)).toEqual([long, sticky])
    expect(t.list.some((x) => x.id === short)).toBe(false)
    vi.advanceTimersByTime(60000)
    expect(t.list.map((x) => x.id)).toEqual([sticky])
  })

  it('pauses while hovered', () => {
    const t = useToasts()
    const id = t.push('info', 'Saved')
    vi.advanceTimersByTime(1000)
    t.pause(id)
    vi.advanceTimersByTime(60000)
    expect(t.list).toHaveLength(1)
    t.resume(id)
    vi.advanceTimersByTime(readableMs('info', 'Saved') - 1000)
    expect(t.list).toHaveLength(0)
  })

  it('keeps every notification, newest first, the bell counts the unread', async () => {
    const t = useToasts()
    t.push('info', 'first')
    t.push('danger', 'second')
    vi.advanceTimersByTime(toastMax)
    expect(t.list).toHaveLength(0)
    expect(t.history.map((n) => n.text)).toEqual(['second', 'first'])

    const w = mount(NotificationBell)
    expect(w.find('[data-test=unread]').text()).toBe('2')
    await w.find('[data-test=bell]').trigger('click')
    expect(w.find('[data-test=history]').classes()).toContain('show')
    expect(w.findAll('li').map((li) => li.text())).toEqual([expect.stringMatching(/error\s*second$/), expect.stringMatching(/info\s*first$/)])
    expect(w.find('[data-test=unread]').exists()).toBe(false)
  })
})
