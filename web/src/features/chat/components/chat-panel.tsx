'use client'

import { useEffect, useMemo, useRef } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { SandboxWorkspaceAlert } from '@/components/sandbox-workspace-alert'
import { ChatComposer } from '@/features/chat/components/chat-composer'
import { ChatMessage } from '@/features/chat/components/chat-message'
import { PermissionRequestCard } from '@/features/chat/components/permission-request-card'
import { ToolCallItem } from '@/features/chat/components/tool-call-item'
import { isWorkspaceInteractionBlocked } from '@/lib/sandbox'
import type {
  Agent,
  Message,
  MessageFile,
  PermissionRequest,
  SlashCommand,
  StreamItem,
  Workspace,
} from '@/lib/types'
import { useI18n } from '@/features/i18n/i18n-provider'

function buildVisibleMessages(messages: Message[]) {
  const result: Array<{ message: Message; hideAgentTag: boolean }> = []
  let lastAgent: string | null = null
  let lastRole: string | null = null

  messages.forEach((message) => {
    const isVisible =
      Boolean(message.content) || Boolean(message.toolCall) || Boolean(message.isError) || message.role !== 'assistant'
    if (!isVisible) return

    let hideAgentTag = false
    if (message.role === 'assistant') {
      if (lastRole === 'assistant' && lastAgent === message.agent) {
        hideAgentTag = true
      }
      lastRole = 'assistant'
      lastAgent = message.agent || null
    } else {
      lastRole = message.role
      lastAgent = null
    }

    result.push({ message, hideAgentTag })
  })

  return result
}

export function ChatPanel({
  agents,
  commands,
  currentAgent,
  currentMessages,
  currentSessionId,
  currentWorkspace,
  currentWorkspaceInfo,
  readonly = false,
  shareToken,
  isSending,
  pendingPermission,
  streamItems,
  onCancel,
  onConfirmPermission,
  onRetryWorkspaceAccess,
  onSend,
  onWorkspaceFilesChanged,
}: {
  agents: Agent[]
  commands: SlashCommand[]
  currentAgent: string
  currentMessages: Message[]
  currentSessionId: string | null
  currentWorkspace: string
  currentWorkspaceInfo?: Workspace | null
  readonly?: boolean
  shareToken?: string
  isSending: boolean
  pendingPermission: PermissionRequest | null
  streamItems: StreamItem[]
  onCancel: () => Promise<boolean>
  onConfirmPermission: () => void
  onRetryWorkspaceAccess?: () => void
  onSend: (message: string, files: MessageFile[]) => Promise<void>
  onWorkspaceFilesChanged?: () => void
}) {
  const { t } = useI18n()
  const viewportRef = useRef<HTMLDivElement | null>(null)
  const visibleMessages = useMemo(() => buildVisibleMessages(currentMessages), [currentMessages])
  const isWorkspaceBlocked = isWorkspaceInteractionBlocked(currentWorkspaceInfo)

  useEffect(() => {
    const viewport = viewportRef.current
    if (!viewport) return
    viewport.scrollTop = viewport.scrollHeight
  }, [currentMessages, pendingPermission, streamItems])

  return (
    <div className="flex h-full flex-col">
      <div className="legacy-hidden-scrollbar flex-1 overflow-y-auto" ref={viewportRef}>
        <div className="mx-auto flex w-full max-w-[800px] flex-col gap-0 px-5 pb-5 pt-10">
          {!currentSessionId || currentMessages.length === 0 ? (
            <div className="px-5 py-20 text-center text-sm text-muted-foreground">
              {!currentWorkspace ? (
                <p>{t('welcome.select_workspace')}</p>
              ) : isWorkspaceBlocked ? (
                <div className="mx-auto max-w-[720px]">
                  <SandboxWorkspaceAlert
                    className="text-left"
                    compact={false}
                    onRetry={onRetryWorkspaceAccess}
                    workspace={currentWorkspaceInfo}
                  />
                </div>
              ) : (
                <div className="space-y-3">
                  <p className="text-sm text-foreground">{t('welcome.start')}</p>
                  <p>
                    {t('welcome.mention')}:{' '}
                    {agents.map((agent) => (
                      <code className="mr-2" key={agent.id}>
                        @{agent.id}
                      </code>
                    ))}
                  </p>
                </div>
              )}
            </div>
          ) : null}

          {visibleMessages.map((item, index) => (
            <ChatMessage
              currentWorkspace={readonly ? undefined : currentWorkspace}
              hideAgentTag={item.hideAgentTag}
              key={`${index}-${item.message.content}`}
              message={item.message}
              shareToken={shareToken}
            />
          ))}

          {streamItems.map((item, index) =>
            item.type === 'tool' ? (
              <ToolCallItem key={`stream-tool-${index}`} tool={item.data} />
            ) : (
              <div className="mb-1" key={`stream-text-${index}`}>
                <div className="mb-1 inline-block text-[10px] font-bold uppercase tracking-[0.1em] text-muted-foreground">
                  {currentAgent}
                </div>
                <div className="markdown">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{item.data}</ReactMarkdown>
                </div>
              </div>
            )
          )}

          {pendingPermission && !readonly ? (
            <PermissionRequestCard
              agentId={currentAgent}
              onConfirmed={onConfirmPermission}
              request={pendingPermission}
            />
          ) : null}

          {isSending && !pendingPermission && !readonly ? (
            <div className="flex justify-start py-4">
              <div className="legacy-loading-dots" aria-label="Loading">
                <span />
                <span />
                <span />
              </div>
            </div>
          ) : null}
        </div>
      </div>

      {!readonly ? (
        <div className="w-full px-10 pb-10">
          <div className="mx-auto w-full max-w-[900px]">
            {isWorkspaceBlocked ? (
              <SandboxWorkspaceAlert
                className="mb-4"
                compact
                onRetry={onRetryWorkspaceAccess}
                workspace={currentWorkspaceInfo}
              />
            ) : null}
            <ChatComposer
              agents={agents}
              commands={commands}
              currentAgent={currentAgent}
              currentWorkspace={currentWorkspace}
              disabled={isSending || !currentWorkspace || isWorkspaceBlocked}
              isSending={isSending}
              onCancel={() => {
                void onCancel()
              }}
              onSend={onSend}
              onWorkspaceFilesChanged={onWorkspaceFilesChanged}
            />
          </div>
        </div>
      ) : null}
    </div>
  )
}
