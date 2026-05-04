'use client'

import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { ConversationFilePreviewDialog } from '@/features/chat/components/conversation-file-preview-dialog'
import { ToolCallItem } from '@/features/chat/components/tool-call-item'
import { formatFileSize } from '@/lib/utils'
import type { Message, MessageFile } from '@/lib/types'

export function ChatMessage({
  message,
  hideAgentTag = false,
  currentWorkspace,
  shareToken,
}: {
  message: Message
  hideAgentTag?: boolean
  currentWorkspace?: string
  shareToken?: string
}) {
  const [selectedFile, setSelectedFile] = useState<MessageFile | null>(null)
  const canPreviewFiles = Boolean(shareToken || currentWorkspace)

  if (message.toolCall) {
    return <ToolCallItem tool={message.toolCall} />
  }

  if (message.isError) {
    return (
      <div className="my-2 flex items-start gap-3 rounded-md border border-destructive bg-destructive/10 px-3 py-3 text-sm text-destructive">
        <div className="text-base leading-none">⚠</div>
        <div className="font-mono leading-6">{message.content}</div>
      </div>
    )
  }

  if (message.role === 'user') {
    return (
      <>
        <div className="my-4 ml-auto max-w-[80%] rounded-lg rounded-br-[2px] border border-[rgb(var(--color-bg-hover))] bg-muted px-3 py-2 text-sm text-foreground">
          {message.files?.length ? (
            <div className="mb-2 flex flex-wrap gap-1.5">
              {message.files.map((file) =>
                canPreviewFiles ? (
                  <button
                    className="flex items-center gap-1.5 rounded-sm border border-border bg-card px-2 py-1 text-xs transition hover:bg-accent hover:text-foreground"
                    key={file.path}
                    onClick={() => setSelectedFile(file)}
                    type="button"
                  >
                    <span className="text-[rgb(var(--color-text-primary))]">{file.name}</span>
                    <span className="text-muted-foreground">{formatFileSize(file.size)}</span>
                  </button>
                ) : (
                  <div
                    className="flex items-center gap-1.5 rounded-sm border border-border bg-card px-2 py-1 text-xs"
                    key={file.path}
                  >
                    <span className="text-[rgb(var(--color-text-primary))]">{file.name}</span>
                    <span className="text-muted-foreground">{formatFileSize(file.size)}</span>
                  </div>
                )
              )}
            </div>
          ) : null}
          <div className="markdown">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{message.content}</ReactMarkdown>
          </div>
        </div>
        <ConversationFilePreviewDialog
          file={selectedFile}
          onOpenChange={(open) => {
            if (!open) {
              setSelectedFile(null)
            }
          }}
          open={Boolean(selectedFile)}
          shareToken={shareToken}
          workspaceId={shareToken ? undefined : currentWorkspace}
        />
      </>
    )
  }

  return (
    <div className="mb-1">
      {message.agent && !hideAgentTag ? (
        <div className="mb-1 inline-block text-[10px] font-bold uppercase tracking-[0.1em] text-muted-foreground">
          {message.agent}
        </div>
      ) : null}
      <div className="markdown">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{message.content}</ReactMarkdown>
      </div>
    </div>
  )
}
