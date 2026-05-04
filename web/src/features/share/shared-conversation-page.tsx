'use client'

import { useSearchParams } from 'next/navigation'
import { useEffect, useState } from 'react'

import { ChatPanel } from '@/features/chat/components/chat-panel'
import { useI18n } from '@/features/i18n/i18n-provider'
import { SharedConversationFilesPanel } from '@/features/share/shared-conversation-files-panel'
import { fetchPublicSharedConversation } from '@/lib/api'
import { getSandboxErrorDisplay, isSandboxErrorCode } from '@/lib/sandbox'
import type { PublicSharedConversation } from '@/lib/types'

export function SharedConversationPage() {
  const { t } = useI18n()
  const searchParams = useSearchParams()
  const token = searchParams.get('token')?.trim() || ''
  const [conversation, setConversation] = useState<PublicSharedConversation | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!token) {
      setConversation(null)
      setError(t('share.not_found'))
      setIsLoading(false)
      return
    }

    let cancelled = false

    const load = async () => {
      setIsLoading(true)
      setError(null)
      try {
        const nextConversation = await fetchPublicSharedConversation(token)
        if (!cancelled) {
          setConversation(nextConversation)
        }
      } catch (err) {
        if (!cancelled) {
          setConversation(null)
          setError(err instanceof Error ? err.message : t('share.load_failed'))
        }
      } finally {
        if (!cancelled) {
          setIsLoading(false)
        }
      }
    }

    void load()

    return () => {
      cancelled = true
    }
  }, [t, token])

  if (isLoading) {
    return (
      <div className="flex h-screen w-screen items-center justify-center bg-background text-sm text-muted-foreground">
        {t('share.loading')}
      </div>
    )
  }

  if (error || !conversation) {
    const errorValue = error || t('share.not_found')
    const sandboxError = isSandboxErrorCode(errorValue) ? getSandboxErrorDisplay(errorValue) : null

    return (
      <div className="flex h-screen w-screen items-center justify-center bg-background px-6">
        {sandboxError ? (
          <div className="w-full max-w-[640px] rounded-[28px] border border-border bg-card p-8 text-center shadow-panel">
            <div className="text-lg font-semibold text-foreground">{sandboxError.title}</div>
            <p className="mt-3 text-sm leading-6 text-muted-foreground">
              {sandboxError.description}
            </p>
            <div className="mt-5 font-mono text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
              Error: {errorValue}
            </div>
          </div>
        ) : (
          <div className="text-center text-sm text-muted-foreground">{errorValue}</div>
        )}
      </div>
    )
  }

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-background">
      <main className="flex min-w-0 flex-1 overflow-hidden bg-background">
        <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden bg-background">
          <ChatPanel
            agents={[]}
            commands={[]}
            currentAgent=""
            currentMessages={conversation.messages}
            currentSessionId={conversation.id}
            currentWorkspace=""
            isSending={false}
            onCancel={async () => false}
            onConfirmPermission={() => {}}
            onSend={async () => {}}
            pendingPermission={null}
            readonly
            shareToken={token}
            streamItems={[]}
          />
        </div>
        <SharedConversationFilesPanel files={conversation.files || []} shareToken={token} />
      </main>
    </div>
  )
}
