<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import client, { ok } from '@/api/client'
import type { JobKind, JobProvider } from '@/api/schemas'
import LoadState from '@/components/LoadState.vue'
import JobSettingsDialog from '@/components/JobSettingsDialog.vue'
import { useAction } from '@/composables/useAction'
import { useLoad } from '@/composables/useLoad'
import { ago, date } from '@/composables/format'
import { kindState, nextLabel, stateClass } from '@/composables/jobKinds'
import { crumbs } from '@/stores/crumbs'
import { useJobs, finishedStatuses } from '@/stores/jobs'
import { useStatus } from '@/stores/status'

const route = useRoute()
const kind = computed(() => String(route.params.kind))
const jobs = useJobs()
const st = useStatus()
const action = useAction()
const admin = computed(() => st.scopes.length >= 4)

const k = useLoad(() => ok(client.GET('/v1/jobs/kinds/{kind}', { params: { path: { kind: kind.value } } })), [kind, () => jobs.finished, () => jobs.running])
crumbs(() => [{ title: 'Jobs', to: { name: 'jobs' } }, { title: k.data.value?.title ?? kind.value }])
// its jobs here, the newest first
const recent = computed(() => jobs.list.filter((j) => j.kind === kind.value).sort((a, b) => b.id - a.id))
const items = computed(() => ({ check: 'watches', sources: 'sources', urls: 'sources' })[kind.value] ?? 'items')

function run(all: boolean) {
  action
    .run(() => ok(client.POST('/v1/jobs/kinds/{kind}/run', { params: { path: { kind: kind.value } }, body: { all } })), (j) => `Started ${j.title}`)
    .then(() => k.load())
}
function pause(j: JobKind) {
  const path = j.paused ? '/v1/jobs/kinds/{kind}/resume' : '/v1/jobs/kinds/{kind}/pause'
  action.run(() => ok(client.POST(path, { params: { path: { kind: kind.value } } }))).then(() => k.load())
}
// toggle turns the provider on or off for the kind (an admin's)
function toggle(p: JobProvider) {
  action
    .run(
      () => ok(client.PUT('/v1/jobs/kinds/{kind}/settings', { params: { path: { kind: kind.value } }, body: { providers: { [p.provider]: p.on ? 'off' : 'on' } } })),
      `${p.name} ${p.on ? 'off' : 'on'}`
    )
    .then(() => k.load())
}
const editing = ref(false)
</script>

