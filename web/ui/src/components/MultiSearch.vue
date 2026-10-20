<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import client, { ok } from '@/api/client'
import { errorMessage } from '@/api/errors'
import { imageURL } from '@/api/urls'
import type { Manga, Provider } from '@/api/schemas'
import SiteIcon from '@/components/SiteIcon.vue'
import { storedProviders, storeProviders, useMultiSearch } from '@/composables/multiSearch'

// searches the ticked providers at once (remembered per browser under storageKey), each
// provider's results filling in as it answers; a card click picks the manga
const props = defineProps<{ storageKey: string; query?: string; exclude?: string; picked?: string; cols?: string }>()
const emit = defineEmits<{ search: [q: string]; pick: [provider: string, manga: Manga] }>()

const providers = ref<Provider[]>([])
const loadError = ref('')
const ticked = reactive(new Set<string>())
const input = ref(props.query ?? '')
const { results, search } = useMultiSearch()
// covers that failed to load show the placeholder
const broken = reactive(new Set<string>())
const listed = computed(() => providers.value.filter((p) => p.id !== props.exclude))

onMounted(async () => {
  try {
    providers.value = await ok(client.GET('/v1/providers'))
  } catch (e) {
    loadError.value = errorMessage(e)
    return
  }
  const stored = storedProviders(props.storageKey)
  for (const p of listed.value) if (!stored || stored.includes(p.id)) ticked.add(p.id)
  if (input.value.trim()) submit()
})

function toggle(id: string) {
  if (ticked.has(id)) ticked.delete(id)
  else ticked.add(id)
  storeProviders(
    props.storageKey,
    listed.value.filter((p) => ticked.has(p.id)).map((p) => p.id)
  )
}

function submit() {
  const q = input.value.trim()
  if (!q) return
  search(
    listed.value.filter((p) => ticked.has(p.id)),
    q
  )
  emit('search', q)
}
</script>

<template>
  <!-- not a form: it may sit in one (a dialog's) -->
  <div class="input-group mb-2" style="max-width: 32rem">
    <input v-model="input" class="form-control" placeholder="search" data-test="multi-input" @keydown.enter.prevent="submit" />
    <button class="btn btn-primary" type="button" :disabled="!ticked.size" data-test="multi-submit" @click="submit">Search</button>
  </div>
  <div v-if="loadError" class="alert alert-danger">{{ loadError }}</div>
  <div class="d-flex flex-wrap gap-3 mb-3 small" data-test="multi-providers">
    <label v-for="p in listed" :key="p.id" class="form-check mb-0">
      <input class="form-check-input" type="checkbox" :checked="ticked.has(p.id)" :data-test="`tick-${p.id}`" @change="toggle(p.id)" />
      <span class="form-check-label">{{ p.name }}</span>
    </label>
  </div>
  <section v-for="r in results" :key="r.provider.id" class="mb-3" data-test="multi-section">
    <h6 class="mb-2">
      <SiteIcon :provider="r.provider.id" class="me-1" />{{ r.provider.name }}
      <small v-if="r.loading" class="text-secondary fw-normal"><span class="spinner-border spinner-border-sm ms-1 me-1" />searching…</small>
      <small v-else-if="!r.error" class="text-secondary fw-normal">{{ r.mangas.length }} found</small>
    </h6>
    <div v-if="r.error" class="alert alert-danger small py-1 mb-0" data-test="multi-error">{{ r.error }}</div>
    <div v-else class="row">
      <template v-if="r.loading">
        <div v-for="n in 3" :key="n" :class="cols ?? 'col-6 col-md-4 col-lg-3 col-xl-2'" class="mb-3">
          <div class="card h-100 placeholder-glow" data-test="multi-placeholder">
            <div class="card-img-top cover-card placeholder" />
            <div class="card-body p-2"><span class="placeholder col-10" /><span class="placeholder col-6" /></div>
          </div>
        </div>
      </template>
      <div v-for="m in r.mangas" :key="m.id" :class="cols ?? 'col-6 col-md-4 col-lg-3 col-xl-2'" class="mb-3">
        <div class="card h-100 clickable" :class="{ 'border-primary': picked === `${r.provider.id}/${m.id}` }" data-test="multi-result" @click="emit('pick', r.provider.id, m)">
          <img v-if="m.cover && !broken.has(r.provider.id + m.id)" :src="imageURL(m.cover, m.url)" class="card-img-top cover-card" loading="lazy" alt="" @error="broken.add(r.provider.id + m.id)" />
          <div v-else class="card-img-top cover-card d-flex align-items-center justify-content-center text-secondary"><i class="bi bi-image fs-2" /></div>
          <div class="card-body p-2">
            <div class="small fw-semibold clamp-2" :title="m.title">{{ m.title }}</div>
            <div class="small text-secondary text-truncate">{{ m.id }}</div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
