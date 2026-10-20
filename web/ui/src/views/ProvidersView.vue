<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import client, { ok } from '@/api/client'
import type { Provider, ProvidersUpdate } from '@/api/schemas'
import LoadState from '@/components/LoadState.vue'
import SiteIcon from '@/components/SiteIcon.vue'
import { useAction } from '@/composables/useAction'
import { useLoad } from '@/composables/useLoad'
import { crumbs } from '@/stores/crumbs'
import { useStatus } from '@/stores/status'

crumbs(() => [{ title: 'Providers' }])
const router = useRouter()
const action = useAction()
const st = useStatus()
const { data, loading, error, load } = useLoad(() => ok(client.GET('/v1/providers')))
const update = ref<ProvidersUpdate>()

function sites(p: Provider): string[] {
  return [p.website ?? '', ...(p.mirrors ?? [])].filter(Boolean)
}

// prefer sets the provider's preferred website (the TUI's mirrors screen)
function prefer(p: Provider, mirror: string) {
  action
    .run(() => ok(client.PUT('/v1/providers/{provider}/mirror', { params: { path: { provider: p.id } }, body: { mirror } })), `${p.name} prefers ${mirror}`)
    .then(() => load())
}

function updateAll() {
  action
    .run(() => ok(client.POST('/v1/providers/update')), (u) => `${u.providers.filter((x) => x.status === 'updated').length} providers updated`)
    .then((u) => {
      if (u) update.value = u
      load()
    })
}
const queries = ref<Record<string, string>>({})
const filter = ref('')

function search(id: string) {
  const q = queries.value[id]?.trim()
  if (q) router.push({ name: 'search', params: { provider: id }, query: { q } })
}
</script>

<template>
  <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
    <h4 class="me-auto mb-0">Providers</h4>
    <RouterLink class="btn btn-sm btn-outline-primary" :to="{ name: 'multi-search' }" data-test="multi-search"><i class="bi bi-search me-1" />Search several</RouterLink>
    <button v-if="st.allows('POST /v1/providers/update')" class="btn btn-sm btn-outline-primary" :disabled="action.busy.value" title="git pull of each installed provider (installing one is mangal providers add)" data-test="update-providers" @click="updateAll">
      <span v-if="action.busy.value" class="spinner-border spinner-border-sm me-1" />Update providers
    </button>
  </div>
  <div v-if="update" class="alert alert-light small py-2" data-test="update-result">
    <button type="button" class="btn-close float-end" aria-label="Close" @click="update = undefined" />
    <div v-for="u in update.providers" :key="u.dir">
      <span class="fw-semibold">{{ u.dir }}</span>: {{ u.status }}<span v-if="u.error" class="text-danger"> ({{ u.error }})</span>
    </div>
    <div v-for="pr in update.problems ?? []" :key="pr" class="text-warning">{{ pr }}</div>
  </div>
  <input v-model="filter" class="form-control form-control-sm mb-3" style="max-width: 20rem" placeholder="filter" />
  <LoadState :loading="loading && !data" :error="error" :empty="data && !data.length" empty-text="No providers installed (mangal providers add)." />
  <div class="row g-2">
    <div v-for="p in (data ?? []).filter((x) => !filter || (x.name + x.id).toLowerCase().includes(filter.toLowerCase()))" :key="p.id" class="col-12 col-md-6 col-xl-4">
      <div class="card h-100" data-test="provider">
        <div class="card-body">
          <h6 class="card-title mb-0"><SiteIcon :provider="p.id" class="me-1" />{{ p.name }} <small class="text-secondary">{{ p.version }}</small></h6>
          <div class="small text-secondary mb-2">{{ p.id }}<span v-if="p.description"> · {{ p.description }}</span></div>
          <div v-if="p.mirrors?.length" class="input-group input-group-sm mb-2" title="the preferred website, the others are tried when it's down">
            <span class="input-group-text">site</span>
            <select class="form-select" :value="p.preferred" :disabled="!st.allows('PUT /v1/providers/{provider}/mirror')" :data-test="`mirror-${p.id}`" @change="prefer(p, ($event.target as HTMLSelectElement).value)">
              <option v-for="site in sites(p)" :key="site" :value="site">{{ site }}{{ site === p.website ? ' (website)' : '' }}</option>
            </select>
          </div>
          <form class="input-group input-group-sm" @submit.prevent="search(p.id)">
            <input v-model="queries[p.id]" class="form-control" placeholder="search" :data-test="`search-${p.id}`" />
            <button class="btn btn-outline-primary" type="submit"><i class="bi bi-search" /></button>
          </form>
        </div>
      </div>
    </div>
  </div>
</template>
