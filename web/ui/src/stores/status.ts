import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import client, { ok } from '@/api/client'
import { ApiError, errorMessage } from '@/api/errors'
import type { Me, Status } from '@/api/schemas'

export type Scope = 'read' | 'watch' | 'progress' | 'write'

// the status of the server and who this browser is logged in as; asks to log in (the
// login dialog) when an action needs a login or a permission the user hasn't.
export const useStatus = defineStore('status', () => {
  const status = ref<Status>()
  const error = ref('')
  const loading = ref(false)
  // who the browser is, undefined until known (or when nobody is logged in)
  const me = ref<Me>()
  const checked = ref(false)
  // the login dialog: open, why, and who waits for it
  const loginAsked = ref(false)
  const loginReason = ref('')
  let waiting: ((ok: boolean) => void)[] = []
  const loginListeners: (() => void)[] = []

  const scopes = computed<string[]>(() => me.value?.scopes ?? [])
  const online = computed(() => !!status.value && !error.value)
  const loggedIn = computed(() => !!me.value)

  function can(scope: Scope): boolean {
    return scopes.value.includes(scope)
  }

  async function refresh() {
    if (!me.value) return status.value
    loading.value = true
    try {
      status.value = await ok(client.GET('/v1/status'))
      error.value = ''
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        // the login ended (a new password, logged out elsewhere)
        me.value = undefined
      } else {
        error.value = errorMessage(e)
      }
    } finally {
      loading.value = false
    }
    return status.value
  }

  // loadMe asks who the browser is; undefined when nobody is logged in.
  async function loadMe(): Promise<Me | undefined> {
    try {
      me.value = await ok(client.GET('/v1/auth/me'))
      error.value = ''
    } catch (e) {
      me.value = undefined
      if (!(e instanceof ApiError && e.status === 401)) error.value = errorMessage(e)
    }
    checked.value = true
    return me.value
  }

  function changed() {
    loginListeners.forEach((fn) => fn())
  }

  // login logs in; the ApiError says why not (401 wrong name or password, 429 too many).
  async function login(name: string, password: string, remember: boolean) {
    me.value = await ok(client.POST('/v1/auth/login', { body: { name: name.trim(), password, remember } }))
    checked.value = true
    await refresh()
    changed()
    settle(true)
  }

  async function logout() {
    try {
      await ok(client.POST('/v1/auth/logout'))
    } finally {
      me.value = undefined
      status.value = undefined
      changed()
    }
  }

  // askLogin opens the login dialog; true once logged in.
  function askLogin(reason: string): Promise<boolean> {
    loginReason.value = reason
    loginAsked.value = true
    return new Promise((resolve) => waiting.push(resolve))
  }

  function settle(done: boolean) {
    loginAsked.value = false
    const w = waiting
    waiting = []
    w.forEach((fn) => fn(done))
  }

  function cancelLogin() {
    settle(false)
  }

  function onLoginChange(fn: () => void) {
    loginListeners.push(fn)
  }

  return {
    status,
    error,
    loading,
    me,
    checked,
    loggedIn,
    loginAsked,
    loginReason,
    scopes,
    online,
    can,
    refresh,
    loadMe,
    login,
    logout,
    askLogin,
    cancelLogin,
    onLoginChange
  }
})
