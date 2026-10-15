<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import client, { ok } from '@/api/client'
import type { CheckResponse } from '@/api/schemas'
import FormModal from '@/components/FormModal.vue'
import LiveLog from '@/components/LiveLog.vue'
import LoadState from '@/components/LoadState.vue'
import { useAction } from '@/composables/useAction'
import { useLoad } from '@/composables/useLoad'
import { ago, date, num } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useWatches } from '@/stores/watches'

const route = useRoute()
const router = useRouter()
const id = computed(() => Number(route.params.id))
const watches = useWatches()
const action = useAction()

const w = useLoad(() => ok(client.GET('/v1/watches/{id}', { params: { path: { id: id.value } } })), [id, () => watches.runsVersion])
const runs = useLoad(() => ok(client.GET('/v1/runs', { params: { query: { watch: id.value, limit: 30 } } })), [id, () => watches.runsVersion])
const providers = useLoad(() => ok(client.GET('/v1/providers')))
crumbs(() => [{ title: 'Watches', to: { name: 'watches' } }, { title: w.data.value?.title ?? `#${id.value}` }])

const live = computed(() => watches.live.filter((e) => e.watch === id.value))

function check() {
  action.run(() => ok(client.POST('/v1/check', { body: { ids: [id.value] } })), (r) => `Check run ${(r as CheckResponse).run} started`)
}
function setEnabled(enabled: boolean) {
  action.run(() => ok(client.PATCH('/v1/watches/{id}', { params: { path: { id: id.value } }, body: { enabled } })), enabled ? 'Resumed' : 'Paused').then(() => w.load())
}

const editOpen = ref(false)
const edit = reactive({ schedule: '', gaps: false, anilist_id: '', from: '', skip: '' })
function openEdit() {
  const d = w.data.value
  if (!d) return
  edit.schedule = d.schedule ?? ''
  edit.gaps = !!d.gaps
  edit.anilist_id = String(d.bindings?.find((b) => b.source === 'al')?.id ?? '')
  edit.from = d.last_downloaded !== undefined ? String(d.last_downloaded) : ''
  edit.skip = ''
  editOpen.value = true
}
function submitEdit() {
  editOpen.value = false
  const body: { schedule?: string; gaps?: boolean; anilist_id?: number; from?: number; skip?: number[] } = { schedule: edit.schedule, gaps: edit.gaps }
  if (edit.anilist_id !== '') body.anilist_id = Number(edit.anilist_id)
  if (edit.from !== '') body.from = Number(edit.from)
  if (edit.skip.trim()) body.skip = edit.skip.split(/[ ,]+/).filter(Boolean).map(Number)
  action.run(() => ok(client.PATCH('/v1/watches/{id}', { params: { path: { id: id.value } }, body })), 'Saved').then(() => w.load())
}

const relinkOpen = ref(false)
const relink = reactive({ provider: '', manga_id: '', query: '' })
function openRelink() {
  relink.provider = w.data.value?.provider ?? ''
  relink.manga_id = ''
  relink.query = w.data.value?.title ?? ''
  relinkOpen.value = true
}
function submitRelink() {
  relinkOpen.value = false
  const body: { provider?: string; manga_id?: string; query?: string } = { provider: relink.provider }
  if (relink.manga_id) body.manga_id = relink.manga_id
  else body.query = relink.query
  action.run(() => ok(client.POST('/v1/watches/{id}/relink', { params: { path: { id: id.value } }, body })), (x) => `Re-linked to ${x.title} (${x.provider})`).then(() => w.load())
}

const removeOpen = ref(false)
function remove() {
  removeOpen.value = false
  let removed = false
  action
    .run(async () => {
      await ok(client.DELETE('/v1/watches/{id}', { params: { path: { id: id.value } } }))
      removed = true
    }, 'Removed')
    .then(() => {
      if (removed) router.push({ name: 'watches' })
    })
}
</script>

