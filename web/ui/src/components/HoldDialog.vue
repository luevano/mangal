<script setup lang="ts">
import { computed, reactive, watch as whenChanged } from 'vue'
import client, { ok } from '@/api/client'
import type { HoldRequest, HoldResult } from '@/api/schemas'
import FormModal from '@/components/FormModal.vue'
import { useAction } from '@/composables/useAction'

// the hold marks of a watch (its library manga) or a library manga (and its watches)
const props = defineProps<{
  open: boolean
  title: string
  watch?: number
  manga?: number
  // its marks now: hold (hiatus, ended), provider when it needs another provider
  hold?: string
  provider?: boolean
  // AniList's publication status, the last check's coverage: the suggested mark
  status?: string
  coverage?: string
}>()
const emit = defineEmits<{ close: []; done: [] }>()
const action = useAction()

const suggested = computed(() => {
  if (props.coverage && !props.provider) return { mark: 'provider', why: props.coverage }
  if (props.hold) return undefined
  if (props.status === 'HIATUS') return { mark: 'hiatus', why: 'hiatus on AniList' }
  if (props.status === 'FINISHED' || props.status === 'CANCELLED') return { mark: 'ended', why: `${props.status.toLowerCase()} on AniList` }
  return undefined
})

const form = reactive<{ mark: HoldRequest['mark']; reason: string; noCheck: boolean }>({ mark: 'hiatus', reason: '', noCheck: false })
whenChanged(
  () => props.open,
  (open) => {
    if (!open) return
    form.mark = (suggested.value?.mark as HoldRequest['mark']) ?? (props.hold || props.provider ? '' : 'hiatus')
    form.reason = suggested.value?.mark === 'provider' ? (props.coverage ?? '') : ''
    form.noCheck = false
  }
)

const labels: Record<string, string> = { hiatus: 'On hold: hiatus', ended: 'On hold: ended (no longer releasing)', provider: 'Needs a new provider', '': 'Clear the marks' }

function submit() {
  emit('close')
  const body: HoldRequest = { mark: form.mark }
  if (form.mark === 'provider' && form.reason) body.reason = form.reason
  if (form.noCheck) body.no_check = true
  const run = props.watch
    ? () => ok(client.POST('/v1/watches/{id}/hold', { params: { path: { id: props.watch! } }, body }))
    : () => ok(client.POST('/v1/library/mangas/{id}/hold', { params: { path: { id: props.manga! } }, body }))
  action
    .run(run, (r) => {
      const res = r as HoldResult
      let msg = `${props.title}: ${labels[form.mark ?? '']?.toLowerCase()}`
      if (res.check?.new_chapters) msg += `, got ${res.check.new_chapters} new chapters first`
      if (res.check_error) msg += ` (the check before failed: ${res.check_error})`
      return msg
    })
    .then(() => emit('done'))
}
</script>

<template>
  <FormModal :title="`Mark ${title}`" :open="open" submit-text="Mark" @close="emit('close')" @submit="submit">
    <p v-if="suggested" class="small" data-test="hold-suggested">Suggested: <strong>{{ labels[suggested.mark] }}</strong> ({{ suggested.why }})</p>
    <div v-for="(label, mark) in labels" :key="mark" class="form-check">
      <input :id="`hold-${mark}`" v-model="form.mark" class="form-check-input" type="radio" :value="mark" :disabled="mark === '' && !hold && !provider" />
      <label class="form-check-label" :for="`hold-${mark}`">{{ label }}</label>
    </div>
    <input v-if="form.mark === 'provider'" v-model.trim="form.reason" class="form-control form-control-sm mt-2" placeholder="why (e.g. only has 60-63)" />
    <div v-if="form.mark === 'hiatus' || form.mark === 'ended'" class="form-check mt-2">
      <input id="hold-nocheck" v-model="form.noCheck" class="form-check-input" type="checkbox" />
      <label class="form-check-label" for="hold-nocheck">pause without checking for the last chapters first</label>
    </div>
    <p class="small text-secondary mt-2 mb-0">A hold checks the watches once more for the last chapters, then pauses them; "Check now" (with gaps) still gets what's missing anytime. A new provider skips the watch until it's re-linked.</p>
  </FormModal>
</template>