<template>
  <LoadState :loading="k.loading.value && !k.data.value" :error="k.error.value" />
  <template v-if="k.data.value">
    <div class="d-flex flex-wrap align-items-center gap-2 mb-2">
      <h4 class="me-auto mb-0">{{ k.data.value.title }} <span class="badge fs-6" :class="stateClass[kindState(k.data.value)]">{{ kindState(k.data.value) }}</span></h4>
      <button v-if="k.data.value.missing !== undefined" class="btn btn-sm btn-primary" :disabled="!!k.data.value.running || !k.data.value.missing" @click="run(false)">Run missing</button>
      <button class="btn btn-sm btn-outline-primary" :disabled="!!k.data.value.running" @click="run(true)">Run all</button>
      <button v-if="!k.data.value.manual" class="btn btn-sm btn-outline-secondary" @click="pause(k.data.value)">{{ k.data.value.paused ? 'Resume' : 'Pause' }}</button>
      <button v-if="admin" class="btn btn-sm btn-outline-secondary" @click="editing = true"><i class="bi bi-gear me-1" />Settings</button>
    </div>
    <p class="text-secondary">{{ k.data.value.about }}</p>
    <dl class="row small">
      <dt class="col-sm-3">Last run</dt>
      <dd class="col-sm-9">
        <template v-if="k.data.value.last">
          {{ date(k.data.value.last.finished || k.data.value.last.started) }} ({{ k.data.value.last.all ? 'all' : 'missing' }}):
          <span v-if="k.data.value.last.error" class="text-danger">{{ k.data.value.last.error }}</span>
          <span v-else>{{ k.data.value.last.summary }}</span>
          <RouterLink v-if="kind === 'check' && k.data.value.last.run" class="ms-1" :to="{ name: 'run', params: { id: k.data.value.last.run } }">run {{ k.data.value.last.run }}</RouterLink>
          <RouterLink v-else-if="(kind === 'sources' || kind === 'health') && k.data.value.last.run" class="ms-1" :to="{ name: 'health' }">findings</RouterLink>
        </template>
        <span v-else>never</span>
      </dd>
      <dt class="col-sm-3">Next run</dt>
      <dd class="col-sm-9">{{ nextLabel(k.data.value) }}</dd>
      <template v-if="k.data.value.schedule">
        <dt class="col-sm-3">Schedule</dt>
        <dd class="col-sm-9"><code>{{ k.data.value.schedule }}</code> <span class="text-secondary">{{ k.data.value.schedule_key }}</span></dd>
      </template>
      <template v-if="k.data.value.delay">
        <dt class="col-sm-3">Pause between requests</dt>
        <dd class="col-sm-9">{{ k.data.value.delay }}</dd>
      </template>
      <template v-if="k.data.value.missing !== undefined">
        <dt class="col-sm-3">Missing</dt>
        <dd class="col-sm-9">{{ k.data.value.missing }} {{ k.data.value.missing_what }}</dd>
      </template>
    </dl>
    <p v-if="kind === 'check'" class="small">
      The watches and their next checks: <RouterLink :to="{ name: 'watches' }">Watches</RouterLink>, the past checks: <RouterLink :to="{ name: 'runs' }">check runs</RouterLink>.
    </p>
    <p v-else-if="kind === 'sources' || kind === 'health'" class="small">What it found: <RouterLink :to="{ name: 'health' }">Health</RouterLink>.</p>
    <table v-if="k.data.value.providers?.length" class="table table-sm small" data-test="kind-providers">
      <thead>
        <tr>
          <th>Provider</th>
          <th>On</th>
          <th>Interval</th>
          <th v-if="!k.data.value.manual" class="text-end">{{ items }}</th>
          <th v-if="!k.data.value.manual" class="text-end">Missing</th>
          <th v-if="!k.data.value.manual" class="d-none d-md-table-cell">Next</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="p in k.data.value.providers" :key="p.provider" :class="{ 'text-secondary': !p.on }">
          <td>{{ p.name }} <span class="text-secondary d-none d-md-inline">{{ p.provider }}</span></td>
          <td>
            <div class="form-check form-switch mb-0">
              <input class="form-check-input" type="checkbox" role="switch" :checked="p.on" :disabled="!admin" :aria-label="`${p.name} on`" @change="toggle(p)" />
            </div>
            <span v-if="p.default" class="text-secondary">default {{ p.default }}</span>
          </td>
          <td>{{ p.interval || '-' }}</td>
          <td v-if="!k.data.value.manual" class="text-end">{{ p.items }}</td>
          <td v-if="!k.data.value.manual" class="text-end">{{ p.missing }}</td>
          <td v-if="!k.data.value.manual" class="d-none d-md-table-cell">{{ p.next ? date(p.next) : '-' }}</td>
        </tr>
      </tbody>
    </table>
    <h6 v-if="recent.length" class="mt-3">Its jobs here</h6>
    <ul v-if="recent.length" class="list-unstyled small">
      <li v-for="j in recent" :key="j.id">
        #{{ j.id }} {{ j.title }} · {{ j.status }}<span v-if="j.message"> · {{ j.message }}</span>
        <span v-if="finishedStatuses.includes(j.status) && j.finished" class="text-secondary"> · {{ ago(j.finished) }}</span>
        <span v-if="j.error" class="text-danger"> · {{ j.error }}</span>
      </li>
    </ul>
    <JobSettingsDialog :open="editing" :kind="k.data.value" @close="editing = false" @done="(editing = false), k.load()" />
  </template>
</template>
