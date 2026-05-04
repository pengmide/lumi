'use client'

import { Settings } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { SettingsModal } from '@/features/settings/settings-modal'
import type { Agent } from '@/lib/types'

export function SettingsButton({
  agents,
  defaultAgent,
  onUpdateAgentEnv,
  onUpdateAgentMode,
}: {
  agents: Agent[]
  defaultAgent: string
  onUpdateAgentEnv: (agentId: string, env: Record<string, string>) => Promise<{ success: boolean; error?: string }>
  onUpdateAgentMode: (
    agentId: string,
    mode: string
  ) => Promise<{ success: boolean; error?: string }>
}) {
  const [open, setOpen] = useState(false)

  return (
    <>
      <Button
        aria-label="Settings"
        className="h-8 w-8 rounded-md border-border bg-card text-[rgb(var(--color-text-secondary))] hover:bg-accent hover:text-foreground"
        onClick={() => setOpen(true)}
        size="icon"
        type="button"
        variant="outline"
      >
        <Settings className="h-4 w-4" />
      </Button>
      <SettingsModal
        agents={agents}
        defaultAgent={defaultAgent}
        onOpenChange={setOpen}
        onUpdateAgentEnv={onUpdateAgentEnv}
        onUpdateAgentMode={onUpdateAgentMode}
        open={open}
      />
    </>
  )
}
