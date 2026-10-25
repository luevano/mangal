<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import client, { ok } from '@/api/client'
import type { ConfigChange, ConfigEntry } from '@/api/schemas'
import LoadState from '@/components/LoadState.vue'
import { useLoad } from '@/composables/useLoad'
import { useAction } from '@/composables/useAction'
import { crumbs } from '@/stores/crumbs'
import { useStatus } from '@/stores/status'
import { useToasts } from '@/stores/toasts'

crumbs(() => [{ title: 'Config' }])
const st = useStatus()
const toasts = useToasts()
const { busy, run } = useAction()
const { data, loading, error, load } = useLoad(() => ok(client.GET('/v1/config')))
const history = useLoad(() =>
  ok(client.GET('/v1/config/history', { params: { query: { limit: 100 } } }))
)
const filter = ref('')
// the edited values by key, as typed
const drafts = reactive<Record<string, string>>({})

// a provider's settings are its own section
function sectionOf(e: ConfigEntry): string {
  return e.provider ? `providers.${e.provider}` : (e.key.split('.')[0] ?? '')
}

const sections = computed(() => {
  const by = new Map<string, ConfigEntry[]>()
  const q = filter.value.toLowerCase()
  for (const e of data.value ?? []) {
    if (q && !e.key.toLowerCase().includes(q) && !e.description.toLowerCase().includes(q)) continue
    const s = sectionOf(e)
    if (!by.has(s)) by.set(s, [])
    by.get(s)!.push(e)
  }
  return [...by.entries()].sort(([a], [b]) => a.localeCompare(b))
})

function show(v: unknown): string {
  if (v === undefined || v === null || v === '') return '—'
  return typeof v === 'string' ? v : JSON.stringify(v)
}

function text(e: ConfigEntry): string {
  return e.key in drafts
    ? drafts[e.key]!
    : typeof e.value === 'string'
      ? e.value
      : JSON.stringify(e.value)
}

// only an admin changes the settings, everyone else sees them read only
const canSet = computed(() => st.allows('PUT /v1/config/{key}'))
function editable(e: ConfigEntry): boolean {
  return !e.secret && canSet.value
}

// an enum's value as listed (the file may have it in another case)
function enumValue(e: ConfigEntry): string {
  const v = String(e.value)
  return e.values?.find((x) => x.toLowerCase() === v.toLowerCase()) ?? v
}

function storedLabel(e: ConfigEntry): string {
  return e.stored === 'db' ? 'database' : 'mangal.toml'
}

function sourceLabel(e: ConfigEntry): string {
  switch (e.source.kind) {
    case 'env':
    case 'flag':
      return `from ${e.source.name}`
    case 'run':
      return 'set for this run'
    case 'default':
      return 'default'
  }
  return 'stored'
}

// the keys whose change may cut the browser off the server once it restarts
const lockout = [
  'serve.host',
  'serve.port',
  'serve.tls_cert',
  'serve.tls_key',
  'serve.auth',
  'serve.web'
]

async function save(e: ConfigEntry, value: unknown) {
  if (
    lockout.includes(e.key) &&
    !window.confirm(
      `${e.key} changes how mangal serve is reached once it restarts: this page may not reach it afterwards. Save it?`
    )
  )
    return
  const r = await run(() =>
    ok(client.PUT('/v1/config/{key}', { params: { path: { key: e.key } }, body: { value } }))
  )
  if (!r) return
  delete drafts[e.key]
  if (r.applies === 'restart')
    toasts.push('warning', `${e.key} saved: the server uses it once it restarts`)
  else if (r.applies === 'reload')
    toasts.push(
      'success',
      `${e.key} saved: it applies when the providers load again (a restart of the server)`
    )
  else toasts.push('success', `${e.key} saved`)
  await Promise.all([load(), history.load(), st.refresh()])
}

function parse(e: ConfigEntry, s: string): unknown {
  if (e.kind === 'int') return Number(s)
  if (e.kind === 'map') return JSON.parse(s || '{}')
  return s
}

async function saveDraft(e: ConfigEntry) {
  let value: unknown
  try {
    value = parse(e, drafts[e.key] ?? '')
  } catch {
    toasts.push('danger', `${e.key}: not a JSON object`)
    return
  }
  await save(e, value)
}

function changed(c?: string | null): string {
  return c == null ? 'default' : c
}

const canUndo = computed(() => st.allows('POST /v1/config/history/{id}/undo'))
async function undo(c: ConfigChange) {
  const r = await run(
    () => ok(client.POST('/v1/config/history/{id}/undo', { params: { path: { id: c.id } } })),
    (r) => `${r.key} set back (undo of change ${c.id})`
  )
  if (r) await Promise.all([load(), history.load(), st.refresh()])
}
</script>

