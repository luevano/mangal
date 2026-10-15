import { defineStore } from 'pinia'
import { ref } from 'vue'
import client, { ok } from '@/api/client'
import { errorMessage } from '@/api/errors'
import type { Event, LiveEvent, Watch } from '@/api/schemas'
import { useEvents } from './events'

const keep = 500

// the watches and the live events of their checks.
export const useWatches = defineStore('watches', () => {
  const list = ref<Watch[]>([])
  const error = ref('')
  const loaded = ref(false)
  const live = ref<LiveEvent[]>([])
  // bumped when a check ends: the views reload their runs
  const runsVersion = ref(0)
  let timer: ReturnType<typeof setTimeout> | undefined

  async function load() {
    try {
      list.value = await ok(client.GET('/v1/watches'))
      error.value = ''
    } catch (e) {
      error.value = errorMessage(e)
    }
    loaded.value = true
  }

  function reloadSoon() {
    clearTimeout(timer)
    timer = setTimeout(load, 500)
  }

  function apply(e: Event) {
    const le = e.data as LiveEvent | undefined
    if (!le) return
    if (le.kind !== 'progress') {
      live.value.push(le)
      if (live.value.length > keep) live.value.splice(0, live.value.length - keep)
    }
    if (['done', 'run_done', 'provider_done'].includes(le.kind)) {
      runsVersion.value++
      reloadSoon()
    }
  }

  let off: (() => void) | undefined
  function listen() {
    off ??= useEvents().on('watch', apply)
  }

  return { list, error, loaded, live, runsVersion, load, apply, listen, reloadSoon }
})
