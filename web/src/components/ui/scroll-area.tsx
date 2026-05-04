'use client'

import * as ScrollAreaPrimitive from '@radix-ui/react-scroll-area'
import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

const ScrollArea = ({
  className,
  children,
  ...props
}: ScrollAreaPrimitive.ScrollAreaProps & { children: ReactNode }) => (
  <ScrollAreaPrimitive.Root className={cn('relative overflow-hidden', className)} {...props}>
    <ScrollAreaPrimitive.Viewport className="h-full w-full rounded-[inherit]">
      {children}
    </ScrollAreaPrimitive.Viewport>
    <ScrollAreaPrimitive.Scrollbar
      className="flex touch-none select-none p-0.5 transition-colors data-[orientation=vertical]:h-full data-[orientation=vertical]:w-2.5"
      orientation="vertical"
    >
      <ScrollAreaPrimitive.Thumb className="relative flex-1 rounded-full bg-border" />
    </ScrollAreaPrimitive.Scrollbar>
    <ScrollAreaPrimitive.Corner />
  </ScrollAreaPrimitive.Root>
)

export { ScrollArea }
