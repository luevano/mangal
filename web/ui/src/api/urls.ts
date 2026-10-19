// URLs the browser loads itself (images, files): no bearer header there, the request gets
// the loopback scopes or the page's basic auth.

export function imageURL(url?: string, referer?: string): string {
  if (!url) return ''
  const q = new URLSearchParams({ url })
  if (referer) q.set('referer', referer)
  return `/api/v1/image?${q}`
}

export function coverURL(manga: number): string {
  return `/api/v1/library/mangas/${manga}/cover`
}

export function libraryPageURL(manga: number, path: string, page: number): string {
  return `/api/v1/library/mangas/${manga}/pages/${page}?${new URLSearchParams({ path })}`
}

export function chapterFileURL(manga: number, path: string, inline = false): string {
  const q = new URLSearchParams({ path })
  if (inline) q.set('inline', 'true')
  return `/api/v1/library/mangas/${manga}/file?${q}`
}

export function providerPageURL(
  provider: string,
  mangaID: string,
  chapter: number,
  volume: number | undefined,
  page: number
): string {
  const q = new URLSearchParams({ manga_id: mangaID, chapter: String(chapter) })
  if (volume !== undefined) q.set('volume', String(volume))
  return `/api/v1/providers/${encodeURIComponent(provider)}/pages/${page}?${q}`
}

export function mangaZipURL(manga: number): string {
  return `/api/v1/library/mangas/${manga}/zip`
}

export function providerIconURL(provider: string): string {
  return `/api/v1/providers/${encodeURIComponent(provider)}/icon`
}

export function metadataIconURL(source: string): string {
  return `/api/v1/metadata/${encodeURIComponent(source)}/icon`
}
