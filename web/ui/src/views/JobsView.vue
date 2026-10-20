<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import client, { ok } from '@/api/client'
import type { Job, JobKind } from '@/api/schemas'
import LoadState from '@/components/LoadState.vue'
import JobSettingsDialog from '@/components/JobSettingsDialog.vue'
import { useAction } from '@/composables/useAction'
import { useLoad } from '@/composables/useLoad'
import { ago, date } from '@/composables/format'
import { kindState, nextLabel, stateClass } from '@/composables/jobKinds'
import { crumbs } from '@/stores/crumbs'
import { useJobs, activeStatuses } from '@/stores/jobs'
import { useStatus } from '@/stores/status'

crumbs(() => [{ title: 'Jobs' }])
const jobs = useJobs()
const st = useStatus()
const action = useAction()
// an admin changes the settings (every permission)
const admin = computed(() => st.scopes.length >= 4)

const kinds = useLoad(() => ok(client.GET('/v1/jobs/kinds')), [() => jobs.finished, () => jobs.running, () => jobs.queued])
const downloads = computed(() => {
  const d = jobs.list.filter((j) => j.kind === 'download')
  return { running: d.filter((j) => activeStatuses.includes(j.status)).length, queued: d.filter((j) => j.status === 'queued').length }
})
// a running kind's job as the events say it now
function live(k: JobKind): Job | undefined {
  return k.running ? (jobs.list.find((j) => j.id === k.running!.id) ?? k.running) : undefined
}

function run(k: JobKind, all: boolean) {
  action
    .run(() => ok(client.POST('/v1/jobs/kinds/{kind}/run', { params: { path: { kind: k.kind } }, body: { all } })), (j) => `Started ${j.title}`)
    .then(() => kinds.load())
}
function pause(k: JobKind) {
  const path = k.paused ? '/v1/jobs/kinds/{kind}/resume' : '/v1/jobs/kinds/{kind}/pause'
  action.run(() => ok(client.POST(path, { params: { path: { kind: k.kind } } })), k.paused ? `${k.title} resumed` : `${k.title} paused`).then(() => kinds.load())
}
const editing = ref<JobKind>()
</script>

<template>
  <div class="d-flex align-items-center mb-3">
    <h4 class="me-auto mb-0">Jobs <small class="text-secondary fs-6">the maintenance jobs; mangal serve runs their schedules</small></h4>
  </div>
  <RouterLink :to="{ name: 'downloads' }" class="card mb-3 text-decoration-none" data-test="downloads-card">
    <div class="card-body py-2 d-flex align-items-center gap-2">
      <i class="bi bi-download" />
      <span class="fw-semibold">Downloads:</span>
      <span>{{ downloads.running }} running, {{ downloads.queued }} queued</span>
      <i class="bi bi-arrow-right ms-auto" />
    </div>
  </RouterLink>
  <LoadState :loading="kinds.loading.value && !kinds.data.value" :error="kinds.error.value" />
  <div class="row row-cols-1 row-cols-lg-2 g-3">
    <div v-for="k in kinds.data.value ?? []" :key="k.kind" class="col">
      <div class="card h-100" :data-test="`kind-${k.kind}`">
        <div class="card-header d-flex align-items-center gap-2">
          <span class="fw-semibold me-auto">{{ k.title }}</span>
          <span class="badge" :class="stateClass[kindState(k)]">{{ kindState(k) }}</span>
        </div>
        <div class="card-body small">
          <p class="text-secondary mb-2">{{ k.about }}</p>
          <div v-if="live(k)" class="mb-2" data-test="kind-running">
            <div>{{ live(k)!.message || live(k)!.title }}<span v-if="live(k)!.total"> · {{ live(k)!.done ?? 0 }}/{{ live(k)!.total }}</span></div>
            <div v-if="live(k)!.total" class="progress mt-1" style="height: 6px">
              <div class="progress-bar" :style="{ width: `${Math.round(((live(k)!.done ?? 0) / live(k)!.total!) * 100)}%` }" />
            </div>
          </div>
          <div v-else-if="k.busy" class="mb-2">{{ k.busy }}</div>
          <dl class="row mb-0">
            <dt class="col-4">Last run</dt>
            <dd class="col-8" data-test="kind-last">
              <template v-if="k.last">
                <span :title="date(k.last.finished || k.last.started)">{{ ago(k.last.finished || k.last.started) }}</span>
                <span v-if="k.last.error" class="text-danger"> · {{ k.last.error }}</span>
                <span v-else-if="k.last.summary"> · {{ k.last.summary }}</span>
                <RouterLink v-if="k.kind === 'check' && k.last.run" class="ms-1" :to="{ name: 'run', params: { id: k.last.run } }">run {{ k.last.run }}</RouterLink>
                <RouterLink v-else-if="(k.kind === 'sources' || k.kind === 'health') && k.last.run" class="ms-1" :to="{ name: 'health' }">findings</RouterLink>
              </template>
              <span v-else class="text-secondary">never</span>
            </dd>
            <dt class="col-4">Next run</dt>
            <dd class="col-8" data-test="kind-next">{{ nextLabel(k) }}</dd>
            <template v-if="k.schedule">
              <dt class="col-4">Schedule</dt>
              <dd class="col-8"><code>{{ k.schedule }}</code></dd>
            </template>
            <template v-if="k.missing !== undefined">
              <dt class="col-4">Missing</dt>
              <dd class="col-8">{{ k.missing }} <span class="text-secondary">{{ k.missing_what }}</span></dd>
            </template>
          </dl>
        </div>
        <div class="card-footer d-flex flex-wrap gap-2">
          <button v-if="k.missing !== undefined" class="btn btn-sm btn-primary" :disabled="!!k.running || !k.missing" data-test="run-missing" @click="run(k, false)">
            <i class="bi bi-play me-1" />Run missing
          </button>
          <button class="btn btn-sm btn-outline-primary" :disabled="!!k.running" data-test="run-all" @click="run(k, true)"><i class="bi bi-play-fill me-1" />Run all</button>
          <button v-if="!k.manual" class="btn btn-sm btn-outline-secondary" data-test="pause" @click="pause(k)">
            <i class="bi me-1" :class="k.paused ? 'bi-play-circle' : 'bi-pause-circle'" />{{ k.paused ? 'Resume' : 'Pause' }}
          </button>
          <button v-if="admin" class="btn btn-sm btn-outline-secondary" data-test="settings" @click="editing = k"><i class="bi bi-gear me-1" />Settings</button>
          <RouterLink class="btn btn-sm btn-link ms-auto" :to="{ name: 'job-kind', params: { kind: k.kind } }">Details</RouterLink>
        </div>
      </div>
    </div>
  </div>
  <JobSettingsDialog v-if="editing" :open="!!editing" :kind="editing" @close="editing = undefined" @done="(editing = undefined), kinds.load()" />
</template>
