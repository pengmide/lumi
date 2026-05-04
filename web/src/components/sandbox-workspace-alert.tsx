'use client'

import { AlertTriangle, LoaderCircle, RefreshCw } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import {
  getSandboxWorkspaceAlert,
  type SandboxStageStep,
} from '@/lib/sandbox'
import type { Workspace } from '@/lib/types'

function StagePill({ step }: { step: SandboxStageStep }) {
  const tone =
    step.status === 'done'
      ? 'border-emerald-500/25 bg-emerald-500/10 text-emerald-200'
      : step.status === 'active'
        ? 'border-sky-500/25 bg-sky-500/10 text-sky-200'
        : 'border-border bg-background/60 text-muted-foreground'

  return (
    <div
      className={cn(
        'flex items-center justify-between gap-3 rounded-[12px] border px-3 py-2 text-sm',
        tone,
      )}
    >
      <span>{step.label}</span>
      <span className="text-[11px] font-semibold uppercase tracking-[0.08em]">
        {step.status === 'done'
          ? 'done'
          : step.status === 'active'
            ? 'running'
            : 'waiting'}
      </span>
    </div>
  )
}

export function SandboxWorkspaceAlert({
  workspace,
  onRetry,
  compact = false,
  className,
}: {
  workspace?: Workspace | null
  onRetry?: () => void
  compact?: boolean
  className?: string
}) {
  const alert = getSandboxWorkspaceAlert(workspace)
  if (!alert) {
    return null
  }

  const tone =
    alert.tone === 'error'
      ? 'border-rose-500/30 bg-rose-500/10 text-rose-100'
      : alert.tone === 'warning'
        ? 'border-amber-500/30 bg-amber-500/10 text-amber-100'
        : 'border-sky-500/30 bg-sky-500/10 text-sky-100'

  return (
    <div className={cn('rounded-[18px] border px-4 py-4', tone, className)}>
      <div className="flex items-start gap-3">
        {alert.tone === 'info' ? (
          <LoaderCircle className="mt-0.5 h-4 w-4 flex-shrink-0 animate-spin" />
        ) : (
          <AlertTriangle className="mt-0.5 h-4 w-4 flex-shrink-0" />
        )}
        <div className="min-w-0 flex-1">
          <div className="text-sm font-semibold">{alert.title}</div>
          <div className="mt-1 text-sm opacity-90">{alert.description}</div>
          {alert.code ? (
            <div className="mt-2 font-mono text-[11px] uppercase tracking-[0.08em] opacity-80">
              {alert.code}
            </div>
          ) : null}
        </div>
      </div>

      {!compact && alert.steps?.length ? (
        <div className="mt-4 space-y-2">
          {alert.steps.map((step) => (
            <StagePill key={step.id} step={step} />
          ))}
        </div>
      ) : null}

      {onRetry ? (
        <div className="mt-4 flex justify-end">
          <Button
            className="rounded-md border-current/20 bg-background/70 text-current hover:bg-background"
            onClick={onRetry}
            type="button"
            variant="outline"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            {alert.actionLabel}
          </Button>
        </div>
      ) : null}
    </div>
  )
}
