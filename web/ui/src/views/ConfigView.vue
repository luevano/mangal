<script setup lang="ts">
import { computed, ref } from 'vue'
import client, { ok } from '@/api/client'
import LoadState from '@/components/LoadState.vue'
import { useLoad } from '@/composables/useLoad'
import { crumbs } from '@/stores/crumbs'

crumbs(() => [{ title: 'Config' }])
const { data, loading, error } = useLoad(() => ok(client.GET('/v1/config')))
const filter = ref('')

const sections = computed(() => {
  const by = new Map<string, NonNullable<typeof data.value>>()
  const q = filter.value.toLowerCase()
  for (const e of data.value ?? []) {
    if (q && !e.key.toLowerCase().includes(q) && !e.description.toLowerCase().includes(q)) continue
    const s = e.key.split('.')[0] ?? ''
    if (!by.has(s)) by.set(s, [])
    by.get(s)!.push(e)
  }
  return [...by.entries()]
})

function show(v: unknown): string {
  if (v === undefined || v === null || v === '') return '—'
  return typeof v === 'string' ? v : JSON.stringify(v)
}
</script>

<template>
  <h4>Config</h4>
  <p class="small text-secondary">Read only here (secrets masked). <code>mangal config set</code> or the TUI change it; "restart" keys apply when serve restarts.</p>
  <input v-model="filter" class="form-control form-control-sm mb-3" style="max-width: 20rem" placeholder="filter" />
  <LoadState :loading="loading && !data" :error="error" />
  <template v-for="[s, entries] in sections" :key="s">
    <h6 class="mt-3">{{ s }}</h6>
    <table class="table table-sm small">
      <tbody>
        <tr v-for="e in entries" :key="e.key">
          <td class="mono" style="width: 30%">{{ e.key }}</td>
          <td class="mono" style="width: 25%" :class="{ 'text-secondary': JSON.stringify(e.value) === JSON.stringify(e.default) }">
            {{ show(e.value) }}<span v-if="e.env" class="badge text-bg-info ms-1" :title="`set by ${e.env}`">env</span>
          </td>
          <td class="text-secondary">{{ e.description }} <span class="badge text-bg-dark">{{ e.applies }}</span></td>
        </tr>
      </tbody>
    </table>
  </template>
</template>
