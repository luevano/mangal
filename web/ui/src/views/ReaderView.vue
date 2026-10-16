<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter, type RouteLocationRaw } from 'vue-router'
import client, { ok } from '@/api/client'
import { ApiError, errorMessage } from '@/api/errors'
import { authHeaders } from '@/api/auth'
import { chapterFileURL, libraryPageURL, providerPageURL } from '@/api/urls'
import type { ChapterPages } from '@/api/schemas'
import type { components } from '@/api/mangal'
import { num } from '@/composables/format'
import { useToasts } from '@/stores/toasts'
import {
  clampPage,
  keyAction,
  keyHelp,
  loadIncognito,
  loadPosition,
  loadSettings,
  modes,
  newer,
  fits,
  neighbors,
  nextFit,
  preload,
  saveIncognito,
  savePosition,
  saveSettings,
  step,
  zoneAction,
  type Action,
  type Settings
} from '@/reader/reader'

type ReadRequest = components['schemas']['ReadRequest']

interface ChapterRef {
  key: string
  label: string
  to: RouteLocationRaw
  read: ReadRequest
}

const route = useRoute()
const router = useRouter()
const toasts = useToasts()

const library = computed(() => route.name === 'read-library')
const mangaKey = computed(() => (library.value ? `lib:${route.params.id}` : `${route.params.provider}:${route.params.manga}`))
const chapterKey = computed(() =>
  library.value ? `lib:${route.params.id}:${route.query.path}` : `${route.params.provider}:${route.params.manga}:${route.query.volume ?? ''}:${route.query.chapter}`
)

const settings = ref<Settings>(loadSettings(mangaKey.value))
const incognito = ref(loadIncognito())
const title = ref('')
const chapters = ref<ChapterRef[]>([])
const pages = ref<ChapterPages>()
const urls = ref<string[]>([])
const pdf = ref('')
const index = ref(0)
const loading = ref(true)
const error = ref('')
const menu = ref(true)
const help = ref(false)
const prompt = ref<'start' | 'end'>()
const jumpOpen = ref(false)
const jumpTo = ref('')
const recorded = ref(false)
const webtoon = ref<HTMLElement>()
let observer: IntersectionObserver | undefined

const current = computed(() => chapters.value.findIndex((c) => c.key === chapterKey.value))
// the chapters before and after, other files of the same chapter skipped
const near = computed(() => {
  const list = chapters.value
  const i = current.value
  const n = list[i]?.read.chapter
  return neighbors(
    list.filter((c, j) => j === i || c.read.chapter !== n),
    list.filter((c, j) => j < i && c.read.chapter !== n).length
  )
})
const chapter = computed(() => chapters.value[current.value])
const count = computed(() => urls.value.length)
const preloads = computed(() => (settings.value.mode === 'webtoon' ? [] : preload(index.value, count.value)))

watch(settings, (s) => saveSettings(mangaKey.value, s), { deep: true })
watch(incognito, saveIncognito)

async function loadLibrary() {
  const id = Number(route.params.id)
  const path = String(route.query.path ?? '')
  const [list, pg] = await Promise.all([
    ok(client.GET('/v1/library/mangas/{id}/chapters', { params: { path: { id } } })),
    ok(client.GET('/v1/library/mangas/{id}/pages', { params: { path: { id }, query: { path } } }))
  ])
  const sorted = [...list].sort((a, b) => a.download.volume - b.download.volume || a.download.chapter - b.download.chapter)
  chapters.value = sorted.map((c) => {
    const d = c.download
    return {
      key: `lib:${id}:${d.path}`,
      label: `${num(d.chapter)}${d.chapter_title ? ` · ${d.chapter_title}` : ''}`,
      to: { name: 'read-library', params: { id }, query: { path: d.path } },
      read: { provider: d.provider, manga_id: d.manga_id, manga_title: d.manga_title, volume: d.volume, chapter: d.chapter, chapter_title: d.chapter_title, chapter_url: d.chapter_url, library_manga: id }
    }
  })
  title.value = sorted[0]?.download.manga_title ?? ''
  pages.value = pg
  if (pg.pdf) {
    // a blob url: the API's answers are sandboxed (CSP), which a pdf viewer can't run in
    const res = await fetch(chapterFileURL(id, path, true), { headers: authHeaders() })
    if (!res.ok) throw new ApiError(res.status, undefined, res.statusText)
    pdf.value = URL.createObjectURL(await res.blob())
    urls.value = []
  } else {
    urls.value = pg.pages.map((p) => libraryPageURL(id, path, p.index))
  }
}

