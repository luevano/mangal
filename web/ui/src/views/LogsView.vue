<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import client, { ok } from '@/api/client'
import type { LogEntry } from '@/api/schemas'
import LoadState from '@/components/LoadState.vue'
import { useLoad } from '@/composables/useLoad'
import { crumbs } from '@/stores/crumbs'
import { useEvents } from '@/stores/events'

crumbs(() => [{ title: 'Logs' }])
const events = useEvents()
const since = ref('1h')
const level = ref('info')
const grep = ref('')
const file = ref('')
const follow = ref(false)
const box = ref<HTMLElement>()
const live = ref<LogEntry[]>([])

const query = computed(() => ({ since: since.value || undefined, level: level.value || undefined, grep: grep.value || undefined, file: file.value || undefined, limit: 500 }))
const { data, loading, error } = useLoad(() => ok(client.GET('/v1/logs', { params: { query: query.value } })), [query])
const files = useLoad(() => ok(client.GET('/v1/logs/files')))

watch(data, () => (live.value = []))

const levels = ['debug', 'info', 'warn', 'error']
function levelOK(l: string) {
  return levels.indexOf(l) >= levels.indexOf(level.value || 'debug')
}
function grepOK(m: string) {
  if (!grep.value) return true
  try {
    return new RegExp(grep.value, /[A-Z]/.test(grep.value) ? '' : 'i').test(m)
  } catch {
    return m.includes(grep.value)
  }
}

let off: (() => void) | undefined
watch(follow, (on) => {
  off?.()
  off = undefined
  if (on)
    off = events.on('log', (e) => {
      const entry = e.data as LogEntry
      if (!entry || !levelOK(entry.level) || !grepOK(entry.message)) return
      if (file.value && entry.file !== file.value) return
      live.value.push(entry)
      if (live.value.length > 2000) live.value.splice(0, live.value.length - 2000)
    })
})
onUnmounted(() => off?.())

const entries = computed(() => [...(data.value ?? []), ...live.value])
watch(
  () => entries.value.length,
  async () => {
    if (!follow.value) return
    await nextTick()
    if (box.value) box.value.scrollTop = box.value.scrollHeight
  }
)

const levelClass: Record<string, string> = { error: 'text-danger', warn: 'text-warning', debug: 'text-secondary' }
</script>

<template>
  <h4>Logs</h4>
  <form class="row g-2 mb-2" @submit.prevent>
    <div class="col-6 col-md-2"><input v-model.lazy.trim="since" class="form-control form-control-sm" placeholder="since (30m, 7d)" title="since" /></div>
    <div class="col-6 col-md-2">
      <select v-model="level" class="form-select form-select-sm">
        <option v-for="l in levels" :key="l" :value="l">{{ l }}+</option>
      </select>
    </div>
    <div class="col-12 col-md-4"><input v-model.lazy.trim="grep" class="form-control form-control-sm" placeholder="grep (regexp)" /></div>
    <div class="col-8 col-md-3">
      <select v-model="file" class="form-select form-select-sm">
        <option value="">every file</option>
        <option v-for="f in files.data.value ?? []" :key="f.name" :value="f.name">{{ f.name }}</option>
      </select>
    </div>
    <div class="col-4 col-md-1 d-flex align-items-center">
      <div class="form-check form-switch mb-0">
        <input id="lf" v-model="follow" class="form-check-input" type="checkbox" data-test="logs-follow" />
        <label class="form-check-label small" for="lf">follow</label>
      </div>
    </div>
  </form>
  <LoadState :loading="loading && !data" :error="error" :empty="data && !entries.length" empty-text="No log lines." />
  <div v-if="entries.length" ref="box" class="font-monospace small border rounded p-2" style="height: 70vh; overflow: auto" data-test="logs">
    <div v-for="(e, i) in entries" :key="i" :class="levelClass[e.level]">
      <span class="text-secondary">{{ new Date(e.time).toLocaleString() }}</span> {{ e.level.toUpperCase() }} {{ e.message }}
    </div>
  </div>
</template>
