<script setup lang="ts">
import { computed, ref } from 'vue'
import client, { ok, fetchJSON } from '@/api/client'
import { change } from '@/api/changes'
import type { HealthFinding, HealthJob, HealthReport, LibraryManga, MetadataResult } from '@/api/schemas'
import FormModal from '@/components/FormModal.vue'
import HealthFindingCard from '@/components/HealthFindingCard.vue'
import LoadState from '@/components/LoadState.vue'
import MetadataPicker from '@/components/MetadataPicker.vue'
import PagerNav from '@/components/PagerNav.vue'
import { useAction } from '@/composables/useAction'
import { useLoad } from '@/composables/useLoad'
import { usePager } from '@/composables/usePager'
import { sourceNames } from '@/composables/format'
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
const categories = computed(() => Object.entries(report.value?.counts.by_category ?? {}).sort())
const findings = computed(() => (report.value?.findings ?? []).filter((f) => !category.value || f.category === category.value))
const pager = usePager(findings, 100)

function planFix(f: HealthFinding, choice?: string, title?: string) {
  plan.open({ title: title ?? `Fix ${f.check}: ${f.subject.name ?? ''}`, verb: 'Fix', run: change('POST', '/health/fix', choice ? { key: f.key, choice } : { key: f.key }) })
}

function scan() {
  action.run(() => ok(client.POST('/v1/library/scan', { body: {} })), (j) => `Scan queued (job ${j.id}): it adds the new folders and finds the ones that moved`)
}

// the fix of a finding, as the TUI's x: some kinds ask first (which manga stays, scan or
// forget, which id), the rest go to the plan dialog (its plan is the confirm)
const merging = ref<{ f: HealthFinding; mangas: LibraryManga[]; keep: number }>()
const forgetting = ref<HealthFinding>()
const picking = ref<HealthFinding>()

async function fix(f: HealthFinding, choice?: string) {
  const a = f.fix.action
  switch (a?.kind) {
    case 'scan':
      scan()
      return
    case 'forget':
      forgetting.value = f
      return
    case 'merge': {
      const ids = a.mangas ?? []
      const mangas = (
        await Promise.all(ids.map((id) => ok(client.GET('/v1/library/mangas/{id}', { params: { path: { id } } })).then((d) => d.manga, () => undefined)))
      ).filter((m): m is LibraryManga => !!m)
      if (mangas.length < 2) {
        planFix(f)
        return
      }
      const keep = Number(choice ?? mangas[0]!.id)
      merging.value = { f, mangas, keep: mangas.some((m) => m.id === keep) ? keep : mangas[0]!.id }
      return
    }
    case 'pick':
      if (!choice) {
        picking.value = f
        return
      }
  }
  planFix(f, choice)
}

function merge() {
  const m = merging.value
  if (!m) return
  merging.value = undefined
  planFix(m.f, String(m.keep), `Merge into ${m.mangas.find((x) => x.id === m.keep)?.title ?? m.keep}`)
}

function forget() {
  const f = forgetting.value
  forgetting.value = undefined
  if (f) planFix(f, undefined, `Forget ${f.subject.name}`)
}

function scanInstead() {
  forgetting.value = undefined
  scan()
}

// a pick's id from the metadata search: the links' plan
function picked(r: MetadataResult) {
  const f = picking.value
  picking.value = undefined
  const manga = f?.fix.action?.manga ?? f?.subject.id
  if (!manga) return
  plan.open({ title: `Link ${f?.subject.name} to ${sourceNames[r.source] ?? r.source} ${r.id}`, run: change('POST', '/library/links', { manga, links: { [r.source]: r.id } }) })
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
    <HealthFindingCard v-for="f in pager.items.value" :key="f.key" :finding="f" @fix="fix" @dismiss="dismiss" />
    <PagerNav v-model="pager.offset.value" :total="pager.total.value" :size="pager.size" />
  </template>

  <FormModal title="Merge: which one stays?" :open="!!merging" submit-text="Plan" size="lg" @close="merging = undefined" @submit="merge">
    <p class="small text-secondary">The other merges into it (the merge's plan comes next).</p>
    <div class="list-group" data-test="merge-choices">
      <label v-for="(m, i) in merging?.mangas ?? []" :key="m.id" class="list-group-item list-group-item-action d-flex gap-2">
        <input v-if="merging" v-model="merging.keep" class="form-check-input" type="radio" :value="m.id" />
        <span>
          {{ m.title }}<span v-if="i === 0" class="badge text-bg-info ms-1">suggested</span>
          <span class="d-block small text-secondary">#{{ m.id }} · {{ m.chapters }} chapters · {{ m.path }}</span>
        </span>
      </label>
    </div>
    <p v-if="(merging?.mangas.length ?? 0) > 2" class="small text-secondary mt-2 mb-0">{{ merging?.mangas.length }} folders: this merges two, the finding stays for the rest.</p>
  </FormModal>

  <FormModal title="Library manga" :open="!!forgetting" submit-text="Forget it" @close="forgetting = undefined" @submit="forget">
    <p>{{ forgetting?.subject.name }}: {{ forgetting?.message }}</p>
    <p class="small text-secondary mb-2">Scan the library (it finds folders that moved), or forget the manga?</p>
    <button type="button" class="btn btn-sm btn-outline-primary" data-test="scan-instead" @click="scanInstead">Scan the library</button>
  </FormModal>

  <MetadataPicker
    :open="!!picking"
    :title="`Pick for ${picking?.subject.name ?? ''}`"
    :query="picking?.subject.name"
    :source="picking?.fix.action?.source"
    :sources="picking?.fix.action?.source ? [picking.fix.action.source] : undefined"
    @close="picking = undefined"
    @pick="picked"
  />
</template>
