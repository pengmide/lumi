import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatRelativeTime(timestamp: number) {
  const date = new Date(timestamp)
  const now = new Date()
  const diff = now.getTime() - date.getTime()

  if (diff < 60_000) return 'now'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h`

  return date.toLocaleDateString()
}

export function formatFileSize(bytes: number): string {
  if (bytes === 0) return '0 B'

  const size = ['B', 'KB', 'MB', 'GB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), size.length - 1)

  return `${Number((bytes / 1024 ** index).toFixed(1))} ${size[index]}`
}

export function isUrl(value?: string) {
  return value?.startsWith('http://') || value?.startsWith('https://')
}
