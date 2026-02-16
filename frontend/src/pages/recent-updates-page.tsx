import { useEffect, useState } from 'react'
import { CalendarDays, Clock3, RefreshCcw } from 'lucide-react'
import { Link } from 'react-router-dom'

import { Card, CardContent } from '@/components/ui/card'
import { getRecentArticles } from '@/lib/api'
import type { Article } from '@/types'

export function RecentUpdatesPage() {
  const [items, setItems] = useState<Article[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    getRecentArticles(30)
      .then((result) => {
        setItems(result)
      })
      .finally(() => {
        setLoading(false)
      })
  }, [])

  if (loading) {
    return <div className="p-10 text-sm text-[#5E6573]">加载最近更新中...</div>
  }

  return (
    <div className="flex min-h-full flex-col gap-6 px-4 py-6 sm:px-6 lg:px-[60px] lg:py-10">
      {items.length === 0 ? (
        <Card className="gap-0 rounded-[10px] border-[#E5E7EB] py-0 shadow-none">
          <CardContent className="p-6 text-sm text-[#5E6573]">暂无文章数据</CardContent>
        </Card>
      ) : (
        <section className="space-y-4">
          {items.map((article) => (
            <Card key={article.id} className="gap-0 rounded-[10px] border-[#E5E7EB] py-0 shadow-none">
              <CardContent className="space-y-3 p-5">
                <Link to={`/article/${encodeURIComponent(article.id)}`} className="block text-[18px] font-semibold text-[#181A1B] hover:text-[#3141F5]">
                  {article.title}
                </Link>
                <p className="text-sm leading-[1.6] text-[#5E6573]">{article.excerpt || '暂无摘要'}</p>
                <div className="flex flex-wrap items-center gap-4 text-xs text-[#5E6573]">
                  <span className="rounded bg-[#EEF2FF] px-2.5 py-1 font-medium text-[#3141F5]">{article.path || article.category}</span>
                  <span className="inline-flex items-center gap-1">
                    <CalendarDays className="h-3.5 w-3.5 text-[#A1A1AA]" />
                    创建 {formatDateTime(article.created_at)}
                  </span>
                  <span className="inline-flex items-center gap-1">
                    <RefreshCcw className="h-3.5 w-3.5 text-[#A1A1AA]" />
                    更新 {formatDateTime(article.updated_at)}
                  </span>
                  <span className="inline-flex items-center gap-1">
                    <Clock3 className="h-3.5 w-3.5 text-[#A1A1AA]" />
                    阅读约 {article.read_minutes} 分钟
                  </span>
                </div>
              </CardContent>
            </Card>
          ))}
        </section>
      )}
    </div>
  )
}

function formatDateTime(value?: string): string {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}
