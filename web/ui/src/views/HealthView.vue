<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import client, { ok, fetchJSON } from '@/api/client'
import { change } from '@/api/changes'
import type { HealthAction, HealthFinding, HealthJob, HealthReport } from '@/api/schemas'
import LoadState from '@/components/LoadState.vue'
import { useAction } from '@/composables/useAction'
import { useLoad } from '@/composables/useLoad'
import { crumbs } from '@/stores/crumbs'
import { useChanges } from '@/stores/changes'
import { usePlan } from '@/stores/plan'

crumbs(() => [{ title: 'Health' }])
const changes = useChanges()
const plan = usePlan()
const action = useAction()
const all = ref(false)
const category = ref('')
const network = ref<HealthReport>()
const job = ref<HealthJob>()

const offline = useLoad(() => ok(client.GET('/v1/health', { params: { query: { all: all.value || undefined } } })) as Promise<HealthReport>, [all, () => changes.version])

const report = computed(() => network.value ?? offline.data.value)
watch(report, (r) => {
  for (const f of r?.findings ?? []) {
    const list = choiceList(f)
    if (list.length && !choices.value[f.key]) choices.value[f.key] = list[0]!
  }
})
const categories = computed(() => Object.entries(report.value?.counts.by_category ?? {}).sort())
const findings = computed(() => (report.value?.findings ?? []).filter((f) => !category.value || f.category === category.value))

const sevClass: Record<string, string> = { error: 'text-bg-danger', warning: 'text-bg-warning', info: 'text-bg-info' }

function actionOf(f: HealthFinding): HealthAction | undefined {
  return f.fix.action
}
const choices = ref<Record<string, string>>({})
function choiceList(f: HealthFinding): string[] {
  const a = actionOf(f)
  if (!a) return []
  if (a.ids && a.ids.length > 1) return a.ids
  if (a.kind === 'merge' && a.mangas && a.mangas.length > 1) return a.mangas.map(String)
  return []
}

function fix(f: HealthFinding) {
  const choice = choices.value[f.key]
  plan.open({ title: `Fix ${f.check}: ${f.subject.name ?? ''}`, verb: 'Fix', run: change('POST', '/health/fix', choice ? { key: f.key, choice } : { key: f.key }) })
}

function dismiss(f: HealthFinding, undo = false) {
  const body = undo ? { undismiss: [f.key], network: !!network.value } : { dismiss: [f.key], network: !!network.value }
  action.run(() => ok(client.POST('/v1/health/dismiss', { body })), undo ? 'Brought back' : 'Dismissed').then(() => {
    offline.load()
    if (network.value) runNetwork()
  })
}

async function poll(id: number) {
  for (;;) {
    const j = await ok(client.GET('/v1/health/jobs/{id}', { params: { path: { id } } }))
    job.value = j
    if (!j.running) {
      if (j.report) network.value = j.report
      return
    }
    await new Promise((r) => setTimeout(r, 1500))
  }
}

function runNetwork() {
  action.run(async () => {
    const { status, data } = await fetchJSON<HealthJob | HealthReport>(`/api/v1/health?network=true${all.value ? '&all=true' : ''}`)
    if (status === 202) await poll((data as HealthJob).id)
    else network.value = data as HealthReport
  })
}
</script>

