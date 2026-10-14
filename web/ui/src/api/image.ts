// imageURL is the url of the image through mangal's proxy (/api/image), so covers that need
// the site's Referer load and the browser doesn't hotlink them.
export function imageURL(url?: string, referer?: string): string {
    if (!url) {
        return ""
    }

    const apiURL = new URL('/api/image', `${location.protocol}//${location.host}`)
    apiURL.searchParams.set('url', url)
    if (referer) {
        apiURL.searchParams.set('referer', referer)
    }
    return apiURL.toString()
}
