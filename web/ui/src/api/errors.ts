import type { ErrorResponse, Plan } from './schemas'

// ApiError is an answer of the API that isn't a success: its status and {error, code, ...}.
export class ApiError extends Error {
  status: number
  code: string
  reasons: string[]
  plan?: Plan
  confirm?: string

  constructor(status: number, body: Partial<ErrorResponse> | undefined, fallback = '') {
    super(body?.error || fallback || `HTTP ${status}`)
    this.name = 'ApiError'
    this.status = status
    this.code = body?.code ?? ''
    this.reasons = body?.reasons ?? []
    this.plan = body?.plan
    this.confirm = body?.confirm
  }

  // needsLogin is a request refused for a missing login or the user's permissions: a
  // login (as another user) may help.
  get needsLogin(): boolean {
    return this.status === 401 || (this.status === 403 && this.code === 'forbidden')
  }
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    const reasons = e.reasons.length ? `: ${e.reasons.join('; ')}` : ''
    return `${e.message}${reasons}`
  }
  if (e instanceof Error) return e.message
  return String(e)
}

// mangaGone is a provider's 404 for a manga it doesn't have (any more), not the one of a
// provider that isn't installed (`provider "x": not found`).
export function mangaGone(e: unknown): boolean {
  return e instanceof ApiError && e.status === 404 && e.code === 'not_found' && !e.message.startsWith('provider ')
}