<template>
  <LoadState :loading="w.loading.value && !w.data.value" :error="w.error.value" />
  <template v-if="w.data.value">
    <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
      <h4 class="me-auto mb-0">{{ w.data.value.title }}</h4>
      <button class="btn btn-sm btn-primary" @click="check">Check now</button>
      <button v-if="w.data.value.enabled" class="btn btn-sm btn-outline-secondary" @click="setEnabled(false)">Pause</button>
      <button v-else class="btn btn-sm btn-outline-secondary" @click="setEnabled(true)">Resume</button>
      <button class="btn btn-sm btn-outline-primary" @click="openEdit">Edit</button>
      <button class="btn btn-sm btn-outline-primary" @click="openRelink">Re-link</button>
      <button class="btn btn-sm btn-outline-danger" @click="removeOpen = true">Remove</button>
    </div>
    <dl class="row small">
      <dt class="col-sm-3">Provider</dt>
      <dd class="col-sm-9">
        <RouterLink :to="{ name: 'provider-manga', params: { provider: w.data.value.provider, manga: w.data.value.manga_id } }">{{ w.data.value.provider }} · {{ w.data.value.manga_id }}</RouterLink>
      </dd>
      <dt class="col-sm-3">State</dt>
      <dd class="col-sm-9">
        {{ w.data.value.enabled ? 'enabled' : 'paused' }}<span v-if="w.data.value.status"> · {{ w.data.value.status }}</span>
        <span v-if="w.data.value.needs_relink" class="badge text-bg-warning ms-1">needs a re-link</span>
      </dd>
      <dt class="col-sm-3">Last downloaded</dt>
      <dd class="col-sm-9">{{ num(w.data.value.last_downloaded) || '—' }}</dd>
      <dt class="col-sm-3">Schedule</dt>
      <dd class="col-sm-9">{{ w.data.value.schedule || 'default' }}{{ w.data.value.gaps ? ' · gaps' : '' }}</dd>
      <dt class="col-sm-3">Checked</dt>
      <dd class="col-sm-9">{{ date(w.data.value.last_checked) || 'never' }}</dd>
      <dt class="col-sm-3">Next check</dt>
      <dd class="col-sm-9">{{ date(w.data.value.next_check) }} ({{ ago(w.data.value.next_check) }})</dd>
      <template v-if="w.data.value.last_error">
        <dt class="col-sm-3">Last error</dt>
        <dd class="col-sm-9 text-danger">{{ w.data.value.last_error }} ({{ w.data.value.failures }}×)</dd>
      </template>
      <template v-if="w.data.value.library_manga">
        <dt class="col-sm-3">Library</dt>
        <dd class="col-sm-9">
          <RouterLink :to="{ name: 'library-manga', params: { id: w.data.value.library_manga } }">#{{ w.data.value.library_manga }}</RouterLink>
          <span v-if="w.data.value.library_issue" class="text-warning"> · {{ w.data.value.library_issue }}</span>
          <span v-if="w.data.value.library_note" class="text-secondary"> · {{ w.data.value.library_note }}</span>
        </dd>
      </template>
    </dl>

    <div class="row g-3">
      <div class="col-12 col-xl-6">
        <h6>Checks</h6>
        <LoadState :loading="runs.loading.value && !runs.data.value" :error="runs.error.value" :empty="runs.data.value && !runs.data.value.length" empty-text="Never checked." />
        <table v-if="runs.data.value?.length" class="table table-sm">
          <thead><tr><th>Run</th><th>Kind</th><th>Started</th><th class="text-end">New</th><th class="text-end">Errors</th></tr></thead>
          <tbody>
            <tr v-for="r in runs.data.value" :key="r.id">
              <td><RouterLink :to="{ name: 'run', params: { id: r.id } }">#{{ r.id }}</RouterLink></td>
              <td class="small">{{ r.kind }}</td>
              <td class="small">{{ date(r.started) }}</td>
              <td class="text-end">{{ r.new_chapters }}</td>
              <td class="text-end">{{ r.errors }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="col-12 col-xl-6">
        <h6>Live</h6>
        <LiveLog :events="live" />
      </div>
    </div>
  </template>

  <FormModal title="Edit the watch" :open="editOpen" submit-text="Save" @close="editOpen = false" @submit="submitEdit">
    <label class="form-label small mb-0">Schedule (cron, empty for the default)</label>
    <input v-model.trim="edit.schedule" class="form-control form-control-sm mb-2" placeholder="0 */6 * * *" />
    <div class="form-check mb-2"><input id="eg" v-model="edit.gaps" class="form-check-input" type="checkbox" /><label class="form-check-label" for="eg">fill gaps (missing older chapters)</label></div>
    <label class="form-label small mb-0">AniList id</label>
    <input v-model.trim="edit.anilist_id" type="number" class="form-control form-control-sm mb-2" />
    <label class="form-label small mb-0">Last downloaded chapter</label>
    <input v-model.trim="edit.from" type="number" step="any" class="form-control form-control-sm mb-2" />
    <label class="form-label small mb-0">Skip chapters (replaces the list)</label>
    <input v-model="edit.skip" class="form-control form-control-sm" placeholder="12, 13.5" />
  </FormModal>

  <FormModal title="Re-link the watch" :open="relinkOpen" submit-text="Re-link" :disabled="!relink.provider || (!relink.manga_id && !relink.query)" @close="relinkOpen = false" @submit="submitRelink">
    <label class="form-label small mb-0">Provider</label>
    <select v-model="relink.provider" class="form-select form-select-sm mb-2">
      <option v-for="p in providers.data.value ?? []" :key="p.id" :value="p.id">{{ p.name }}</option>
    </select>
    <label class="form-label small mb-0">Manga id</label>
    <input v-model.trim="relink.manga_id" class="form-control form-control-sm mb-2" />
    <label class="form-label small mb-0">or a search</label>
    <input v-model.trim="relink.query" class="form-control form-control-sm" />
  </FormModal>

  <FormModal :title="`Remove the watch of ${w.data.value?.title}?`" :open="removeOpen" submit-text="Remove" @close="removeOpen = false" @submit="remove">
    <p class="small mb-0">Its check history stays; the library isn't touched.</p>
  </FormModal>
</template>
