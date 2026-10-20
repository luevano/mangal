// openLookedUp opens the page lookup finds in a new tab: the tab is opened at once, in the
// click (one opened after an await is a popup the browser blocks), and sent to the url
// when it comes; it's closed when the lookup fails.
export async function openLookedUp(
  lookup: () => Promise<string>,
  open: (url: string, target: string, features?: string) => Window | null = (u, t, f) =>
    window.open(u, t, f)
): Promise<string> {
  const tab = open('', '_blank')
  if (tab) tab.opener = null
  try {
    const url = await lookup()
    if (tab) tab.location.href = url
    else open(url, '_blank', 'noopener')
    return url
  } catch (e) {
    tab?.close()
    throw e
  }
}