async function loadOnline() {
  const provider = String(route.params.provider)
  const mangaID = String(route.params.manga)
  const number = Number(route.query.chapter)
  const volume = route.query.volume !== undefined && route.query.volume !== '' ? Number(route.query.volume) : undefined
  const [list, pg, pm] = await Promise.all([
    ok(client.GET('/v1/providers/{provider}/chapters', { params: { path: { provider }, query: { manga_id: mangaID } } })),
    ok(client.GET('/v1/providers/{provider}/pages', { params: { path: { provider }, query: { manga_id: mangaID, chapter: number, volume } } })),
    ok(client.GET('/v1/providers/{provider}/manga', { params: { path: { provider }, query: { manga_id: mangaID } } }))
  ])
  title.value = pm.manga.title
  // in reading order: the providers list them their way
  const ordered = [...list].sort((a, b) => a.number - b.number || a.volume - b.volume)
  chapters.value = ordered.map((c) => ({
    key: `${provider}:${mangaID}:${c.volume}:${c.number}`,
    label: `${num(c.number)}${c.title ? ` · ${c.title}` : ''}`,
    to: { name: 'read-online', params: { provider, manga: mangaID }, query: { chapter: String(c.number), volume: String(c.volume) } },
    read: { provider, manga_id: mangaID, manga_title: pm.manga.title, volume: c.volume, chapter: c.number, chapter_title: c.title, chapter_url: c.url }
  }))
  // a chapter asked without its volume: the first with the number
  if (volume === undefined) {
    const c = list.find((x) => x.number === number)
    if (c) router.replace({ query: { ...route.query, volume: String(c.volume) } })
  }
  pages.value = pg
  urls.value = pg.pages.map((p) => providerPageURL(provider, mangaID, number, volume ?? list.find((x) => x.number === number)?.volume, p.index))
}

async function load() {
  loading.value = true
  error.value = ''
  prompt.value = undefined
  recorded.value = false
  if (pdf.value) URL.revokeObjectURL(pdf.value)
  pdf.value = ''
  settings.value = loadSettings(mangaKey.value)
  try {
    if (library.value) await loadLibrary()
    else await loadOnline()
    const asked = route.query.page === 'last' ? count.value : Number(route.query.page ?? 0)
    const saved = newer(loadPosition(chapterKey.value), await remotePosition(chapterKey.value)).page
    index.value = clampPage(asked || (saved < count.value - 1 ? saved + 1 : 1), count.value)
    if (pdf.value) record()
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    loading.value = false
  }
  await nextTick()
  setupWebtoon()
  if (settings.value.mode === 'webtoon') scrollToPage(index.value, false)
}

watch(() => route.fullPath, load)
onMounted(load)

async function record() {
  if (recorded.value || incognito.value || !chapter.value) return
  recorded.value = true
  try {
    const res = await ok(client.POST('/v1/reads', { body: chapter.value.read }))
    if (res.error) toasts.push('warning', `Not synced: ${res.error}`, 6000)
    if (res.note) toasts.push('info', `Not in the read history: ${res.note}`, 4000)
    else if (res.synced.length) toasts.push('success', `Read; synced to ${res.synced.join(', ')}`, 3000)
  } catch (e) {
    recorded.value = false
    const hint = e instanceof ApiError && e.needsToken ? ' (needs a token with the progress scope)' : ''
    toasts.push('warning', `The read wasn't recorded: ${errorMessage(e)}${hint}`, 6000)
  }
}

// the position on the server, for another device to resume; none when it can't be read
async function remotePosition(chapter: string) {
  try {
    const p = await ok(client.GET('/v1/reader/position', { params: { query: { chapter } } }))
    if (p.found && p.updated) return { page: p.page, at: Date.parse(p.updated) }
  } catch {
    // offline or no read scope: the browser's position
  }
  return undefined
}

// the page goes to the server a moment after it settles (not on every page turn), and
// right away when the chapter is left; not in incognito
let pending: { chapter: string; page: number; at: number } | undefined
let pushTimer: ReturnType<typeof setTimeout> | undefined
function pushPosition() {
  clearTimeout(pushTimer)
  const p = pending
  pending = undefined
  if (!p) return
  client.PUT('/v1/reader/position', { body: { chapter: p.chapter, page: p.page, updated: new Date(p.at).toISOString() } }).catch(() => {})
}

