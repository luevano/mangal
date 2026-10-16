<script setup lang="ts">
import { RouterLink } from 'vue-router'
import client, { ok } from '@/api/client'
import { change } from '@/api/changes'
import type { Duplicate } from '@/api/schemas'
import LoadState from '@/components/LoadState.vue'
import PagerNav from '@/components/PagerNav.vue'
import { usePager } from '@/composables/usePager'
import { useLoad } from '@/composables/useLoad'
import { sourceNames } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useChanges } from '@/stores/changes'
import { usePlan } from '@/stores/plan'

crumbs(() => [{ title: 'Library', to: { name: 'library' } }, { title: 'Duplicates' }])
const changes = useChanges()
const plan = usePlan()
const { data, loading, error } = useLoad(() => ok(client.GET('/v1/library/duplicates')), [() => changes.version])
const pager = usePager(data, 50)

function merge(d: Duplicate, keep: number, other: number) {
  const k = d.mangas.find((m) => m.id === keep)
  const o = d.mangas.find((m) => m.id === other)
  plan.open({ title: `Merge ${o?.title} into ${k?.title}`, verb: 'Merge', run: change('POST', '/library/merge', { keep, other }) })
}
</script>

<template>
  <h4>Duplicates</h4>
  <p class="small text-secondary">Library mangas sharing a metadata id; the suggested one to keep is first.</p>
  <LoadState :loading="loading && !data" :error="error" :empty="data && !data.length" empty-text="No duplicates." />
  <div v-for="d in pager.items.value" :key="d.source + d.id" class="card mb-2" data-test="duplicate">
    <div class="card-header small">{{ sourceNames[d.source] ?? d.source }} {{ d.id }}</div>
    <ul class="list-group list-group-flush">
      <li v-for="(m, i) in d.mangas" :key="m.id" class="list-group-item d-flex align-items-center gap-2">
        <span class="badge" :class="i === 0 ? 'text-bg-success' : 'text-bg-secondary'">{{ i === 0 ? 'keep' : 'other' }}</span>
        <div class="me-auto">
          <RouterLink :to="{ name: 'library-manga', params: { id: m.id } }">{{ m.title }}</RouterLink>
          <div class="small text-secondary">#{{ m.id }} · {{ m.chapters }} chapters · {{ m.sources.map((s) => s.provider).join(', ') }}</div>
        </div>
        <button v-if="i > 0" class="btn btn-sm btn-outline-primary" data-test="merge-into-first" @click="merge(d, d.mangas[0]!.id, m.id)">Merge into the first</button>
        <button v-if="i > 0" class="btn btn-sm btn-outline-secondary" @click="merge(d, m.id, d.mangas[0]!.id)">Keep this one</button>
      </li>
    </ul>
  </div>
  <PagerNav v-model="pager.offset.value" :total="pager.total.value" :size="pager.size" />
</template>
