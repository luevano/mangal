<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { RouterLink } from 'vue-router'
import client, { ok } from '@/api/client'
import type { Job } from '@/api/schemas'
import LoadState from '@/components/LoadState.vue'
import PagerNav from '@/components/PagerNav.vue'
import { usePager } from '@/composables/usePager'
import { useAction } from '@/composables/useAction'
import { useLoad } from '@/composables/useLoad'
import { date, num } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useStatus } from '@/stores/status'
import { useJobs, activeStatuses, finishedStatuses } from '@/stores/jobs'

crumbs(() => [{ title: 'Downloads' }])
const jobs = useJobs()
const action = useAction()
const st = useStatus()
onMounted(() => jobs.load())

const sorted = computed(() => [...jobs.list].sort((a, b) => b.id - a.id))
const pager = usePager(sorted, 50)
const recent = useLoad(() => ok(client.GET('/v1/library/downloads', { params: { query: { limit: 50 } } })), [() => jobs.finished])

const statusClass: Record<string, string> = { running: 'text-bg-info', waiting: 'text-bg-info', queued: 'text-bg-secondary', done: 'text-bg-success', failed: 'text-bg-danger', cancelled: 'text-bg-dark' }

function cancel(j: Job) {
  action.run(() => ok(client.POST('/v1/jobs/{id}/cancel', { params: { path: { id: j.id } } })), 'Cancelling')
}
function retry(j: Job) {
  action.run(() => ok(client.POST('/v1/jobs/{id}/retry', { params: { path: { id: j.id } } })), (n) => `Queued again as ${n.id}`)
}
function remove(j: Job) {
  action.run(() => ok(client.DELETE('/v1/jobs/{id}', { params: { path: { id: j.id } } }))).then(() => jobs.load())
}
function clearQueue() {
  action.run(() => ok(client.POST('/v1/jobs/clear', { body: {} })), (r) => `${r.count} removed from the queue`).then(() => jobs.load())
}
</script>

<template>
  <div class="d-flex align-items-center mb-2">
    <h4 class="me-auto mb-0">Downloads <small class="text-secondary fs-6">{{ jobs.running }} running · {{ jobs.queued }} queued</small></h4>
    <RouterLink class="btn btn-sm btn-outline-secondary me-2" :to="{ name: 'jobs' }" data-test="to-jobs"><i class="bi bi-list-task me-1" />Jobs</RouterLink>
    <button v-if="st.allows('POST /v1/jobs/clear')" class="btn btn-sm btn-outline-danger" :disabled="!jobs.queued" @click="clearQueue">Clear the queue</button>
  </div>
  <LoadState :loading="!jobs.loaded" :error="jobs.error" :empty="jobs.loaded && !jobs.list.length" empty-text="No jobs (downloads and scans asked here or through the API show here)." />
  <div v-for="j in pager.items.value" :key="j.id" class="card mb-2" data-test="job">
    <div class="card-body py-2">
      <div class="d-flex flex-wrap gap-2 align-items-center">
        <span class="badge" :class="statusClass[j.status] ?? 'text-bg-secondary'">{{ j.status }}</span>
        <span class="small text-secondary">{{ j.kind }} #{{ j.id }}</span>
        <span class="fw-semibold me-auto">{{ j.title }}</span>
        <span v-if="j.total" class="small">{{ j.done ?? 0 }}/{{ j.total }}</span>
        <button v-if="!finishedStatuses.includes(j.status) && st.allows('POST /v1/jobs/{id}/cancel')" class="btn btn-sm btn-outline-danger py-0" @click="cancel(j)">Cancel</button>
        <button v-if="(j.status === 'failed' || j.status === 'cancelled') && st.allows('POST /v1/jobs/{id}/retry')" class="btn btn-sm btn-outline-primary py-0" @click="retry(j)">Retry</button>
        <button v-if="finishedStatuses.includes(j.status) && st.allows('DELETE /v1/jobs/{id}')" class="btn btn-sm btn-outline-secondary py-0" title="Remove from the list" @click="remove(j)"><i class="bi bi-x" /></button>
      </div>
      <div v-if="activeStatuses.includes(j.status) && j.part" class="progress mt-2" style="height: 6px">
        <div class="progress-bar" :style="{ width: `${Math.round((j.part ?? 0) * 100)}%` }" />
      </div>
      <div class="small text-secondary mt-1">
        <span v-if="j.message">{{ j.message }}</span>
        <span v-if="j.note"> · {{ j.note }}</span>
        <span v-if="j.behind"> · behind {{ j.behind }}</span>
        <span v-if="j.error" class="text-danger"> · {{ j.error }}</span>
        <span> · added {{ date(j.added) }}</span>
      </div>
      <ul v-if="j.results?.length" class="small mb-0 mt-1">
        <li v-for="r in j.results" :key="r.chapter + r.title" :class="{ 'text-danger': r.error }">
          {{ num(r.chapter) }} {{ r.title }}
          <span v-if="r.skipped" class="text-secondary"> skipped: {{ r.skipped }}</span>
          <span v-if="r.unavailable" class="text-warning"> unavailable: {{ r.unavailable }}</span>
          <span v-if="r.error"> {{ r.error }}</span>
        </li>
      </ul>
    </div>
  </div>
  <PagerNav v-model="pager.offset.value" :total="pager.total.value" :size="pager.size" />
  <h6 class="mt-4">Downloaded lately</h6>
  <LoadState :loading="recent.loading.value && !recent.data.value" :error="recent.error.value" :empty="recent.data.value && !recent.data.value.downloads.length" empty-text="Nothing downloaded yet." />
  <table v-if="recent.data.value?.downloads.length" class="table table-sm">
    <thead><tr><th>Manga</th><th>Chapter</th><th class="d-none d-md-table-cell">Provider</th><th class="d-none d-md-table-cell">Format</th><th>When</th></tr></thead>
    <tbody>
      <tr v-for="d in recent.data.value.downloads" :key="d.path">
        <td>
          <RouterLink v-if="d.provider && d.manga_id" :to="{ name: 'provider-manga', params: { provider: d.provider, manga: d.manga_id } }">{{ d.manga_title }}</RouterLink>
          <span v-else>{{ d.manga_title }}</span>
        </td>
        <td>{{ num(d.chapter) }} <span class="small text-secondary">{{ d.chapter_title }}</span></td>
        <td class="d-none d-md-table-cell small">{{ d.provider }}</td>
        <td class="d-none d-md-table-cell small">{{ d.format }}</td>
        <td class="small">{{ date(d.downloaded) }}</td>
      </tr>
    </tbody>
  </table>
</template>
