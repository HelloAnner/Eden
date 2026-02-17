export type SiteConfig = {
  title: string
  subtitle: string
  description: string
  icon: string
}

export type FooterConfig = {
  copyright: string
}

export type ProfileConfig = {
  name: string
  role: string
  bio: string
  avatar: string
  github: string
  email: string
  twitter: string
  zhihu: string
  xiaohongshu: string
  douyin: string
}

export type SubscribeConfig = {
  title: string
  description: string
  placeholder: string
  button_text: string
  benefits: string[]
  privacy_note: string
}

export type BlogConfig = {
  site: SiteConfig
  footer: FooterConfig
  profile: ProfileConfig
  subscribe: SubscribeConfig
}

export type Article = {
  id: string
  parent_id: string
  title: string
  slug: string
  category: string
  path: string
  tags: string[]
  excerpt: string
  content: string
  rendered_html?: string
  published_at: string
  read_minutes: number
  views: number
  order_index: number
  created_at?: string
  updated_at?: string
}

export type NoteTreeNode = {
  id: string
  name: string
  type: 'folder' | 'note'
  article_id?: string
  icon?: string
  children?: NoteTreeNode[]
}

export type Comment = {
  id: number
  article_id: string
  parent_id?: number
  author: string
  content: string
  likes: number
  created_at: string
  updated_at?: string
  replies?: Comment[]
}

export type SearchResponse = {
  keyword: string
  items: Article[]
  total: number
}

export type ProfileStats = {
  articles: number
  views: number
  years: number
}
