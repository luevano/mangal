import { ref } from 'vue'
import { ApiError, errorMessage } from '@/api/errors'
import { useStatus } from '@/stores/status'
import { useToasts } from '@/stores/toasts'

// useAction runs a non-destructive action (a download, a pause): a missing login or
// permission asks to log in and runs it again, an error is a toast.
export function useAction() {
  const busy = ref(false)

  async function run<T>(fn: () => Promise<T>, success?: string | ((r: T) => string)): Promise<T | undefined> {
    const toasts = useToasts()
    busy.value = true
    try {
      let r: T
      try {
        r = await fn()
      } catch (e) {
        if (e instanceof ApiError && e.needsLogin && (await useStatus().askLogin(e.message))) {
          r = await fn()
        } else {
          throw e
        }
      }
      if (success) toasts.push('success', typeof success === 'string' ? success : success(r))
      return r
    } catch (e) {
      toasts.push('danger', errorMessage(e), 8000)
      return undefined
    } finally {
      busy.value = false
    }
  }

  return { busy, run }
}