watch(index, (i) => {
  const at = Date.now()
  savePosition(chapterKey.value, i, at)
  if (!incognito.value) {
    if (pending && pending.chapter !== chapterKey.value) pushPosition()
    pending = { chapter: chapterKey.value, page: i, at }
    clearTimeout(pushTimer)
    pushTimer = setTimeout(pushPosition, 1500)
  }
  if (count.value && i >= count.value - 1) record()
})

function go(i: number) {
  index.value = Math.min(Math.max(i, 0), Math.max(0, count.value - 1))
  prompt.value = undefined
  if (settings.value.mode === 'webtoon') scrollToPage(index.value)
  else window.scrollTo({ top: 0 })
}

function openChapter(c: ChapterRef | undefined, last = false) {
  if (!c) return
  prompt.value = undefined
  const to = c.to as { name: string; params: Record<string, unknown>; query: Record<string, string> }
  router.replace({ ...to, query: { ...to.query, ...(last ? { page: 'last' } : { page: '1' }) } } as RouteLocationRaw)
}

function back() {
  if (window.history.length > 1) router.back()
  else if (library.value) router.push({ name: 'library-manga', params: { id: route.params.id } })
  else router.push({ name: 'provider-manga', params: { provider: route.params.provider, manga: route.params.manga } })
}

function act(a: Action) {
  switch (a) {
    case 'next':
    case 'prev':
    case 'first':
    case 'last': {
      if (prompt.value && (a === 'next' || a === 'prev')) {
        if (prompt.value === 'end' && a === 'next') return openChapter(near.value.next)
        if (prompt.value === 'start' && a === 'prev') return openChapter(near.value.prev, true)
        prompt.value = undefined
        return
      }
      const r = step(index.value, count.value, a)
      if (r.edge === 'end') {
        prompt.value = 'end'
        record()
      } else if (r.edge === 'start') prompt.value = 'start'
      else go(r.index)
      return
    }
    case 'next-chapter':
      return openChapter(near.value.next)
    case 'prev-chapter':
      return openChapter(near.value.prev)
    case 'menu':
      menu.value = !menu.value
      return
    case 'help':
      help.value = !help.value
      return
    case 'close':
      if (help.value || jumpOpen.value || prompt.value) {
        help.value = jumpOpen.value = false
        prompt.value = undefined
      } else back()
      return
    case 'fit':
      settings.value.fit = nextFit(settings.value.fit)
      return
    case 'fullscreen':
      if (document.fullscreenElement) document.exitFullscreen()
      else document.documentElement.requestFullscreen?.().catch(() => {})
      return
    case 'jump':
      menu.value = true
      jumpOpen.value = true
      jumpTo.value = String(index.value + 1)
      return
    case 'mode-ltr':
    case 'mode-rtl':
    case 'mode-webtoon':
      setMode(a.slice(5) as Settings['mode'])
      return
  }
}

function setMode(m: Settings['mode']) {
  settings.value.mode = m
  nextTick(() => {
    setupWebtoon()
    if (m === 'webtoon') scrollToPage(index.value, false)
  })
}

function onKey(e: KeyboardEvent) {
  const t = e.target as HTMLElement | null
  if (t && ['INPUT', 'SELECT', 'TEXTAREA'].includes(t.tagName)) {
    if (e.key === 'Escape') (t as HTMLInputElement).blur()
    return
  }
  if (e.ctrlKey || e.metaKey || e.altKey) return
  const a = keyAction(e.key, settings.value.mode)
  if (!a) return
  e.preventDefault()
  act(a)
}

function onTap(e: MouseEvent) {
  const el = e.currentTarget as HTMLElement
  const rect = el.getBoundingClientRect()
  act(zoneAction(e.clientX - rect.left, rect.width, settings.value.mode))
}

function submitJump() {
  jumpOpen.value = false
  go(clampPage(Number(jumpTo.value), count.value))
}

// webtoon: the page crossing the middle of the screen is the current one
function setupWebtoon() {
  observer?.disconnect()
  observer = undefined
  if (settings.value.mode !== 'webtoon' || !webtoon.value) return
  observer = new IntersectionObserver(
    (entries) => {
      for (const en of entries) {
        if (!en.isIntersecting) continue
        const i = Number((en.target as HTMLElement).dataset.index)
        if (Number.isFinite(i) && i !== index.value) index.value = i
      }
    },
    { rootMargin: '-50% 0px -50% 0px' }
  )
  webtoon.value.querySelectorAll('img[data-index]').forEach((img) => observer!.observe(img))
}

