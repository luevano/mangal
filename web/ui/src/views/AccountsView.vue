<script setup lang="ts">
import { ref } from 'vue'
import client, { ok } from '@/api/client'
import LoadState from '@/components/LoadState.vue'
import { useLoad } from '@/composables/useLoad'
import { useAction } from '@/composables/useAction'
import { date } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useStatus } from '@/stores/status'
import type { APIClient, TrackerAccount } from '@/api/schemas'

crumbs(() => [{ title: 'Accounts' }])
const st = useStatus()
const { busy, run } = useAction()

// your own trackers (your reads sync there); the server's accounts are an admin's
const mine = useLoad(() => (st.allows('GET /v1/me/accounts') ? ok(client.GET('/v1/me/accounts')) : Promise.resolve([] as TrackerAccount[])))
const server = useLoad(() => (st.allows('GET /v1/accounts') ? ok(client.GET('/v1/accounts')) : Promise.resolve([])))

type Source = 'anilist' | 'kitsu' | 'myanimelist'
// the login in progress: its tracker, the authorize url once started, what's typed
const loggingIn = ref<Source>()
const url = ref('')
const paste = ref('')
const name = ref('')
const password = ref('')

function open(t: TrackerAccount) {
  loggingIn.value = t.source as Source
  url.value = paste.value = name.value = password.value = ''
}

async function start() {
  const source = loggingIn.value!
  const r = await run(() => ok(client.POST('/v1/me/accounts/{source}/login', { params: { path: { source } }, body: {} })))
  if (r?.url) url.value = r.url
}

async function finish() {
  const source = loggingIn.value!
  const body = source === 'kitsu' ? { name: name.value.trim(), password: password.value } : { paste: paste.value.trim() }
  const r = await run(
    () => ok(client.POST('/v1/me/accounts/{source}/login', { params: { path: { source } }, body })),
    (r: { account?: string }) => `Logged in as ${r.account}`
  )
  password.value = ''
  if (r === undefined) return
  loggingIn.value = undefined
  mine.load()
}

async function logout(t: TrackerAccount) {
  if (!confirm(`Log out of ${t.name}? Your reads stop syncing there.`)) return
  await run(() => ok(client.POST('/v1/me/accounts/{source}/logout', { params: { path: { source: t.source as Source } } })), `Logged out of ${t.name}`)
  mine.load()
}

// the server's API clients (an admin's): every user's AniList/MyAnimeList login goes through them
const clients = useLoad(() => (st.allows('GET /v1/accounts/clients') ? ok(client.GET('/v1/accounts/clients')) : Promise.resolve([] as APIClient[])))
type ClientSource = APIClient['source']
const editing = ref<ClientSource>()
const clientID = ref('')
const clientSecret = ref('')

function editClient(c: APIClient) {
  editing.value = c.source
  clientID.value = c.id ?? ''
  clientSecret.value = ''
}

async function saveClient(c: APIClient) {
  // an empty secret keeps the saved one (same id): it's never sent back to fill the field
  const body = { id: clientID.value.trim(), ...(clientSecret.value ? { secret: clientSecret.value.trim() } : {}) }
  const r = await run(() => ok(client.PUT('/v1/accounts/clients/{source}', { params: { path: { source: c.source } }, body })), `Saved the ${c.name} API client`)
  clientSecret.value = ''
  if (r === undefined) return
  editing.value = undefined
  clients.load()
  mine.load()
}

async function noSecret(c: APIClient) {
  await run(() => ok(client.PUT('/v1/accounts/clients/{source}', { params: { path: { source: c.source } }, body: { id: c.id!, secret: '' } })), `Dropped the ${c.name} client secret`)
  clients.load()
}

async function removeClient(c: APIClient) {
  if (!confirm(`Remove the ${c.name} API client? New logins need one again (the logins made stay).`)) return
  await run(() => ok(client.DELETE('/v1/accounts/clients/{source}', { params: { path: { source: c.source } } })), `Removed the ${c.name} API client`)
  clients.load()
  mine.load()
}

async function setRedirect(value: string) {
  await run(() => ok(client.PUT('/v1/config/{key}', { params: { path: { key: 'metadata.anilist.redirect' } }, body: { value } })), `AniList logins: ${value}`)
  clients.load()
}

