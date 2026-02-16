import { type Dispatch, type ReactNode, type SetStateAction, useEffect, useMemo, useState } from 'react'
import { Calendar, Eye, MessageCircle, MessageSquare, Send, ThumbsUp, Timer, User } from 'lucide-react'
import { useParams } from 'react-router-dom'

import { ObsidianRenderer } from '@/components/article/obsidian-renderer'
import { Button } from '@/components/ui/button'
import { extractTocItems, preprocessObsidianMarkdown } from '@/lib/obsidian'
import { Textarea } from '@/components/ui/textarea'
import { createComment, getArticle, likeComment } from '@/lib/api'
import type { Article, Comment } from '@/types'

type HomePageProps = {
  articleId?: string
}

export function HomePage({ articleId = 'java-generics-deep-dive' }: HomePageProps) {
  const params = useParams<{ articleId: string }>()
  const targetArticleId = params.articleId ?? articleId
  const [article, setArticle] = useState<Article | null>(null)
  const [comments, setComments] = useState<Comment[]>([])
  const [commentText, setCommentText] = useState('')
  const [replyTextMap, setReplyTextMap] = useState<Record<number, string>>({})
  const [replyTargetId, setReplyTargetId] = useState<number | null>(null)
  const [likeLoadingMap, setLikeLoadingMap] = useState<Record<number, boolean>>({})
  const [loading, setLoading] = useState(true)
  const [activeHeadingId, setActiveHeadingId] = useState('')

  useEffect(() => {
    if (!targetArticleId) {
      return
    }
    const mainContainer = document.querySelector('main')
    if (mainContainer instanceof HTMLElement) {
      mainContainer.scrollTo({ top: 0, left: 0, behavior: 'auto' })
    } else {
      window.scrollTo({ top: 0, left: 0, behavior: 'auto' })
    }
    let mounted = true
    getArticle(targetArticleId)
      .then((result) => {
        if (!mounted) {
          return
        }
        setArticle(result.article)
        setComments(result.comments)
      })
      .finally(() => {
        if (mounted) {
          setLoading(false)
        }
      })
    return () => {
      mounted = false
    }
  }, [targetArticleId])

  const renderedMarkdown = useMemo(
    () => preprocessObsidianMarkdown(article?.content ?? '', article?.title ?? '', article?.id ?? targetArticleId ?? ''),
    [article?.content, article?.id, article?.title, targetArticleId],
  )
  const tocItems = useMemo(() => extractTocItems(renderedMarkdown).filter((item) => item.level <= 3), [renderedMarkdown])

  useEffect(() => {
    setActiveHeadingId(tocItems[0]?.id ?? '')
  }, [tocItems])

  useEffect(() => {
    if (tocItems.length === 0) {
      return
    }
    const mainContainer = document.querySelector('main')
    if (!(mainContainer instanceof HTMLElement)) {
      return
    }

    const collectHeadings = () =>
      tocItems
        .map((item) => document.getElementById(item.id))
        .filter((node): node is HTMLElement => node instanceof HTMLElement)

    let headings = collectHeadings()
    let ticking = false

    const updateActiveHeading = () => {
      if (headings.length === 0) {
        headings = collectHeadings()
      }
      if (headings.length === 0) {
        return
      }
      const containerRect = mainContainer.getBoundingClientRect()
      const threshold = mainContainer.scrollTop + 140
      let current = headings[0].id

      for (const heading of headings) {
        const headingTop = mainContainer.scrollTop + heading.getBoundingClientRect().top - containerRect.top
        if (headingTop <= threshold) {
          current = heading.id
          continue
        }
        break
      }

      setActiveHeadingId((prev) => (prev === current ? prev : current))
    }

    const onScroll = () => {
      if (ticking) {
        return
      }
      ticking = true
      requestAnimationFrame(() => {
        updateActiveHeading()
        ticking = false
      })
    }

    updateActiveHeading()
    mainContainer.addEventListener('scroll', onScroll, { passive: true })
    window.addEventListener('scroll', onScroll, { passive: true })
    window.addEventListener('resize', onScroll)
    return () => {
      mainContainer.removeEventListener('scroll', onScroll)
      window.removeEventListener('scroll', onScroll)
      window.removeEventListener('resize', onScroll)
    }
  }, [tocItems, renderedMarkdown])

  const submitComment = async () => {
    if (!article || !commentText.trim()) {
      return
    }
    const newComment = await createComment({
      article_id: article.id,
      author: '访客',
      content: commentText.trim(),
    })
    setComments((prev) => [...prev, { ...newComment, replies: [] }])
    setCommentText('')
  }

  const submitReply = async (parentId: number) => {
    if (!article) {
      return
    }
    const text = (replyTextMap[parentId] ?? '').trim()
    if (!text) {
      return
    }
    const reply = await createComment({
      article_id: article.id,
      author: '访客',
      content: text,
      parent_id: parentId,
    })
    setComments((prev) => appendReply(prev, parentId, { ...reply, replies: [] }))
    setReplyTextMap((prev) => ({ ...prev, [parentId]: '' }))
    setReplyTargetId(null)
  }

  const likeCommentAction = async (commentId: number) => {
    if (likeLoadingMap[commentId]) {
      return
    }
    setLikeLoadingMap((prev) => ({ ...prev, [commentId]: true }))
    try {
      const updated = await likeComment(commentId)
      setComments((prev) => updateCommentInTree(prev, updated))
    } finally {
      setLikeLoadingMap((prev) => ({ ...prev, [commentId]: false }))
    }
  }

  const totalComments = useMemo(() => countComments(comments), [comments])

  if (loading || !article) {
    return <div className="p-10 text-sm text-[#5E6573]">加载中...</div>
  }

  return (
    <div className="flex min-h-full flex-col px-4 py-6 sm:px-6 lg:px-[60px] lg:py-10">
      <div className="mb-6 flex items-center gap-2 text-sm">
        <span className="text-[#5E6573]">首页</span>
        <span className="text-[#A1A1AA]">/</span>
        <span className="text-[#5E6573]">{article.category}</span>
        <span className="text-[#A1A1AA]">/</span>
        <span className="text-[#3141F5]">{article.path || article.category}</span>
      </div>

      <h1 className="mb-4 text-[32px] font-bold leading-[1.3] text-[#181A1B]">{article.title}</h1>
      <div className="mb-6 flex items-center gap-4 text-[13px] text-[#5E6573]">
        <Meta icon={<Calendar className="h-3.5 w-3.5 text-[#A1A1AA]" />} text={article.published_at} />
        <Meta icon={<Timer className="h-3.5 w-3.5 text-[#A1A1AA]" />} text={`阅读约 ${article.read_minutes} 分钟`} />
        <Meta icon={<Eye className="h-3.5 w-3.5 text-[#A1A1AA]" />} text={`${article.views.toLocaleString()} 次阅读`} />
      </div>

      <div className="flex flex-col gap-10 lg:flex-row lg:gap-12">
        <div className="min-w-0 flex-1 space-y-6">
          <ObsidianRenderer markdown={renderedMarkdown} />

          <section className="border-t border-[#E5E7EB] pt-8">
            <div className="mb-6 flex items-center gap-2">
              <MessageCircle className="h-5 w-5 text-[#181A1B]" />
              <h2 className="text-[20px] font-semibold text-[#181A1B]">评论 ({totalComments})</h2>
            </div>

            <div className="mb-5 flex gap-3 rounded-lg bg-[#F4F7FA] p-4">
              <div className="flex h-9 w-9 items-center justify-center rounded-full bg-[#E5E7EB]">
                <User className="h-4 w-4 text-[#A1A1AA]" />
              </div>
              <div className="flex-1 space-y-3">
                <Textarea
                  value={commentText}
                  onChange={(event) => setCommentText(event.target.value)}
                  placeholder="写下你的评论..."
                  className="min-h-20 resize-none border-[#E5E7EB] text-sm"
                />
                <Button onClick={submitComment} className="h-8 rounded-md bg-[#3141F5] px-4 text-sm hover:bg-[#192bf4]">
                  <Send className="mr-1.5 h-3.5 w-3.5" />
                  发表评论
                </Button>
              </div>
            </div>

            <div className="space-y-5">
              {comments.map((comment, index) => (
                <CommentItem
                  key={comment.id}
                  comment={comment}
                  depth={0}
                  colorIndex={index}
                  replyTargetId={replyTargetId}
                  replyTextMap={replyTextMap}
                  setReplyTargetId={setReplyTargetId}
                  setReplyTextMap={setReplyTextMap}
                  likeLoadingMap={likeLoadingMap}
                  onLike={likeCommentAction}
                  onSubmitReply={submitReply}
                />
              ))}
            </div>
          </section>
        </div>

        <aside className="hidden w-[220px] shrink-0 border-l border-[#E5E7EB] pl-6 lg:sticky lg:top-6 lg:block lg:self-start">
          <p className="mb-3 text-xs font-semibold tracking-[1px] text-[#A1A1AA]">目录</p>
          <div className="max-h-[calc(100vh-130px)] space-y-2 overflow-y-auto pr-2">
            {tocItems.map((item, index) => (
              <a
                key={`${item.id}-${index}`}
                href={`#${item.id}`}
                aria-current={activeHeadingId === item.id ? 'true' : undefined}
                onClick={() => setActiveHeadingId(item.id)}
                className="flex items-center gap-2 text-sm"
                style={{ paddingLeft: `${(item.level - 1) * 10}px` }}
              >
                <span className={`h-1.5 w-1.5 rounded-full ${activeHeadingId === item.id ? 'bg-[#3141F5]' : 'bg-[#E5E7EB]'}`} />
                <span className={activeHeadingId === item.id ? 'font-medium text-[#3141F5]' : 'text-[#5E6573]'}>{item.text}</span>
              </a>
            ))}
          </div>
        </aside>
      </div>
    </div>
  )
}

