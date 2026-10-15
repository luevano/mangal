// The token the web UI sends as a bearer (serve.auth = token). A page opened with basic
// auth needs none: the browser sends those credentials with every request. Kept in
// sessionStorage, or localStorage when "remember" is ticked.
const key = 'mangal.token'

function storages(): Storage[] {
  const list: Storage[] = []
  try {
    list.push(sessionStorage, localStorage)
  } catch {
    // storage blocked: the token lives in memory only
  }
  return list
}

let memory = ''

export function getToken(): string {
  if (memory) return memory
  for (const s of storages()) {
    try {
      const t = s.getItem(key)
      if (t) return t
    } catch {
      // ignore
    }
  }
  return ''
}

export function setToken(token: string, remember: boolean) {
  memory = token
  for (const s of storages()) {
    try {
      s.removeItem(key)
    } catch {
      // ignore
    }
  }
  if (!token) return
  try {
    ;(remember ? localStorage : sessionStorage).setItem(key, token)
  } catch {
    // memory only
  }
}

export function authHeaders(): Record<string, string> {
  const t = getToken()
  return t ? { Authorization: `Bearer ${t}` } : {}
}
