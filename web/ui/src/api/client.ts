import createClient, { type Middleware } from 'openapi-fetch'
import type { paths } from './mangal'
import { authHeaders } from './auth'
import { ApiError } from './errors'

const auth: Middleware = {
  onRequest({ request }) {
    for (const [k, v] of Object.entries(authHeaders())) request.headers.set(k, v)
    return request
  }
}

// client is the typed client of the API (web/openapi.yaml), paths relative to /api.
const client = createClient<paths>({ baseUrl: '/api' })
client.use(auth)

export default client

// ok is the data of an openapi-fetch answer, or throws its ApiError.
export async function ok<T>(
  p: Promise<{ data?: T; error?: unknown; response: Response }>
): Promise<T> {
  const { data, error, response } = await p
  if (error !== undefined || !response.ok) {
    throw new ApiError(
      response.status,
      (error ?? undefined) as ConstructorParameters<typeof ApiError>[1],
      response.statusText
    )
  }
  return data as T
}

// fetchJSON is a plain request of the API with the auth, for what the typed client can't
// say (a status other than 200 that isn't an error).
export async function fetchJSON<T>(path: string, init: RequestInit = {}): Promise<{ status: number; data: T }> {
  const headers = new Headers(init.headers)
  for (const [k, v] of Object.entries(authHeaders())) headers.set(k, v)
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  const response = await fetch(path, { ...init, headers })
  const text = await response.text()
  const data = text ? JSON.parse(text) : undefined
  if (!response.ok) throw new ApiError(response.status, data, response.statusText)
  return { status: response.status, data: data as T }
}
