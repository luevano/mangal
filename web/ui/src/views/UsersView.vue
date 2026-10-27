<script setup lang="ts">
import { computed, ref } from 'vue'
import client, { ok } from '@/api/client'
import FormModal from '@/components/FormModal.vue'
import LoadState from '@/components/LoadState.vue'
import { useLoad } from '@/composables/useLoad'
import { useAction } from '@/composables/useAction'
import { ago, date } from '@/composables/format'
import { crumbs } from '@/stores/crumbs'
import { useStatus } from '@/stores/status'
import type { User, Token } from '@/api/schemas'

crumbs(() => [{ title: 'Users' }])
const st = useStatus()
const { busy, run } = useAction()
// what each permission and role allows (the server's words)
const permissions = useLoad(() => ok(client.GET('/v1/permissions')))
const roles = computed(() => permissions.data.value?.roles ?? [])

// the users are an admin's; the tokens permission alone shows your own tokens
const isAdmin = computed(() => st.allows('GET /v1/users'))
const users = useLoad(() => (isAdmin.value ? ok(client.GET('/v1/users')) : Promise.resolve([] as User[])))
const tokens = useLoad(() => ok(client.GET('/v1/tokens')))
const admins = computed(() => (users.data.value ?? []).filter((u) => u.admin).length)

// withImplied adds what the permissions imply, and read
function withImplied(list: string[]): string[] {
  const out = new Set(list.length ? ['read', ...list] : [])
  let grew = true
  while (grew) {
    grew = false
    for (const p of permissions.data.value?.permissions ?? []) {
      if (!out.has(p.name)) continue
      for (const i of p.implies ?? []) {
        if (!out.has(i)) {
          out.add(i)
          grew = true
        }
      }
    }
  }
  return (permissions.data.value?.permissions ?? []).map((p) => p.name).filter((n) => out.has(n))
}

// roleOf is the role the permissions match ("custom" else), the tokens permission aside
function roleOf(list: string[]): string {
  const want = withImplied(list.filter((p) => p !== 'tokens' || list.includes('admin'))).join(',')
  return roles.value.find((r) => withImplied(r.perms).join(',') === want)?.name ?? 'custom'
}

const showAll = ref(false)
const role = computed(() => roleOf(perms.value))
const canTokens = computed({
  get: () => perms.value.includes('tokens'),
  set: (on: boolean) => {
    perms.value = on ? withImplied([...perms.value, 'tokens']) : perms.value.filter((p) => p !== 'tokens')
  }
})

function pickRole(name: string) {
  const r = roles.value.find((r) => r.name === name)
  if (!r) return
  perms.value = withImplied(canTokens.value && name !== 'admin' ? [...r.perms, 'tokens'] : r.perms)
}

function togglePerm(name: string, on: boolean) {
  perms.value = withImplied(on ? [...perms.value, name] : perms.value.filter((p) => p !== name))
}

// the open form: add a user, a user's permissions or password, add a token
type Form = 'add' | 'perms' | 'password' | 'token'
const form = ref<Form>()
const target = ref<User>()
const name = ref('')
const password = ref('')
const perms = ref<string[]>([])
const secret = ref<{ name: string; secret: string }>()

function open(f: Form, u?: User) {
  form.value = f
  target.value = u
  name.value = ''
  password.value = ''
  showAll.value = false
  perms.value = f === 'perms' && u ? [...u.perms] : f === 'token' ? ['read'] : []
  // a new user: a reader, the first one an admin
  if (f === 'add') pickRole(users.data.value?.length ? 'reader' : 'admin')
}

function close() {
  form.value = undefined
  password.value = ''
}

const valid = computed(() => {
  switch (form.value) {
    case 'add':
      return name.value.trim() !== '' && password.value.length >= 8
    case 'password':
      return password.value.length >= 8
    case 'perms':
      return perms.value.length > 0
    case 'token':
      return name.value.trim() !== ''
  }
  return false
})

const titles: Record<Form, string> = { add: 'Add a user', perms: 'Permissions', password: 'New password', token: 'Make an API token' }