<template>
  <h4>Config</h4>
  <p class="small text-secondary">
    Every setting of the server: <b>mangal.toml</b> ones are this server's machine,
    <b>database</b> ones are shared by every mangal using its data. Secrets are masked and set from
    the accounts or <code>mangal config set</code>. Only an admin changes them and sees the paths,
    network, security and log settings.
  </p>
  <input
    v-model="filter"
    class="form-control form-control-sm mb-3"
    style="max-width: 20rem"
    placeholder="filter"
  />
  <LoadState :loading="loading && !data" :error="error" />
  <template v-for="[s, entries] in sections" :key="s">
    <h6 class="mt-3">{{ s }}</h6>
    <table class="table table-sm small align-middle">
      <tbody>
        <tr v-for="e in entries" :key="e.key" :data-test="`key-${e.key}`">
          <td class="mono" style="width: 25%">
            {{ e.provider ? e.key.slice(s.length + 1) : e.key }}
          </td>
          <td style="width: 30%">
            <template v-if="editable(e)">
              <input
                v-if="e.kind === 'bool'"
                type="checkbox"
                class="form-check-input"
                :checked="e.value === true"
                :disabled="busy"
                :data-test="`edit-${e.key}`"
                @change="save(e, !e.value)"
              />
              <select
                v-else-if="e.values?.length"
                class="form-select form-select-sm"
                :value="enumValue(e)"
                :disabled="busy"
                @change="save(e, ($event.target as HTMLSelectElement).value)"
              >
                <option v-for="v in e.values" :key="v" :value="v">{{ v }}</option>
              </select>
              <div v-else class="input-group input-group-sm">
                <input
                  class="form-control form-control-sm mono"
                  :value="text(e)"
                  :data-test="`edit-${e.key}`"
                  @input="drafts[e.key] = ($event.target as HTMLInputElement).value"
                  @keyup.enter="saveDraft(e)"
                />
                <button
                  v-if="e.key in drafts"
                  class="btn btn-outline-primary"
                  :disabled="busy"
                  @click="saveDraft(e)"
                >
                  Save
                </button>
              </div>
            </template>
            <span v-else class="mono" :class="{ 'text-secondary': e.source.kind === 'default' }">{{
              show(e.value)
            }}</span>
          </td>
          <td class="text-secondary">
            {{ e.description }}
            <div class="mt-1">
              <span class="badge text-bg-light border me-1" title="where it's stored">{{
                storedLabel(e)
              }}</span>
              <span
                class="badge me-1"
                :class="
                  e.source.kind === 'env' || e.source.kind === 'flag'
                    ? 'text-bg-info'
                    : 'text-bg-light border'
                "
                title="where the server's value comes from"
                >{{ sourceLabel(e) }}</span
              >
              <span class="badge text-bg-dark me-1" title="when a change applies">{{
                e.applies
              }}</span>
              <span v-if="e.restart" class="badge text-bg-warning me-1">restart needed</span>
              <span
                v-if="e.risky"
                class="badge text-bg-danger me-1"
                title="weakens access: only an admin sees it"
                >admin</span
              >
              <button
                v-if="
                  editable(e) &&
                  (e.source.kind === 'file' || e.source.kind === 'db') &&
                  e.kind !== 'map'
                "
                class="btn btn-link btn-sm p-0 ms-1"
                :disabled="busy"
                @click="save(e, e.default)"
              >
                reset to default
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </template>

  <h5 class="mt-4">History</h5>
  <p class="small text-secondary">
    The changes of the database settings, newest first (the mangal.toml ones have none). Undo sets a
    key back to its value before the change.
  </p>
  <LoadState :loading="history.loading.value && !history.data.value" :error="history.error.value" />
  <table v-if="history.data.value?.length" class="table table-sm small" data-test="history">
    <tbody>
      <tr v-for="c in history.data.value" :key="c.id">
        <td class="text-nowrap">{{ new Date(c.at).toLocaleString() }}</td>
        <td class="mono">{{ c.key }}</td>
        <td class="mono">{{ changed(c.old) }} → {{ changed(c.new) }}</td>
        <td class="text-secondary">{{ c.who }}</td>
        <td>
          <button
            v-if="canUndo"
            class="btn btn-outline-secondary btn-sm py-0"
            :disabled="busy"
            @click="undo(c)"
          >
            Undo
          </button>
        </td>
      </tr>
    </tbody>
  </table>
  <p v-else-if="history.data.value" class="small text-secondary">No changes yet.</p>
</template>
