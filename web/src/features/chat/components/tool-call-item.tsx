'use client'

import { useState } from 'react'

import { cn } from '@/lib/utils'
import type { ToolCall } from '@/lib/types'

function formatRawInput(value?: string) {
  if (!value || value === '{}') return ''

  try {
    const parsed = JSON.parse(value)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return value
  }
}

export function ToolCallItem({ tool }: { tool: ToolCall }) {
  const [expanded, setExpanded] = useState(false)
  const displayTitle =
    tool.title && !tool.title.startsWith('toolu_')
      ? tool.title
      : tool.input && tool.input.length > 80
        ? `${tool.input.slice(0, 80)}...`
        : tool.input || ''
  const showBranch = expanded || (tool.error && tool.status === 'error') || (tool.output && tool.status === 'completed')

  return (
    <div className="relative my-0.5 pl-[14px] font-mono text-[13px]">
      {showBranch ? <span className="absolute left-[5px] top-[18px] bottom-0 w-px bg-border" /> : null}
      <div>
        <button
          className="relative flex w-full items-baseline gap-1.5 overflow-hidden pb-0.5 text-left hover:opacity-80"
          onClick={() => setExpanded((current) => !current)}
          type="button"
        >
          <span
            className={cn(
              'absolute left-[-14px] top-[5px] h-2.5 w-2.5 rounded-full',
              tool.status === 'completed' && 'bg-[#5cb85c]',
              tool.status === 'pending' && 'animate-pulse bg-[rgb(var(--color-text-tertiary))]',
              tool.status === 'error' && 'bg-[#d9534f]'
            )}
          />
          <span className="shrink-0 font-bold text-foreground">{tool.toolName}</span>
          {displayTitle ? (
            <span className="min-w-0 truncate text-xs text-[rgb(var(--color-text-secondary))]">{displayTitle}</span>
          ) : null}
          <span className="ml-auto shrink-0 text-[8px] text-muted-foreground">{expanded ? '▼' : '▶'}</span>
        </button>

        {tool.description ? (
          <div className="mt-0.5 text-[11px] italic text-muted-foreground">{tool.description}</div>
        ) : null}

        {expanded ? (
          <div className="mt-1 rounded-sm border border-border bg-card p-2 text-xs text-[rgb(var(--color-text-secondary))]">
            {tool.input ? (
              <div className="mb-1.5">
                <div className="mb-1 text-[10px] uppercase text-muted-foreground">Input:</div>
                <pre className="whitespace-pre-wrap break-all text-[11px]">
                  {tool.input}
                </pre>
              </div>
            ) : null}

            {tool.rawInput && tool.rawInput !== '{}' ? (
              <div>
                <div className="mb-1 text-[10px] uppercase text-muted-foreground">Raw:</div>
                <pre className="whitespace-pre-wrap break-all text-[11px]">
                  {formatRawInput(tool.rawInput)}
                </pre>
              </div>
            ) : null}
          </div>
        ) : null}

        {tool.error && tool.status === 'error' ? (
          <div className="relative ml-2 mt-1 pl-5 text-xs text-[#ff8888]">
            <span className="absolute left-[-9px] top-[0.9em] h-px w-4 bg-border" />
            <pre className="whitespace-pre-wrap break-all">{tool.error}</pre>
          </div>
        ) : null}

        {tool.output && tool.status === 'completed' ? (
          <div className="relative ml-2 mt-1 pl-5 text-xs text-muted-foreground">
            <span className="absolute left-[-9px] top-[0.9em] h-px w-4 bg-border" />
            <pre className="legacy-hidden-scrollbar max-h-[300px] overflow-y-auto whitespace-pre-wrap break-all">
              {tool.output}
            </pre>
          </div>
        ) : null}
      </div>
    </div>
  )
}
