'use client'

import SyntaxHighlighter from 'react-syntax-highlighter'
import { vs, vs2015 } from 'react-syntax-highlighter/dist/esm/styles/hljs'

import { ScrollArea } from '@/components/ui/scroll-area'
import { useTheme } from '@/features/theme/theme-provider'
import { normalizeHighlighterLanguage } from '@/features/workspace-preview/language'

const LARGE_CONTENT_CHARACTER_THRESHOLD = 150_000
const LARGE_CONTENT_LINE_THRESHOLD = 2_500

function PlainTextFallback({ lines }: { lines: string[] }) {
  return (
    <div className="pt-3">
      {lines.map((line, index) => (
        <div className="grid grid-cols-[auto_1fr] gap-4 py-0.5 font-mono text-[12px] leading-6" key={`${line}-${index}`}>
          <span className="select-none text-right text-[11px] text-muted-foreground">{index + 1}</span>
          <span className="overflow-x-auto whitespace-pre text-foreground">{line || ' '}</span>
        </div>
      ))}
    </div>
  )
}

export function CodePreview({ content, language }: { content?: string; language?: string }) {
  const { currentTheme } = useTheme()
  const source = content || ''
  const lines = source.split('\n')
  const normalizedLanguage = normalizeHighlighterLanguage(language)
  const isLargeContent =
    source.length > LARGE_CONTENT_CHARACTER_THRESHOLD || lines.length > LARGE_CONTENT_LINE_THRESHOLD

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden bg-transparent">
      <ScrollArea className="min-h-0 flex-1">
        {isLargeContent ? (
          <PlainTextFallback lines={lines} />
        ) : (
          <SyntaxHighlighter
            PreTag="div"
            codeTagProps={{ style: { fontFamily: 'var(--font-mono)' } }}
            customStyle={{
              background: 'transparent',
              borderRadius: 0,
              fontSize: '12px',
              lineHeight: '1.5',
              margin: 0,
              minHeight: '100%',
              padding: '12px 0 0',
            }}
            language={normalizedLanguage}
            lineNumberStyle={{
              color: currentTheme === 'dark' ? '#71717a' : '#a1a1aa',
              minWidth: '2.5rem',
              paddingRight: '1rem',
              textAlign: 'right',
              userSelect: 'none',
            }}
            showLineNumbers
            style={currentTheme === 'dark' ? vs2015 : vs}
            wrapLongLines={normalizedLanguage === 'plaintext'}
          >
            {source}
          </SyntaxHighlighter>
        )}
      </ScrollArea>
    </div>
  )
}
