<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import client, { ok } from '@/api/client'
import { errorMessage } from '@/api/errors'
import { imageURL } from '@/api/urls'
import type { MetadataResult, MetadataSource } from '@/api/schemas'
import { num } from '@/composables/format'

// MetadataPicker is the TUI's metadata search (bindings s, links): search a source, the
// closest match first, pick one (the caller plans the binding or the link).
const props = defineProps<{ open: boolean; title: string; query?: string; source?: string; sources?: string[] }>()
const emit = defineEmits<{ close: []; pick: [result: MetadataResult, source: MetadataSource] }>()

const all = ref<MetadataSource[]>([])
const source = ref(props.source ?? 'al')
const q = ref(props.query ?? '')
const results = ref<MetadataResult[]>()
const loading = ref(false)
const error = ref('')
let seq = 0

const sources = computed(() => all.value.filter((s) => !props.sources || props.sources.includes(s.code)))
const current = computed(() => all.value.find((s) => s.code === source.value))

async function search() {
  const query = q.value.trim()
  if (!query || !current.value?.enabled) return
  const mine = ++seq
  loading.value = true
  error.value = ''
  try {
    const r = await ok(client.GET('/v1/metadata/{source}/search', { params: { path: { source: source.value }, query: { q: query } } }))
    if (mine === seq) results.value = r
  } catch (e) {
    if (mine === seq) error.value = errorMessage(e)
  } finally {
    if (mine === seq) loading.value = false
  }
}

watch(
  () => props.open,
  async (open) => {
    if (!open) return
    q.value = props.query ?? ''
    if (props.source) source.value = props.source
    results.value = undefined
    error.value = ''
    if (!all.value.length) {
      try {
        all.value = await ok(client.GET('/v1/metadata/sources'))
      } catch (e) {
        error.value = errorMessage(e)
        return
      }
    }
    if (!current.value?.enabled) source.value = sources.value.find((s) => s.enabled)?.code ?? source.value
    search()
  },
  { immediate: true }
)
watch(source, () => {
  results.value = undefined
  search()
})

function pick(r: MetadataResult) {
  if (current.value) emit('pick', r, current.value)
}
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
              <select v-model="source" class="form-select form-select-sm w-auto">
                <option v-for="s in sources" :key="s.code" :value="s.code" :disabled="!s.enabled" :title="s.reason">{{ s.name }}{{ s.enabled ? '' : ' (off)' }}</option>
              </select>
              <input v-model="q" class="form-control form-control-sm flex-grow-1 w-auto" placeholder="title, or an id" data-test="metadata-query" />
              <button class="btn btn-sm btn-primary" type="submit" :disabled="loading || !current?.enabled"><i class="bi bi-search" /></button>
            </form>
            <p v-if="current && !current.enabled" class="small text-warning">{{ current.reason }}</p>
            <p v-if="error" class="small text-danger">{{ error }}</p>
            <div v-if="loading" class="text-secondary small"><span class="spinner-border spinner-border-sm me-1" />Searching {{ current?.name }}…</div>
            <p v-else-if="results && !results.length" class="text-secondary small">Nothing found.</p>
            <div class="list-group">
              <div v-for="r in results ?? []" :key="r.id" class="list-group-item d-flex gap-3 align-items-start" data-test="metadata-result">
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
                    <a :href="r.url" target="_blank" rel="noopener noreferrer">{{ current?.name }} {{ r.id }}</a>
                    <span v-if="r.bad" class="badge text-bg-warning ms-2" title="too unlike the search to be used automatically">bad match {{ Math.round(r.score * 100) }}%</span>
                  </div>
                </div>
                <button class="btn btn-sm btn-outline-primary" data-test="metadata-pick" @click="pick(r)">Pick</button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
    <div class="modal-backdrop show" />
  </template>
</template>
