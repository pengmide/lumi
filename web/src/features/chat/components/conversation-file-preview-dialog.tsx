'use client'

import { useEffect, useMemo, useState } from 'react'

import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useI18n } from '@/features/i18n/i18n-provider'
import { CodePreview } from '@/features/workspace-preview/viewers/code-preview'
import { inferPreviewLanguage } from '@/features/workspace-preview/language'
import { MarkdownPreview } from '@/features/workspace-preview/viewers/markdown-preview'
import {
  buildPublicSharedFileBufferUrl,
  buildPublicSharedHTMLPreviewUrl,
  buildWorkspaceFileBufferUrl,
  buildWorkspaceHTMLPreviewUrl,
  fetchPublicSharedFileMeta,
  fetchPublicSharedTextFile,
  fetchWorkspaceFileMeta,
  fetchWorkspaceTextFile,
} from '@/lib/api'
import { getSandboxErrorDisplay, isSandboxErrorCode } from '@/lib/sandbox'
import type { MessageFile, WorkspaceFileMeta } from '@/lib/types'

type LoadedPreview =
  | {
      kind: 'code' | 'markdown'
      content: string
      meta: WorkspaceFileMeta
    }
  | {
      kind: 'image' | 'pdf' | 'html'
      meta: WorkspaceFileMeta
      src: string
    }
  | {
      kind: 'unsupported'
      meta: WorkspaceFileMeta
    }

async function loadWorkspacePreview(
  workspaceId: string,
  file: MessageFile,
): Promise<LoadedPreview> {
  const meta = await fetchWorkspaceFileMeta(workspaceId, file.path)

  if (meta.previewKind === 'code' || meta.previewKind === 'markdown') {
    const textFile = await fetchWorkspaceTextFile(workspaceId, file.path)
    return {
      kind: meta.previewKind,
      content: textFile.content,
      meta,
    }
  }

  if (meta.previewKind === 'html') {
    return {
      kind: 'html',
      meta,
      src: buildWorkspaceHTMLPreviewUrl(workspaceId, file.path),
    }
  }

  if (meta.previewKind === 'image' || meta.previewKind === 'pdf') {
    return {
      kind: meta.previewKind,
      meta,
      src: buildWorkspaceFileBufferUrl(workspaceId, file.path),
    }
  }

  return {
    kind: 'unsupported',
    meta,
  }
}

async function loadSharedPreview(
  shareToken: string,
  file: MessageFile,
): Promise<LoadedPreview> {
  const meta = await fetchPublicSharedFileMeta(shareToken, file.path)

  if (meta.previewKind === 'code' || meta.previewKind === 'markdown') {
    const textFile = await fetchPublicSharedTextFile(shareToken, file.path)
    return {
      kind: meta.previewKind,
      content: textFile.content,
      meta,
    }
  }

  if (meta.previewKind === 'html') {
    return {
      kind: 'html',
      meta,
      src: buildPublicSharedHTMLPreviewUrl(shareToken, file.path),
    }
  }

  if (meta.previewKind === 'image' || meta.previewKind === 'pdf') {
    return {
      kind: meta.previewKind,
      meta,
      src: buildPublicSharedFileBufferUrl(shareToken, file.path),
    }
  }

  return {
    kind: 'unsupported',
    meta,
  }
}

function PreviewBody({
  preview,
  shareToken,
  workspaceId,
}: {
  preview: LoadedPreview
  shareToken?: string
  workspaceId?: string
}) {
  const { t } = useI18n()

  if (preview.kind === 'code') {
    return (
      <div className="h-full px-4 py-4">
        <CodePreview content={preview.content} language={inferPreviewLanguage(preview.meta.name)} />
      </div>
    )
  }

  if (preview.kind === 'markdown') {
    return (
      <ScrollArea className="h-full">
        <div className="mx-auto max-w-[860px] px-6 py-6">
          <MarkdownPreview
            content={preview.content}
            filePath={preview.meta.path}
            workspaceId={!shareToken ? workspaceId : undefined}
          />
        </div>
      </ScrollArea>
    )
  }

  if (preview.kind === 'image') {
    return (
      <div className="flex h-full items-center justify-center p-4">
        <img
          alt={preview.meta.name}
          className="max-h-full max-w-full rounded-[18px] border border-border bg-card object-contain"
          src={preview.src}
        />
      </div>
    )
  }

  if (preview.kind === 'html' || preview.kind === 'pdf') {
    return (
      <div className="h-full p-4">
        <iframe
          className="h-full w-full rounded-[18px] border border-border bg-white"
          referrerPolicy="no-referrer"
          sandbox={preview.kind === 'html' ? 'allow-same-origin allow-scripts' : undefined}
          src={preview.src}
          title={preview.meta.name}
        />
      </div>
    )
  }

  return (
    <div className="flex h-full items-center justify-center px-6 text-center text-sm text-muted-foreground">
      {t('share.preview.unsupported')}
    </div>
  )
}

function PreviewErrorBody({ error }: { error: string }) {
  const sandboxError = isSandboxErrorCode(error)
    ? getSandboxErrorDisplay(error)
    : null

  if (sandboxError) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="w-full max-w-[560px] rounded-[22px] border border-rose-500/30 bg-rose-500/10 p-6 text-center text-rose-100">
          <div className="text-base font-semibold">{sandboxError.title}</div>
          <div className="mt-3 text-sm leading-6 opacity-90">
            {sandboxError.description}
          </div>
          <div className="mt-4 font-mono text-[11px] uppercase tracking-[0.12em] opacity-75">
            {error}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full items-center justify-center px-6 text-center text-sm text-muted-foreground">
      {error}
    </div>
  )
}

export function ConversationFilePreviewDialog({
  file,
  open,
  onOpenChange,
  shareToken,
  workspaceId,
}: {
  file: MessageFile | null
  open: boolean
  onOpenChange: (open: boolean) => void
  shareToken?: string
  workspaceId?: string
}) {
  const { t } = useI18n()
  const [preview, setPreview] = useState<LoadedPreview | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open || !file) {
      setPreview(null)
      setError(null)
      setIsLoading(false)
      return
    }

    let cancelled = false

    const load = async () => {
      setIsLoading(true)
      setError(null)
      try {
        const nextPreview =
          shareToken && !workspaceId
            ? await loadSharedPreview(shareToken, file)
            : workspaceId
              ? await loadWorkspacePreview(workspaceId, file)
              : null

        if (!cancelled) {
          if (!nextPreview) {
            setError(t('share.preview.unavailable'))
            return
          }
          setPreview(nextPreview)
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : t('share.preview.failed'))
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
  }, [file, open, shareToken, t, workspaceId])

  const title = useMemo(() => file?.name || t('share.preview.title'), [file?.name, t])

  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent className="flex h-[min(88vh,840px)] w-[min(92vw,1080px)] flex-col overflow-hidden p-0">
        <DialogHeader className="border-b border-border px-6 py-4 pr-14">
          <DialogTitle className="truncate text-base normal-case tracking-normal">{title}</DialogTitle>
        </DialogHeader>
        <div className="min-h-0 flex-1 bg-[linear-gradient(180deg,rgba(12,12,13,0.85),rgba(8,8,8,0.96))]">
          {isLoading ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              {t('share.preview.loading')}
            </div>
          ) : error ? (
            <PreviewErrorBody error={error} />
          ) : preview ? (
            <PreviewBody preview={preview} shareToken={shareToken} workspaceId={workspaceId} />
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  )
}
