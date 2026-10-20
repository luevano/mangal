<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink } from 'vue-router'
import client, { ok } from '@/api/client'
import { change } from '@/api/changes'
import type { Read } from '@/api/schemas'
import LoadState from '@/components/LoadState.vue'
import { useLoad } from '@/composables/useLoad'
import { date, num } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useStatus } from '@/stores/status'
import { useChanges } from '@/stores/changes'
import { usePlan } from '@/stores/plan'

crumbs(() => [{ title: 'Read history' }])
const changes = useChanges()
const plan = usePlan()
const st = useStatus()
const offset = ref(0)
const pageSize = 100
const { data, loading, error } = useLoad(
  () => ok(client.GET('/v1/history', { params: { query: { limit: pageSize, offset: offset.value } } })),
  [offset, () => changes.version]
)

function forget(r: Read) {
  plan.open({ title: `Forget the reads of ${r.manga_title}`, verb: 'Forget', run: change('POST', '/history/clear', { provider: r.provider, manga_id: r.manga_id }) })
}
function clearAll() {
  plan.open({ title: 'Clear the read history', verb: 'Clear', run: change('POST', '/history/clear') })
}
</script>

<template>
  <div class="d-flex align-items-center mb-2">
    <h4 class="me-auto mb-0">Read history</h4>
    <button v-if="st.allows('POST /v1/history/clear')" class="btn btn-sm btn-outline-danger" :disabled="!data?.total" @click="clearAll">Clear all</button>
  </div>
  <LoadState :loading="loading && !data" :error="error" :empty="data && !data.reads.length" empty-text="Nothing read yet." />
  <table v-if="data?.reads.length" class="table table-sm">
    <thead><tr><th>Manga</th><th>Chapter</th><th class="d-none d-md-table-cell">Provider</th><th>Times</th><th class="d-none d-md-table-cell">Last</th><th></th></tr></thead>
    <tbody>
      <tr v-for="r in data.reads" :key="`${r.provider}${r.manga_id}${r.chapter}`">
        <td>
          <RouterLink v-if="r.library_manga" :to="{ name: 'library-manga', params: { id: r.library_manga } }">{{ r.manga_title }}</RouterLink>
          <RouterLink v-else :to="{ name: 'provider-manga', params: { provider: r.provider, manga: r.manga_id } }">{{ r.manga_title }}</RouterLink>
        </td>
        <td>{{ num(r.chapter) }} <span class="small text-secondary">{{ r.chapter_title }}</span></td>
        <td class="d-none d-md-table-cell small">{{ r.provider }}</td>
        <td>{{ r.count }}</td>
        <td class="d-none d-md-table-cell small">{{ date(r.last) }}</td>
        <td class="text-end"><button v-if="st.allows('POST /v1/history/clear')" class="btn btn-sm btn-link text-danger py-0" title="Forget this manga's reads" @click="forget(r)"><i class="bi bi-trash" /></button></td>
      </tr>
    </tbody>
  </table>
  <nav v-if="data && data.total > pageSize" class="d-flex gap-2 align-items-center">
    <button class="btn btn-sm btn-outline-secondary" :disabled="offset === 0" @click="offset = Math.max(0, offset - pageSize)">‹ prev</button>
    <span class="small">{{ offset + 1 }}–{{ Math.min(offset + pageSize, data.total) }} of {{ data.total }}</span>
    <button class="btn btn-sm btn-outline-secondary" :disabled="offset + pageSize >= data.total" @click="offset += pageSize">next ›</button>
  </nav>
</template>
