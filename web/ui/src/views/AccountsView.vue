<script setup lang="ts">
import { ref } from 'vue'
import client, { ok } from '@/api/client'
import LoadState from '@/components/LoadState.vue'
import { useLoad } from '@/composables/useLoad'
import { useAction } from '@/composables/useAction'
import { date } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useStatus } from '@/stores/status'
import type { TrackerAccount } from '@/api/schemas'

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
            <div v-if="!t.ready" class="small text-secondary">needs the server's API client: an admin logs in once from mangal on the server</div>
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
