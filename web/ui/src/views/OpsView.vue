<script setup lang="ts">
import { useRoute } from 'vue-router'
import client, { ok } from '@/api/client'
import { change } from '@/api/changes'
import type { Op } from '@/api/schemas'
import LoadState from '@/components/LoadState.vue'
import PagerNav from '@/components/PagerNav.vue'
import { usePager } from '@/composables/usePager'
import { useLoad } from '@/composables/useLoad'
import { date } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useStatus } from '@/stores/status'
import { useChanges } from '@/stores/changes'
import { usePlan } from '@/stores/plan'

crumbs(() => [{ title: 'Library', to: { name: 'library' } }, { title: 'Operations' }])
const route = useRoute()
const changes = useChanges()
const plan = usePlan()
const st = useStatus()
const { data, loading, error } = useLoad(() => ok(client.GET('/v1/ops', { params: { query: { limit: 0 } } })), [() => changes.version])
const pager = usePager(data, 100)

function undo(o: Op) {
  plan.open({ title: `Undo ${o.kind} #${o.id}`, verb: 'Undo', run: change('POST', `/ops/${o.id}/undo`) })
}
</script>

<template>
  <h4>Operations</h4>
  <p class="small text-secondary">The journal: every library change, newest first. Undo restores the snapshot and the files.</p>
  <LoadState :loading="loading && !data" :error="error" :empty="data && !data.length" empty-text="No operations yet." />
  <table v-if="data?.length" class="table table-sm" data-test="ops-table">
    <thead><tr><th>#</th><th>Kind</th><th>Summary</th><th class="d-none d-md-table-cell">When</th><th></th></tr></thead>
    <tbody>
      <tr v-for="o in pager.items.value" :key="o.id" :class="{ 'table-active': String(o.id) === String(route.query.op) }">
        <td>{{ o.id }}</td>
        <td>{{ o.kind }}</td>
        <td class="small">
          {{ o.summary }}
          <span v-if="o.error" class="text-danger"> · {{ o.error }}</span>
          <span v-if="o.interfered" class="text-warning"> · {{ o.interfered }}</span>
        </td>
        <td class="d-none d-md-table-cell small">{{ date(o.started) }}</td>
        <td class="text-end">
          <span v-if="o.undone" class="small text-secondary">undone {{ date(o.undone) }}</span>
          <button v-else-if="st.allows('POST /v1/ops/{id}/undo')" class="btn btn-sm btn-outline-warning py-0" data-test="undo" @click="undo(o)">Undo</button>
        </td>
      </tr>
    </tbody>
  </table>
  <PagerNav v-model="pager.offset.value" :total="pager.total.value" :size="pager.size" />
</template>
