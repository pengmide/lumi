'use client'

import { useEffect, useId, useState } from 'react'

import { useTheme } from '@/features/theme/theme-provider'

export function MermaidBlock({ code }: { code: string }) {
  const { currentTheme } = useTheme()
  const blockId = useId()
  const [svg, setSvg] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let isActive = true

    async function renderDiagram() {
      try {
        const mermaid = (await import('mermaid')).default
        mermaid.initialize({
          securityLevel: 'strict',
          startOnLoad: false,
          theme: currentTheme === 'dark' ? 'dark' : 'default',
        })

        const { svg: renderedSvg } = await mermaid.render(`workspace-preview-${blockId.replace(/:/g, '-')}`, code)
        if (!isActive) return
        setSvg(renderedSvg)
        setError(null)
      } catch (err) {
        if (!isActive) return
        setSvg(null)
        setError(err instanceof Error ? err.message : 'Unable to render Mermaid diagram')
      }
    }

    void renderDiagram()

    return () => {
      isActive = false
    }
  }, [blockId, code, currentTheme])

  if (error) {
    return (
      <div className="rounded-[16px] border border-amber-500/40 bg-amber-500/10 p-4">
        <p className="mb-3 text-sm font-medium text-amber-200">Mermaid render failed</p>
        <p className="mb-3 text-xs text-amber-100/80">{error}</p>
        <pre className="overflow-x-auto whitespace-pre rounded-[12px] bg-black/20 p-3 font-mono text-[12px] leading-6 text-amber-50">
          {code}
        </pre>
      </div>
    )
  }

  if (!svg) {
    return (
      <div className="rounded-[16px] border border-border bg-background/60 px-4 py-5 text-sm text-muted-foreground">
        Rendering Mermaid diagram…
      </div>
    )
  }

  return (
    <div
      className="overflow-x-auto rounded-[16px] border border-border bg-background/60 p-4"
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  )
}
