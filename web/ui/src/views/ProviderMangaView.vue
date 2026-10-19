<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import client, { ok, fetchJSON } from '@/api/client'
import { errorMessage, mangaGone } from '@/api/errors'
import { change } from '@/api/changes'
import { imageURL } from '@/api/urls'
import type { ApplyFields, Binding, ChangeResponse, Job, MetadataResult, MetadataSource, ProviderChapter } from '@/api/schemas'
import FillDialog from '@/components/FillDialog.vue'
import MetadataPicker from '@/components/MetadataPicker.vue'
import LoadState from '@/components/LoadState.vue'
import PagerNav from '@/components/PagerNav.vue'
import SiteIcon from '@/components/SiteIcon.vue'
import { usePager } from '@/composables/usePager'
import { useAction } from '@/composables/useAction'
import { useLoad } from '@/composables/useLoad'
import { num, sourceNames } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useChanges } from '@/stores/changes'
import { useJobs } from '@/stores/jobs'
import { usePlan } from '@/stores/plan'
import { useToasts } from '@/stores/toasts'

const route = useRoute()
const router = useRouter()
const provider = computed(() => String(route.params.provider))
const mangaID = computed(() => String(route.params.manga))
const changes = useChanges()
const jobs = useJobs()
const plan = usePlan()
const action = useAction()

// gone: the provider doesn't have the manga (any more), said as such; another error of the
// provider is said with its reason
const gone = ref(false)
const pm = useLoad(async () => {
  gone.value = false
  try {
    return await ok(client.GET('/v1/providers/{provider}/manga', { params: { path: { provider: provider.value }, query: { manga_id: mangaID.value } } }))
  } catch (e) {
    gone.value = mangaGone(e)
    throw new Error(gone.value ? errorMessage(e) : `Couldn't get it from ${provider.value}: ${errorMessage(e)}`)
  }
}, [provider, mangaID, () => changes.version])
const chapters = useLoad(
  () => ok(client.GET('/v1/providers/{provider}/chapters', { params: { path: { provider: provider.value }, query: { manga_id: mangaID.value } } })),
  [provider, mangaID, () => jobs.finished]
)
const info = useLoad(() => ok(client.GET('/v1/providers/{provider}', { params: { path: { provider: provider.value } } })), [provider])
const providerName = computed(() => info.data.value?.name ?? provider.value)
crumbs(() => [{ title: 'Providers', to: { name: 'providers' } }, { title: providerName.value }, { title: pm.data.value?.manga.title ?? mangaID.value }])

