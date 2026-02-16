import type {
  Article,
  BlogConfig,
  Comment,
  NoteTreeNode,
  ProfileStats,
  SearchResponse,
} from '@/types'

const API_PREFIX = '/api/v1'

async function parseJson<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || 'Request failed')
  }
  return (await response.json()) as T
}

export async function getConfig(): Promise<BlogConfig> {
  const response = await fetch(`${API_PREFIX}/config`)
  return parseJson<BlogConfig>(response)
}

export async function getArticleTree(): Promise<NoteTreeNode[]> {
  const response = await fetch(`${API_PREFIX}/articles/tree`)
  return parseJson<NoteTreeNode[]>(response)
}

export async function getRecentArticles(limit = 20): Promise<Article[]> {
  const response = await fetch(`${API_PREFIX}/articles/recent?limit=${limit}`)
  return parseJson<Article[]>(response)
}

export async function getArticle(
  articleId: string,
): Promise<{ article: Article; comments: Comment[] }> {
  const response = await fetch(`${API_PREFIX}/articles/${articleId}`)
  return parseJson<{ article: Article; comments: Comment[] }>(response)
}

export async function getComments(articleId: string): Promise<Comment[]> {
  const response = await fetch(
    `${API_PREFIX}/comments?article_id=${encodeURIComponent(articleId)}`,
  )
  return parseJson<Comment[]>(response)
}

export async function createComment(input: {
  article_id: string
  author?: string
  content: string
  parent_id?: number
}): Promise<Comment> {
  const response = await fetch(`${API_PREFIX}/comments`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  return parseJson<Comment>(response)
}

export async function likeComment(commentId: number): Promise<Comment> {
  const response = await fetch(`${API_PREFIX}/comments/${commentId}/like`, {
    method: 'POST',
  })
  return parseJson<Comment>(response)
}

export async function getProfileStats(): Promise<ProfileStats> {
  const response = await fetch(`${API_PREFIX}/profile/stats`)
  return parseJson<ProfileStats>(response)
}

export async function subscribe(email: string): Promise<void> {
  const response = await fetch(`${API_PREFIX}/subscribe`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email }),
  })
  await parseJson<{ status: string }>(response)
}

export async function searchArticles(keyword: string): Promise<SearchResponse> {
  const response = await fetch(
    `${API_PREFIX}/articles/search?q=${encodeURIComponent(keyword)}`,
  )
  return parseJson<SearchResponse>(response)
}
