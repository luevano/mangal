import type { JobKind, JobProvider, JobSettings } from '@/api/schemas'
import { date } from './format'

// the kind's state in a word, as the TUI and `mangal jobs` say it
export function kindState(k: JobKind): string {
  if (k.running) return 'running'
  if (k.busy) return 'busy'
  if (k.manual) return 'manual'
  if (!k.enabled) return 'off'
  if (k.paused) return 'paused'
  return 'on'
}

export const stateClass: Record<string, string> = {
  running: 'text-bg-info',
  busy: 'text-bg-info',
  manual: 'text-bg-secondary',
  off: 'text-bg-dark',
  paused: 'text-bg-warning',
  on: 'text-bg-success'
}

// when it runs next, or why it doesn't
export function nextLabel(k: JobKind): string {
  return k.next ? date(k.next) : (k.no_next ?? '')
}

// a provider's setting as typed: on, off, its interval; '' is the default
export function providerValue(p: JobProvider): string {
  if (!p.on) return 'off'
  if (p.interval) return p.interval
  return p.default ? 'on' : ''
}

export interface SettingsForm {
  enabled: boolean
  schedule: string
  delay: string
  providers: Record<string, string>
}

// formOf is the settings form of the kind, delay the shared pause ("1s-3s")
export function formOf(k: JobKind, delay: string): SettingsForm {
  const providers: Record<string, string> = {}
  for (const p of k.providers ?? []) providers[p.provider] = providerValue(p)
  return { enabled: k.enabled, schedule: k.schedule ?? '', delay, providers }
}

// changes are what the form changes of the kind, for PUT .../settings
export function changes(k: JobKind, was: SettingsForm, f: SettingsForm): JobSettings {
  const out: JobSettings = {}
  if (!k.manual && f.enabled !== was.enabled) out.enabled = f.enabled
  if (k.schedule_key && f.schedule.trim() !== was.schedule) out.schedule = f.schedule.trim()
  if (f.delay.trim() !== was.delay) {
    const [lo = '', hi] = f.delay.split('-').map((s) => s.trim())
    out.delay_min = lo
    out.delay_max = hi || lo
  }
  for (const [id, v] of Object.entries(f.providers)) {
    const value = v.trim().toLowerCase()
    if (value !== was.providers[id]) (out.providers ??= {})[id] = value
  }
  return out
}
