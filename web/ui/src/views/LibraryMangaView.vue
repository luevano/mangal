<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import client, { ok } from '@/api/client'
import { change } from '@/api/changes'
import { chapterFileURL, coverURL, mangaZipURL } from '@/api/urls'
import type { LibraryChapter, MetadataResult } from '@/api/schemas'
import FormModal from '@/components/FormModal.vue'
import MetadataPicker from '@/components/MetadataPicker.vue'
import LoadState from '@/components/LoadState.vue'
import PagerNav from '@/components/PagerNav.vue'
import { usePager } from '@/composables/usePager'
import MetaIDs from '@/components/MetaIDs.vue'
import { useLoad } from '@/composables/useLoad'
import { useAction } from '@/composables/useAction'
import { date, num, sourceNames } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useChanges } from '@/stores/changes'
import { useJobs } from '@/stores/jobs'
import { usePlan } from '@/stores/plan'

const route = useRoute()
const router = useRouter()
const id = computed(() => Number(route.params.id))
const changes = useChanges()
const jobs = useJobs()
const plan = usePlan()
const action = useAction()

const detail = useLoad(() => ok(client.GET('/v1/library/mangas/{id}', { params: { path: { id: id.value } } })), [id, () => changes.version])
const chapters = useLoad(
  () => ok(client.GET('/v1/library/mangas/{id}/chapters', { params: { path: { id: id.value } } })),
  [id, () => changes.version, () => jobs.finished]
)
const providers = useLoad(() => ok(client.GET('/v1/providers')))

const manga = computed(() => detail.data.value?.manga)
crumbs(() => [{ title: 'Library', to: { name: 'library' } }, { title: manga.value?.title ?? `#${id.value}` }])

const sorted = computed(() =>
  [...(chapters.data.value ?? [])].sort((a, b) => a.download.volume - b.download.volume || a.download.chapter - b.download.chapter)
)
// the chapter after the last one read, else the first
const pager = usePager(sorted, 100)
const next = computed<LibraryChapter | undefined>(() => {
  const list = sorted.value
  let last = -1
  list.forEach((c, i) => {
    if (c.reads > 0) last = i
  })
  return list[last + 1] ?? list[0]
})

function read(c: LibraryChapter) {
  router.push({ name: 'read-library', params: { id: id.value }, query: { path: c.download.path } })
}

function deleteChapter(c: LibraryChapter) {
  plan.open({ title: `Delete chapter ${num(c.download.chapter)}`, verb: 'Delete', run: change('POST', '/library/delete', { manga: id.value, chapter: c.download.path }) })
}

function deleteManga() {
  plan.open({ title: `Delete ${manga.value?.title}`, verb: 'Delete', run: change('POST', '/library/delete', { manga: id.value }), onApplied: () => router.push({ name: 'library' }) })
}

function forget() {
  plan.open({ title: `Forget ${manga.value?.title}`, verb: 'Forget', run: change('POST', '/library/forget', { mangas: [id.value] }), onApplied: () => router.push({ name: 'library' }) })
}

function mark() {
  plan.open({ title: `Mark ${manga.value?.title}`, run: change('POST', '/library/mark', { mangas: [id.value] }) })
}

function rename() {
  plan.open({ title: `Rename ${manga.value?.title}`, verb: 'Rename', run: change('POST', '/library/rename', { paths: [manga.value?.path], chapters: true }) })
}

// links
const linksOpen = ref(false)
const links = reactive<Record<string, string>>({ al: '', mal: '', kt: '', mu: '' })
const linkRename = ref(false)
const linkWatches = ref(true)
function openLinks() {
  for (const k of Object.keys(links)) links[k] = manga.value?.metadata_ids.find((i) => i.source === k)?.id ?? ''
  linksOpen.value = true
}
// the metadata search for a link's id (the TUI's links pick)
const pickSource = ref<string>()
function pickedLink(r: MetadataResult) {
  links[r.source] = r.id
  pickSource.value = undefined
}
function planLinks() {
  linksOpen.value = false
  const current = Object.fromEntries((manga.value?.metadata_ids ?? []).map((i) => [i.source, i.id]))
  const changed: Record<string, string> = {}
  for (const [k, v] of Object.entries(links)) if ((current[k] ?? '') !== v.trim()) changed[k] = v.trim()
  plan.open({ title: `Links of ${manga.value?.title}`, run: change('POST', '/library/links', { manga: id.value, links: changed, rename: linkRename.value, watches: linkWatches.value }) })
}

