import type { HealthFinding } from '@/api/schemas'
import { sourceNames } from '@/composables/format'

// the health fixes as the TUI labels them (tui/state/health applyLabel)

export function choiceList(f: HealthFinding): string[] {
  const a = f.fix.action
  if (!a) return []
  if ((a.kind === 'link' || a.kind === 'pick') && a.ids && a.ids.length > 1) return a.ids
  if (a.kind === 'merge' && a.mangas && a.mangas.length > 1) return a.mangas.map(String)
  return []
}

export function choiceLabel(f: HealthFinding, choice: string): string {
  const a = f.fix.action
  if (a?.kind === 'merge') return `keep ${choice}${choice === String(a.mangas?.[0]) ? ' (suggested)' : ''}`
  return `${sourceNames[a?.source ?? ''] ?? a?.source ?? ''} ${choice}`
}

export function applyLabel(f: HealthFinding): string {
  const a = f.fix.action
  if (!a) return ''
  const source = sourceNames[a.source ?? ''] ?? a.source ?? ''
  switch (a.kind) {
    case 'resume':
      return 'Resume'
    case 'watch':
      return 'Watch it'
    case 'link':
      return a.ids?.length === 1 ? `Link ${source} ${a.ids[0]}` : `Link ${source}`
    case 'pick':
      return `Pick ${source}`
    case 'merge':
      return 'Merge'
    case 'mark':
      return 'Mark'
    case 'forget':
      return 'Scan or forget'
    case 'scan':
      return 'Scan'
    case 'rename':
      return 'Rename'
    case 'empty-trash':
      return 'Empty the trash'
  }
  return 'Fix'
}
