<script setup lang="ts">
import { RouterLink } from 'vue-router'
import client, { ok } from '@/api/client'
import LoadState from '@/components/LoadState.vue'
import PagerNav from '@/components/PagerNav.vue'
import { usePager } from '@/composables/usePager'
import { useLoad } from '@/composables/useLoad'
import { date } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useWatches } from '@/stores/watches'

crumbs(() => [{ title: 'Watches', to: { name: 'watches' } }, { title: 'Check runs' }])
const watches = useWatches()
const { data, loading, error } = useLoad(() => ok(client.GET('/v1/runs', { params: { query: { limit: 0 } } })), [() => watches.runsVersion])
const pager = usePager(data, 100)
</script>

<template>
  <h4>Check runs</h4>
  <LoadState :loading="loading && !data" :error="error" :empty="data && !data.length" empty-text="No checks yet." />
  <table v-if="data?.length" class="table table-sm">
    <thead><tr><th>Run</th><th>Kind</th><th>Started</th><th>Finished</th><th class="text-end">Watches</th><th class="text-end">New</th><th class="text-end">Errors</th></tr></thead>
    <tbody>
      <tr v-for="r in pager.items.value" :key="r.id">
        <td><RouterLink :to="{ name: 'run', params: { id: r.id } }">#{{ r.id }}</RouterLink></td>
        <td class="small">{{ r.kind }}</td>
        <td class="small">{{ date(r.started) }}</td>
        <td class="small">{{ date(r.finished) || 'running' }}</td>
        <td class="text-end">{{ r.watches }}</td>
        <td class="text-end">{{ r.new_chapters }}</td>
        <td class="text-end" :class="{ 'text-danger': r.errors }">{{ r.errors }}</td>
      </tr>
    </tbody>
  </table>
  <PagerNav v-model="pager.offset.value" :total="pager.total.value" :size="pager.size" />
</template>
