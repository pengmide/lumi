const PREVIEW_LANGUAGE_MAP: Record<string, string> = {
  bash: 'bash',
  c: 'c',
  cc: 'cpp',
  conf: 'ini',
  cpp: 'cpp',
  css: 'css',
  csv: 'plaintext',
  dart: 'dart',
  go: 'go',
  h: 'c',
  hpp: 'cpp',
  html: 'html',
  htm: 'html',
  ini: 'ini',
  java: 'java',
  js: 'javascript',
  json: 'json',
  jsonc: 'json',
  jsx: 'jsx',
  kt: 'kotlin',
  less: 'less',
  log: 'plaintext',
  md: 'markdown',
  markdown: 'markdown',
  mdown: 'markdown',
  mdx: 'markdown',
  mkd: 'markdown',
  mjs: 'javascript',
  php: 'php',
  py: 'python',
  rb: 'ruby',
  rs: 'rust',
  sass: 'scss',
  scss: 'scss',
  sh: 'bash',
  shell: 'bash',
  sql: 'sql',
  svg: 'xml',
  svelte: 'svelte',
  swift: 'swift',
  toml: 'toml',
  ts: 'typescript',
  tsx: 'tsx',
  txt: 'plaintext',
  vue: 'vue',
  xml: 'xml',
  yaml: 'yaml',
  yml: 'yaml',
  zsh: 'bash',
}

const HIGHLIGHT_LANGUAGE_ALIASES: Record<string, string> = {
  cjs: 'javascript',
  config: 'ini',
  docker: 'dockerfile',
  env: 'bash',
  plaintext: 'plaintext',
  text: 'plaintext',
}

export function inferPreviewLanguage(path: string) {
  const normalized = path.split('/').pop()?.toLowerCase()
  if (!normalized) return undefined

  if (normalized === 'dockerfile') {
    return 'dockerfile'
  }

  const extension = normalized.includes('.') ? normalized.split('.').pop() : normalized
  if (!extension) return undefined

  return PREVIEW_LANGUAGE_MAP[extension] || extension
}

export function normalizeHighlighterLanguage(language?: string) {
  const candidate = (language || '').trim().toLowerCase()
  if (!candidate) return 'plaintext'

  if (candidate === 'dockerfile') {
    return candidate
  }

  return HIGHLIGHT_LANGUAGE_ALIASES[candidate] || PREVIEW_LANGUAGE_MAP[candidate] || candidate
}
