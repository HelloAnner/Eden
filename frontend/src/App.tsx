import { lazy, Suspense, useEffect, useState } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'

import { AppShell } from '@/components/layout/app-shell'
import { applyFavicon, resolveDataAssetUrl } from '@/lib/asset'
import { getArticleTree, getConfig } from '@/lib/api'
import type { BlogConfig, NoteTreeNode } from '@/types'

const HomePage = lazy(() => import('@/pages/home-page').then((module) => ({ default: module.HomePage })))
const ProfilePage = lazy(() => import('@/pages/profile-page').then((module) => ({ default: module.ProfilePage })))
const RecentUpdatesPage = lazy(() =>
  import('@/pages/recent-updates-page').then((module) => ({ default: module.RecentUpdatesPage })),
)
const SearchPage = lazy(() => import('@/pages/search-page').then((module) => ({ default: module.SearchPage })))
const SubscribePage = lazy(() => import('@/pages/subscribe-page').then((module) => ({ default: module.SubscribePage })))

const fallbackConfig: BlogConfig = {
  site: {
    title: "Anner's Blog",
    subtitle: '技术与生活',
    description: '专注于 Java 与工程实践的技术博客',
    icon: 'site/favicon.svg',
  },
  footer: {
    copyright: '© 2026 Anner. All rights reserved.',
  },
  profile: {
    name: 'Anner',
    role: 'Software Engineer @ FineReport',
    bio: '热爱技术，专注于 Java 后端开发和系统架构设计。喜欢分享技术心得，记录学习和成长的点滴。',
    avatar: 'profile/avatar.svg',
    github: 'https://github.com',
    email: 'anner@example.com',
    twitter: 'https://x.com',
    zhihu: 'https://www.zhihu.com',
    xiaohongshu: 'https://www.xiaohongshu.com',
    douyin: 'https://www.douyin.com',
  },
  subscribe: {
    title: '订阅博客更新',
    description: '输入您的邮箱地址，第一时间获取最新文章推送。我们承诺不会发送垃圾邮件，您可以随时取消订阅。',
    placeholder: '请输入您的邮箱地址',
    button_text: '立即订阅',
    benefits: ['每周精选技术文章推送', '独家技术资料和学习资源', '新文章发布即时通知'],
    privacy_note: '您的邮箱信息将被严格保密',
  },
}

function App() {
  const [config, setConfig] = useState<BlogConfig>(fallbackConfig)
  const [noteTree, setNoteTree] = useState<NoteTreeNode[]>([])

  useEffect(() => {
    Promise.all([getConfig(), getArticleTree()])
      .then(([nextConfig, nextTree]) => {
        setConfig(nextConfig)
        setNoteTree(nextTree)
      })
      .catch(() => {
        setConfig(fallbackConfig)
        setNoteTree([])
      })
  }, [])

  useEffect(() => {
    document.title = config.site.title || "Anner's Blog"
    applyFavicon(resolveDataAssetUrl(config.site.icon))
  }, [config.site.icon, config.site.title])

  return (
    <BrowserRouter>
      <AppShell config={config} noteTree={noteTree}>
        <Suspense fallback={<RouteLoading />}>
          <Routes>
            <Route path="/" element={<RecentUpdatesPage />} />
            <Route path="/article/:articleId" element={<HomePage />} />
            <Route path="/profile" element={<ProfilePage config={config} />} />
            <Route path="/subscribe" element={<SubscribePage config={config} />} />
            <Route path="/search" element={<SearchPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Suspense>
      </AppShell>
    </BrowserRouter>
  )
}

function RouteLoading() {
  return <div className="p-8 text-sm text-[#5E6573]">页面加载中...</div>
}

export default App
