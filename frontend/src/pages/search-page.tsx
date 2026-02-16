import { useEffect, useMemo, useState } from 'react'
import { FileText, Search, Target, X } from 'lucide-react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { searchArticles } from '@/lib/api'
import type { Article } from '@/types'

export function SearchPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const initialKeyword = useMemo(() => params.get('q') ?? 'Java 泛型', [params])
  const [keyword, setKeyword] = useState(initialKeyword)
  const [items, setItems] = useState<Article[]>([])
  const [costMs, setCostMs] = useState(120)

  useEffect(() => {
    const current = params.get('q') ?? ''
    const startedAt = Date.now()
    searchArticles(current)
      .then((result) => {
        setItems(result.items)
        setKeyword(result.keyword || current)
      })
      .finally(() => {
        setCostMs(Date.now() - startedAt)
      })
  }, [params])

  const submitSearch = (value: string) => {
    const next = value.trim()
    if (!next) {
      navigate('/search')
      return
    }
    navigate(`/search?q=${encodeURIComponent(next)}`)
  }

  return (
    <div className="flex min-h-full flex-col gap-6 px-4 py-6 sm:px-6 lg:px-[60px] lg:py-10">
      <section className="space-y-5">
        <div className="relative">
          <Search className="absolute left-5 top-4 h-[22px] w-[22px] text-[#A1A1AA]" />
          <Input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                submitSearch(keyword)
              }
            }}
            className="h-[52px] rounded-[10px] border-2 border-[#E5E7EB] pl-14 pr-14 text-base font-medium text-[#181A1B]"
          />
          <button
            type="button"
            className="absolute right-4 top-3.5 rounded-full bg-[#F4F7FA] p-1.5"
            onClick={() => {
              setKeyword('')
              navigate('/search')
            }}
          >
            <X className="h-3.5 w-3.5 text-[#A1A1AA]" />
          </button>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <FileText className="h-4 w-4 text-[#A1A1AA]" />
          <span className="text-[#5E6573]">找到 {items.length} 篇相关文章</span>
          <span className="text-[#A1A1AA]">· 搜索用时 {(costMs / 1000).toFixed(2)} 秒</span>
        </div>
      </section>

      <section className="space-y-4">
        {items.map((article, index) => (
          <Card key={article.id} className="gap-0 rounded-[10px] border-[#E5E7EB] py-0 shadow-none">
            <CardContent className="space-y-3 p-5">
              <Link to={`/article/${encodeURIComponent(article.id)}`} className="block text-[18px] font-semibold text-[#181A1B] hover:text-[#3141F5]">
                {article.title}
              </Link>
              <p className="text-sm leading-[1.6] text-[#5E6573]">{article.excerpt}</p>
              <div className="flex items-center gap-4 text-[13px]">
                <span
                  className={`rounded px-2.5 py-1 text-xs font-medium ${
                    article.category === '架构设计'
                      ? 'bg-[#FEF3C7] text-[#D97706]'
                      : 'bg-[#EEF2FF] text-[#3141F5]'
                  }`}
                >
                  {article.category}
                </span>
                <span className="text-[#A1A1AA]">{article.published_at}</span>
                {index === 0 && (
                  <span className="inline-flex items-center gap-1 text-xs text-[#10B981]">
                    <Target className="h-3 w-3" />
                    高度匹配
                  </span>
                )}
              </div>
            </CardContent>
          </Card>
        ))}
      </section>
    </div>
  )
}
