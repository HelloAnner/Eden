import { type ReactNode, useEffect, useState } from 'react'
import { Github, Mail, Twitter, User } from 'lucide-react'
import { SiTiktok, SiXiaohongshu, SiZhihu } from 'react-icons/si'

import { Card, CardContent } from '@/components/ui/card'
import { resolveDataAssetUrl } from '@/lib/asset'
import { getProfileStats } from '@/lib/api'
import type { BlogConfig, ProfileStats } from '@/types'

type ProfilePageProps = {
  config: BlogConfig
}

export function ProfilePage({ config }: ProfilePageProps) {
  const [stats, setStats] = useState<ProfileStats>({ articles: 56, views: 12000, years: 3 })
  const [brokenAvatarSrc, setBrokenAvatarSrc] = useState('')
  const avatarSrc = resolveDataAssetUrl(config.profile.avatar)

  useEffect(() => {
    getProfileStats()
      .then(setStats)
      .catch(() => {
        setStats({ articles: 56, views: 12000, years: 3 })
      })
  }, [])

  return (
    <div className="flex min-h-full flex-col items-center gap-10 bg-white px-4 py-8 sm:px-8 lg:px-20 lg:py-[60px]">
      <Card className="w-full max-w-[600px] gap-0 rounded-2xl border-[#E5E7EB] py-0 shadow-none">
        <CardContent className="p-12 text-center">
          <div className="mx-auto mb-8 flex h-[120px] w-[120px] items-center justify-center overflow-hidden rounded-full bg-[#3141F5]">
            {avatarSrc && brokenAvatarSrc !== avatarSrc ? (
              <img
                src={avatarSrc}
                alt={config.profile.name}
                className="h-full w-full object-cover"
                onError={() => setBrokenAvatarSrc(avatarSrc)}
              />
            ) : (
              <User className="h-12 w-12 text-white" />
            )}
          </div>
          <h1 className="mb-2 text-[32px] font-bold text-[#181A1B]">{config.profile.name}</h1>
          <p className="mb-8 text-base text-[#5E6573]">{config.profile.role}</p>
          <p className="mb-8 text-[15px] leading-[1.8] text-[#5E6573]">{config.profile.bio}</p>

          <div className="flex flex-wrap items-center justify-center gap-3">
            <SocialLink href={config.profile.github} icon={<Github className="h-[18px] w-[18px]" />} label="GitHub" />
            <SocialLink
              href={config.profile.email ? toMailto(config.profile.email) : ''}
              icon={<Mail className="h-[18px] w-[18px]" />}
              label="Email"
            />
            <SocialLink href={config.profile.twitter} icon={<Twitter className="h-[18px] w-[18px]" />} label="Twitter" />
            <SocialLink href={config.profile.zhihu} icon={<SiZhihu className="h-[18px] w-[18px] text-[#1772F6]" />} label="知乎" />
            <SocialLink href={config.profile.xiaohongshu} icon={<SiXiaohongshu className="h-[18px] w-[18px] text-[#FF2442]" />} label="小红书" />
            <SocialLink href={config.profile.douyin} icon={<SiTiktok className="h-[18px] w-[18px] text-[#111111]" />} label="抖音" />
          </div>
        </CardContent>
      </Card>

      <section className="grid w-full max-w-[600px] grid-cols-1 gap-4 sm:grid-cols-3 sm:gap-6">
        <StatCard value={String(stats.articles)} label="文章" />
        <StatCard value={formatViews(stats.views)} label="阅读量" />
        <StatCard value={String(stats.years)} label="年经验" />
      </section>
    </div>
  )
}

function SocialLink({ href, icon, label }: { href?: string; icon: ReactNode; label: string }) {
  if (!href || href.trim() === '') {
    return null
  }
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex items-center gap-2 rounded-lg bg-[#F4F7FA] px-4 py-2.5 text-sm font-medium text-[#181A1B]"
    >
      {icon}
      {label}
    </a>
  )
}

function StatCard({ value, label }: { value: string; label: string }) {
  return (
    <Card className="min-w-[110px] gap-0 rounded-xl border-[#E5E7EB] py-0 shadow-none">
      <CardContent className="flex flex-col items-center gap-1 px-8 py-5">
        <span className="text-[28px] font-bold text-[#3141F5]">{value}</span>
        <span className="text-sm text-[#5E6573]">{label}</span>
      </CardContent>
    </Card>
  )
}

function formatViews(value: number): string {
  if (value >= 1000) {
    return `${Math.round(value / 1000)}K`
  }
  return String(value)
}

function toMailto(value: string): string {
  if (value.startsWith('mailto:')) {
    return value
  }
  return `mailto:${value}`
}
