import { ref, watch, type WatchSource } from 'vue'
import { errorMessage } from '@/api/errors'

// useLoad loads data with fn, again when a source changes; loading and error for the view.
export function useLoad<T>(fn: () => Promise<T>, sources: WatchSource[] = []) {
  const data = ref<T>()
  const loading = ref(false)
  const error = ref('')
  let seq = 0

  async function load() {
    const mine = ++seq
    loading.value = true
    try {
      const d = await fn()
      if (mine === seq) {
        data.value = d as never
        error.value = ''
      }
    } catch (e) {
      if (mine === seq) error.value = errorMessage(e)
    } finally {
      if (mine === seq) loading.value = false
    }
  }

  if (sources.length) watch(sources, load)
  load()
  return { data, loading, error, load }
}
