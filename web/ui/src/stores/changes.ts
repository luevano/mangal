import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Event } from '@/api/schemas'
import { useEvents } from './events'

// counts the library's changes (op events: applies and undos, through this server), for the
// views to reload what they show.
export const useChanges = defineStore('changes', () => {
  const version = ref(0)
  const last = ref<Event>()

  let off: (() => void) | undefined
  function listen() {
    off ??= useEvents().on('op', (e) => {
      last.value = e
      version.value++
    })
  }

  function bump() {
    version.value++
  }

  return { version, last, listen, bump }
})
