// The web reader's logic, apart from the view: modes, keys and tap zones, page steps,
// preloading, settings remembered per manga.

export type Mode = 'ltr' | 'rtl' | 'webtoon'
export type Fit = 'width' | 'height' | 'screen' | 'original'
export type Background = 'black' | 'gray' | 'white'

export interface Settings {
  mode: Mode
  fit: Fit
  background: Background
  // double: two pages side by side in the paged modes (the first and wide ones alone)
  double: boolean
}

export const defaultSettings: Settings = { mode: 'ltr', fit: 'screen', background: 'black', double: false }
export const fits: Fit[] = ['screen', 'width', 'height', 'original']
export const modes: Mode[] = ['ltr', 'rtl', 'webtoon']

export type Action =
  | 'next'
  | 'prev'
  | 'first'
  | 'last'
  | 'next-chapter'
  | 'prev-chapter'
  | 'menu'
  | 'help'
  | 'close'
  | 'fit'
  | 'fullscreen'
  | 'jump'
  | 'mode-ltr'
  | 'mode-rtl'
  | 'mode-webtoon'
  | 'double'
  | 'thumbnails'
  | 'zoom-in'
  | 'zoom-out'
  | 'zoom-reset'

// keyAction is what a key does in the mode (one key per action, like the TUI's): arrows
// follow the reading direction; in webtoon mode ↑/↓ and space scroll the page natively.
export function keyAction(key: string, mode: Mode): Action | undefined {
  const paged = mode !== 'webtoon'
  switch (key) {
    case 'ArrowRight':
      return mode === 'rtl' ? 'prev' : 'next'
    case 'ArrowLeft':
      return mode === 'rtl' ? 'next' : 'prev'
    case 'ArrowDown':
    case 'PageDown':
    case ' ':
      return paged ? 'next' : undefined
    case 'ArrowUp':
    case 'PageUp':
      return paged ? 'prev' : undefined
    case 'Home':
      return 'first'
    case 'End':
      return 'last'
    case 'N':
      return 'next-chapter'
    case 'P':
      return 'prev-chapter'
    case 'm':
      return 'menu'
    case 'h':
    case '?':
      return 'help'
    case 'Escape':
      return 'close'
    case 'c':
      return 'fit'
    case 'f':
      return 'fullscreen'
    case 'g':
      return 'jump'
    case 'l':
      return 'mode-ltr'
    case 'r':
      return 'mode-rtl'
    case 'w':
      return 'mode-webtoon'
    case 'd':
      return paged ? 'double' : undefined
    case 't':
      return 'thumbnails'
    case '+':
    case '=':
      return paged ? 'zoom-in' : undefined
    case '-':
      return paged ? 'zoom-out' : undefined
    case '0':
      return paged ? 'zoom-reset' : undefined
  }
  return undefined
}

// keyHelp are the keys of the mode, for the help overlay.
export function keyHelp(mode: Mode): [string, string][] {
  const list: [string, string][] =
    mode === 'webtoon'
      ? [
          ['↓ ↑ space', 'scroll'],
          ['→ / ←', 'next / previous page']
        ]
      : [
          [mode === 'rtl' ? '← ↓ space' : '→ ↓ space', 'next page'],
          [mode === 'rtl' ? '→ ↑' : '← ↑', 'previous page'],
          ['tap the sides', mode === 'rtl' ? 'left: next, right: previous' : 'left: previous, right: next'],
          ['d', 'double page (the first and wide pages alone)'],
          ['+ - 0, ctrl+wheel, pinch', 'zoom in, out, back; drag to move, double-click/tap zooms']
        ]
  return [
    ...list,
    ['Home / End', 'first / last page'],
    ['N / P', 'next / previous chapter'],
    ['g', 'jump to a page'],
    ['t', 'thumbnails'],
    ['l r w', 'left to right, right to left, webtoon'],
    ['c', 'fit: screen, width, height, original'],
    ['f', 'fullscreen'],
    ['m, tap the middle', 'menu'],
    ['h ?', 'this help'],
    ['esc', 'close a panel, else back']
  ]
}

// zoneAction is what a tap at x (of width) does: the middle third opens the menu, the
// sides turn the page the way the mode reads.
export function zoneAction(x: number, width: number, mode: Mode): Action {
  const third = width / 3
  if (x >= third && x <= 2 * third) return 'menu'
  const left = x < third
  if (mode === 'rtl') return left ? 'next' : 'prev'
  return left ? 'prev' : 'next'
}

// step moves from index by the action among count pages; past an end it stays and says
// which end (the chapter prompt).
export function step(index: number, count: number, action: Action): { index: number; edge?: 'start' | 'end' } {
  const last = Math.max(0, count - 1)
  switch (action) {
    case 'next':
      return index >= last ? { index: last, edge: 'end' } : { index: index + 1 }
    case 'prev':
      return index <= 0 ? { index: 0, edge: 'start' } : { index: index - 1 }
    case 'first':
      return { index: 0 }
    case 'last':
      return { index: last }
  }
  return { index }
}

// preload are the pages to load around index: ±radius, nearest first.
export function preload(index: number, count: number, radius = 2): number[] {
  const out: number[] = []
  for (let d = 1; d <= radius; d++) {
    if (index + d < count) out.push(index + d)
    if (index - d >= 0) out.push(index - d)
  }
  return out
}

// PageSize is a page's size in pixels, when known.
export interface PageSize {
  width?: number
  height?: number
}

