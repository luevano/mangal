<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import client, { ok } from '@/api/client'
import LiveLog from '@/components/LiveLog.vue'
import LoadState from '@/components/LoadState.vue'
import PagerNav from '@/components/PagerNav.vue'
import { usePager } from '@/composables/usePager'
import { useAction } from '@/composables/useAction'
import { useLoad } from '@/composables/useLoad'
import { date, num } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useWatches } from '@/stores/watches'

const route = useRoute()
const id = computed(() => Number(route.params.id))
const watches = useWatches()
const action = useAction()
const run = useLoad(() => ok(client.GET('/v1/runs/{id}', { params: { path: { id: id.value } } })), [id, () => watches.runsVersion])
crumbs(() => [{ title: 'Watches', to: { name: 'watches' } }, { title: 'Check runs', to: { name: 'runs' } }, { title: `#${id.value}` }])
const live = computed(() => watches.live.filter((e) => e.run === id.value))
const pager = usePager(computed(() => run.data.value?.results), 100)

function cancel() {
  action.run(() => ok(client.POST('/v1/runs/{id}/cancel', { params: { path: { id: id.value } } })), 'Cancelling')
}
</script>

<template>
  <LoadState :loading="run.loading.value && !run.data.value" :error="run.error.value" />
  <template v-if="run.data.value">
    <div class="d-flex align-items-center mb-2">
      <h4 class="me-auto mb-0">Check run #{{ run.data.value.id }} <small class="text-secondary fs-6">{{ run.data.value.kind }}</small></h4>
      <button v-if="!run.data.value.finished" class="btn btn-sm btn-outline-danger" @click="cancel">Cancel</button>
    </div>
    <p class="small text-secondary">
      {{ date(run.data.value.started) }} → {{ date(run.data.value.finished) || 'running' }} · {{ run.data.value.watches }} watches · {{ run.data.value.new_chapters }} new · {{ run.data.value.errors }} errors
    </p>
    <table v-if="run.data.value.results?.length" class="table table-sm">
      <thead><tr><th>Watch</th><th>Provider</th><th>Status</th><th>Chapters</th></tr></thead>
      <tbody>
        <tr v-for="r in pager.items.value" :key="r.id">
          <td><RouterLink v-if="r.watch" :to="{ name: 'watch', params: { id: r.watch } }">{{ r.title }}</RouterLink><span v-else>{{ r.title }}</span></td>
          <td class="small">{{ r.provider }}</td>
          <td :class="{ 'text-danger': r.status === 'error', 'text-success': r.status === 'new' }">{{ r.status }}<span v-if="r.error" class="small"> · {{ r.error }}</span></td>
          <td class="small">{{ (r.chapters ?? []).map((c) => num(c.number)).join(', ') }}<span v-if="r.skipped?.length" class="text-secondary"> (skipped {{ r.skipped.map((n) => num(n)).join(', ') }})</span></td>
        </tr>
      </tbody>
    </table>
    <PagerNav v-model="pager.offset.value" :total="pager.total.value" :size="pager.size" />
    <template v-if="!run.data.value.finished">
      <h6>Live</h6>
      <LiveLog :events="live" />
    </template>
  </template>
</template>
