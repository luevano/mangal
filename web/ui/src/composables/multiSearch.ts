import { onScopeDispose, ref } from 'vue'
import client, { ok } from '@/api/client'
import { errorMessage } from '@/api/errors'
import type { Manga, Provider } from '@/api/schemas'

// ProviderResults is a provider's part of a multi-provider search.
export interface ProviderResults {
  provider: Provider
  loading: boolean
  error: string
  mangas: Manga[]
}

// useMultiSearch searches several providers at once, one request each: every provider's
// results fill in as it answers. A new search aborts the one before.
export function useMultiSearch() {
  const results = ref<ProviderResults[]>([])
  let abort: AbortController | undefined

  function search(providers: Provider[], q: string) {
    abort?.abort()
    const ctl = (abort = new AbortController())
    results.value = providers.map((provider) => ({ provider, loading: true, error: '', mangas: [] }))
    results.value.forEach(async (r) => {
      try {
        r.mangas = await ok(client.GET('/v1/providers/{provider}/search', { params: { path: { provider: r.provider.id }, query: { q } }, signal: ctl.signal }))
      } catch (e) {
        if (ctl.signal.aborted) return
        r.error = errorMessage(e)
      }
      r.loading = false
    })
  }

  onScopeDispose(() => abort?.abort())
  return { results, search }
}

// storedProviders are the provider ids ticked last under the key, undefined when none
// were (or the storage can't be read).
export function storedProviders(key: string): string[] | undefined {
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? 'null')
    return Array.isArray(v) ? v.filter((x) => typeof x === 'string') : undefined
  } catch {
    return undefined
  }
}

// storeProviders remembers the provider ids ticked under the key, in this browser.
export function storeProviders(key: string, ids: string[]) {
  try {
    localStorage.setItem(key, JSON.stringify(ids))
  } catch {
    // private mode: not remembered
  }
}