// move
const moveOpen = ref(false)
const move = reactive({ provider: '', manga_id: '', query: '', full: false })
function planMove() {
  moveOpen.value = false
  const body: Record<string, unknown> = { manga: id.value, provider: move.provider, full: move.full }
  if (move.manga_id) body.manga_id = move.manga_id
  if (move.query) body.query = move.query
  plan.open({ title: `Move ${manga.value?.title} to ${move.provider}`, verb: 'Move', run: change('POST', '/library/move', body) })
}

// merge
const mergeOpen = ref(false)
const mergeQ = ref('')
const mergeOther = ref<number>()
const mergeKeepThis = ref(true)
const candidates = useLoad(
  () => (mergeOpen.value ? ok(client.GET('/v1/library/mangas', { params: { query: { q: mergeQ.value || undefined, limit: 30 } } })) : Promise.resolve(undefined)),
  [mergeOpen, mergeQ]
)
function planMerge() {
  if (!mergeOther.value) return
  mergeOpen.value = false
  const [keep, other] = mergeKeepThis.value ? [id.value, mergeOther.value] : [mergeOther.value, id.value]
  plan.open({
    title: 'Merge',
    verb: 'Merge',
    run: change('POST', '/library/merge', { keep, other }),
    onApplied: () => keep !== id.value && router.push({ name: 'library-manga', params: { id: keep } })
  })
}

// watch a source
function watchSource(provider: string, mangaID: string) {
  action.run(() => ok(client.POST('/v1/watches', { body: { provider, manga_id: mangaID } })), (w) => `Watching ${w.title}`).then(() => detail.load())
}
</script>