const pasteHint: Record<string, string> = {
  code: 'the code AniList shows (or the address the page ends on)',
  address: 'the address MyAnimeList sends you to (the page may not load: copy it from the address bar)'
}
</script>

<template>
  <h4>Accounts</h4>
  <template v-if="st.allows('GET /v1/me/accounts')">
    <h5>Your trackers</h5>
    <p class="small text-secondary">Your reads sync to your own accounts (read.history.anilist, .kitsu, .myanimelist say which).</p>
    <LoadState :loading="mine.loading.value && !mine.data.value" :error="mine.error.value" />
    <table v-if="mine.data.value?.length" class="table table-sm align-middle" data-test="my-accounts">
      <thead><tr><th>Tracker</th><th>Account</th><th class="d-none d-md-table-cell">Expires</th><th /></tr></thead>
      <tbody>
        <tr v-for="t in mine.data.value" :key="t.source">
          <td>{{ t.name }}</td>
          <td>
            <template v-if="t.account">{{ t.account }}<span v-if="t.expired" class="text-danger small"> · expired, log in again</span></template>
            <span v-else class="text-secondary">not logged in</span>
            <div v-if="!t.ready" class="small text-secondary">needs the server's API client: an admin sets it<template v-if="st.allows('GET /v1/accounts/clients')"> below</template></div>
          </td>
          <td class="d-none d-md-table-cell small">{{ date(t.expires) }}</td>
          <td class="text-end text-nowrap">
            <button class="btn btn-sm btn-outline-primary" :disabled="busy || !t.ready" :data-test="`login-${t.source}`" @click="open(t)">Log in</button>
            <button v-if="t.account" class="btn btn-sm btn-outline-secondary ms-1" :disabled="busy" @click="logout(t)">Log out</button>
          </td>
        </tr>
      </tbody>
    </table>

    <div v-if="loggingIn" class="card card-body mb-3" data-test="tracker-login">
      <template v-if="loggingIn === 'kitsu'">
        <div class="row g-2">
          <div class="col-12 col-md-5"><input v-model="name" class="form-control form-control-sm" placeholder="Kitsu email or name" autocomplete="username" /></div>
          <div class="col-12 col-md-5"><input v-model="password" type="password" class="form-control form-control-sm" placeholder="password (not kept)" autocomplete="current-password" /></div>
          <div class="col-12 col-md-2"><button class="btn btn-sm btn-primary w-100" :disabled="busy || !name.trim() || !password" @click="finish">Log in</button></div>
        </div>
      </template>
      <template v-else>
        <p v-if="!url" class="mb-2"><button class="btn btn-sm btn-primary" :disabled="busy" data-test="login-start" @click="start">Get the login link</button></p>
        <template v-else>
          <p class="small mb-2">1. <a :href="url" target="_blank" rel="noopener noreferrer">Open the login page</a> and allow mangal. 2. Paste {{ mine.data.value?.find((t) => t.source === loggingIn)?.login === 'code' ? pasteHint.code : pasteHint.address }}:</p>
          <div class="input-group input-group-sm">
            <input v-model="paste" class="form-control" autocomplete="off" spellcheck="false" data-test="login-paste" />
            <button class="btn btn-primary" :disabled="busy || !paste.trim()" @click="finish">Log in</button>
          </div>
        </template>
      </template>
      <div class="mt-2"><button class="btn btn-sm btn-link px-0" @click="loggingIn = undefined">Cancel</button></div>
    </div>
  </template>

  <template v-if="st.allows('GET /v1/accounts/clients')">
    <h5 class="mt-4">API clients</h5>
    <p class="small text-secondary">
      Every user's AniList and MyAnimeList login goes through the server's API client: register your own at the tracker (none ships with mangal) and set it here. The client ID isn't secret, the
      secret is never shown again.
    </p>
    <LoadState :loading="clients.loading.value && !clients.data.value" :error="clients.error.value" />
    <table v-if="clients.data.value?.length" class="table table-sm align-middle" data-test="api-clients">
      <thead><tr><th>Tracker</th><th>Client</th><th /></tr></thead>
      <tbody>
        <template v-for="c in clients.data.value" :key="c.source">
          <tr>
            <td>{{ c.name }}</td>
            <td>
              <template v-if="c.configured">{{ c.id }}<span class="small text-secondary"> · {{ c.secret ? 'with a secret' : 'no secret' }}</span></template>
              <span v-else class="text-secondary">not set</span>
              <div class="small text-secondary">
                Register it at <a :href="c.registerUrl" target="_blank" rel="noopener noreferrer">{{ c.registerUrl }}</a> with the redirect URL <code>{{ c.redirectUrl }}</code>
              </div>
              <div v-if="c.redirect" class="small mt-1">
                Logins:
                <select
                  class="form-select form-select-sm d-inline-block w-auto"
                  :value="c.redirect"
                  :disabled="busy || !st.allows('PUT /v1/config/{key}')"
                  :data-test="`redirect-${c.source}`"
                  @change="setRedirect(($event.target as HTMLSelectElement).value)"
                >
                  <option value="pin">pin: AniList shows a code to paste (a server behind a proxy)</option>
                  <option value="local">local: back to mangal on the server's machine</option>
                </select>
                <span class="text-secondary"> (metadata.anilist.redirect; AniList takes one redirect URL per client)</span>
              </div>
            </td>
            <td class="text-end text-nowrap">
              <button class="btn btn-sm btn-outline-primary" :disabled="busy" :data-test="`client-${c.source}`" @click="editClient(c)">{{ c.configured ? 'Change' : 'Set' }}</button>
              <button v-if="c.configured && c.secret" class="btn btn-sm btn-outline-secondary ms-1" :disabled="busy" @click="noSecret(c)">No secret</button>
              <button v-if="c.configured" class="btn btn-sm btn-outline-danger ms-1" :disabled="busy" @click="removeClient(c)">Remove</button>
            </td>
          </tr>
          <tr v-if="editing === c.source">
            <td colspan="3">
              <div class="row g-2" data-test="client-form">
                <div class="col-12 col-md-5"><input v-model="clientID" class="form-control form-control-sm" placeholder="client ID" autocomplete="off" spellcheck="false" data-test="client-id" /></div>
                <div class="col-12 col-md-5">
                  <input
                    v-model="clientSecret"
                    type="password"
                    class="form-control form-control-sm"
                    :placeholder="c.secret && clientID.trim() === c.id ? 'client secret (empty keeps the saved one)' : 'client secret (optional)'"
                    autocomplete="new-password"
                    data-test="client-secret"
                  />
                </div>
                <div class="col-12 col-md-2 text-nowrap">
                  <button class="btn btn-sm btn-primary" :disabled="busy || !clientID.trim()" data-test="client-save" @click="saveClient(c)">Save</button>
                  <button class="btn btn-sm btn-link" @click="editing = undefined">Cancel</button>
                </div>
              </div>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </template>

  <template v-if="st.allows('GET /v1/accounts')">
    <h5 class="mt-4">The server's accounts</h5>
    <p class="small text-secondary">As last checked (no request here). Logins happen in the TUI or the CLI (<code>mangal accounts</code>).<template v-if="st.allows('GET /v1/users')"> Who logs into mangal: <RouterLink :to="{ name: 'users' }">Users</RouterLink>.</template></p>
    <LoadState :loading="server.loading.value && !server.data.value" :error="server.error.value" :empty="server.data.value && !server.data.value.length" empty-text="No accounts." />
    <table v-if="server.data.value?.length" class="table table-sm">
      <thead><tr><th>Service</th><th>Provider</th><th>User</th><th>Status</th><th class="d-none d-md-table-cell">Expires</th><th class="d-none d-md-table-cell">Checked</th></tr></thead>
      <tbody>
        <tr v-for="a in server.data.value" :key="a.service + (a.provider ?? '')">
          <td>{{ a.service }}</td>
          <td class="small">{{ a.provider }}</td>
          <td>{{ a.user }}</td>
          <td>
            {{ a.status }}<span v-if="a.error" class="text-danger small"> · {{ a.error }}</span>
            <div v-for="d in a.details ?? []" :key="d" class="small text-secondary">{{ d }}</div>
          </td>
          <td class="d-none d-md-table-cell small">{{ date(a.expires) }}</td>
          <td class="d-none d-md-table-cell small">{{ date(a.checked) }}</td>
        </tr>
      </tbody>
    </table>
  </template>
</template>
