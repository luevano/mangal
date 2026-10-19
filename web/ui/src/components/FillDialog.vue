<script setup lang="ts">
import { reactive, ref, watch as whenChanged } from 'vue'
import client, { ok } from '@/api/client'
import type { FillPlan, FillRequest, Job, Provider } from '@/api/schemas'
import FormModal from '@/components/FormModal.vue'
import { useAction } from '@/composables/useAction'

// gets a provider manga's unavailable chapters from another provider: plan, then the job
const props = defineProps<{ open: boolean; provider: string; mangaId: string; title: string }>()
const emit = defineEmits<{ close: []; queued: [job: Job] }>()
const action = useAction()

const providers = ref<Provider[]>([])
const form = reactive({ from: '', from_manga_id: '', from_query: '', chapters: '' })
const plan = ref<FillPlan>()
whenChanged(
  () => props.open,
  async (open) => {
    plan.value = undefined
    if (open && !providers.value.length) providers.value = (await action.run(() => ok(client.GET('/v1/providers')))) ?? []
  }
)

function body(): FillRequest {
  const b: FillRequest = { manga_id: props.mangaId, from: form.from }
  if (form.from_manga_id) b.from_manga_id = form.from_manga_id
  if (form.from_query) b.from_query = form.from_query
  if (form.chapters) b.chapters = form.chapters
  return b
}

function post(apply: boolean) {
  return action.run(() => ok(client.POST('/v1/providers/{provider}/fill', { params: { path: { provider: props.provider }, query: apply ? { apply: true } : {} }, body: body() })))
}

async function submit() {
  if (!plan.value) {
    plan.value = await post(false)
    return
  }
  const r = await post(true)
  if (r?.job) {
    emit('queued', r.job)
    emit('close')
  }
}
</script>

<template>
  <FormModal
    title="Get unavailable chapters from another provider"
    :open="open"
    :submit-text="plan ? `Get ${plan.found.length} chapters` : 'Plan'"
    :disabled="!form.from || action.busy.value || (!!plan && !plan.found.length)"
    size="lg"
    @close="emit('close')"
    @submit="submit"
  >
    <template v-if="!plan">
      <p class="small text-secondary">The chapters of {{ title }} that can't be downloaded here, from another provider's manga (matched by number): they go into this manga's folder with its titles, recorded as the other provider's.</p>
      <label class="form-label small mb-0">Provider</label>
      <select v-model="form.from" class="form-select form-select-sm mb-2" data-test="fill-from">
        <option v-for="p in providers.filter((p) => p.id !== provider)" :key="p.id" :value="p.id">{{ p.name }}</option>
      </select>
      <label class="form-label small mb-0">Manga id</label>
      <input v-model.trim="form.from_manga_id" class="form-control form-control-sm mb-2" placeholder="the provider's manga id" />
      <label class="form-label small mb-0">or a search</label>
      <input v-model.trim="form.from_query" class="form-control form-control-sm mb-2" :placeholder="title" />
      <label class="form-label small mb-0">Chapters</label>
      <input v-model.trim="form.chapters" class="form-control form-control-sm" placeholder="every unavailable one, or 1-2, 10-" />
    </template>
    <template v-else>
      <p v-if="plan.from" class="small mb-1">From {{ plan.from }} "{{ plan.from_manga_title }}"</p>
      <ul class="small mb-0" data-test="fill-plan">
        <li v-for="l in plan.lines" :key="l">{{ l }}</li>
      </ul>
      <p v-if="!plan.found.length" class="small text-secondary mt-2 mb-0">Nothing to get.</p>
    </template>
  </FormModal>
</template>