async function submit() {
  const f = form.value
  const u = target.value
  let r: unknown
  switch (f) {
    case 'add':
      r = await run(
        () => ok(client.POST('/v1/users', { body: { name: name.value.trim(), password: password.value, perms: perms.value.length ? perms.value : undefined } })),
        (u: User) => `Added ${u.name} (${u.role})`
      )
      break
    case 'perms':
      r = await run(() => ok(client.PATCH('/v1/users/{name}', { params: { path: { name: u!.name } }, body: { perms: perms.value } })), `Changed the permissions of ${u!.name}, its logins ended`)
      break
    case 'password':
      r = await run(() => ok(client.PATCH('/v1/users/{name}', { params: { path: { name: u!.name } }, body: { password: password.value } })), `Changed the password of ${u!.name}, its logins ended`)
      break
    case 'token': {
      const made = await run(() => ok(client.POST('/v1/tokens', { body: { name: name.value.trim(), scopes: perms.value } })))
      if (made) secret.value = { name: made.token.name, secret: made.secret }
      r = made
      break
    }
  }
  if (r === undefined) return
  close()
  users.load()
  tokens.load()
  // my own password or permissions: this login ended
  if (u && u.name === st.me?.name && (f === 'password' || f === 'perms')) st.loadMe()
}

async function endLogins(u: User) {
  if ((await run(() => ok(client.POST('/v1/users/{name}/logout', { params: { path: { name: u.name } } })), `Ended the logins of ${u.name}`)) === undefined) return
  if (u.name === st.me?.name) st.loadMe()
}

async function removeUser(u: User) {
  if (!confirm(`Remove the user ${u.name}?`)) return
  await run(() => ok(client.DELETE('/v1/users/{name}', { params: { path: { name: u.name } } })), `Removed ${u.name}`)
  users.load()
}

async function removeToken(t: Token) {
  if (!confirm(`Remove the API token ${t.name}? Programs using it stop working.`)) return
  await run(() => ok(client.DELETE('/v1/tokens/{name}', { params: { path: { name: t.name } } })), `Removed the token ${t.name}`)
  tokens.load()
}

async function copySecret() {
  if (!secret.value) return
  try {
    await navigator.clipboard.writeText(secret.value.secret)
  } catch {
    // not over plain HTTP off localhost: select it by hand
  }
}
</script>

