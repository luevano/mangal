<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import client, { ok } from '@/api/client'
import LoadState from '@/components/LoadState.vue'
import { useLoad } from '@/composables/useLoad'
import { crumbs } from '@/stores/crumbs'

crumbs(() => [{ title: 'Providers' }])
const router = useRouter()
const { data, loading, error } = useLoad(() => ok(client.GET('/v1/providers')))
const queries = ref<Record<string, string>>({})
const filter = ref('')

function search(id: string) {
  const q = queries.value[id]?.trim()
  if (q) router.push({ name: 'search', params: { provider: id }, query: { q } })
}
</script>

<template>
  <h4>Providers</h4>
  <input v-model="filter" class="form-control form-control-sm mb-3" style="max-width: 20rem" placeholder="filter" />
  <LoadState :loading="loading && !data" :error="error" :empty="data && !data.length" empty-text="No providers installed (mangal providers add)." />
  <div class="row g-2">
    <div v-for="p in (data ?? []).filter((x) => !filter || (x.name + x.id).toLowerCase().includes(filter.toLowerCase()))" :key="p.id" class="col-12 col-md-6 col-xl-4">
      <div class="card h-100" data-test="provider">
        <div class="card-body">
          <h6 class="card-title mb-0">{{ p.name }} <small class="text-secondary">{{ p.version }}</small></h6>
          <div class="small text-secondary mb-2">{{ p.id }}<span v-if="p.description"> · {{ p.description }}</span></div>
          <form class="input-group input-group-sm" @submit.prevent="search(p.id)">
            <input v-model="queries[p.id]" class="form-control" placeholder="search" :data-test="`search-${p.id}`" />
            <button class="btn btn-outline-primary" type="submit"><i class="bi bi-search" /></button>
          </form>
        </div>
      </div>
    </div>
  </div>
</template>
