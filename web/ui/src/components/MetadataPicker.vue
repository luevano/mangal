<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import client, { ok } from '@/api/client'
import { errorMessage } from '@/api/errors'
import { imageURL } from '@/api/urls'
import type { MetadataResult, MetadataSource } from '@/api/schemas'
import { num } from '@/composables/format'

// MetadataPicker is the TUI's metadata search (bindings s, links): search every source at
// once (or the given ones), one request each, so each fills in as it answers: the closest
// match first, pick one (the caller plans the binding or the link).
const props = defineProps<{ open: boolean; title: string; query?: string; source?: string; sources?: string[] }>()
const emit = defineEmits<{ close: []; pick: [result: MetadataResult, source: MetadataSource] }>()

interface SourceResults {
  source: MetadataSource
  loading: boolean
  error: string
  results?: MetadataResult[]
}

const all = ref<MetadataSource[]>([])
const q = ref(props.query ?? '')
const searches = ref<SourceResults[]>([])
const error = ref('')
let abort: AbortController | undefined

// the one asked for first
const sources = computed(() =>
  all.value.filter((s) => !props.sources || props.sources.includes(s.code)).sort((a, b) => Number(b.code === props.source) - Number(a.code === props.source))
)
const enabled = computed(() => sources.value.filter((s) => s.enabled))
const searching = computed(() => searches.value.some((s) => s.loading))

function search() {
  abort?.abort()
  const query = q.value.trim()
  if (!query) return
  const ctl = (abort = new AbortController())
  searches.value = enabled.value.map((source) => ({ source, loading: true, error: '' }))
  searches.value.forEach(async (s) => {
    try {
      s.results = await ok(client.GET('/v1/metadata/{source}/search', { params: { path: { source: s.source.code }, query: { q: query } }, signal: ctl.signal }))
    } catch (e) {
      if (ctl.signal.aborted) return
      s.error = errorMessage(e)
    }
    s.loading = false
  })
}

watch(
  () => props.open,
  async (open) => {
    if (!open) {
      abort?.abort()
      return
    }
    q.value = props.query ?? ''
    searches.value = []
    error.value = ''
    if (!all.value.length) {
      try {
        all.value = await ok(client.GET('/v1/metadata/sources'))
      } catch (e) {
        error.value = errorMessage(e)
        return
      }
    }
    search()
  },
  { immediate: true }
)
onScopeDispose(() => abort?.abort())
</script>

<template>
  <template v-if="open">
    <div class="modal d-block" tabindex="-1" role="dialog" aria-modal="true" data-test="metadata-picker">
      <div class="modal-dialog modal-dialog-scrollable modal-lg">
        <div class="modal-content">
          <div class="modal-header">
            <h5 class="modal-title">{{ title }}</h5>
            <button type="button" class="btn-close" aria-label="Close" @click="emit('close')" />
          </div>
          <div class="modal-body">
            <form class="d-flex flex-wrap gap-2 mb-2" @submit.prevent="search">
              <input v-model="q" class="form-control form-control-sm flex-grow-1 w-auto" placeholder="title, or an id" data-test="metadata-query" />
              <button class="btn btn-sm btn-primary" type="submit" :disabled="searching || !enabled.length"><i class="bi bi-search" /></button>
            </form>
            <p v-for="s in sources.filter((s) => !s.enabled)" :key="s.code" class="small text-warning mb-1" data-test="metadata-off">{{ s.name }}: {{ s.reason }}</p>
            <p v-if="error" class="small text-danger">{{ error }}</p>
            <section v-for="s in searches" :key="s.source.code" class="mb-3" :data-test="`metadata-source-${s.source.code}`">
              <h6 class="mb-1">
                {{ s.source.name }}
                <span v-if="s.loading" class="spinner-border spinner-border-sm ms-1" role="status" aria-label="Searching" />
                <span v-else-if="s.results" class="badge text-bg-secondary ms-1">{{ s.results.length }}</span>
              </h6>
              <p v-if="s.error" class="small text-danger mb-1" data-test="metadata-error">{{ s.error }}</p>
              <p v-else-if="s.results && !s.results.length" class="text-secondary small mb-1">Nothing found.</p>
              <div class="list-group">
                <div v-for="r in s.results ?? []" :key="r.id" class="list-group-item d-flex gap-3 align-items-start" data-test="metadata-result">
                  <img v-if="r.cover" :src="imageURL(r.cover)" alt="" style="width: 3.5rem; aspect-ratio: 5 / 7; object-fit: cover" loading="lazy" />
                  <div v-else class="bg-body-secondary" style="width: 3.5rem; aspect-ratio: 5 / 7" />
                  <div class="me-auto">
                    <div class="fw-semibold">{{ r.title }}<span v-if="r.year" class="text-secondary fw-normal"> ({{ r.year }})</span></div>
                    <div class="small text-secondary">
                      <span v-if="r.format">{{ r.format.toLowerCase() }}</span>
                      <span v-if="r.status"> · {{ r.status.replace(/_/g, ' ') }}</span>
                      <span v-if="r.chapters"> · {{ num(r.chapters) }} chapters</span>
                      <span v-if="r.authors?.length"> · {{ r.authors.slice(0, 2).join(', ') }}</span>
                    </div>
                    <div class="small">
                      <a :href="r.url" target="_blank" rel="noopener noreferrer">{{ s.source.name }} {{ r.id }}</a>
                      <span v-if="r.bad" class="badge text-bg-warning ms-2" title="too unlike the search to be used automatically">bad match {{ Math.round(r.score * 100) }}%</span>
                    </div>
                  </div>
                  <button class="btn btn-sm btn-outline-primary" data-test="metadata-pick" @click="emit('pick', r, s.source)">Pick</button>
                </div>
              </div>
              <p v-if="s.source.code === 'mu' && s.results?.length" class="small text-secondary mt-1 mb-0" data-test="metadata-credit">
                Data from <a href="https://www.mangaupdates.com/" target="_blank" rel="noopener noreferrer">MangaUpdates</a>
              </p>
            </section>
          </div>
        </div>
      </div>
    </div>
    <div class="modal-backdrop show" />
  </template>
</template>
