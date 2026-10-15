<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import client, { ok } from '@/api/client'
import { change } from '@/api/changes'
import { coverURL } from '@/api/urls'
import LoadState from '@/components/LoadState.vue'
import MetaIDs from '@/components/MetaIDs.vue'
import { useLoad } from '@/composables/useLoad'
import { useAction } from '@/composables/useAction'
import { crumbs } from '@/stores/crumbs'
import { useChanges } from '@/stores/changes'
import { useJobs } from '@/stores/jobs'
import { usePlan } from '@/stores/plan'

crumbs(() => [{ title: 'Library' }])

const route = useRoute()
const router = useRouter()
const changes = useChanges()
const jobs = useJobs()
const plan = usePlan()
const action = useAction()

const pageSize = 100
const q = ref(String(route.query.q ?? ''))
const status = ref(String(route.query.status ?? ''))
const provider = ref(String(route.query.provider ?? ''))
const watched = ref(String(route.query.watched ?? ''))
const duplicates = ref(route.query.duplicates === 'true')
const offset = ref(Number(route.query.offset ?? 0))

const filters = computed(() => ({
  q: q.value || undefined,
  status: status.value || undefined,
  provider: provider.value || undefined,
  watched: watched.value === '' ? undefined : watched.value === 'true',
  duplicates: duplicates.value || undefined,
  limit: pageSize,
  offset: offset.value || undefined
}))

watch(filters, (f) => {
  const query: Record<string, string> = {}
  for (const [k, v] of Object.entries(f)) if (v !== undefined && k !== 'limit') query[k] = String(v)
  router.replace({ query })
})

const { data, loading, error } = useLoad(
  () => ok(client.GET('/v1/library/mangas', { params: { query: filters.value } })),
  [filters, () => changes.version, () => jobs.finished]
)
const providers = useLoad(() => ok(client.GET('/v1/providers')))

function setFilter() {
  offset.value = 0
}

const statuses = ['releasing', 'finished', 'hiatus', 'cancelled', 'not_yet_released', 'unknown']

function scan() {
  action.run(() => ok(client.POST('/v1/library/scan', { body: { statuses: true } })), (j) => `Scan queued (job ${j.id})`)
}

function planAll(title: string, path: string, body: object = {}) {
  plan.open({ title, run: change('POST', path, body) })
}
</script>

<template>
  <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
    <h4 class="me-auto mb-0">Library <small v-if="data" class="text-secondary fs-6">{{ data.total }}</small></h4>
    <RouterLink class="btn btn-sm btn-outline-secondary" :to="{ name: 'duplicates' }">Duplicates</RouterLink>
    <RouterLink class="btn btn-sm btn-outline-secondary" :to="{ name: 'trash' }">Trash</RouterLink>
    <RouterLink class="btn btn-sm btn-outline-secondary" :to="{ name: 'ops' }">Operations</RouterLink>
    <div class="btn-group btn-group-sm">
      <button class="btn btn-outline-primary" :disabled="action.busy.value" data-test="scan" @click="scan">Scan</button>
      <button class="btn btn-outline-primary" @click="planAll('Rename folders and chapters', '/library/rename', { chapters: true })">Rename</button>
      <button class="btn btn-outline-primary" @click="planAll('Mark the folders without a marker', '/library/mark')">Mark</button>
      <button class="btn btn-outline-primary" @click="planAll('Fill the backup links', '/library/backups')">Backups</button>
      <button class="btn btn-outline-primary" @click="planAll('Migrate old provider ids', '/library/migrate')">Migrate</button>
    </div>
  </div>

  <form class="row g-2 mb-3" @submit.prevent>
    <div class="col-12 col-md-4">
      <input v-model.trim="q" class="form-control form-control-sm" placeholder="filter (title, folder, source)" data-test="library-filter" @input="setFilter" />
    </div>
    <div class="col-6 col-md-2">
      <select v-model="status" class="form-select form-select-sm" @change="setFilter">
        <option value="">any status</option>
        <option v-for="s in statuses" :key="s" :value="s">{{ s }}</option>
      </select>
    </div>
    <div class="col-6 col-md-2">
      <select v-model="provider" class="form-select form-select-sm" @change="setFilter">
        <option value="">any provider</option>
        <option v-for="p in providers.data.value ?? []" :key="p.id" :value="p.id">{{ p.name }}</option>
      </select>
    </div>
    <div class="col-6 col-md-2">
      <select v-model="watched" class="form-select form-select-sm" @change="setFilter">
        <option value="">watched or not</option>
        <option value="true">watched</option>
        <option value="false">not watched</option>
      </select>
    </div>
    <div class="col-6 col-md-2 d-flex align-items-center">
      <div class="form-check form-switch mb-0">
        <input id="dups" v-model="duplicates" class="form-check-input" type="checkbox" @change="setFilter" />
        <label class="form-check-label small" for="dups">duplicates</label>
      </div>
    </div>
  </form>

  <LoadState :loading="loading && !data" :error="error" :empty="data && !data.mangas.length" empty-text="No mangas match." />

  <table v-if="data?.mangas.length" class="table table-sm table-hover" data-test="library-table">
    <thead>
      <tr>
        <th style="width: 56px"></th>
        <th>Title</th>
        <th class="text-end">Chapters</th>
        <th class="d-none d-md-table-cell">Status</th>
        <th class="d-none d-lg-table-cell">Sources</th>
        <th class="d-none d-lg-table-cell">Ids</th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="m in data.mangas" :key="m.id" class="clickable" @click="router.push({ name: 'library-manga', params: { id: m.id } })">
        <td><img :src="coverURL(m.id)" class="cover-thumb" loading="lazy" alt="" @error="($event.target as HTMLImageElement).style.visibility = 'hidden'" /></td>
        <td>
          <RouterLink :to="{ name: 'library-manga', params: { id: m.id } }" class="text-decoration-none" @click.stop>{{ m.title }}</RouterLink>
          <i v-if="m.watched" class="bi bi-eye ms-1 text-info" title="watched" />
          <div v-if="m.dir && m.dir !== m.title" class="small text-secondary">{{ m.dir }}</div>
        </td>
        <td class="text-end">{{ m.chapters }}</td>
        <td class="d-none d-md-table-cell small">{{ m.status }}</td>
        <td class="d-none d-lg-table-cell small">
          <span v-for="s in m.sources" :key="s.provider + s.manga_id" class="badge me-1" :class="s.current ? 'text-bg-primary' : 'text-bg-dark'">{{ s.provider }}</span>
        </td>
        <td class="d-none d-lg-table-cell"><MetaIDs :ids="m.metadata_ids" /></td>
      </tr>
    </tbody>
  </table>

  <nav v-if="data && data.total > pageSize" class="d-flex gap-2 align-items-center">
    <button class="btn btn-sm btn-outline-secondary" :disabled="offset === 0" @click="offset = Math.max(0, offset - pageSize)">‹ prev</button>
    <span class="small">{{ offset + 1 }}–{{ Math.min(offset + pageSize, data.total) }} of {{ data.total }}</span>
    <button class="btn btn-sm btn-outline-secondary" :disabled="offset + pageSize >= data.total" @click="offset += pageSize">next ›</button>
  </nav>
</template>
