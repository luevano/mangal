import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Event } from '@/api/schemas'
import { useEvents } from './events'

// counts the library's changes, for the views to reload what they show: op events (applies
// and undos through this server) and the library or read history changing in any process
// (the CLI, the TUI, a watch check).
export const useChanges = defineStore('changes', () => {
  const version = ref(0)
  const last = ref<Event>()

  let off: (() => void) | undefined
  function listen() {
    if (off) return
    const events = useEvents()
    const offs = [
      events.on('op', (e) => {
        last.value = e
        version.value++
      }),
      events.on('library', bump),
      events.on('history', bump)
    ]
    off = () => offs.forEach((o) => o())
  }

  function bump() {
    version.value++
  }

  return { version, last, listen, bump }
})
