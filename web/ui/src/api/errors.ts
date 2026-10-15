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

  // needsToken is a request refused for its scopes or a missing token: a token may help.
  get needsToken(): boolean {
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
