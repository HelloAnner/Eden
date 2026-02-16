export type TocItem = {
  id: string
  text: string
  level: number
}

const WIKILINK = /(?<!!)\[\[([^[\]]+)\]\]/g
const EMBED_LINK = /!\[\[([^[\]]+)\]\]/g
const HIGHLIGHT = /==([^=\n]+)==/g

export function preprocessObsidianMarkdown(content: string, articleTitle: string, articleId: string): string {
  const normalized = normalizeMarkdown(content, articleTitle)
  const lines = normalized.split('\n')
  const calloutReady = lines.map(transformCalloutHeader)

  return replaceObsidianSyntax(calloutReady, articleId).trim()
}

export function extractTocItems(markdown: string): TocItem[] {
  const lines = markdown.split('\n')
  const items: TocItem[] = []
  let inCodeFence = false

  for (const rawLine of lines) {
    const line = rawLine.trim()
    if (line.startsWith('```')) {
      inCodeFence = !inCodeFence
      continue
    }
    if (inCodeFence) {
      continue
    }
    const match = /^(#{1,6})\s+(.*)$/.exec(line)
    if (!match) {
      continue
    }
    const text = match[2].trim()
    if (!text) {
      continue
    }
    items.push({
      id: slugifyHeading(text),
      text,
      level: match[1].length,
    })
  }
  return items
}

export function slugifyHeading(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\w\u4e00-\u9fa5]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

function normalizeMarkdown(content: string, articleTitle: string): string {
  let source = content.replaceAll('\r\n', '\n')
  source = removeFrontmatter(source)

  let lines = source.split('\n')

  const firstNonEmptyIndex = lines.findIndex((line) => line.trim() !== '')
  if (firstNonEmptyIndex >= 0) {
    const firstLine = lines[firstNonEmptyIndex].trim()
    if (firstLine === `# ${articleTitle}`) {
      lines[firstNonEmptyIndex] = ''
      if (firstNonEmptyIndex + 1 < lines.length && lines[firstNonEmptyIndex + 1].trim() === '') {
        lines[firstNonEmptyIndex + 1] = ''
      }
    }
  }

  lines = removeTrailingTagBlock(lines)
  return lines.join('\n').trim()
}

function removeTrailingTagBlock(lines: string[]): string[] {
  let index = lines.length - 1
  while (index >= 0 && lines[index].trim() === '') {
    index--
  }
  if (index < 0) {
    return lines
  }

  let hasTag = false
  let start = index
  while (start >= 0) {
    const line = lines[start].trim()
    if (line === '') {
      start--
      continue
    }
    if (isObsidianTagLine(line)) {
      hasTag = true
      start--
      continue
    }
    break
  }

  if (!hasTag) {
    return lines
  }
  return lines.slice(0, start + 1)
}

function isObsidianTagLine(line: string): boolean {
  if (!line.startsWith('#')) {
    return false
  }
  if (line.startsWith('# ')) {
    return false
  }
  if (line.startsWith('##')) {
    return false
  }
  return true
}

function transformCalloutHeader(rawLine: string): string {
  const match = /^(\s*>\s*)\[!([A-Za-z0-9_-]+)([+-])?\]\s*(.*)$/.exec(rawLine)
  if (!match) {
    return rawLine
  }

  const prefix = match[1]
  const calloutType = match[2].toLowerCase()
  const foldState = match[3] ?? ''
  const title = match[4].trim()
  const label = calloutLabel(calloutType)
  const extra = foldState === '-' ? '（默认折叠）' : foldState === '+' ? '（可折叠）' : ''
  const text = `${label}${title ? `: ${title}` : ''}${extra}`
  return `${prefix}**${text}**`
}

function calloutLabel(calloutType: string): string {
  const map: Record<string, string> = {
    note: '📝 Note',
    abstract: '📌 Abstract',
    summary: '📌 Summary',
    tldr: '📌 TL;DR',
    info: 'ℹ️ Info',
    todo: '✅ Todo',
    tip: '💡 Tip',
    hint: '💡 Hint',
    important: '💡 Important',
    success: '✅ Success',
    check: '✅ Check',
    done: '✅ Done',
    question: '❓ Question',
    help: '❓ Help',
    faq: '❓ FAQ',
    warning: '⚠️ Warning',
    caution: '⚠️ Caution',
    attention: '⚠️ Attention',
    failure: '❌ Failure',
    fail: '❌ Fail',
    missing: '❌ Missing',
    danger: '🚨 Danger',
    error: '🚨 Error',
    bug: '🐞 Bug',
    example: '🧪 Example',
    quote: '💬 Quote',
    cite: '💬 Cite',
  }
  return map[calloutType] ?? `📎 ${capitalize(calloutType)}`
}

function convertEmbed(rawValue: string, articleId: string): string {
  const cleaned = rawValue.replace(/\\\|/g, '|')
  const [rawTarget] = cleaned.split('|')
  const target = rawTarget.trim()
  if (!target) {
    return ''
  }

  const lower = target.toLowerCase()
  if (isMarkdownFile(lower)) {
    const noteName = removeMarkdownExt(target)
    return `[🧩 嵌入笔记：${displayName(noteName)}](/search?q=${encodeURIComponent(noteName)})`
  }

  const url = resolveAttachmentUrl(target, articleId)
  if (isImageFile(lower)) {
    return `![${displayName(target)}](${url})`
  }
  if (isVideoFile(lower)) {
    return `[🎬 嵌入视频：${displayName(target)}](${url})`
  }
  if (isAudioFile(lower)) {
    return `[🎧 嵌入音频：${displayName(target)}](${url})`
  }
  if (isPdfFile(lower)) {
    return `[📄 嵌入文档：${displayName(target)}](${url})`
  }
  return `[📎 嵌入文件：${displayName(target)}](${url})`
}

function convertWikiLink(rawValue: string, articleId: string): string {
  const cleaned = rawValue.replace(/\\\|/g, '|')
  const [rawTarget, rawAlias] = cleaned.split('|')
  const target = (rawTarget ?? '').trim()
  const alias = (rawAlias ?? '').trim()
  if (!target) {
    return alias || ''
  }

  if (target.startsWith('http://') || target.startsWith('https://')) {
    return `[${alias || displayName(target)}](${target})`
  }
  if (target.startsWith('#')) {
    const heading = target.replace(/^#+/, '').trim()
    return `[${alias || heading}](#${slugifyHeading(heading)})`
  }

  const [notePart, anchorPart] = target.split('#')
  const noteName = removeMarkdownExt((notePart ?? '').trim())
  const lowTarget = target.toLowerCase()
  if (isImageFile(lowTarget) || isVideoFile(lowTarget) || isAudioFile(lowTarget) || isPdfFile(lowTarget)) {
    const fileURL = resolveAttachmentUrl(target, articleId)
    return `[${alias || displayName(target)}](${fileURL})`
  }
  const linkText = alias || displayName(noteName || target)
  if (!noteName && anchorPart) {
    return `[${linkText}](#${slugifyHeading(anchorPart)})`
  }
  const search = `/search?q=${encodeURIComponent(noteName || target)}`
  return `[${linkText}](${search})`
}

function removeFrontmatter(content: string): string {
  if (!content.startsWith('---\n')) {
    return content
  }
  const endIndex = content.indexOf('\n---\n', 4)
  if (endIndex < 0) {
    return content
  }
  return content.slice(endIndex + 5)
}

function replaceObsidianSyntax(lines: string[], articleId: string): string {
  let inCodeFence = false
  const nextLines: string[] = []

  for (const line of lines) {
    if (line.trim().startsWith('```')) {
      inCodeFence = !inCodeFence
      nextLines.push(line)
      continue
    }
    if (inCodeFence) {
      nextLines.push(line)
      continue
    }

    const replaced = line
      .replace(EMBED_LINK, (_all, rawValue: string) => convertEmbed(rawValue, articleId))
      .replace(WIKILINK, (_all, rawValue: string) => convertWikiLink(rawValue, articleId))
      .replace(HIGHLIGHT, (_all, rawText: string) => `<mark>${escapeHtml(rawText.trim())}</mark>`)
    nextLines.push(replaced)
  }

  return nextLines.join('\n')
}

function resolveAttachmentUrl(target: string, articleId: string): string {
  if (target.startsWith('http://') || target.startsWith('https://') || target.startsWith('data:')) {
    return target
  }

  const cleanTarget = target.replace(/^\.?\//, '')
  if (cleanTarget.startsWith('/')) {
    return `/api/v1/data/${encodePathSegments(cleanTarget.slice(1))}`
  }
  if (cleanTarget.startsWith('attachments/')) {
    return `/api/v1/data/${encodePathSegments(`${articleId}/${cleanTarget}`)}`
  }
  if (cleanTarget.includes('/')) {
    return `/api/v1/data/${encodePathSegments(cleanTarget)}`
  }
  return `/api/v1/data/${encodePathSegments(`${articleId}/attachments/${cleanTarget}`)}`
}

function encodePathSegments(value: string): string {
  return value
    .split('/')
    .filter((segment) => segment.trim() !== '')
    .map((segment) => encodeURIComponent(segment))
    .join('/')
}

function removeMarkdownExt(value: string): string {
  return value.replace(/\.md$/i, '')
}

function displayName(value: string): string {
  const normalized = removeMarkdownExt(value).trim()
  if (!normalized) {
    return value
  }
  const segments = normalized.split('/')
  return segments[segments.length - 1]
}

function capitalize(value: string): string {
  if (!value) {
    return value
  }
  return value.charAt(0).toUpperCase() + value.slice(1)
}

function isMarkdownFile(value: string): boolean {
  return value.endsWith('.md')
}

function isImageFile(value: string): boolean {
  return /\.(png|jpe?g|gif|svg|webp|bmp|avif)$/i.test(value)
}

function isVideoFile(value: string): boolean {
  return /\.(mp4|webm|mov|m4v|avi|mkv)$/i.test(value)
}

function isAudioFile(value: string): boolean {
  return /\.(mp3|wav|ogg|flac|m4a|aac)$/i.test(value)
}

function isPdfFile(value: string): boolean {
  return /\.pdf$/i.test(value)
}

function escapeHtml(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
}
