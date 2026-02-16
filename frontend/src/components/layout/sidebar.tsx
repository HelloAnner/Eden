import { type Dispatch, type SetStateAction, useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import {
  ChevronDown,
  ChevronRight,
  Mail,
  Search,
  User,
} from 'lucide-react'

import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'
import type { BlogConfig, NoteTreeNode } from '@/types'

type SidebarProps = {
  config: BlogConfig
  noteTree: NoteTreeNode[]
}

export function Sidebar({ config, noteTree }: SidebarProps) {
  const location = useLocation()
  const navigate = useNavigate()

  const isProfile = location.pathname === '/profile'
  const isSubscribe = location.pathname === '/subscribe'
  const isSearch = location.pathname === '/search'
  const activeArticleId = useMemo(() => {
    const match = /^\/article\/([^/]+)/.exec(location.pathname)
    return match ? decodeURIComponent(match[1]) : ''
  }, [location.pathname])

  const defaultSearch = useMemo(() => {
    const params = new URLSearchParams(location.search)
    return params.get('q') ?? ''
  }, [location.search])
  const [searchValue, setSearchValue] = useState(defaultSearch)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

  useEffect(() => {
    setSearchValue(defaultSearch)
  }, [defaultSearch])

  useEffect(() => {
    const activeFolderIDs = findExpandedFolderIdsForArticle(noteTree, activeArticleId)
    setExpanded((previous) => {
      const next: Record<string, boolean> = { ...previous }
      for (const folderID of activeFolderIDs) {
        next[folderID] = true
      }
      return next
    })
  }, [activeArticleId, noteTree])

  const submitSearch = () => {
    const value = searchValue.trim()
    if (!value) {
      navigate('/search')
      return
    }
    navigate(`/search?q=${encodeURIComponent(value)}`)
  }

  return (
    <aside className="flex h-full min-h-0 w-[280px] flex-col overflow-hidden border-r border-[#E5E7EB] bg-[#F9FAFB]">
      <div className="flex flex-col gap-4 px-5 pb-4 pt-5">
        <Link to="/" className="flex items-center gap-2.5">
          <div className="h-8 w-8 rounded-md bg-[#3141F5]" />
          <span className="text-[18px] font-semibold text-[#181A1B]">{config.site.title}</span>
        </Link>
        <div className="relative">
          <Search className={cn('absolute left-3 top-2.5 h-4 w-4', isSearch ? 'text-[#3141F5]' : 'text-[#A1A1AA]')} />
          <Input
            value={searchValue}
            onChange={(event) => setSearchValue(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                submitSearch()
              }
            }}
            className={cn(
              'h-9 rounded-md border pl-9 text-sm',
              isSearch ? 'border-2 border-[#3141F5] text-[#181A1B]' : 'border-[#E5E7EB] text-[#A1A1AA]',
            )}
            placeholder={isSearch ? searchValue || '输入关键字搜索' : 'Search...'}
          />
        </div>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto px-3 pb-3">
        <p className="px-2 py-1 text-[11px] font-semibold tracking-[1px] text-[#A1A1AA]">CATEGORIES</p>
        <div className="space-y-0.5">
          {noteTree.map((node) => (
            <TreeNode
              key={node.id}
              node={node}
              depth={0}
              expanded={expanded}
              setExpanded={setExpanded}
              activeArticleId={activeArticleId}
            />
          ))}
        </div>
      </div>

      <div className="px-4 pb-4">
        <Separator className="mb-4 bg-[#E5E7EB]" />
        <div className="grid grid-cols-2 gap-2.5">
          <Link
            to="/subscribe"
            className={cn(
              'flex items-center justify-center gap-2 rounded-lg px-2 py-2.5 text-xs font-semibold',
              isSubscribe ? 'bg-[#3141F5] text-white' : 'border border-[#3141F5] bg-[#EEF2FF] text-[#3141F5]',
            )}
          >
            <Mail className="h-4 w-4" />
            <span>订阅更新</span>
          </Link>

          <Link
            to="/profile"
            className={cn(
              'flex items-center justify-center gap-2 rounded-lg px-2 py-2.5 text-xs font-semibold',
              isProfile ? 'border-2 border-[#3141F5] bg-[#EFF6FF] text-[#3141F5]' : 'border border-[#E5E7EB] bg-white text-[#181A1B]',
            )}
          >
            <User className="h-4 w-4" />
            <span>个人主页</span>
          </Link>
        </div>
      </div>
    </aside>
  )
}

type TreeNodeProps = {
  node: NoteTreeNode
  depth: number
  expanded: Record<string, boolean>
  setExpanded: Dispatch<SetStateAction<Record<string, boolean>>>
  activeArticleId: string
}

function TreeNode({ node, depth, expanded, setExpanded, activeArticleId }: TreeNodeProps) {
  const hasChildren = !!node.children?.length
  const isFolder = node.type === 'folder'
  const isOpen = expanded[node.id] ?? false
  const paddingLeft = 8 + depth * 16
  const icon = node.icon?.trim() || (isFolder ? '📁' : '📝')

  if (isFolder) {
    return (
      <div>
        <button
          type="button"
          onClick={() => setExpanded((previous) => ({ ...previous, [node.id]: !isOpen }))}
          className="flex w-full items-center gap-2 rounded-md py-1.5 text-left text-sm text-[#181A1B]"
          style={{ paddingLeft }}
        >
          {isOpen ? <ChevronDown className="h-3.5 w-3.5 text-[#A1A1AA]" /> : <ChevronRight className="h-3.5 w-3.5 text-[#A1A1AA]" />}
          <span className="inline-flex h-4 w-4 items-center justify-center text-[14px] leading-none">{icon}</span>
          <span className="truncate font-medium">{node.name}</span>
        </button>
        {hasChildren && isOpen ? (
          <div>
            {node.children?.map((child) => (
              <TreeNode
                key={child.id}
                node={child}
                depth={depth + 1}
                expanded={expanded}
                setExpanded={setExpanded}
                activeArticleId={activeArticleId}
              />
            ))}
          </div>
        ) : null}
      </div>
    )
  }

  const articleID = node.article_id ?? ''
  const active = articleID !== '' && articleID === activeArticleId
  return (
    <Link
      to={articleID === '' ? '#' : `/article/${encodeURIComponent(articleID)}`}
      className={cn(
        'flex items-center gap-2 rounded-md py-1.5 text-[13px]',
        active ? 'bg-[#EFF6FF] text-[#3141F5]' : 'text-[#5E6573] hover:bg-white',
      )}
      style={{ paddingLeft: paddingLeft + 18 }}
    >
      <span className="inline-flex h-3.5 w-3.5 items-center justify-center text-[13px] leading-none">{icon}</span>
      <span className="truncate">{node.name}</span>
    </Link>
  )
}

function findExpandedFolderIdsForArticle(nodes: NoteTreeNode[], articleId: string): string[] {
  if (!articleId) {
    return []
  }
  const result = findFolderPath(nodes, articleId, [])
  if (!result) {
    return []
  }
  return result
}

function findFolderPath(nodes: NoteTreeNode[], articleId: string, folders: string[]): string[] | null {
  for (const node of nodes) {
    if (node.type === 'note' && node.article_id === articleId) {
      return folders
    }
    if (node.type === 'folder' && node.children?.length) {
      const nextFolders = [...folders, node.id]
      const found = findFolderPath(node.children, articleId, nextFolders)
      if (found) {
        return found
      }
    }
  }
  return null
}
