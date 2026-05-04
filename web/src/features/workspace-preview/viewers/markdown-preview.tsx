'use client'

import katex from 'katex'
import type { ComponentPropsWithoutRef } from 'react'
import ReactMarkdown from 'react-markdown'
import SyntaxHighlighter from 'react-syntax-highlighter'
import { vs, vs2015 } from 'react-syntax-highlighter/dist/esm/styles/hljs'
import rehypeKatex from 'rehype-katex'
import rehypeRaw from 'rehype-raw'
import remarkBreaks from 'remark-breaks'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'

import { useTheme } from '@/features/theme/theme-provider'
import { buildWorkspaceFileBufferUrl } from '@/lib/api'
import { normalizeHighlighterLanguage } from '@/features/workspace-preview/language'
import { MermaidBlock } from '@/features/workspace-preview/viewers/mermaid-block'
import styles from '@/features/workspace-preview/viewers/markdown-preview.module.css'

const LOCAL_URL_PATTERN = /^(?![a-z]+:|\/\/|data:|blob:|mailto:|tel:|#)/i
const EXTERNAL_URL_PATTERN = /^(https?:|\/\/|mailto:|tel:)/i
type MarkdownCodeProps = ComponentPropsWithoutRef<'code'> & { inline?: boolean; node?: unknown }

function toStringProp(value: unknown) {
  return typeof value === 'string' ? value : undefined
}

function normalizeWorkspaceRelativePath(path: string) {
  const segments = path
    .replace(/\\/g, '/')
    .split('/')
    .filter(Boolean)

  const resolved: string[] = []
  for (const segment of segments) {
    if (segment === '.') continue
    if (segment === '..') {
      if (!resolved.length) return null
      resolved.pop()
      continue
    }

    resolved.push(segment)
  }

  return resolved.join('/')
}

function getDirectory(path: string) {
  const normalized = path.replace(/\\/g, '/')
  const index = normalized.lastIndexOf('/')
  if (index === -1) return ''
  return normalized.slice(0, index)
}

function resolveWorkspaceAssetUrl(src: string | undefined, workspaceId?: string, filePath?: string) {
  if (!src || !workspaceId || !filePath) return src
  if (!LOCAL_URL_PATTERN.test(src)) return src

  const pathOnly = src.split('#')[0].split('?')[0]
  if (!pathOnly) return src

  const baseDir = getDirectory(filePath)
  const candidate = pathOnly.startsWith('/') ? pathOnly.slice(1) : [baseDir, pathOnly].filter(Boolean).join('/')
  const normalized = normalizeWorkspaceRelativePath(candidate)
  if (!normalized) return src

  return buildWorkspaceFileBufferUrl(workspaceId, normalized)
}

function MarkdownImage({
  alt,
  filePath,
  src,
  workspaceId,
  ...props
}: ComponentPropsWithoutRef<'img'> & { workspaceId?: string; filePath?: string }) {
  const resolvedSrc = resolveWorkspaceAssetUrl(toStringProp(src), workspaceId, filePath)

  if (!resolvedSrc) {
    return alt ? <span>{alt}</span> : null
  }

  return <img alt={alt} referrerPolicy="no-referrer" src={resolvedSrc} {...props} />
}

export function MarkdownPreview({
  content,
  filePath,
  workspaceId,
}: {
  content?: string
  filePath?: string
  workspaceId?: string
}) {
  const { currentTheme } = useTheme()

  return (
    <article className={styles.root}>
      <ReactMarkdown
        components={{
          a({ children, href, node: _node, ...props }) {
            const resolvedHref = resolveWorkspaceAssetUrl(toStringProp(href), workspaceId, filePath)
            const isExternal = Boolean(resolvedHref && EXTERNAL_URL_PATTERN.test(resolvedHref))

            return (
              <a href={resolvedHref} rel={isExternal ? 'noreferrer' : undefined} target={isExternal ? '_blank' : undefined} {...props}>
                {children}
              </a>
            )
          },
          code({
            children,
            className,
            node: _node,
            ...props
          }: MarkdownCodeProps) {
            const match = /language-([^\s]+)/.exec(className || '')
            const codeContent = String(children).replace(/\n$/, '')
            const language = normalizeHighlighterLanguage(match?.[1])

            if (!match) {
              return (
                <code className={className} {...props}>
                  {children}
                </code>
              )
            }

            if (language === 'latex' || language === 'math' || language === 'tex') {
              try {
                const html = katex.renderToString(codeContent, {
                  displayMode: true,
                  throwOnError: false,
                })

                return <div dangerouslySetInnerHTML={{ __html: html }} />
              } catch {
                // Fall back to highlighted source below.
              }
            }

            if (language === 'mermaid') {
              return <MermaidBlock code={codeContent} />
            }

            return (
              <SyntaxHighlighter
                PreTag="div"
                customStyle={{
                  background: 'transparent',
                  border: `1px solid rgb(var(--color-border))`,
                  borderRadius: '16px',
                  fontSize: '13px',
                  lineHeight: '1.6',
                  margin: 0,
                  padding: '16px',
                }}
                language={language}
                style={currentTheme === 'dark' ? vs2015 : vs}
                wrapLongLines={language === 'plaintext'}
              >
                {codeContent}
              </SyntaxHighlighter>
            )
          },
          img({ alt, node: _node, src, ...props }) {
            return <MarkdownImage alt={alt} filePath={filePath} src={src} workspaceId={workspaceId} {...props} />
          },
          pre({ children, node: _node }) {
            return <div className="overflow-x-auto">{children}</div>
          },
          table({ children, node: _node, ...props }) {
            return (
              <div className="overflow-x-auto">
                <table {...props}>{children}</table>
              </div>
            )
          },
        }}
        rehypePlugins={[rehypeRaw, rehypeKatex]}
        remarkPlugins={[remarkGfm, remarkMath, remarkBreaks]}
      >
        {content || ''}
      </ReactMarkdown>
    </article>
  )
}
