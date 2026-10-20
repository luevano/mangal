<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import client, { ok } from '@/api/client'
import type { JobKind } from '@/api/schemas'
import FormModal from './FormModal.vue'
import { useAction } from '@/composables/useAction'
import { changes, formOf, type SettingsForm } from '@/composables/jobKinds'

// the settings of a job kind (an admin's): on, its schedule, the pause between requests
// and each provider's on, off or own interval; saved in the config file
const props = defineProps<{ open: boolean; kind: JobKind }>()
const emit = defineEmits<{ close: []; done: [k: JobKind] }>()
const action = useAction()

const was = ref<SettingsForm>(formOf(props.kind, ''))
const form = ref<SettingsForm>(formOf(props.kind, ''))
watch(
  () => props.open,
  (open) => {
    if (!open) return
    was.value = formOf(props.kind, props.kind.delay ?? '')
    form.value = formOf(props.kind, props.kind.delay ?? '')
  },
  { immediate: true }
)
const intervalHint = computed(() => (props.kind.kind === 'check' ? 'a cron schedule, e.g. 0 */8 * * *' : 'a duration, e.g. 336h'))

async function save() {
  const body = changes(props.kind, was.value, form.value)
  if (!Object.keys(body).length) {
    emit('close')
    return
  }
  const k = await action.run(
    () => ok(client.PUT('/v1/jobs/kinds/{kind}/settings', { params: { path: { kind: props.kind.kind } }, body })),
    'Settings saved'
  )
  if (k) emit('done', k)
}
</script>

<template>
  <FormModal :open="open" :title="`${kind.title} settings`" submit-text="Save" size="lg" :disabled="action.busy.value" @close="emit('close')" @submit="save">
    <div v-if="!kind.manual" class="form-check form-switch mb-3">
      <input id="job-enabled" v-model="form.enabled" class="form-check-input" type="checkbox" role="switch" />
      <label class="form-check-label" for="job-enabled">mangal serve runs it on its schedule</label>
    </div>
    <div v-if="kind.schedule_key" class="mb-3">
      <label class="form-label" for="job-schedule">Schedule <span class="small text-secondary">{{ kind.schedule_key }}</span></label>
      <input id="job-schedule" v-model="form.schedule" class="form-control" data-test="job-schedule" />
      <div class="form-text">{{ kind.kind === 'check' ? 'a cron schedule, e.g. 0 */4 * * * or @daily' : 'a Go duration (168h is a week), 0 turns it off' }}</div>
    </div>
    <div v-if="kind.delay !== undefined" class="mb-3">
      <label class="form-label" for="job-delay">Pause between requests</label>
      <input id="job-delay" v-model="form.delay" class="form-control" placeholder="1s-3s" data-test="job-delay" />
      <div class="form-text">min-max, a random pause between two requests to a provider (every maintenance job)</div>
    </div>
    <template v-if="kind.providers_key">
      <h6>Providers <span class="small text-secondary">{{ kind.providers_key }}</span></h6>
      <p class="form-text mt-0">on, off or its own interval ({{ intervalHint }}); empty is the default</p>
      <div v-for="p in kind.providers" :key="p.provider" class="row g-2 align-items-center mb-1">
        <label class="col-6 col-form-label col-form-label-sm text-truncate" :for="`job-p-${p.provider}`">{{ p.name }} <span class="text-secondary">{{ p.provider }}</span></label>
        <div class="col-6">
          <input
            :id="`job-p-${p.provider}`"
            v-model="form.providers[p.provider]"
            class="form-control form-control-sm"
            :placeholder="p.default ?? 'on'"
            :data-test="`job-p-${p.provider}`"
          />
        </div>
      </div>
    </template>
  </FormModal>
</template>
