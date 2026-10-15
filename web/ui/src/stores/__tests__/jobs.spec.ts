import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useJobs } from '../jobs'
import type { Event, Job } from '@/api/schemas'

const job = (id: number, status: string): Job => ({ id, kind: 'download', title: `job ${id}`, providers: ['p'], status, added: '' })
const ev = (kind: string, data: Job): Event => ({ seq: 1, time: '', topic: 'job', kind, data })

describe('jobs store', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('follows the job events', () => {
    const j = useJobs()
    j.apply(ev('added', job(1, 'running')))
    j.apply(ev('added', job(2, 'queued')))
    j.apply(ev('added', job(3, 'queued')))
    expect([j.running, j.queued]).toEqual([1, 2])
    j.apply(ev('changed', job(1, 'done')))
    j.apply(ev('changed', job(1, 'done')))
    expect(j.finished).toBe(1)
    j.apply(ev('changed', job(2, 'running')))
    j.apply(ev('removed', job(3, 'queued')))
    expect([j.running, j.queued, j.list.length]).toEqual([1, 0, 2])
  })
})
