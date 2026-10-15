import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import client, { ok } from '@/api/client'
import { errorMessage } from '@/api/errors'
import type { Event, Job } from '@/api/schemas'
import { useEvents } from './events'

export const activeStatuses = ['running', 'waiting']
export const finishedStatuses = ['done', 'failed', 'cancelled']

// the server's jobs (downloads, scans), kept up to date by the job events.
export const useJobs = defineStore('jobs', () => {
  const list = ref<Job[]>([])
  const error = ref('')
  const loaded = ref(false)
  // the jobs that finished since the page listened, for the views that refresh on them
  const finished = ref(0)

  const running = computed(() => list.value.filter((j) => activeStatuses.includes(j.status)).length)
  const queued = computed(() => list.value.filter((j) => j.status === 'queued').length)

  async function load() {
    try {
      list.value = await ok(client.GET('/v1/jobs'))
      error.value = ''
    } catch (e) {
      error.value = errorMessage(e)
    }
    loaded.value = true
  }

  function apply(e: Event) {
    const job = e.data as Job | undefined
    if (!job || typeof job.id !== 'number') return
    const i = list.value.findIndex((j) => j.id === job.id)
    if (e.kind === 'removed') {
      if (i >= 0) list.value.splice(i, 1)
      return
    }
    const was = i >= 0 ? list.value[i] : undefined
    if (i >= 0) list.value[i] = job
    else list.value.push(job)
    if (finishedStatuses.includes(job.status) && (!was || !finishedStatuses.includes(was.status))) {
      finished.value++
    }
  }

  let off: (() => void) | undefined
  function listen() {
    off ??= useEvents().on('job', apply)
  }

  return { list, error, loaded, running, queued, finished, load, apply, listen }
})
