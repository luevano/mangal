<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import client, { ok } from '@/api/client'
import { imageURL } from '@/api/urls'
import LoadState from '@/components/LoadState.vue'
import { useLoad } from '@/composables/useLoad'
import { crumbs } from '@/stores/crumbs'

const route = useRoute()
const router = useRouter()
const provider = computed(() => String(route.params.provider))
const q = computed(() => String(route.query.q ?? ''))
const input = ref(q.value)
// covers that failed to load show the placeholder
const broken = reactive(new Set<string>())

crumbs(() => [{ title: 'Providers', to: { name: 'providers' } }, { title: provider.value }, { title: q.value ? `“${q.value}”` : 'Search' }])

const { data, loading, error } = useLoad(
  () => (q.value ? ok(client.GET('/v1/providers/{provider}/search', { params: { path: { provider: provider.value }, query: { q: q.value } } })) : Promise.resolve([])),
  [provider, q]
)
function submit() {
  router.replace({ query: { q: input.value } })
}
function open(id: string) {
  router.push({ name: 'provider-manga', params: { provider: provider.value, manga: id } })
}
</script>

<template>
  <form class="input-group mb-3" style="max-width: 32rem" @submit.prevent="submit">
    <input v-model="input" class="form-control" placeholder="search" data-test="search-input" />
    <button class="btn btn-primary" type="submit">Search</button>
  </form>
  <LoadState :loading="loading" :error="error" :empty="!loading && q !== '' && data && !data.length" empty-text="Nothing found." />
  <div v-if="data?.length" class="row">
    <div v-for="m in data" :key="m.id" class="col-6 col-md-4 col-lg-3 col-xl-2 mb-3">
      <div class="card h-100 clickable" data-test="search-result" @click="open(m.id)">
        <img v-if="m.cover && !broken.has(m.id)" :src="imageURL(m.cover, m.url)" class="card-img-top cover-card" loading="lazy" alt="" @error="broken.add(m.id)" />
        <div v-else class="card-img-top cover-card d-flex align-items-center justify-content-center text-secondary"><i class="bi bi-image fs-2" /></div>
        <div class="card-body p-2">
          <div class="small fw-semibold clamp-2" :title="m.title">{{ m.title }}</div>
          <div class="small text-secondary text-truncate">{{ m.id }}</div>
        </div>
      </div>
    </div>
  </div>
</template>
