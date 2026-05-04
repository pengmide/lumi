'use client'

import {
  ChevronDown,
  ChevronRight,
  FileText,
  Folder,
  FolderOpen,
  Share2,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import { SandboxWorkspaceAlert } from '@/components/sandbox-workspace-alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Switch } from '@/components/ui/switch'
import { useI18n } from '@/features/i18n/i18n-provider'
import { isWorkspaceInteractionBlocked } from '@/lib/sandbox'
import {
  buildPublicShareUrl,
  createConversationShare,
  fetchConversationShare,
  fetchWorkspaceTree,
  revokeConversationShare,
} from '@/lib/api'
import type { ConversationShare, Workspace, WorkspaceTreeEntry } from '@/lib/types'

function collectRootFolderPaths(nodes: WorkspaceTreeEntry[]) {
  return nodes.filter((node) => node.isDir).map((node) => node.path)
}

function collectFilePaths(nodes: WorkspaceTreeEntry[], paths = new Set<string>()) {
  for (const node of nodes) {
    if (node.isDir) {
      collectFilePaths(node.children || [], paths)
    } else {
      paths.add(node.path)
    }
  }

  return paths
}

function countFiles(nodes: WorkspaceTreeEntry[]): number {
  return nodes.reduce((count, node) => {
    if (!node.isDir) {
      return count + 1
    }
    return count + countFiles(node.children || [])
  }, 0)
}

function selectionSignature(paths: Iterable<string>) {
  return Array.from(paths).sort().join('\n')
}

function ShareTreeNode({
  node,
  depth,
  expandedPaths,
  selectedPaths,
  onToggleFile,
  onToggleFolder,
}: {
  node: WorkspaceTreeEntry
  depth: number
  expandedPaths: Set<string>
  selectedPaths: Set<string>
  onToggleFile: (path: string) => void
  onToggleFolder: (path: string) => void
}) {
  const isExpanded = expandedPaths.has(node.path)
  const paddingLeft = 12 + depth * 16

  if (node.isDir) {
    const FolderIcon = isExpanded ? FolderOpen : Folder
    const ChevronIcon = isExpanded ? ChevronDown : ChevronRight

    return (
      <div>
        <button
          className="flex h-8 w-full items-center gap-2 rounded-sm pr-3 text-left text-sm text-muted-foreground transition hover:bg-accent hover:text-foreground"
          onClick={() => onToggleFolder(node.path)}
          style={{ paddingLeft }}
          type="button"
        >
          <ChevronIcon className="h-3.5 w-3.5 flex-shrink-0" />
          <FolderIcon className="h-4 w-4 flex-shrink-0 text-sky-300" />
          <span className="truncate">{node.name}</span>
        </button>
        {isExpanded ? (
          <div>
            {(node.children || []).map((child) => (
              <ShareTreeNode
                depth={depth + 1}
                expandedPaths={expandedPaths}
                key={child.path}
                node={child}
                onToggleFile={onToggleFile}
                onToggleFolder={onToggleFolder}
                selectedPaths={selectedPaths}
              />
            ))}
          </div>
        ) : null}
      </div>
    )
  }

  return (
    <label
      className="flex h-8 cursor-pointer items-center gap-2 rounded-sm pr-3 text-sm text-foreground transition hover:bg-accent"
      style={{ paddingLeft }}
    >
      <input
        checked={selectedPaths.has(node.path)}
        className="h-3.5 w-3.5 flex-shrink-0 accent-primary"
        onChange={() => onToggleFile(node.path)}
        type="checkbox"
      />
      <FileText className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
      <span className="truncate font-mono text-[12px]">{node.path}</span>
    </label>
  )
}

export function ShareButton({
  conversationId,
  currentWorkspace,
  workspace,
  onRetryWorkspaceAccess,
}: {
  conversationId: string
  currentWorkspace: string
  workspace: Workspace | null
  onRetryWorkspaceAccess?: () => void
}) {
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  const [share, setShare] = useState<ConversationShare | null>(null)
  const [tree, setTree] = useState<WorkspaceTreeEntry[]>([])
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set())
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set())
  const [isLoading, setIsLoading] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    setShare(null)
    setCopied(false)
    setSelectedPaths(new Set())
  }, [conversationId])

  useEffect(() => {
    if (!open) {
      return
    }

    let cancelled = false

    const loadShareState = async () => {
      setIsLoading(true)
      setError(null)
      try {
        const nextShare = await fetchConversationShare(conversationId)

        if (cancelled) {
          return
        }

        setShare(nextShare)
        if (!nextShare) {
          setTree([])
          setExpandedPaths(new Set())
          setSelectedPaths(new Set())
          return
        }

        try {
          const nextTree = currentWorkspace ? await fetchWorkspaceTree(currentWorkspace) : []
          if (cancelled) {
            return
          }

          setTree(nextTree)
          setExpandedPaths(new Set(collectRootFolderPaths(nextTree)))
          const availableFilePaths = collectFilePaths(nextTree)
          const selectedSharePaths = nextShare.files
            .map((file) => file.path)
            .filter((path) => availableFilePaths.has(path))
          setSelectedPaths(new Set(selectedSharePaths))
        } catch (treeErr) {
          if (!cancelled) {
            setTree([])
            setExpandedPaths(new Set())
            setSelectedPaths(new Set())
            setError(treeErr instanceof Error ? treeErr.message : t('share.files.load_failed'))
          }
        }
      } catch (err) {
        if (!cancelled) {
          setShare(null)
          setTree([])
          setExpandedPaths(new Set())
          setSelectedPaths(new Set())
          setError(err instanceof Error ? err.message : t('share.load_failed'))
        }
      } finally {
        if (!cancelled) {
          setIsLoading(false)
        }
      }
    }

    void loadShareState()

    return () => {
      cancelled = true
    }
  }, [conversationId, currentWorkspace, open, t])

  const shareUrl = useMemo(() => {
    if (!share) {
      return ''
    }
    return buildPublicShareUrl(share.token)
  }, [share])

  const visibleFileCount = useMemo(() => countFiles(tree), [tree])
  const shareSelectionSignature = useMemo(
    () => selectionSignature((share?.files || []).map((file) => file.path)),
    [share]
  )
  const selectedSignature = useMemo(() => selectionSignature(selectedPaths), [selectedPaths])
  const hasSelectionChanges = Boolean(share) && shareSelectionSignature !== selectedSignature
  const isWorkspaceBlocked = isWorkspaceInteractionBlocked(workspace)

  const saveShare = async () => {
    setIsSaving(true)
    setCopied(false)
    setError(null)
    try {
      const files = Array.from(selectedPaths)
        .sort()
        .map((path) => ({ path }))
      const nextShare = await createConversationShare(conversationId, files)
      setShare(nextShare)
      setSelectedPaths(new Set((nextShare.files || []).map((file) => file.path)))
    } catch (err) {
      setError(err instanceof Error ? err.message : t('share.load_failed'))
    } finally {
      setIsSaving(false)
    }
  }

  const handleToggle = async (checked: boolean) => {
    if (checked) {
      setIsSaving(true)
      setCopied(false)
      setError(null)
      try {
        const nextShare = await createConversationShare(conversationId, [])
        setShare(nextShare)
        setSelectedPaths(new Set())

        try {
          const nextTree = currentWorkspace ? await fetchWorkspaceTree(currentWorkspace) : []
          setTree(nextTree)
          setExpandedPaths(new Set(collectRootFolderPaths(nextTree)))
        } catch (treeErr) {
          setTree([])
          setExpandedPaths(new Set())
          setError(treeErr instanceof Error ? treeErr.message : t('share.files.load_failed'))
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : t('share.load_failed'))
      } finally {
        setIsSaving(false)
      }
      return
    }

    setIsSaving(true)
    setCopied(false)
    setError(null)
    try {
      await revokeConversationShare(conversationId)
      setShare(null)
      setTree([])
      setExpandedPaths(new Set())
      setSelectedPaths(new Set())
    } catch (err) {
      setError(err instanceof Error ? err.message : t('share.load_failed'))
    } finally {
      setIsSaving(false)
    }
  }

  const handleCopy = async () => {
    if (!shareUrl) {
      return
    }

    try {
      await navigator.clipboard.writeText(shareUrl)
      setCopied(true)
      window.setTimeout(() => {
        setCopied(false)
      }, 1500)
    } catch {
      setCopied(false)
    }
  }

  const toggleFile = (path: string) => {
    setSelectedPaths((current) => {
      const next = new Set(current)
      if (next.has(path)) {
        next.delete(path)
      } else {
        next.add(path)
      }
      return next
    })
  }

  const toggleFolder = (path: string) => {
    setExpandedPaths((current) => {
      const next = new Set(current)
      if (next.has(path)) {
        next.delete(path)
      } else {
        next.add(path)
      }
      return next
    })
  }

  return (
    <>
      <Button
        aria-label={t('share.title')}
        className="h-8 w-8 rounded-sm bg-transparent text-[rgb(var(--color-text-tertiary))] hover:bg-accent hover:text-foreground"
        onClick={() => setOpen(true)}
        size="icon"
        type="button"
        variant="ghost"
      >
        <Share2 className="h-4 w-4" />
      </Button>
      <Dialog onOpenChange={setOpen} open={open}>
        <DialogContent className="flex max-h-[88vh] w-[min(92vw,760px)] flex-col overflow-hidden">
          <DialogHeader>
            <DialogTitle className="normal-case tracking-normal">{t('share.title')}</DialogTitle>
          </DialogHeader>
          <div className="flex min-h-0 flex-1 flex-col gap-4">
            <div className="flex items-center justify-end">
              <Switch
                aria-label={t('share.toggle')}
                checked={Boolean(share)}
                disabled={isLoading || isSaving}
                onCheckedChange={(checked) => {
                  void handleToggle(checked)
                }}
              />
            </div>

            {share ? (
              <div className="min-h-0 rounded-md border border-border bg-background/50">
                <ScrollArea className="h-[min(42vh,360px)]">
                  {isLoading ? (
                    <div className="flex h-[180px] items-center justify-center text-sm text-muted-foreground">
                      {t('share.loading')}
                    </div>
                  ) : isWorkspaceBlocked ? (
                    <div className="p-3">
                      <SandboxWorkspaceAlert
                        compact
                        onRetry={onRetryWorkspaceAccess}
                        workspace={workspace}
                      />
                    </div>
                  ) : visibleFileCount > 0 ? (
                    <div className="p-2">
                      {tree.map((node) => (
                        <ShareTreeNode
                          depth={0}
                          expandedPaths={expandedPaths}
                          key={node.path}
                          node={node}
                          onToggleFile={toggleFile}
                          onToggleFolder={toggleFolder}
                          selectedPaths={selectedPaths}
                        />
                      ))}
                    </div>
                  ) : (
                    <div className="flex h-[180px] items-center justify-center px-6 text-center text-sm text-muted-foreground">
                      {t('share.files.empty')}
                    </div>
                  )}
                </ScrollArea>
              </div>
            ) : null}

            {error ? <div className="text-sm text-destructive">{error}</div> : null}

            {share ? (
              <div className="flex items-center gap-2">
                <Input readOnly value={shareUrl} />
                <Button disabled={isSaving} onClick={() => void handleCopy()} type="button" variant="outline">
                  {copied ? t('share.copied') : t('share.copy')}
                </Button>
              </div>
            ) : null}

            {share ? (
              <div className="flex justify-end">
                <Button
                  disabled={isLoading || isSaving || isWorkspaceBlocked || !hasSelectionChanges}
                  onClick={() => void saveShare()}
                  type="button"
                >
                  {isSaving ? t('share.saving') : t('share.save')}
                </Button>
              </div>
            ) : null}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
