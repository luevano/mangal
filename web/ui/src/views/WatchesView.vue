<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import client, { ok } from '@/api/client'
import type { CheckResponse, Watch } from '@/api/schemas'
import FormModal from '@/components/FormModal.vue'
import LiveLog from '@/components/LiveLog.vue'
import LoadState from '@/components/LoadState.vue'
import PagerNav from '@/components/PagerNav.vue'
import { usePager } from '@/composables/usePager'
import { useAction } from '@/composables/useAction'
import { useLoad } from '@/composables/useLoad'
import { ago, num } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useWatches } from '@/stores/watches'

crumbs(() => [{ title: 'Watches' }])
const router = useRouter()
const watches = useWatches()
const action = useAction()
const filter = ref('')
const view = ref<'all' | 'paused' | 'relink' | 'errors'>('all')

onMounted(() => watches.load())

const shown = computed(() => {
  const q = filter.value.toLowerCase()
  return watches.list.filter((w) => {
    if (q && !w.title.toLowerCase().includes(q) && !w.provider.includes(q)) return false
    if (view.value === 'paused') return !w.enabled
    if (view.value === 'relink') return !!w.needs_relink || !!w.provider_missing
    if (view.value === 'errors') return !!w.last_error
    return true
  })
})

const providers = useLoad(() => ok(client.GET('/v1/providers')))
const pager = usePager(shown, 100)

function setEnabled(w: Watch, enabled: boolean) {
  action.run(() => ok(client.PATCH('/v1/watches/{id}', { params: { path: { id: w.id } }, body: { enabled } })), `${w.title} ${enabled ? 'resumed' : 'paused'}`).then(() => watches.load())
}

function check(ids: number[]) {
  action.run(() => ok(client.POST('/v1/check', { body: { ids } })), (res) => {
    const r = res as CheckResponse
    return `Check run ${r.run} started${r.busy?.length ? ` (busy: ${r.busy.join(', ')})` : ''}`
  })
}

const removing = ref<Watch>()
function remove() {
  const w = removing.value
  removing.value = undefined
  if (!w) return
  action.run(() => ok(client.DELETE('/v1/watches/{id}', { params: { path: { id: w.id } } })), `${w.title} isn't watched anymore`).then(() => watches.load())
}

const addOpen = ref(false)
const add = reactive({ provider: '', query: '', manga_id: '', from: '' })
function submitAdd() {
  addOpen.value = false
  const body: { provider: string; query?: string; manga_id?: string; from?: number } = { provider: add.provider }
  if (add.manga_id) body.manga_id = add.manga_id
  else body.query = add.query
  if (add.from !== '') body.from = Number(add.from)
  action.run(() => ok(client.POST('/v1/watches', { body })), (w) => `Watching ${w.title}`).then(() => watches.load())
}
</script>

<template>
  <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
    <h4 class="me-auto mb-0">Watches <small class="text-secondary fs-6">{{ watches.list.length }}</small></h4>
    <RouterLink class="btn btn-sm btn-outline-secondary" :to="{ name: 'runs' }">Check runs</RouterLink>
    <button class="btn btn-sm btn-outline-primary" @click="addOpen = true">Add</button>
    <button class="btn btn-sm btn-primary" :disabled="action.busy.value" @click="check([])">Check all</button>
  </div>
  <div class="d-flex flex-wrap gap-2 mb-2">
    <input v-model="filter" class="form-control form-control-sm" style="max-width: 20rem" placeholder="filter" />
    <div class="btn-group btn-group-sm">
      <button v-for="v in ['all', 'paused', 'relink', 'errors'] as const" :key="v" class="btn" :class="view === v ? 'btn-secondary' : 'btn-outline-secondary'" @click="view = v">{{ v }}</button>
    </div>
  </div>
  <LoadState :loading="!watches.loaded" :error="watches.error" :empty="watches.loaded && !shown.length" empty-text="No watches." />
  <table v-if="shown.length" class="table table-sm table-hover" data-test="watches-table">
    <thead>
      <tr><th>Title</th><th class="d-none d-md-table-cell">Provider</th><th>Last</th><th class="d-none d-lg-table-cell">Checked</th><th class="d-none d-lg-table-cell">Next</th><th></th></tr>
    </thead>
    <tbody>
      <tr v-for="w in pager.items.value" :key="w.id" :class="{ 'text-secondary': !w.enabled }">
        <td>
          <RouterLink :to="{ name: 'watch', params: { id: w.id } }">{{ w.title }}</RouterLink>
          <span v-if="!w.enabled" class="badge text-bg-secondary ms-1">paused</span>
          <span v-if="w.needs_relink" class="badge text-bg-warning ms-1">re-link</span>
          <span v-if="w.provider_missing" class="badge text-bg-danger ms-1">no provider</span>
          <span v-if="w.last_error" class="badge text-bg-danger ms-1" :title="w.last_error">error{{ w.failures ? ` ×${w.failures}` : '' }}</span>
        </td>
        <td class="d-none d-md-table-cell small">{{ w.provider }}</td>
        <td>{{ num(w.last_downloaded) }}</td>
        <td class="d-none d-lg-table-cell small">{{ ago(w.last_checked) }}</td>
        <td class="d-none d-lg-table-cell small">{{ ago(w.next_check) }}</td>
        <td class="text-end text-nowrap">
          <button class="btn btn-sm btn-link py-0" title="Check now" @click="check([w.id])"><i class="bi bi-arrow-repeat" /></button>
          <button v-if="w.enabled" class="btn btn-sm btn-link py-0" title="Pause" @click="setEnabled(w, false)"><i class="bi bi-pause" /></button>
          <button v-else class="btn btn-sm btn-link py-0" title="Resume" @click="setEnabled(w, true)"><i class="bi bi-play" /></button>
          <button v-if="w.library_manga" class="btn btn-sm btn-link py-0" title="Library manga" @click="router.push({ name: 'library-manga', params: { id: w.library_manga } })"><i class="bi bi-collection" /></button>
          <button class="btn btn-sm btn-link text-danger py-0" title="Remove" @click="removing = w"><i class="bi bi-trash" /></button>
        </td>
      </tr>
    </tbody>
  </table>
  <PagerNav v-model="pager.offset.value" :total="pager.total.value" :size="pager.size" />

  <h6 class="mt-4">Live</h6>
  <LiveLog :events="watches.live" />

  <FormModal :title="`Remove the watch of ${removing?.title}?`" :open="!!removing" submit-text="Remove" @close="removing = undefined" @submit="remove">
    <p class="small mb-0">Its check history stays; the library isn't touched.</p>
  </FormModal>

  <FormModal title="Add a watch" :open="addOpen" submit-text="Add" :disabled="!add.provider || (!add.query && !add.manga_id)" @close="addOpen = false" @submit="submitAdd">
    <label class="form-label small mb-0">Provider</label>
    <select v-model="add.provider" class="form-select form-select-sm mb-2">
      <option v-for="p in providers.data.value ?? []" :key="p.id" :value="p.id">{{ p.name }}</option>
    </select>
    <label class="form-label small mb-0">Search</label>
    <input v-model.trim="add.query" class="form-control form-control-sm mb-2" placeholder="title (exact match)" />
    <label class="form-label small mb-0">or the manga id</label>
    <input v-model.trim="add.manga_id" class="form-control form-control-sm mb-2" />
    <label class="form-label small mb-0">From chapter (the last one you have)</label>
    <input v-model="add.from" type="number" step="any" class="form-control form-control-sm" />
  </FormModal>
</template>
