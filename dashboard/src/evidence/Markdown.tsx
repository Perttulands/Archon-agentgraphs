// Adapted from CHROTE's Markdown (dashboard/src/components/Markdown.tsx at
// 48947850): react-markdown with GitHub flavour, raw HTML left as text and only
// http, https and mailto links followed. File links resolve against the
// document's own artifact name instead of host paths, and bare-token controls
// are dropped.

import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { AnchorHTMLAttributes, ImgHTMLAttributes, MouseEvent as ReactMouseEvent } from 'react'
import './Markdown.css'

export interface MarkdownProps {
  /** The Markdown source. */
  content: string
  /** The document's own artifact name, so relative links can be resolved. */
  basePath?: string
  /** Where a link to another artifact goes. Without it file links are drawn as text. */
  onOpenPath?: (path: string) => void
  /** The URL an image beside the document loads from. Without it such images are not loaded. */
  imageUrl?: (path: string) => string
  className?: string
}

/** Schemes the cockpit follows. Anything else is drawn as text, never as a link. */
const SAFE_SCHEME = /^(https?:|mailto:)/i
const HAS_SCHEME = /^[a-z][a-z0-9+.-]*:/i

/** Resolve `href` against the directory holding `basePath`, POSIX style, without a leading slash. */
export function resolveMarkdownPath(basePath: string, href: string): string {
  const target = href.replace(/[?#].*$/, '')
  if (!target) return ''
  const base = target.startsWith('/') ? [] : basePath.split('/').slice(0, -1).filter(Boolean)
  const parts = target.startsWith('/') ? target.split('/').filter(Boolean) : base.concat(target.split('/'))
  const stack: string[] = []
  for (const part of parts) {
    if (part === '' || part === '.') continue
    if (part === '..') {
      stack.pop()
      continue
    }
    stack.push(part)
  }
  return stack.join('/')
}

/** A file link is anything without a scheme. A bare fragment stays on the page. */
export function isFileLink(href: string): boolean {
  return href !== '' && !href.startsWith('#') && !HAS_SCHEME.test(href)
}

function MarkdownLink(
  { href, children, basePath, onOpenPath, ...rest }: AnchorHTMLAttributes<HTMLAnchorElement> & {
    basePath: string
    onOpenPath?: (path: string) => void
  },
) {
  const target = (href || '').trim()
  if (onOpenPath && isFileLink(target)) {
    const path = resolveMarkdownPath(basePath, target)
    return (
      <a
        {...rest}
        href={`#${path}`}
        className="archon-markdown-file-link"
        onClick={(event: ReactMouseEvent<HTMLAnchorElement>) => {
          event.preventDefault()
          onOpenPath(path)
        }}
      >
        {children}
      </a>
    )
  }
  if (!SAFE_SCHEME.test(target)) return <span {...rest}>{children}</span>
  return <a {...rest} href={target} target="_blank" rel="noopener noreferrer">{children}</a>
}

function MarkdownImage({ src, alt, basePath, imageUrl }: ImgHTMLAttributes<HTMLImageElement> & { basePath: string; imageUrl?: (path: string) => string }) {
  const target = typeof src === 'string' ? src.trim() : ''
  if (isFileLink(target) && imageUrl) return <img src={imageUrl(resolveMarkdownPath(basePath, target))} alt={alt} />
  if (/^https?:/i.test(target)) return <img src={target} alt={alt} />
  return <span>{alt}</span>
}

export default function Markdown({ content, basePath = '', onOpenPath, imageUrl, className }: MarkdownProps) {
  return (
    <div className={className ? `archon-markdown ${className}` : 'archon-markdown'}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: props => <MarkdownLink {...props} basePath={basePath} onOpenPath={onOpenPath} />,
          img: props => <MarkdownImage {...props} basePath={basePath} imageUrl={imageUrl} />,
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}
