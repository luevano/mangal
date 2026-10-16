<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import client, { ok, fetchJSON } from '@/api/client'
import { change } from '@/api/changes'
import { imageURL } from '@/api/urls'
import type { ApplyFields, Binding, ChangeResponse, MetadataResult, MetadataSource, ProviderChapter } from '@/api/schemas'
import MetadataPicker from '@/components/MetadataPicker.vue'
import LoadState from '@/components/LoadState.vue'
import { useAction } from '@/composables/useAction'
import { useLoad } from '@/composables/useLoad'
import { num, sourceNames } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useChanges } from '@/stores/changes'
import { useJobs } from '@/stores/jobs'
import { usePlan } from '@/stores/plan'

const route = useRoute()
const router = useRouter()
const provider = computed(() => String(route.params.provider))
const mangaID = computed(() => String(route.params.manga))
const changes = useChanges()
const jobs = useJobs()
const plan = usePlan()
const action = useAction()

const pm = useLoad(
  () => ok(client.GET('/v1/providers/{provider}/manga', { params: { path: { provider: provider.value }, query: { manga_id: mangaID.value } } })),
  [provider, mangaID, () => changes.version]
)
const chapters = useLoad(
  () => ok(client.GET('/v1/providers/{provider}/chapters', { params: { path: { provider: provider.value }, query: { manga_id: mangaID.value } } })),
  [provider, mangaID, () => jobs.finished]
)
crumbs(() => [{ title: 'Providers', to: { name: 'providers' } }, { title: provider.value }, { title: pm.data.value?.manga.title ?? mangaID.value }])

const selected = ref(new Set<string>())
const key = (c: ProviderChapter) => `${c.volume}/${c.number}`
function toggle(c: ProviderChapter) {
  const s = new Set(selected.value)
  if (s.has(key(c))) s.delete(key(c))
  else s.add(key(c))
  selected.value = s
}
function selectMissing() {
  selected.value = new Set((chapters.data.value ?? []).filter((c) => !c.downloaded).map(key))
}

// selectorRanges are the chapter selectors (from-to) of the runs of selected chapters, in
// the list's order: one download each.
function selectorRanges(): string[] {
  const list = chapters.data.value ?? []
  const ranges: string[] = []
  let start: ProviderChapter | undefined
  let last: ProviderChapter | undefined
  for (const c of list) {
    if (selected.value.has(key(c))) {
      start ??= c
      last = c
    } else if (start && last) {
      ranges.push(start === last ? num(start.number) : `${num(start.number)}-${num(last.number)}`)
      start = last = undefined
    }
  }
  if (start && last) ranges.push(start === last ? num(start.number) : `${num(start.number)}-${num(last.number)}`)
  return ranges
}

async function download(selectors: string[]) {
  for (const chapters of selectors) {
    await action.run(() => ok(client.POST('/v1/downloads', { body: { provider: provider.value, manga_id: mangaID.value, chapters } })), (j) => `Queued: ${j.title}`)
  }
  selected.value = new Set()
}

function read(c: ProviderChapter) {
  const lib = pm.data.value?.library
  if (c.downloaded && c.path && lib) {
    router.push({ name: 'read-library', params: { id: lib }, query: { path: c.path } })
    return
  }
  router.push({ name: 'read-online', params: { provider: provider.value, manga: mangaID.value }, query: { chapter: String(c.number), volume: String(c.volume) } })
}

function watchIt() {
  action.run(() => ok(client.POST('/v1/watches', { body: { provider: provider.value, manga_id: mangaID.value } })), (w) => `Watching ${w.title}`).then(() => pm.load())
}

// bindings
const bindOpen = ref(false)
const bindSource = ref<string>()
function openBind(source?: string) {
  bindSource.value = source
  bindOpen.value = true
}
function planBind(r: MetadataResult, src: MetadataSource) {
  bindOpen.value = false
  const current = pm.data.value?.bindings.find((b) => b.source === r.source)
  const verb = current ? 'Replace' : 'Bind'
  plan.open({
    title: `${verb} ${current ? `${src.name} ${current.id} with ${r.id}` : `to ${src.name} ${r.id}`}: ${r.title}`,
    verb,
    run: change('PUT', '/bindings', { provider: provider.value, manga_id: mangaID.value, source: r.source, id: r.id })
  })
}
function unbind(b: Binding) {
  plan.open({
    title: `Unbind ${sourceNames[b.source] ?? b.source} ${b.id}`,
    verb: 'Unbind',
    run: async (apply: boolean, fields: ApplyFields) => {
      const q = new URLSearchParams({ provider: provider.value, manga_id: mangaID.value, source: b.source })
      if (apply) {
        q.set('apply', '1')
        if (fields.expect) q.set('expect', fields.expect)
      }
      return (await fetchJSON<ChangeResponse>(`/api/v1/bindings?${q}`, { method: 'DELETE' })).data
    }
  })
}
</script>

