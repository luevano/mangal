import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import client, { ok } from '@/api/client'
import { getToken, setToken } from '@/api/auth'
import { errorMessage } from '@/api/errors'
import type { Status } from '@/api/schemas'

export type Scope = 'read' | 'watch' | 'progress' | 'write'

// the status of the server and of this browser's authorization; asks for a token when an
// action needs one.
export const useStatus = defineStore('status', () => {
  const status = ref<Status>()
  const error = ref('')
  const loading = ref(false)
  const hasToken = ref(getToken() !== '')
  // the token dialog: open, why, and who waits for it
  const tokenAsked = ref(false)
  const tokenReason = ref('')
  let waiting: ((ok: boolean) => void)[] = []
  const tokenListeners: (() => void)[] = []

  const scopes = computed<string[]>(() => status.value?.scopes ?? [])
  const online = computed(() => !!status.value && !error.value)

  function can(scope: Scope): boolean {
    return scopes.value.includes(scope)
  }

  async function refresh() {
    loading.value = true
    try {
      status.value = await ok(client.GET('/v1/status'))
      error.value = ''
    } catch (e) {
      error.value = errorMessage(e)
    } finally {
      loading.value = false
    }
    return status.value
  }

  // askToken opens the token dialog; true once a token was saved.
  function askToken(reason: string): Promise<boolean> {
    tokenReason.value = reason
    tokenAsked.value = true
    return new Promise((resolve) => waiting.push(resolve))
  }

  function settle(saved: boolean) {
    tokenAsked.value = false
    const w = waiting
    waiting = []
    w.forEach((fn) => fn(saved))
  }

  async function saveToken(token: string, remember: boolean) {
    setToken(token.trim(), remember)
    hasToken.value = token.trim() !== ''
    await refresh()
    tokenListeners.forEach((fn) => fn())
    settle(hasToken.value)
  }

  function cancelToken() {
    settle(false)
  }

  async function forgetToken() {
    setToken('', false)
    hasToken.value = false
    await refresh()
    tokenListeners.forEach((fn) => fn())
  }

  function onTokenChange(fn: () => void) {
    tokenListeners.push(fn)
  }

  return {
    status,
    error,
    loading,
    hasToken,
    tokenAsked,
    tokenReason,
    scopes,
    online,
    can,
    refresh,
    askToken,
    saveToken,
    cancelToken,
    forgetToken,
    onTokenChange
  }
})