function wide(s?: PageSize): boolean {
  return !!s?.width && !!s?.height && s.width > s.height
}

// spreads are the pages shown together, in reading order: one each, or with double two
// side by side, the first (the cover) and wide pages (already a spread) alone. A page of
// unknown size counts as a single page.
export function spreads(sizes: (PageSize | undefined)[], double: boolean): number[][] {
  if (!double) return sizes.map((_, i) => [i])
  const out: number[][] = []
  let i = 0
  while (i < sizes.length) {
    if (i === 0 || i === sizes.length - 1 || wide(sizes[i]) || wide(sizes[i + 1])) {
      out.push([i])
      i++
    } else {
      out.push([i, i + 1])
      i += 2
    }
  }
  return out
}

// spreadOf is the index of the spread with the page.
export function spreadOf(list: number[][], page: number): number {
  const i = list.findIndex((s) => s.includes(page))
  return i < 0 ? 0 : i
}

// spreadStep is step over spreads: the first page of the spread to show.
export function spreadStep(list: number[][], page: number, action: Action): { index: number; edge?: 'start' | 'end' } {
  const r = step(spreadOf(list, page), list.length, action)
  return { index: list[r.index]?.[0] ?? 0, edge: r.edge }
}

// View is the zoom of the paged image: the scale, and the offset of its center (px).
export interface View {
  scale: number
  x: number
  y: number
}

export const unzoomed: View = { scale: 1, x: 0, y: 0 }
export const maxZoom = 5

// zoomAt scales the view by factor keeping the point (cx, cy) where it is (px from the
// container's center); back at 1 it's centered again.
export function zoomAt(v: View, factor: number, cx = 0, cy = 0): View {
  const scale = Math.min(Math.max(v.scale * factor, 1), maxZoom)
  if (scale === 1) return { ...unzoomed }
  const k = scale / v.scale
  return { scale, x: cx - (cx - v.x) * k, y: cy - (cy - v.y) * k }
}

// clampPan keeps the zoomed image over the container (width and height in px).
export function clampPan(v: View, width: number, height: number): View {
  const mx = ((v.scale - 1) * width) / 2
  const my = ((v.scale - 1) * height) / 2
  return { scale: v.scale, x: Math.min(Math.max(v.x, -mx), mx), y: Math.min(Math.max(v.y, -my), my) }
}

export function nextFit(f: Fit): Fit {
  return fits[(fits.indexOf(f) + 1) % fits.length]!
}

// neighbors are the chapters before and after the one at i.
export function neighbors<T>(list: T[], i: number): { prev?: T; next?: T } {
  if (i < 0) return {}
  return { prev: i > 0 ? list[i - 1] : undefined, next: i < list.length - 1 ? list[i + 1] : undefined }
}

// clampPage is a page asked for (1-based, typed) as an index.
export function clampPage(n: number, count: number): number {
  if (!Number.isFinite(n)) return 0
  return Math.min(Math.max(Math.round(n) - 1, 0), Math.max(0, count - 1))
}

const prefix = 'mangal.reader.'

function get(key: string): string | null {
  try {
    return localStorage.getItem(prefix + key)
  } catch {
    return null
  }
}

function put(key: string, value: string) {
  try {
    localStorage.setItem(prefix + key, value)
  } catch {
    // remembered for this page only
  }
}

function parse(raw: string | null): Partial<Settings> {
  if (!raw) return {}
  try {
    const v = JSON.parse(raw) as Partial<Settings>
    const out: Partial<Settings> = {}
    if (v.mode && modes.includes(v.mode)) out.mode = v.mode
    if (v.fit && fits.includes(v.fit)) out.fit = v.fit
    if (v.background && ['black', 'gray', 'white'].includes(v.background)) out.background = v.background
    if (typeof v.double === 'boolean') out.double = v.double
    return out
  } catch {
    return {}
  }
}

// loadSettings are the manga's settings, else the last ones chosen, else the defaults.
export function loadSettings(manga: string): Settings {
  return { ...defaultSettings, ...parse(get('default')), ...parse(get(`manga.${manga}`)) }
}

// saveSettings remembers them for the manga, and as the default of the others.
export function saveSettings(manga: string, s: Settings) {
  const raw = JSON.stringify(s)
  put(`manga.${manga}`, raw)
  put('default', raw)
}

// Position is the page index a chapter was left on and when (ms, 0 if unknown).
export interface Position {
  page: number
  at: number
}

export function loadPosition(chapter: string): Position {
  const raw = get(`pos.${chapter}`)
  if (!raw) return { page: 0, at: 0 }
  try {
    const v = JSON.parse(raw) as unknown
    // a plain number: saved before the time was kept
    if (typeof v === 'number') return { page: Number.isFinite(v) && v > 0 ? v : 0, at: 0 }
    const p = v as Partial<Position>
    const page = Number(p.page)
    return { page: Number.isFinite(page) && page > 0 ? page : 0, at: Number(p.at) || 0 }
  } catch {
    return { page: 0, at: 0 }
  }
}

export function savePosition(chapter: string, index: number, at = Date.now()) {
  put(`pos.${chapter}`, JSON.stringify({ page: index, at }))
}

// newer is the position saved last (the server's when it's another device's).
export function newer(local: Position, remote?: Position): Position {
  return remote && remote.at > local.at ? remote : local
}

export function loadIncognito(): boolean {
  return get('incognito') === 'true'
}

export function saveIncognito(on: boolean) {
  put('incognito', String(on))
}
