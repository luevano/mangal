<script setup lang="ts">
import { computed } from 'vue'
import client, { ok } from '@/api/client'
import { change } from '@/api/changes'
import LoadState from '@/components/LoadState.vue'
import PagerNav from '@/components/PagerNav.vue'
import { usePager } from '@/composables/usePager'
import { useLoad } from '@/composables/useLoad'
import { size, plural } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useStatus } from '@/stores/status'
import { useChanges } from '@/stores/changes'
import { usePlan } from '@/stores/plan'

crumbs(() => [{ title: 'Library', to: { name: 'library' } }, { title: 'Trash' }])
const changes = useChanges()
const plan = usePlan()
const st = useStatus()
const { data, loading, error } = useLoad(() => ok(client.GET('/v1/library/trash')), [() => changes.version])
const pager = usePager(computed(() => data.value?.batches), 100)

function empty() {
  plan.open({ title: 'Empty the trash', verb: 'Empty', run: change('POST', '/library/trash/empty') })
}
</script>

<template>
  <div class="d-flex align-items-center mb-2">
    <h4 class="me-auto mb-0">Trash</h4>
    <button v-if="st.allows('POST /v1/library/trash/empty')" class="btn btn-sm btn-outline-danger" :disabled="!data?.files" data-test="empty-trash" @click="empty">Empty</button>
  </div>
  <LoadState :loading="loading && !data" :error="error" :empty="data && !data.batches.length" empty-text="The trash is empty." />
  <template v-if="data?.batches.length">
    <p class="small text-secondary">{{ plural(data.files, 'file') }}, {{ size(data.size) }}. Undoing an operation brings its files back; emptying the trash can't be undone.</p>
    <table class="table table-sm">
      <thead><tr><th>Batch</th><th class="text-end">Files</th><th class="text-end">Size</th></tr></thead>
      <tbody>
        <tr v-for="b in pager.items.value" :key="b.path">
          <td class="mono small">{{ b.name }}</td>
          <td class="text-end">{{ b.files }}</td>
          <td class="text-end">{{ size(b.size) }}</td>
        </tr>
      </tbody>
    </table>
    <PagerNav v-model="pager.offset.value" :total="pager.total.value" :size="pager.size" />
  </template>
</template>
