export function resolveDataAssetUrl(value?: string | null): string {
  const source = (value ?? '').trim()
  if (!source) {
    return ''
  }
  if (source.startsWith('http://') || source.startsWith('https://') || source.startsWith('data:') || source.startsWith('/')) {
    return source
  }
  const encoded = source
    .split('/')
    .filter((item) => item.trim() !== '')
    .map((item) => encodeURIComponent(item))
    .join('/')
  if (!encoded) {
    return ''
  }
  return `/api/v1/data/${encoded}`
}

export function applyFavicon(iconHref: string) {
  if (!iconHref) {
    return
  }
  let link = document.querySelector("link[rel='icon']") as HTMLLinkElement | null
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }
  link.href = iconHref
}
