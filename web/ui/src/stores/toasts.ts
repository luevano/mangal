import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface Toast {
  id: number
  kind: 'info' | 'success' | 'warning' | 'danger'
  text: string
}

export interface Notice extends Toast {
  at: Date
}

// the TUI's rule (tui/base/notifications.go): a minimum plus the time to read it, longer
// for a warning or an error, at most toastMax
export const toastMin = 4000
export const toastPerChar = 60
export const toastMax = 30000

export function readableMs(kind: Toast['kind'], text: string) {
  let ms = toastMin + [...text].length * toastPerChar
  if (kind === 'warning' || kind === 'danger') ms *= 1.5
  return Math.min(ms, toastMax)
}

// shortcut: this browser session's last kept only, in memory; persist them if they're wanted across reloads
const kept = 200

export const useToasts = defineStore('toasts', () => {
  const list = ref<Toast[]>([])
  // newest first
  const history = ref<Notice[]>([])
  const unread = ref(0)
  const timers = new Map<number, { timer?: ReturnType<typeof setTimeout>; left: number; since: number }>()
  let next = 1

  // ms is at least the readable time, 0 keeps it until dismissed
  function push(kind: Toast['kind'], text: string, ms?: number) {
    const id = next++
    list.value.push({ id, kind, text })
    history.value.unshift({ id, kind, text, at: new Date() })
    history.value.splice(kept)
    unread.value++
    if (ms !== 0) {
      timers.set(id, { left: Math.max(ms ?? 0, readableMs(kind, text)), since: 0 })
      resume(id)
    }
    return id
  }

  function dismiss(id: number) {
    clearTimeout(timers.get(id)?.timer)
    timers.delete(id)
    list.value = list.value.filter((t) => t.id !== id)
  }

  // while the pointer is on it
  function pause(id: number) {
    const t = timers.get(id)
    if (!t?.timer) return
    clearTimeout(t.timer)
    t.timer = undefined
    t.left -= Date.now() - t.since
  }

  function resume(id: number) {
    const t = timers.get(id)
    if (!t || t.timer) return
    t.since = Date.now()
    t.timer = setTimeout(() => dismiss(id), Math.max(t.left, 0))
  }

  function markRead() {
    unread.value = 0
  }

  return { list, history, unread, push, dismiss, pause, resume, markRead }
})
