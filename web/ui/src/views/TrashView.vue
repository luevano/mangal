<script setup lang="ts">
import client, { ok } from '@/api/client'
import { change } from '@/api/changes'
import LoadState from '@/components/LoadState.vue'
import { useLoad } from '@/composables/useLoad'
import { size, plural } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useChanges } from '@/stores/changes'
import { usePlan } from '@/stores/plan'

crumbs(() => [{ title: 'Library', to: { name: 'library' } }, { title: 'Trash' }])
const changes = useChanges()
const plan = usePlan()
const { data, loading, error } = useLoad(() => ok(client.GET('/v1/library/trash')), [() => changes.version])

function empty() {
  plan.open({ title: 'Empty the trash', verb: 'Empty', run: change('POST', '/library/trash/empty') })
}
</script>

<template>
  <div class="d-flex align-items-center mb-2">
    <h4 class="me-auto mb-0">Trash</h4>
    <button class="btn btn-sm btn-outline-danger" :disabled="!data?.files" data-test="empty-trash" @click="empty">Empty</button>
  </div>
  <LoadState :loading="loading && !data" :error="error" :empty="data && !data.batches.length" empty-text="The trash is empty." />
  <template v-if="data?.batches.length">
    <p class="small text-secondary">{{ plural(data.files, 'file') }}, {{ size(data.size) }}. Undoing an operation brings its files back; emptying the trash can't be undone.</p>
    <table class="table table-sm">
      <thead><tr><th>Batch</th><th class="text-end">Files</th><th class="text-end">Size</th></tr></thead>
      <tbody>
        <tr v-for="b in data.batches" :key="b.path">
          <td class="mono small">{{ b.name }}</td>
          <td class="text-end">{{ b.files }}</td>
          <td class="text-end">{{ size(b.size) }}</td>
        </tr>
      </tbody>
    </table>
  </template>
</template>