const selected = ref(new Set<string>())
const pager = usePager(computed(() => chapters.data.value), 100)
const key = (c: ProviderChapter) => `${c.volume}/${c.number}`
function toggle(c: ProviderChapter) {
  const s = new Set(selected.value)
  if (s.has(key(c))) s.delete(key(c))
  else s.add(key(c))
  selected.value = s
}
// selectMissing skips the unavailable chapters, saying how many when there are any.
function selectMissing() {
  const missing = (chapters.data.value ?? []).filter((c) => !c.downloaded)
  const available = missing.filter((c) => !c.unavailable)
  selected.value = new Set(available.map(key))
  const skipped = missing.length - available.length
  if (skipped) useToasts().push('info', `Selected ${available.length} chapters (${skipped} unavailable skipped)`)
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

// fill: the unavailable chapters from another provider
const fillOpen = ref(false)
const hasUnavailable = computed(() => (chapters.data.value ?? []).some((c) => c.unavailable))
function queued(j: Job) {
  useToasts().push('success', `Queued: ${j.title}`)
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
  <div v-if="gone" class="alert alert-warning" data-test="manga-gone">
    This manga isn't on {{ provider }} anymore (it may have been removed). Its downloaded chapters stay in the library; moving the library manga to another provider gets it a new source.
    <div class="small text-secondary mt-1">{{ pm.error.value }}</div>
  </div>
  <LoadState v-else :loading="pm.loading.value && !pm.data.value" :error="pm.error.value" />
  <template v-if="pm.data.value">
    <div class="row g-3 mb-3">
      <div class="col-4 col-md-2">
        <img v-if="pm.data.value.manga.cover" :src="imageURL(pm.data.value.manga.cover, pm.data.value.manga.url)" class="cover-large" alt="" />
      </div>
      <div class="col">
        <h4 class="mb-1">{{ pm.data.value.manga.title }}</h4>
        <div class="d-flex flex-wrap align-items-center gap-2 mb-2" data-test="live-source">
          <span class="badge text-bg-info d-inline-flex align-items-center gap-1" title="The site's data right now, not the library's"><SiteIcon :provider="provider" /> {{ providerName }} — live from the site</span>
          <span class="small text-secondary">{{ pm.data.value.manga.id }}</span>
          <a
            v-if="pm.data.value.manga.url || info.data.value?.preferred"
            class="btn btn-sm btn-outline-secondary py-0"
            :href="pm.data.value.manga.url || info.data.value?.preferred"
            target="_blank"
            rel="noopener noreferrer"
            data-test="open-site"
            ><i class="bi bi-box-arrow-up-right me-1" />Open on {{ providerName }}</a
          >
        </div>
        <div class="d-flex flex-wrap gap-2 mb-2">
          <RouterLink v-if="pm.data.value.library" class="btn btn-sm btn-outline-secondary" :to="{ name: 'library-manga', params: { id: pm.data.value.library } }">In the library</RouterLink>
          <RouterLink v-if="pm.data.value.watch" class="btn btn-sm btn-outline-secondary" :to="{ name: 'watch', params: { id: pm.data.value.watch } }">Watched</RouterLink>
          <button v-else class="btn btn-sm btn-outline-primary" @click="watchIt">Watch</button>
          <button class="btn btn-sm btn-outline-primary" data-test="bind" @click="openBind()">Bind</button>
        </div>
        <div class="d-flex flex-wrap gap-1">
          <span v-for="b in pm.data.value.bindings" :key="b.source" class="badge text-bg-secondary">
            <SiteIcon :source="b.source" /> {{ sourceNames[b.source] ?? b.source }} {{ b.id }}{{ b.default ? ' (default)' : '' }}
            <a href="#" class="text-reset ms-1" title="Replace" @click.prevent="openBind(b.source)"><i class="bi bi-arrow-repeat" /></a>
            <a href="#" class="text-reset ms-1" title="Unbind" @click.prevent="unbind(b)"><i class="bi bi-x" /></a>
          </span>
        </div>
      </div>
    </div>

    <div class="d-flex flex-wrap gap-2 align-items-center mb-2">
      <h6 class="me-auto mb-0">Chapters <small v-if="chapters.data.value" class="text-secondary">{{ chapters.data.value.length }}</small></h6>
      <button v-if="hasUnavailable" class="btn btn-sm btn-outline-primary" data-test="fill" @click="fillOpen = true">Get unavailable chapters from another provider</button>
      <button class="btn btn-sm btn-outline-secondary" @click="selectMissing">Select the missing</button>
      <button class="btn btn-sm btn-outline-secondary" :disabled="!selected.size" @click="selected = new Set()">Unselect</button>
      <button class="btn btn-sm btn-primary" :disabled="!selected.size || action.busy.value" data-test="download-selected" @click="download(selectorRanges())">Download {{ selected.size || '' }}</button>
    </div>
    <LoadState :loading="chapters.loading.value && !chapters.data.value" :error="chapters.error.value" :empty="chapters.data.value && !chapters.data.value.length" empty-text="No chapters." />
    <table v-if="chapters.data.value?.length" class="table table-sm table-hover" data-test="provider-chapters">
      <thead><tr><th style="width: 2rem"></th><th>#</th><th>Title</th><th>Vol</th><th></th><th></th></tr></thead>
      <tbody>
        <tr v-for="c in pager.items.value" :key="key(c)" :class="{ 'table-active': selected.has(key(c)), 'text-secondary': c.unavailable }">
          <td><input class="form-check-input" type="checkbox" :checked="selected.has(key(c))" :disabled="!!c.unavailable" @change="toggle(c)" /></td>
          <td>{{ num(c.number) }}</td>
          <td v-if="c.unavailable" data-test="unavailable" :title="`Chapter unavailable: ${c.unavailable}`">{{ c.title }} [{{ c.unavailable }}]</td>
          <td v-else><a href="#" class="text-decoration-none" @click.prevent="read(c)">{{ c.title }}</a></td>
          <td class="small">{{ num(c.volume) || '' }}</td>
          <td class="text-nowrap">
            <i v-if="c.downloaded" class="bi bi-download text-success me-1" title="downloaded" />
            <i v-if="c.reads" class="bi bi-check2 text-success" :title="`read ${c.reads}×`" />
          </td>
          <td class="text-end text-nowrap">
            <button v-if="!c.unavailable" class="btn btn-sm btn-link py-0" :title="c.downloaded ? 'Read (downloaded)' : 'Read online'" data-test="read-online" @click="read(c)"><i class="bi bi-book" /></button>
            <button v-if="!c.downloaded && !c.unavailable" class="btn btn-sm btn-link py-0" title="Download" @click="download([num(c.number)])"><i class="bi bi-download" /></button>
          </td>
        </tr>
      </tbody>
    </table>
    <PagerNav v-model="pager.offset.value" :total="pager.total.value" :size="pager.size" />
  </template>

  <FillDialog :open="fillOpen" :provider="provider" :manga-id="mangaID" :title="pm.data.value?.manga.title ?? mangaID" @close="fillOpen = false" @queued="queued" />
  <MetadataPicker :open="bindOpen" :title="`Bind ${pm.data.value?.manga.title ?? ''}`" :query="pm.data.value?.manga.title" :source="bindSource" @close="bindOpen = false" @pick="planBind" />
</template>