<template>
  <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
    <h4 class="me-auto mb-0">Health</h4>
    <div class="form-check form-switch mb-0">
      <input id="hall" v-model="all" class="form-check-input" type="checkbox" />
      <label class="form-check-label small" for="hall">dismissed too</label>
    </div>
    <button class="btn btn-sm btn-outline-primary" :disabled="action.busy.value" @click="runNetwork">
      <span v-if="job?.running" class="spinner-border spinner-border-sm me-1" />Network checks
    </button>
    <button v-if="network" class="btn btn-sm btn-outline-secondary" @click="network = undefined">Offline only</button>
  </div>
  <p v-if="job?.running" class="small text-secondary">{{ job.status }}</p>
  <LoadState :loading="offline.loading.value && !report" :error="offline.error.value" />
  <template v-if="report">
    <div class="d-flex flex-wrap gap-2 mb-3 small">
      <span class="badge text-bg-danger">{{ report.counts.error ?? 0 }} errors</span>
      <span class="badge text-bg-warning">{{ report.counts.warning ?? 0 }} warnings</span>
      <span class="badge text-bg-info">{{ report.counts.info ?? 0 }} info</span>
      <span v-if="report.dismissed" class="badge text-bg-secondary">{{ report.dismissed }} dismissed</span>
      <span v-if="report.network" class="badge text-bg-primary">with the network checks</span>
    </div>
    <div v-for="e in report.errors ?? []" :key="e" class="alert alert-warning py-1 small">{{ e }}</div>
    <ul class="nav nav-pills small mb-3">
      <li class="nav-item"><a class="nav-link" :class="{ active: !category }" href="#" @click.prevent="category = ''">all</a></li>
      <li v-for="[c, n] in categories" :key="c" class="nav-item">
        <a class="nav-link" :class="{ active: category === c }" href="#" @click.prevent="category = c">{{ c }} {{ n }}</a>
      </li>
    </ul>
    <p v-if="!findings.length" class="text-success">Nothing to fix.</p>
    <div v-for="f in findings" :key="f.key" class="card mb-2" :class="{ 'opacity-50': f.dismissed }" data-test="finding">
      <div class="card-body py-2">
        <div class="d-flex flex-wrap gap-2 align-items-center">
          <span class="badge" :class="sevClass[f.severity] ?? 'text-bg-secondary'">{{ f.severity }}</span>
          <span class="badge text-bg-dark">{{ f.check }}</span>
          <RouterLink v-if="f.subject.kind === 'manga' && f.subject.id" :to="{ name: 'library-manga', params: { id: f.subject.id } }" class="fw-semibold">{{ f.subject.name }}</RouterLink>
          <RouterLink v-else-if="f.subject.kind === 'watch' && f.subject.id" :to="{ name: 'watch', params: { id: f.subject.id } }" class="fw-semibold">{{ f.subject.name }}</RouterLink>
          <span v-else class="fw-semibold">{{ f.subject.name }}</span>
          <span class="me-auto">{{ f.message }}</span>
          <template v-if="actionOf(f)">
            <select v-if="choiceList(f).length" v-model="choices[f.key]" class="form-select form-select-sm w-auto" title="the id to link, or the manga to keep">
              <option v-for="c in choiceList(f)" :key="c" :value="c">{{ c }}</option>
            </select>
            <button class="btn btn-sm btn-outline-primary py-0" data-test="fix" @click="fix(f)">Fix</button>
          </template>
          <button v-if="!f.dismissed" class="btn btn-sm btn-outline-secondary py-0" title="Fine as it is" @click="dismiss(f)">Dismiss</button>
          <button v-else class="btn btn-sm btn-outline-secondary py-0" @click="dismiss(f, true)">Bring back</button>
        </div>
        <ul v-if="f.details?.length" class="small mb-1 mt-1">
          <li v-for="(d, i) in f.details" :key="i">{{ d }}</li>
        </ul>
        <div v-if="f.fix.note" class="small text-secondary">{{ f.fix.note }}</div>
        <div v-if="f.fix.commands?.length" class="small">
          <code v-for="c in f.fix.commands" :key="c" class="me-3 d-inline-block">{{ c }}</code>
        </div>
        <details v-if="f.items?.length" class="small mt-1">
          <summary>{{ f.items.length }} items</summary>
          <ul class="mb-0">
            <li v-for="(it, i) in f.items" :key="i">{{ it.subject?.name }}: {{ it.message }}
              <button v-if="it.fix?.action" class="btn btn-sm btn-link py-0" @click="fix(it)">fix</button>
            </li>
          </ul>
        </details>
      </div>
    </div>
  </template>
</template>
