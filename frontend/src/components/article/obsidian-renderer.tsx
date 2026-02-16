import type { ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import rehypeKatex from 'rehype-katex'
import rehypeRaw from 'rehype-raw'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'

import { slugifyHeading } from '@/lib/obsidian'

type ObsidianRendererProps = {
  markdown: string
}

export function ObsidianRenderer({ markdown }: ObsidianRendererProps) {
  return (
    <article className="space-y-4 text-[#5E6573]">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeRaw, rehypeKatex]}
        components={{
          h1: ({ children }) => <Heading level={1}>{children}</Heading>,
          h2: ({ children }) => <Heading level={2}>{children}</Heading>,
          h3: ({ children }) => <Heading level={3}>{children}</Heading>,
          h4: ({ children }) => <Heading level={4}>{children}</Heading>,
          h5: ({ children }) => <Heading level={5}>{children}</Heading>,
          h6: ({ children }) => <Heading level={6}>{children}</Heading>,
          p: ({ children }) => <p className="text-base leading-[1.8] text-[#5E6573]">{children}</p>,
          ul: ({ children }) => <ul className="list-disc space-y-2 pl-6 text-base leading-[1.8] text-[#5E6573]">{children}</ul>,
          ol: ({ children }) => <ol className="list-decimal space-y-2 pl-6 text-base leading-[1.8] text-[#5E6573]">{children}</ol>,
          li: ({ children }) => <li className="marker:text-[#A1A1AA]">{children}</li>,
          blockquote: ({ children }) => <blockquote className="rounded-r-lg border-l-4 border-[#3141F5] bg-[#F7FAFF] px-4 py-3 text-[15px] leading-[1.8] text-[#3F4A5A]">{children}</blockquote>,
          table: ({ children }) => <div className="overflow-x-auto"><table className="w-full border-collapse text-sm">{children}</table></div>,
          thead: ({ children }) => <thead className="bg-[#F4F7FA]">{children}</thead>,
          tr: ({ children }) => <tr className="border-b border-[#E5E7EB]">{children}</tr>,
          th: ({ children }) => <th className="px-3 py-2 text-left font-semibold text-[#181A1B]">{children}</th>,
          td: ({ children }) => <td className="px-3 py-2 text-[#5E6573]">{children}</td>,
          a: ({ href, children }) => (
            <a
              href={href}
              className="text-[#3141F5] underline decoration-[#CBD5E1] underline-offset-2 hover:decoration-[#3141F5]"
              target={href?.startsWith('http') ? '_blank' : undefined}
              rel={href?.startsWith('http') ? 'noreferrer' : undefined}
            >
              {children}
            </a>
          ),
          img: ({ src, alt }) => (
            <img
              src={src}
              alt={alt ?? ''}
              className="max-h-[460px] rounded-lg border border-[#E5E7EB] bg-white object-contain"
              loading="lazy"
            />
          ),
          hr: () => <hr className="my-8 border-[#E5E7EB]" />,
          pre: ({ children }) => (
            <pre className="overflow-x-auto rounded-lg border border-[#E2E8F0] bg-[#F8FAFC] p-5 text-sm leading-[1.6] text-[#0F172A]">
              {children}
            </pre>
          ),
          code: (props) => {
            const { children } = props as { children?: ReactNode; inline?: boolean }
            const isInline = Boolean((props as { inline?: boolean }).inline)
            if (isInline) {
              return <code className="rounded bg-[#F4F7FA] px-1.5 py-0.5 text-[13px] text-[#1E293B]">{children}</code>
            }
            return <code>{children}</code>
          },
        }}
      >
        {markdown}
      </ReactMarkdown>
    </article>
  )
}

function Heading({ level, children }: { level: number; children: ReactNode }) {
  const text = plainText(children)
  const id = slugifyHeading(text)
  const styleMap: Record<number, string> = {
    1: 'text-[28px]',
    2: 'text-[24px]',
    3: 'text-[20px]',
    4: 'text-[18px]',
    5: 'text-[16px]',
    6: 'text-[15px]',
  }
  const sizeClass = styleMap[level] ?? 'text-[20px]'
  const className = `scroll-mt-24 font-semibold leading-[1.4] text-[#181A1B] ${sizeClass}`
  switch (level) {
    case 1:
      return <h1 id={id} className={className}>{children}</h1>
    case 2:
      return <h2 id={id} className={className}>{children}</h2>
    case 3:
      return <h3 id={id} className={className}>{children}</h3>
    case 4:
      return <h4 id={id} className={className}>{children}</h4>
    case 5:
      return <h5 id={id} className={className}>{children}</h5>
    default:
      return <h6 id={id} className={className}>{children}</h6>
  }
}

function plainText(node: ReactNode): string {
  if (node == null || typeof node === 'boolean') {
    return ''
  }
  if (typeof node === 'string' || typeof node === 'number') {
    return String(node)
  }
  if (Array.isArray(node)) {
    return node.map((item) => plainText(item)).join('')
  }
  if (typeof node === 'object' && 'props' in node) {
    const children = (node as { props?: { children?: ReactNode } }).props?.children
    return plainText(children)
  }
  return ''
}