<template>
  <template v-if="isAdmin">
  <h4>Users</h4>
  <p class="small text-secondary">
    Who logs in (the web UI, OPDS readers). A new password or new permissions end the user's logins. Also from the
    server: <code>mangal user</code>, the TUI's accounts screen.
  </p>
  <div class="mb-2"><button class="btn btn-sm btn-primary" data-test="user-add" @click="open('add')">Add a user</button></div>
  <LoadState :loading="users.loading.value && !users.data.value" :error="users.error.value" :empty="users.data.value && !users.data.value.length" empty-text="No users." />
  <div v-if="users.data.value?.length" class="table-responsive">
    <table class="table table-sm align-middle" data-test="users-table">
      <thead><tr><th>Name</th><th>Role</th><th class="d-none d-md-table-cell">Last login</th><th /></tr></thead>
      <tbody>
        <tr v-for="u in users.data.value" :key="u.name">
          <td>{{ u.name }}<span v-if="u.name === st.me?.name" class="badge text-bg-secondary ms-1">you</span></td>
          <td><span class="badge text-bg-light" :title="u.perms.join(', ')">{{ u.role }}</span></td>
          <td class="d-none d-md-table-cell small">{{ u.last_login ? ago(u.last_login) : 'never' }}</td>
          <td class="text-end text-nowrap">
            <button class="btn btn-sm btn-outline-secondary" :disabled="busy" @click="open('perms', u)">Permissions</button>
            <button class="btn btn-sm btn-outline-secondary ms-1" :disabled="busy" @click="open('password', u)">Password</button>
            <button class="btn btn-sm btn-outline-secondary ms-1" :disabled="busy" title="End the logins (every browser and app)" @click="endLogins(u)">Log out</button>
            <button class="btn btn-sm btn-outline-danger ms-1" :disabled="busy || (u.admin && admins <= 1)" :title="u.admin && admins <= 1 ? 'the last admin' : ''" :data-test="`user-rm-${u.name}`" @click="removeUser(u)">
              <i class="bi bi-trash" />
            </button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  </template>

  <h5 :class="{ 'mt-4': isAdmin }">{{ isAdmin ? 'API tokens' : 'Your API tokens' }}</h5>
  <p class="small text-secondary">
    For programs (scripts, other apps): <code>Authorization: Bearer &lt;token&gt;</code>. Not a password for the web UI.
    <template v-if="!isAdmin">A token never has more than you do.</template>
  </p>
  <div class="mb-2"><button class="btn btn-sm btn-outline-primary" data-test="token-add" @click="open('token')">Make a token</button></div>
  <LoadState :loading="tokens.loading.value && !tokens.data.value" :error="tokens.error.value" :empty="tokens.data.value && !tokens.data.value.length" empty-text="No API tokens." />
  <table v-if="tokens.data.value?.length" class="table table-sm align-middle" data-test="tokens-table">
    <thead><tr><th>Name</th><th>Role</th><th class="d-none d-md-table-cell">Made</th><th /></tr></thead>
    <tbody>
      <tr v-for="t in tokens.data.value" :key="t.name">
        <td>{{ t.name }}</td>
        <td><span class="badge text-bg-light" :title="t.scopes.join(', ')">{{ t.role }}</span></td>
        <td class="d-none d-md-table-cell small">{{ date(t.created) }}<template v-if="t.owner"> · {{ t.owner }}'s</template></td>
        <td class="text-end">
          <button class="btn btn-sm btn-outline-danger" :disabled="busy" @click="removeToken(t)"><i class="bi bi-trash" /></button>
        </td>
      </tr>
    </tbody>
  </table>

  <FormModal :title="form ? titles[form] + (target ? `: ${target.name}` : '') : ''" :open="!!form" :disabled="!valid || busy" submit-text="Save" @close="close" @submit="submit">
    <div v-if="form === 'add' || form === 'token'" class="mb-3">
      <label class="form-label" for="u-name">Name</label>
      <input id="u-name" v-model="name" class="form-control" autocomplete="off" autocapitalize="none" spellcheck="false" :placeholder="form === 'token' ? 'what it\'s for: a script, an app' : ''" data-test="u-name" />
    </div>
    <div v-if="form === 'add' || form === 'password'" class="mb-3">
      <label class="form-label" for="u-password">Password</label>
      <input id="u-password" v-model="password" type="password" class="form-control" autocomplete="new-password" data-test="u-password" />
      <div class="form-text">8 characters or more.</div>
    </div>
    <div v-if="form !== 'password'">
      <div class="form-label">Role</div>
      <div v-for="r in roles" :key="r.name" class="form-check">
        <input :id="`role-${r.name}`" class="form-check-input" type="radio" name="role" :checked="role === r.name" :data-test="`role-${r.name}`" @change="pickRole(r.name)" />
        <label class="form-check-label" :for="`role-${r.name}`"><span class="text-capitalize fw-semibold">{{ r.name }}</span> <span class="small text-secondary">· {{ r.line }}</span></label>
      </div>
      <div v-if="role === 'custom'" class="form-text">Custom: {{ perms.join(', ') || 'nothing' }}.</div>
      <div v-if="role === 'admin'" class="alert alert-warning py-1 px-2 small mt-2 mb-0">An admin manages the users, every token and every setting.</div>
      <div v-if="role !== 'admin'" class="form-check mt-2">
        <input id="perm-tokens" v-model="canTokens" class="form-check-input" type="checkbox" data-test="perm-tokens" />
        <label class="form-check-label" for="perm-tokens">Can make API tokens <span class="small text-secondary">· their own, never with more than they have</span></label>
      </div>
      <button type="button" class="btn btn-link btn-sm px-0 mt-1" data-test="show-all" @click="showAll = !showAll">{{ showAll ? 'Hide' : 'Show all' }} permissions</button>
      <div v-if="showAll">
        <div v-for="p in permissions.data.value?.permissions ?? []" :key="p.name" class="form-check">
          <input :id="`perm-${p.name}`" class="form-check-input" type="checkbox" :checked="perms.includes(p.name)" :disabled="p.name === 'read'" @change="togglePerm(p.name, ($event.target as HTMLInputElement).checked)" />
          <label class="form-check-label" :for="`perm-${p.name}`">{{ p.name }} <span class="small text-secondary">· {{ p.line }}</span></label>
        </div>
      </div>
    </div>
  </FormModal>

  <FormModal title="API token made" :open="!!secret" submit-text="Done" @close="secret = undefined" @submit="secret = undefined">
    <p>The token <b>{{ secret?.name }}</b>, shown only now:</p>
    <div class="input-group">
      <input class="form-control font-monospace" readonly :value="secret?.secret" data-test="token-secret" @focus="($event.target as HTMLInputElement).select()" />
      <button class="btn btn-outline-secondary" type="button" title="Copy" @click="copySecret"><i class="bi bi-clipboard" /></button>
    </div>
  </FormModal>
</template>