type CommentItemProps = {
  comment: Comment
  depth: number
  colorIndex: number
  replyTargetId: number | null
  replyTextMap: Record<number, string>
  setReplyTargetId: Dispatch<SetStateAction<number | null>>
  setReplyTextMap: Dispatch<SetStateAction<Record<number, string>>>
  likeLoadingMap: Record<number, boolean>
  onLike: (commentId: number) => Promise<void>
  onSubmitReply: (parentId: number) => Promise<void>
}

function CommentItem({
  comment,
  depth,
  colorIndex,
  replyTargetId,
  replyTextMap,
  setReplyTargetId,
  setReplyTextMap,
  likeLoadingMap,
  onLike,
  onSubmitReply,
}: CommentItemProps) {
  const blueTone = colorIndex % 2 !== 0
  const replies = comment.replies ?? []

  return (
    <div className={depth > 0 ? 'ml-8 mt-4 border-l border-[#E5E7EB] pl-4' : ''}>
      <div className="flex gap-3">
        <div className={`flex h-10 w-10 items-center justify-center rounded-full ${blueTone ? 'bg-[#E3F2FD]' : 'bg-[#E8F5E9]'}`}>
          <User className={`h-[18px] w-[18px] ${blueTone ? 'text-[#2196F3]' : 'text-[#4CAF50]'}`} />
        </div>
        <div className="flex-1 space-y-2">
          <div className="flex items-center gap-3 text-xs">
            <span className="text-sm font-semibold text-[#181A1B]">{comment.author}</span>
            <span className="text-[#A1A1AA]">{comment.created_at}</span>
          </div>
          <p className="text-sm leading-[1.6] text-[#5E6573]">{comment.content}</p>
          <div className="flex items-center gap-4 text-xs text-[#A1A1AA]">
            <button
              type="button"
              onClick={() => onLike(comment.id)}
              disabled={likeLoadingMap[comment.id]}
              className="inline-flex items-center gap-1 hover:text-[#3141F5] disabled:opacity-50"
            >
              <ThumbsUp className="h-3.5 w-3.5" />
              {comment.likes}
            </button>
            <button
              type="button"
              onClick={() => setReplyTargetId((prev) => (prev === comment.id ? null : comment.id))}
              className="inline-flex items-center gap-1 hover:text-[#3141F5]"
            >
              <MessageSquare className="h-3.5 w-3.5" />
              回复
            </button>
          </div>

          {replyTargetId === comment.id ? (
            <div className="space-y-2 rounded-md bg-[#F4F7FA] p-3">
              <Textarea
                value={replyTextMap[comment.id] ?? ''}
                onChange={(event) => setReplyTextMap((prev) => ({ ...prev, [comment.id]: event.target.value }))}
                placeholder="写下你的回复..."
                className="min-h-20 resize-none border-[#E5E7EB] text-sm"
              />
              <Button
                onClick={() => {
                  void onSubmitReply(comment.id)
                }}
                className="h-8 rounded-md bg-[#3141F5] px-4 text-sm hover:bg-[#192bf4]"
              >
                <Send className="mr-1.5 h-3.5 w-3.5" />
                提交回复
              </Button>
            </div>
          ) : null}
        </div>
      </div>

      {replies.map((reply, index) => (
        <CommentItem
          key={reply.id}
          comment={reply}
          depth={depth + 1}
          colorIndex={index + 1}
          replyTargetId={replyTargetId}
          replyTextMap={replyTextMap}
          setReplyTargetId={setReplyTargetId}
          setReplyTextMap={setReplyTextMap}
          likeLoadingMap={likeLoadingMap}
          onLike={onLike}
          onSubmitReply={onSubmitReply}
        />
      ))}
    </div>
  )
}

function Meta({ icon, text }: { icon: ReactNode; text: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      {icon}
      <span>{text}</span>
    </span>
  )
}

function countComments(nodes: Comment[]): number {
  let total = 0
  for (const node of nodes) {
    total += 1
    total += countComments(node.replies ?? [])
  }
  return total
}

function appendReply(nodes: Comment[], parentId: number, reply: Comment): Comment[] {
  return nodes.map((node) => {
    if (node.id === parentId) {
      const replies = node.replies ?? []
      return { ...node, replies: [...replies, reply] }
    }
    return { ...node, replies: appendReply(node.replies ?? [], parentId, reply) }
  })
}

function updateCommentInTree(nodes: Comment[], updated: Comment): Comment[] {
  return nodes.map((node) => {
    if (node.id === updated.id) {
      return { ...node, likes: updated.likes, updated_at: updated.updated_at }
    }
    return { ...node, replies: updateCommentInTree(node.replies ?? [], updated) }
  })
}
