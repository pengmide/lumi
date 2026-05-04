'use client'

import { FileText, PanelRightClose, PanelRightOpen } from 'lucide-react'
import { useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { ConversationFilePreviewDialog } from '@/features/chat/components/conversation-file-preview-dialog'
import { useI18n } from '@/features/i18n/i18n-provider'
import { formatFileSize } from '@/lib/utils'
import type { MessageFile } from '@/lib/types'

export function SharedConversationFilesPanel({
  files,
  shareToken,
}: {
  files: MessageFile[]
  shareToken: string
}) {
  const { t } = useI18n()
  const [collapsed, setCollapsed] = useState(false)
  const [selectedFile, setSelectedFile] = useState<MessageFile | null>(null)

  const sortedFiles = useMemo(
    () =>
      [...files].sort((a, b) =>
        a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
      ),
    [files]
  )

  if (!sortedFiles.length) {
    return null
  }

  if (collapsed) {
    return (
      <>
        <div className="flex h-full flex-shrink-0 items-start px-3 pt-5">
          <Button
            className="rounded-[12px] border-border bg-card text-foreground shadow-panel"
            onClick={() => setCollapsed(false)}
            size="icon"
            title={t('share.files.open')}
            type="button"
            variant="outline"
          >
            <PanelRightOpen className="h-4 w-4" />
          </Button>
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
        />
      </>
    )
  }

  return (
    <>
      <div className="group relative my-3 mr-3 flex h-[calc(100%-24px)] w-[320px] min-w-[280px] flex-shrink-0">
        <section className="flex h-full min-h-0 w-full flex-col overflow-hidden rounded-[22px] border border-border bg-card shadow-panel">
          <div className="border-b border-border bg-background/60 px-4 py-4">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-[14px] border border-border bg-background text-sky-300">
                    <FileText className="h-5 w-5" />
                  </div>
                  <div className="min-w-0">
                    <div className="truncate text-sm font-semibold text-foreground">
                      {t('share.files.title')}
                    </div>
                    <div className="mt-1 text-[11px] uppercase tracking-[0.18em] text-muted-foreground">
                      {sortedFiles.length} {t('share.files.count')}
                    </div>
                  </div>
                </div>
              </div>
              <Button
                className="rounded-[10px]"
                onClick={() => setCollapsed(true)}
                size="icon"
                title={t('share.files.close')}
                type="button"
                variant="ghost"
              >
                <PanelRightClose className="h-4 w-4" />
              </Button>
            </div>
          </div>

          <div className="min-h-0 flex-1 bg-[linear-gradient(180deg,rgba(15,15,17,0.96),rgba(10,10,11,0.98))]">
            <ScrollArea className="h-full px-3 py-3">
              <div className="space-y-2">
                {sortedFiles.map((file) => (
                  <button
                    className="w-full rounded-[16px] border border-border bg-background/55 p-4 text-left transition hover:border-border/80 hover:bg-background"
                    key={file.path}
                    onClick={() => setSelectedFile(file)}
                    type="button"
                  >
                    <div className="truncate font-mono text-[12px] text-foreground">
                      {file.name}
                    </div>
                    <div className="mt-2 text-sm text-muted-foreground">
                      {formatFileSize(file.size)}
                    </div>
                  </button>
                ))}
              </div>
            </ScrollArea>
          </div>
        </section>
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
      />
    </>
  )
}
