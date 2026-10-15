// formatting helpers for the views

export function num(n?: number | null): string {
  if (n === undefined || n === null) return ''
  return Number.isInteger(n) ? String(n) : String(Math.round(n * 100) / 100)
}

export function size(bytes?: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let n = bytes
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n < 10 && i > 0 ? n.toFixed(1) : Math.round(n)} ${units[i]}`
}

export function date(s?: string): string {
  if (!s) return ''
  const d = new Date(s)
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return ''
  return d.toLocaleString()
}

export function ago(s?: string): string {
  if (!s) return ''
  const d = new Date(s).getTime()
  if (isNaN(d)) return ''
  const sec = Math.round((Date.now() - d) / 1000)
  const abs = Math.abs(sec)
  const unit =
    abs < 60 ? [abs, 's'] : abs < 3600 ? [Math.round(abs / 60), 'm'] : abs < 86400 ? [Math.round(abs / 3600), 'h'] : [Math.round(abs / 86400), 'd']
  return sec >= 0 ? `${unit[0]}${unit[1]} ago` : `in ${unit[0]}${unit[1]}`
}

export function plural(n: number, one: string, many = one + 's'): string {
  return `${n} ${n === 1 ? one : many}`
}

export const sourceNames: Record<string, string> = {
  al: 'AniList',
  mal: 'MyAnimeList',
  kt: 'Kitsu',
  mu: 'MangaUpdates',
  ap: 'Anime-Planet',
  mb: 'MangaBaka'
}

export function sourceURL(source: string, id: string): string {
  switch (source) {
    case 'al':
      return `https://anilist.co/manga/${id}`
    case 'mal':
      return `https://myanimelist.net/manga/${id}`
    case 'kt':
      return `https://kitsu.app/manga/${id}`
    case 'mu':
      return `https://www.mangaupdates.com/series/${id}`
  }
  return ''
}