function scrollToPage(i: number, smooth = true) {
  const img = webtoon.value?.querySelector(`img[data-index="${i}"]`)
  img?.scrollIntoView({ behavior: smooth ? 'smooth' : 'instant', block: 'start' })
}

function onWebtoonEnd() {
  if (settings.value.mode !== 'webtoon') return
  const el = document.scrollingElement
  if (el && el.scrollTop + el.clientHeight >= el.scrollHeight - 4 && count.value) {
    record()
  }
}

onMounted(() => {
  window.addEventListener('keydown', onKey)
  window.addEventListener('scroll', onWebtoonEnd, { passive: true })
})
onUnmounted(() => {
  pushPosition()
  window.removeEventListener('keydown', onKey)
  window.removeEventListener('scroll', onWebtoonEnd)
  observer?.disconnect()
  if (pdf.value) URL.revokeObjectURL(pdf.value)
})

const bg = computed(() => ({ black: '#000', gray: '#555', white: '#fff' })[settings.value.background])
const imgClass = computed(() => `fit-${settings.value.fit}`)
document.title = 'Reader · mangal'
watch([title, chapter], () => {
  document.title = `${title.value}${chapter.value ? ` ${chapter.value.label}` : ''} · mangal`
})
</script>

<template>
  <div class="reader" :style="{ background: bg }" data-test="reader" :data-mode="settings.mode">
    <!-- toolbar -->
    <div v-show="menu" class="reader-bar top d-flex flex-wrap align-items-center gap-2 px-2 py-1" data-test="reader-bar">
      <button class="btn btn-sm btn-outline-light" title="Back (esc)" @click="back"><i class="bi bi-arrow-left" /></button>
      <span class="text-truncate small me-auto" style="max-width: 40vw">
        <strong>{{ title }}</strong>
        <span v-if="chapter"> · {{ chapter.label }}</span>
      </span>
      <select class="form-select form-select-sm w-auto" :value="chapterKey" title="Chapter" @change="openChapter(chapters.find((c) => c.key === ($event.target as HTMLSelectElement).value))">
        <option v-for="c in chapters" :key="c.key" :value="c.key">{{ c.label }}</option>
      </select>
      <div class="btn-group btn-group-sm" role="group" aria-label="Mode">
        <button v-for="m in modes" :key="m" class="btn" :class="settings.mode === m ? 'btn-light' : 'btn-outline-light'" :data-test="`mode-${m}`" @click="setMode(m)">
          {{ m === 'ltr' ? 'LTR' : m === 'rtl' ? 'RTL' : 'Webtoon' }}
        </button>
      </div>
      <select v-model="settings.fit" class="form-select form-select-sm w-auto" title="Fit (c)">
        <option v-for="f in fits" :key="f" :value="f">fit {{ f }}</option>
      </select>
      <select v-model="settings.background" class="form-select form-select-sm w-auto" title="Background">
        <option value="black">black</option>
        <option value="gray">gray</option>
        <option value="white">white</option>
      </select>
      <div class="form-check form-switch mb-0 small text-light" title="Incognito: no read history, no sync">
        <input id="incog" v-model="incognito" class="form-check-input" type="checkbox" data-test="incognito" />
        <label class="form-check-label" for="incog"><i class="bi bi-incognito" /></label>
      </div>
      <button class="btn btn-sm btn-outline-light" title="Fullscreen (f)" @click="act('fullscreen')"><i class="bi bi-arrows-fullscreen" /></button>
      <button class="btn btn-sm btn-outline-light" title="Help (h)" @click="help = !help"><i class="bi bi-question-lg" /></button>
    </div>

    <div v-if="loading" class="text-light p-4"><span class="spinner-border spinner-border-sm me-2" />Loading…</div>
    <div v-else-if="error" class="alert alert-danger m-3">{{ error }}</div>

    <iframe v-else-if="pdf" :src="pdf" class="reader-pdf" title="PDF" />

    <!-- paged -->
    <div v-else-if="settings.mode !== 'webtoon'" class="reader-page" data-test="reader-paged" @click="onTap">
      <img v-if="urls[index]" :key="urls[index]" :src="urls[index]" :class="imgClass" :alt="`page ${index + 1}`" data-test="reader-image" />
      <img v-for="i in preloads" :key="'p' + i" :src="urls[i]" class="d-none" alt="" loading="eager" />
    </div>

    <!-- webtoon -->
    <div v-else ref="webtoon" class="reader-strip" data-test="reader-webtoon" @click="onTap">
      <img v-for="(u, i) in urls" :key="u" :src="u" :data-index="i" :class="imgClass" :alt="`page ${i + 1}`" loading="lazy" />
    </div>

    <!-- bottom bar: page -->
    <div v-show="menu && count" class="reader-bar bottom d-flex align-items-center gap-2 px-2 py-1">
      <button class="btn btn-sm btn-outline-light" :disabled="!near.prev" title="Previous chapter (P)" @click="act('prev-chapter')"><i class="bi bi-skip-start" /></button>
      <input type="range" class="form-range flex-grow-1" min="1" :max="count" :value="index + 1" :style="settings.mode === 'rtl' ? 'direction: rtl' : ''" @change="go(Number(($event.target as HTMLInputElement).value) - 1)" />
      <form v-if="jumpOpen" class="d-flex" @submit.prevent="submitJump">
        <input v-model="jumpTo" type="number" min="1" :max="count" class="form-control form-control-sm" style="width: 5rem" autofocus />
      </form>
      <button v-else class="btn btn-sm btn-outline-light text-nowrap" title="Jump to a page (g)" data-test="reader-page" @click="act('jump')">{{ index + 1 }} / {{ count }}</button>
      <button class="btn btn-sm btn-outline-light" :disabled="!near.next" title="Next chapter (N)" @click="act('next-chapter')"><i class="bi bi-skip-end" /></button>
    </div>

    <!-- end / start of chapter -->
    <div v-if="prompt" class="reader-prompt card text-bg-dark shadow" data-test="reader-prompt">
      <div class="card-body">
        <template v-if="prompt === 'end'">
          <p class="mb-2">End of chapter {{ chapter?.label }}.</p>
          <button v-if="near.next" class="btn btn-primary btn-sm me-2" data-test="next-chapter" @click="openChapter(near.next)">Next: {{ near.next.label }}</button>
          <span v-else class="text-secondary me-2">That was the last chapter.</span>
        </template>
        <template v-else>
          <p class="mb-2">Start of chapter {{ chapter?.label }}.</p>
          <button v-if="near.prev" class="btn btn-primary btn-sm me-2" @click="openChapter(near.prev, true)">Previous: {{ near.prev.label }}</button>
          <span v-else class="text-secondary me-2">That's the first chapter.</span>
        </template>
        <button class="btn btn-outline-light btn-sm" @click="prompt = undefined">Stay</button>
      </div>
    </div>

    <!-- help -->
    <div v-if="help" class="reader-prompt card text-bg-dark shadow" data-test="reader-help">
      <div class="card-body small">
        <h6>Keys ({{ settings.mode }})</h6>
        <table class="table table-sm table-dark mb-2">
          <tbody>
            <tr v-for="[k, what] in keyHelp(settings.mode)" :key="k"><td class="text-nowrap"><kbd>{{ k }}</kbd></td><td>{{ what }}</td></tr>
          </tbody>
        </table>
        <p class="text-secondary mb-2">The mode and fit are remembered per manga. A chapter read to its end is recorded in the read history and synced (AniList, MyAnimeList, Kitsu per the config), unless incognito.</p>
        <button class="btn btn-outline-light btn-sm" @click="help = false">Close</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.reader {
  min-height: 100vh;
  color: #ddd;
}
.reader-bar {
  position: fixed;
  left: 0;
  right: 0;
  z-index: 10;
  background: rgba(20, 20, 20, 0.88);
}
.reader-bar.top {
  top: 0;
}
.reader-bar.bottom {
  bottom: 0;
}
.reader-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  user-select: none;
}
.reader-strip {
  display: flex;
  flex-direction: column;
  align-items: center;
  user-select: none;
}
.reader-strip img {
  display: block;
  min-height: 2rem;
}
.reader-pdf {
  width: 100vw;
  height: 100vh;
  border: 0;
}
.fit-screen {
  max-width: 100vw;
  max-height: 100vh;
  object-fit: contain;
}
.reader-strip .fit-screen,
.reader-strip .fit-height {
  max-width: 100%;
  max-height: none;
}
.fit-width {
  width: 100vw;
  height: auto;
}
.reader-strip .fit-width {
  width: 100%;
}
.fit-height {
  height: 100vh;
  width: auto;
}
.fit-original {
  max-width: none;
}
.reader-prompt {
  position: fixed;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  z-index: 20;
  max-width: 90vw;
  max-height: 80vh;
  overflow: auto;
}
</style>