<template>
  <LoadState :loading="pm.loading.value && !pm.data.value" :error="pm.error.value" />
  <template v-if="pm.data.value">
    <div class="row g-3 mb-3">
      <div class="col-4 col-md-2">
        <img v-if="pm.data.value.manga.cover" :src="imageURL(pm.data.value.manga.cover, pm.data.value.manga.url)" class="cover-large" alt="" />
      </div>
      <div class="col">
        <h4 class="mb-1">{{ pm.data.value.manga.title }}</h4>
        <div class="small text-secondary mb-2">
          {{ provider }} · {{ pm.data.value.manga.id }}
          <a v-if="pm.data.value.manga.url" :href="pm.data.value.manga.url" target="_blank" rel="noopener noreferrer"> · site</a>
        </div>
        <div class="d-flex flex-wrap gap-2 mb-2">
          <RouterLink v-if="pm.data.value.library" class="btn btn-sm btn-outline-secondary" :to="{ name: 'library-manga', params: { id: pm.data.value.library } }">In the library</RouterLink>
          <RouterLink v-if="pm.data.value.watch" class="btn btn-sm btn-outline-secondary" :to="{ name: 'watch', params: { id: pm.data.value.watch } }">Watched</RouterLink>
          <button v-else class="btn btn-sm btn-outline-primary" @click="watchIt">Watch</button>
          <button class="btn btn-sm btn-outline-primary" data-test="bind" @click="openBind()">Bind</button>
        </div>
        <div class="d-flex flex-wrap gap-1">
          <span v-for="b in pm.data.value.bindings" :key="b.source" class="badge text-bg-secondary">
            {{ sourceNames[b.source] ?? b.source }} {{ b.id }}{{ b.default ? ' (default)' : '' }}
            <a href="#" class="text-reset ms-1" title="Replace" @click.prevent="openBind(b.source)"><i class="bi bi-arrow-repeat" /></a>
            <a href="#" class="text-reset ms-1" title="Unbind" @click.prevent="unbind(b)"><i class="bi bi-x" /></a>
          </span>
        </div>
      </div>
    </div>

    <div class="d-flex flex-wrap gap-2 align-items-center mb-2">
      <h6 class="me-auto mb-0">Chapters <small v-if="chapters.data.value" class="text-secondary">{{ chapters.data.value.length }}</small></h6>
      <button class="btn btn-sm btn-outline-secondary" @click="selectMissing">Select the missing</button>
      <button class="btn btn-sm btn-outline-secondary" :disabled="!selected.size" @click="selected = new Set()">Unselect</button>
      <button class="btn btn-sm btn-primary" :disabled="!selected.size || action.busy.value" data-test="download-selected" @click="download(selectorRanges())">Download {{ selected.size || '' }}</button>
    </div>
    <LoadState :loading="chapters.loading.value && !chapters.data.value" :error="chapters.error.value" :empty="chapters.data.value && !chapters.data.value.length" empty-text="No chapters." />
    <table v-if="chapters.data.value?.length" class="table table-sm table-hover" data-test="provider-chapters">
      <thead><tr><th style="width: 2rem"></th><th>#</th><th>Title</th><th>Vol</th><th></th><th></th></tr></thead>
      <tbody>
        <tr v-for="c in chapters.data.value" :key="key(c)" :class="{ 'table-active': selected.has(key(c)) }">
          <td><input class="form-check-input" type="checkbox" :checked="selected.has(key(c))" @change="toggle(c)" /></td>
          <td>{{ num(c.number) }}</td>
          <td><a href="#" class="text-decoration-none" @click.prevent="read(c)">{{ c.title }}</a></td>
          <td class="small">{{ num(c.volume) || '' }}</td>
          <td class="text-nowrap">
            <i v-if="c.downloaded" class="bi bi-download text-success me-1" title="downloaded" />
            <i v-if="c.reads" class="bi bi-check2 text-success" :title="`read ${c.reads}×`" />
          </td>
          <td class="text-end text-nowrap">
            <button class="btn btn-sm btn-link py-0" :title="c.downloaded ? 'Read (downloaded)' : 'Read online'" data-test="read-online" @click="read(c)"><i class="bi bi-book" /></button>
            <button v-if="!c.downloaded" class="btn btn-sm btn-link py-0" title="Download" @click="download([num(c.number)])"><i class="bi bi-download" /></button>
          </td>
        </tr>
      </tbody>
    </table>
  </template>

  <MetadataPicker :open="bindOpen" :title="`Bind ${pm.data.value?.manga.title ?? ''}`" :query="pm.data.value?.manga.title" :source="bindSource" @close="bindOpen = false" @pick="planBind" />
</template>