<template>
  <LoadState :loading="detail.loading.value && !detail.data.value" :error="detail.error.value" />
  <template v-if="manga && detail.data.value">
    <div class="row g-3 mb-3">
      <div class="col-4 col-md-2">
        <img :src="coverURL(manga.id)" class="cover-large" alt="" @error="($event.target as HTMLImageElement).style.visibility = 'hidden'" />
      </div>
      <div class="col">
        <h4 class="mb-1" data-test="manga-title">{{ manga.title }}</h4>
        <div class="small text-secondary mono mb-2">{{ manga.path }}</div>
        <div class="d-flex flex-wrap gap-1 mb-2">
          <span v-if="manga.status" class="badge text-bg-info">{{ manga.status }}</span>
          <span v-if="detail.data.value.missing" class="badge text-bg-danger">folder missing</span>
          <span v-if="!detail.data.value.marker && !detail.data.value.missing" class="badge text-bg-warning">no marker</span>
          <span v-if="manga.watched" class="badge text-bg-primary">watched</span>
          <MetaIDs :ids="manga.metadata_ids" />
        </div>
        <div class="d-flex flex-wrap gap-2">
          <button v-if="next" class="btn btn-sm btn-primary" data-test="read-next" @click="read(next)"><i class="bi bi-book" /> Read {{ num(next.download.chapter) }}</button>
          <button class="btn btn-sm btn-outline-primary" @click="openLinks">Links</button>
          <a v-if="sorted.length" class="btn btn-sm btn-outline-secondary" :href="mangaZipURL(manga.id)" title="Every downloaded chapter in one zip"><i class="bi bi-file-earmark-zip" /> Zip</a>
          <button class="btn btn-sm btn-outline-primary" @click="moveOpen = true">Move</button>
          <button class="btn btn-sm btn-outline-primary" data-test="merge" @click="mergeOpen = true">Merge</button>
          <button class="btn btn-sm btn-outline-primary" @click="rename">Rename</button>
          <button v-if="!detail.data.value.marker && !detail.data.value.missing" class="btn btn-sm btn-outline-primary" @click="mark">Mark</button>
          <button v-if="detail.data.value.missing" class="btn btn-sm btn-outline-danger" @click="forget">Forget</button>
          <button v-else class="btn btn-sm btn-outline-danger" data-test="delete-manga" @click="deleteManga">Delete</button>
        </div>
      </div>
    </div>

    <div class="row g-3">
      <div class="col-12 col-xl-8">
        <h6>Chapters</h6>
        <LoadState :loading="chapters.loading.value && !chapters.data.value" :error="chapters.error.value" :empty="!sorted.length" empty-text="No chapters downloaded." />
        <table v-if="sorted.length" class="table table-sm table-hover" data-test="chapters-table">
          <thead>
            <tr>
              <th>#</th>
              <th>Title</th>
              <th class="d-none d-md-table-cell">Provider</th>
              <th class="d-none d-md-table-cell">Format</th>
              <th class="d-none d-lg-table-cell">Downloaded</th>
              <th>Read</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="c in pager.items.value" :key="c.download.path">
              <td>{{ num(c.download.chapter) }}<span v-if="c.download.volume" class="text-secondary small"> v{{ num(c.download.volume) }}</span></td>
              <td><a href="#" class="text-decoration-none" @click.prevent="read(c)">{{ c.download.chapter_title }}</a></td>
              <td class="d-none d-md-table-cell small">{{ c.download.provider }}</td>
              <td class="d-none d-md-table-cell small">{{ c.download.format }}</td>
              <td class="d-none d-lg-table-cell small">{{ date(c.download.downloaded) }}</td>
              <td>
                <span v-if="c.reads" class="text-success" :title="`read ${c.reads}×, last ${date(c.last_read)}`"><i class="bi bi-check2" />{{ c.reads > 1 ? c.reads : '' }}</span>
              </td>
              <td class="text-end text-nowrap">
                <button class="btn btn-sm btn-link py-0" title="Read" data-test="read-chapter" @click="read(c)"><i class="bi bi-book" /></button>
                <a class="btn btn-sm btn-link py-0" title="Download the file" :href="chapterFileURL(manga.id, c.download.path)"><i class="bi bi-file-earmark-arrow-down" /></a>
                <button class="btn btn-sm btn-link text-danger py-0" title="Delete" @click="deleteChapter(c)"><i class="bi bi-trash" /></button>
              </td>
            </tr>
          </tbody>
        </table>
        <PagerNav v-model="pager.offset.value" :total="pager.total.value" :size="pager.size" />
      </div>
      <div class="col-12 col-xl-4">
        <h6>Sources</h6>
        <ul class="list-group mb-3">
          <li v-if="!manga.sources.length" class="list-group-item text-secondary small">No source (a scanned folder): Move links it to a provider.</li>
          <li v-for="s in manga.sources" :key="s.provider + s.manga_id" class="list-group-item d-flex align-items-center gap-2">
            <div class="me-auto">
              <RouterLink :to="{ name: 'provider-manga', params: { provider: s.provider, manga: s.manga_id } }">{{ s.title }}</RouterLink>
              <div class="small text-secondary">{{ s.provider }} · {{ s.manga_id }}<span v-if="s.current"> · current</span><span v-if="s.moved"> · moved {{ date(s.moved) }}</span></div>
            </div>
            <button v-if="s.current && !detail.data.value.watches.some((w) => w.provider === s.provider && w.manga_id === s.manga_id)" class="btn btn-sm btn-outline-secondary" @click="watchSource(s.provider, s.manga_id)">Watch</button>
          </li>
        </ul>
        <h6>Watches</h6>
        <ul class="list-group mb-3">
          <li v-for="w in detail.data.value.watches" :key="w.id" class="list-group-item">
            <RouterLink :to="{ name: 'watch', params: { id: w.id } }">{{ w.title }}</RouterLink>
            <span class="small text-secondary"> · {{ w.provider }} · {{ w.enabled ? 'enabled' : 'paused' }}</span>
          </li>
          <li v-if="!detail.data.value.watches.length" class="list-group-item text-secondary small">Not watched.</li>
        </ul>
        <h6>Bindings</h6>
        <ul class="list-group">
          <li v-for="b in detail.data.value.bindings" :key="`${b.provider}${b.manga_id}${b.source}`" class="list-group-item small">
            <span v-if="b.provider">{{ b.provider }} {{ b.manga_id }} → </span>{{ sourceNames[b.source] ?? b.source }} {{ b.id }}<span v-if="b.default"> (default)</span>
          </li>
          <li v-if="!detail.data.value.bindings.length" class="list-group-item text-secondary small">No bindings.</li>
        </ul>
      </div>
    </div>
  </template>

  <FormModal title="Links" :open="linksOpen" submit-text="Plan" @close="linksOpen = false" @submit="planLinks">
    <div v-for="(_, k) in links" :key="k" class="mb-2">
      <label class="form-label small mb-0">{{ sourceNames[k] ?? k }} id</label>
      <div class="input-group input-group-sm">
        <input v-model="links[k]" class="form-control" :placeholder="`empty removes it`" />
        <button v-if="k !== 'mu'" type="button" class="btn btn-outline-secondary" :title="`Search ${sourceNames[k] ?? k}`" :data-test="`search-link-${k}`" @click="pickSource = k"><i class="bi bi-search" /></button>
      </div>
    </div>
    <div class="form-check"><input id="lr" v-model="linkRename" class="form-check-input" type="checkbox" /><label class="form-check-label" for="lr">rename the folder</label></div>
    <div class="form-check"><input id="lw" v-model="linkWatches" class="form-check-input" type="checkbox" /><label class="form-check-label" for="lw">update the watches' AniList binding</label></div>
  </FormModal>

  <MetadataPicker :open="!!pickSource" :title="`Link ${manga?.title ?? ''}`" :query="manga?.title" :source="pickSource" :sources="pickSource ? [pickSource] : undefined" @close="pickSource = undefined" @pick="pickedLink" />

  <FormModal title="Move to another provider" :open="moveOpen" submit-text="Plan" :disabled="!move.provider || (!move.manga_id && !move.query)" @close="moveOpen = false" @submit="planMove">
    <label class="form-label small mb-0">Provider</label>
    <select v-model="move.provider" class="form-select form-select-sm mb-2">
      <option v-for="p in providers.data.value ?? []" :key="p.id" :value="p.id">{{ p.name }}</option>
    </select>
    <label class="form-label small mb-0">Manga id</label>
    <input v-model.trim="move.manga_id" class="form-control form-control-sm mb-2" placeholder="the provider's manga id" />
    <label class="form-label small mb-0">or a search</label>
    <input v-model.trim="move.query" class="form-control form-control-sm mb-2" :placeholder="manga?.title" />
    <div class="form-check"><input id="mf" v-model="move.full" class="form-check-input" type="checkbox" /><label class="form-check-label" for="mf">adopt the new provider's title and folder name</label></div>
  </FormModal>

  <FormModal title="Merge" :open="mergeOpen" submit-text="Plan" :disabled="!mergeOther" size="lg" @close="mergeOpen = false" @submit="planMerge">
    <input v-model.trim="mergeQ" class="form-control form-control-sm mb-2" placeholder="find the other manga" />
    <div class="list-group mb-2" style="max-height: 40vh; overflow: auto">
      <label v-for="m in (candidates.data.value?.mangas ?? []).filter((x) => x.id !== id)" :key="m.id" class="list-group-item list-group-item-action d-flex gap-2">
        <input v-model="mergeOther" class="form-check-input" type="radio" :value="m.id" />
        <span>{{ m.title }} <span class="small text-secondary">#{{ m.id }} · {{ m.chapters }} chapters</span></span>
      </label>
    </div>
    <div class="form-check"><input id="mk" v-model="mergeKeepThis" class="form-check-input" type="checkbox" /><label class="form-check-label" for="mk">keep this one (the other merges into it)</label></div>
  </FormModal>
</template>
