import type { ReactNode } from 'react'
import 'katex/dist/katex.min.css'
import ReactMarkdown from 'react-markdown'
import rehypeKatex from 'rehype-katex'
import rehypeRaw from 'rehype-raw'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'

import { slugifyHeading } from '@/lib/obsidian'

type ObsidianRendererProps = {
  markdown: string
  renderedHtml?: string
}

export function ObsidianRenderer({ markdown, renderedHtml }: ObsidianRendererProps) {
  if (renderedHtml && renderedHtml.trim().length > 0) {
    return (
      <article
        className="space-y-4 text-[#5E6573] [&_a]:text-[#3141F5] [&_a]:underline [&_a]:decoration-[#CBD5E1] [&_a]:underline-offset-2 [&_a:hover]:decoration-[#3141F5] [&_blockquote]:rounded-r-lg [&_blockquote]:border-l-4 [&_blockquote]:border-[#3141F5] [&_blockquote]:bg-[#F7FAFF] [&_blockquote]:px-4 [&_blockquote]:py-3 [&_blockquote]:text-[15px] [&_blockquote]:leading-[1.8] [&_blockquote]:text-[#3F4A5A] [&_code]:rounded [&_code]:bg-[#F4F7FA] [&_code]:px-1.5 [&_code]:py-0.5 [&_code]:text-[13px] [&_code]:text-[#1E293B] [&_h1]:scroll-mt-24 [&_h1]:text-[28px] [&_h1]:font-semibold [&_h1]:leading-[1.4] [&_h1]:text-[#181A1B] [&_h2]:scroll-mt-24 [&_h2]:text-[24px] [&_h2]:font-semibold [&_h2]:leading-[1.4] [&_h2]:text-[#181A1B] [&_h3]:scroll-mt-24 [&_h3]:text-[20px] [&_h3]:font-semibold [&_h3]:leading-[1.4] [&_h3]:text-[#181A1B] [&_h4]:scroll-mt-24 [&_h4]:text-[18px] [&_h4]:font-semibold [&_h4]:leading-[1.4] [&_h4]:text-[#181A1B] [&_h5]:scroll-mt-24 [&_h5]:text-[16px] [&_h5]:font-semibold [&_h5]:leading-[1.4] [&_h5]:text-[#181A1B] [&_h6]:scroll-mt-24 [&_h6]:text-[15px] [&_h6]:font-semibold [&_h6]:leading-[1.4] [&_h6]:text-[#181A1B] [&_hr]:my-8 [&_hr]:border-[#E5E7EB] [&_img]:max-h-[460px] [&_img]:rounded-lg [&_img]:border [&_img]:border-[#E5E7EB] [&_img]:bg-white [&_img]:object-contain [&_li]:marker:text-[#A1A1AA] [&_ol]:list-decimal [&_ol]:space-y-2 [&_ol]:pl-6 [&_ol]:text-base [&_ol]:leading-[1.8] [&_ol]:text-[#5E6573] [&_p]:text-base [&_p]:leading-[1.8] [&_p]:text-[#5E6573] [&_pre]:overflow-x-auto [&_pre]:rounded-lg [&_pre]:border [&_pre]:border-[#E2E8F0] [&_pre]:bg-[#F8FAFC] [&_pre]:p-5 [&_pre]:text-sm [&_pre]:leading-[1.6] [&_pre]:text-[#0F172A] [&_table]:w-full [&_table]:border-collapse [&_table]:text-sm [&_tbody_tr]:border-b [&_tbody_tr]:border-[#E5E7EB] [&_td]:px-3 [&_td]:py-2 [&_td]:text-[#5E6573] [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:font-semibold [&_th]:text-[#181A1B] [&_thead]:bg-[#F4F7FA] [&_ul]:list-disc [&_ul]:space-y-2 [&_ul]:pl-6 [&_ul]:text-base [&_ul]:leading-[1.8] [&_ul]:text-[#5E6573]"
        dangerouslySetInnerHTML={{ __html: renderedHtml }}
      />
    )
  }

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
