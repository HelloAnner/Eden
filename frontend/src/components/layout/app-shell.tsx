import { Link, useLocation } from 'react-router-dom'

import type { ReactNode } from 'react'

import { Sidebar } from '@/components/layout/sidebar'
import type { BlogConfig, NoteTreeNode } from '@/types'

type AppShellProps = {
  config: BlogConfig
  noteTree: NoteTreeNode[]
  children: ReactNode
}

export function AppShell({ config, noteTree, children }: AppShellProps) {
  const location = useLocation()

  return (
    <div className="h-screen overflow-hidden bg-white">
      <div className="mx-auto h-full max-w-[1440px] lg:flex">
        <div className="hidden h-full shrink-0 lg:block">
          <Sidebar config={config} noteTree={noteTree} />
        </div>
        <main className="h-full flex-1 overflow-y-auto bg-white">
          <header className="border-b border-[#E5E7EB] px-4 py-3 lg:hidden">
            <Link to="/" className="text-base font-semibold text-[#181A1B]">
              {config.site.title}
            </Link>
            <nav className="mt-2 flex gap-4 text-sm text-[#5E6573]">
              <Link className={location.pathname === '/' ? 'text-[#3141F5]' : ''} to="/">
                首页
              </Link>
              <Link className={location.pathname === '/profile' ? 'text-[#3141F5]' : ''} to="/profile">
                个人
              </Link>
              <Link className={location.pathname === '/subscribe' ? 'text-[#3141F5]' : ''} to="/subscribe">
                订阅
              </Link>
              <Link className={location.pathname === '/search' ? 'text-[#3141F5]' : ''} to="/search?q=Java%20泛型">
                搜索
              </Link>
            </nav>
          </header>
          {children}
        </main>
      </div>
    </div>
  )
}
