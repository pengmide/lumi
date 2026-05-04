import type { Workspace } from '@/lib/types'

import type {
  WorkspaceChange,
  WorkspacePreviewDocument,
  WorkspacePreviewModel,
  WorkspaceTreeNode,
} from '@/features/workspace-preview/types'

function file(name: string, path: string, previewPath: string, kind: WorkspacePreviewDocument['kind']): WorkspaceTreeNode {
  return {
    path,
    name,
    type: 'file',
    kind,
    previewPath,
  }
}

function folder(name: string, path: string, children: WorkspaceTreeNode[]): WorkspaceTreeNode {
  return {
    path,
    name,
    type: 'folder',
    children,
  }
}

function buildImageDataUri(workspaceName: string, workspacePath: string) {
  const safeName = workspaceName.replace(/[<&>"]/g, '')
  const safePath = workspacePath.replace(/[<&>"]/g, '')

  const svg = `
    <svg width="1600" height="1040" viewBox="0 0 1600 1040" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect width="1600" height="1040" rx="36" fill="#0B0D12"/>
      <rect x="32" y="32" width="1536" height="976" rx="28" fill="url(#bg)"/>
      <rect x="84" y="96" width="348" height="784" rx="28" fill="#111620" stroke="#2A3444"/>
      <rect x="460" y="96" width="676" height="784" rx="28" fill="#0D1118" stroke="#2A3444"/>
      <rect x="1164" y="96" width="352" height="784" rx="28" fill="#111620" stroke="#2A3444"/>
      <rect x="1188" y="128" width="304" height="64" rx="18" fill="#171E2B"/>
      <rect x="492" y="136" width="208" height="20" rx="10" fill="#7DD3FC" fill-opacity="0.22"/>
      <rect x="84" y="96" width="348" height="76" rx="28" fill="#171E2B"/>
      <rect x="1188" y="220" width="304" height="238" rx="24" fill="#0B1020"/>
      <rect x="1188" y="484" width="304" height="170" rx="24" fill="#0B1020"/>
      <rect x="1188" y="680" width="304" height="170" rx="24" fill="#0B1020"/>
      <rect x="520" y="216" width="550" height="120" rx="26" fill="#131B28"/>
      <rect x="520" y="368" width="550" height="120" rx="26" fill="#131B28"/>
      <rect x="520" y="520" width="550" height="120" rx="26" fill="#131B28"/>
      <rect x="520" y="672" width="550" height="120" rx="26" fill="#131B28"/>
      <rect x="1122" y="216" width="14" height="576" rx="7" fill="#253145"/>
      <circle cx="1294" cy="565" r="88" fill="#1E293B"/>
      <circle cx="1294" cy="565" r="60" fill="#38BDF8" fill-opacity="0.2"/>
      <path d="M1294 516L1326 565L1294 614L1262 565L1294 516Z" fill="#38BDF8"/>
      <text x="112" y="148" fill="#F8FAFC" font-size="30" font-family="ui-sans-serif, system-ui" font-weight="700">${safeName}</text>
      <text x="112" y="902" fill="#94A3B8" font-size="24" font-family="ui-monospace, SFMono-Regular">${safePath}</text>
      <text x="494" y="198" fill="#94A3B8" font-size="20" font-family="ui-sans-serif, system-ui">Mock file canvas inspired by Ditto WebUI</text>
      <text x="1210" y="168" fill="#E2E8F0" font-size="24" font-family="ui-sans-serif, system-ui" font-weight="700">Preview Tabs</text>
      <text x="548" y="288" fill="#E2E8F0" font-size="28" font-family="ui-sans-serif, system-ui" font-weight="700">src/features/workspace-preview/workspace-preview-pane.tsx</text>
      <text x="548" y="440" fill="#E2E8F0" font-size="28" font-family="ui-sans-serif, system-ui" font-weight="700">docs/ditto-right-panel.md</text>
      <text x="548" y="592" fill="#E2E8F0" font-size="28" font-family="ui-sans-serif, system-ui" font-weight="700">assets/preview-canvas.png</text>
      <text x="548" y="744" fill="#E2E8F0" font-size="28" font-family="ui-sans-serif, system-ui" font-weight="700">reports/mock-review.pdf</text>
      <defs>
        <linearGradient id="bg" x1="88" y1="48" x2="1504" y2="972" gradientUnits="userSpaceOnUse">
          <stop stop-color="#08111E"/>
          <stop offset="0.58" stop-color="#0C1422"/>
          <stop offset="1" stop-color="#131B28"/>
        </linearGradient>
      </defs>
    </svg>
  `

  return `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(svg)}`
}

function buildMarkdown(workspaceName: string, workspacePath: string) {
  return `# Right Panel Mock Review

This document is mock content for **${workspaceName}**.

## Intent

- Match Ditto WebUI's right-side file tree and preview rhythm
- Keep the chat area in the middle and the workspace list on the far right
- Start with mock data so layout and interaction can be reviewed before backend wiring

## Working Assumptions

| Field | Value |
| --- | --- |
| Workspace | \`${workspaceName}\` |
| Path | \`${workspacePath}\` |
| Preview scope | code, markdown, image, pdf |
| Data source | client-side mock adapter |

## First-pass Notes

1. The workspace panel should stay visible even when no preview tab is open.
2. Clicking a file should reuse an existing preview tab instead of opening duplicates.
3. The preview panel should disappear when its last tab is closed.

> This mock file exists so the team can review the overall shape without waiting for the API contract.
`
}

function buildCodeSample(workspaceName: string) {
  return `import { WorkspacePreviewPane } from '@/features/workspace-preview'
import { ChatPanel } from '@/features/chat/components/chat-panel'

export function MockWorkspaceShell() {
  const workspaceName = '${workspaceName}'

  return (
    <div className="flex min-w-0 flex-1 overflow-hidden">
      <div className="min-w-0 flex-1">
        <ChatPanel />
      </div>

      <WorkspacePreviewPane workspaceName={workspaceName} />
    </div>
  )
}
`
}

function buildStateSample(workspaceName: string) {
  return `export function createWorkspacePreviewState() {
  return {
    workspaceName: '${workspaceName}',
    activeSection: 'files',
    workspaceCollapsed: false,
    previewTabs: [],
    activePreviewPath: null,
  }
}
`
}

function buildPackageJson(workspaceName: string) {
  return `{
  "name": "${workspaceName.toLowerCase().replace(/\s+/g, '-')}-preview-pass",
  "private": true,
  "scripts": {
    "dev": "next dev",
    "typecheck": "tsc --noEmit",
    "mock:review": "echo review workspace preview"
  }
}
`
}

function buildPdfContent(workspaceName: string, workspacePath: string) {
  return `Implementation Review

Workspace
${workspaceName}

Location
${workspacePath}

Objectives
- Reproduce the Ditto-style right rail in the browser chat shell
- Keep the file list and preview layout visually stable
- Limit first pass to mock data and base preview types

Acceptance
- Workspace tree always accessible
- Preview tabs support open, switch, and close
- Chat stage remains usable while right rail is active
`
}

function buildShellDiff() {
  return `diff --git a/web/src/features/chat/chat-shell.tsx b/web/src/features/chat/chat-shell.tsx
index 5f1f720..8b02c40 100644
--- a/web/src/features/chat/chat-shell.tsx
+++ b/web/src/features/chat/chat-shell.tsx
@@ -6,6 +6,7 @@
 import { ChatPanel } from '@/features/chat/components/chat-panel'
 import { Sidebar } from '@/features/chat/components/sidebar'
+import { WorkspacePreviewPane } from '@/features/workspace-preview'
 
 export function ChatShell() {
   return (
@@ -49,7 +50,16 @@ export function ChatShell() {
       </div>
 
-      <main className="relative flex min-w-0 flex-1 flex-col overflow-hidden bg-background">
+      <main className="flex min-w-0 flex-1 overflow-hidden bg-background">
+        <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden">
+          <ChatPanel />
+        </div>
+        <WorkspacePreviewPane workspace={controller.currentWorkspaceInfo} />
+      </main>
   )
 }
`
}

function buildPaneDiff() {
  return `diff --git a/web/src/features/workspace-preview/mock-data.ts b/web/src/features/workspace-preview/mock-data.ts
new file mode 100644
index 0000000..ae12a9b
--- /dev/null
+++ b/web/src/features/workspace-preview/mock-data.ts
@@ -0,0 +1,15 @@
+export function createMockWorkspacePreviewModel(workspace) {
+  return {
+    workspaceId: workspace.id,
+    workspaceName: workspace.name,
+    documents: {
+      'docs/ditto-right-panel.md': {
+        kind: 'markdown',
+        title: 'ditto-right-panel.md',
+      },
+    },
+  }
+}
`
}

function buildDocsDiff() {
  return `diff --git a/docs/preview-contract.md b/docs/preview-contract.md
index e1987d1..af03de9 100644
--- a/docs/preview-contract.md
+++ b/docs/preview-contract.md
@@ -4,8 +4,11 @@
 - list workspace tree
 - read file content
 - write file content
+- publish file change events
 
 ## Initial contract
 
-- GET /api/workspaces/files?q=
+- GET /api/workspaces/tree
+- GET /api/workspaces/file?path=
+- GET /api/workspaces/file-buffer?path=
+- GET /api/workspaces/events
`
}

export function collectInitialExpandedPaths(nodes: WorkspaceTreeNode[]) {
  const expanded = new Set(
    nodes.filter((node) => node.type === 'folder').map((node) => node.path)
  )

  const collectSyntheticFolders = (items: WorkspaceTreeNode[]) => {
    for (const item of items) {
      if (item.type !== 'folder') continue
      if (item.isSynthetic) {
        expanded.add(item.path)
      }
      collectSyntheticFolders(item.children || [])
    }
  }

  collectSyntheticFolders(nodes)

  return [...expanded]
}

export function createMockWorkspacePreviewModel(workspace: Workspace | null): WorkspacePreviewModel | null {
  if (!workspace) return null

  const workspaceName = workspace.name || 'Workspace'
  const workspacePath = workspace.path || '/mock/workspace'

  const documents: Record<string, WorkspacePreviewDocument> = {
    'docs/ditto-right-panel.md': {
      path: 'docs/ditto-right-panel.md',
      title: 'ditto-right-panel.md',
      kind: 'markdown',
      source: 'mock',
      content: buildMarkdown(workspaceName, workspacePath),
      summary: 'Planning notes and UI review checklist',
      updatedAtLabel: '4m ago',
    },
    'src/features/workspace-preview/workspace-preview-pane.tsx': {
      path: 'src/features/workspace-preview/workspace-preview-pane.tsx',
      title: 'workspace-preview-pane.tsx',
      kind: 'code',
      source: 'mock',
      language: 'tsx',
      content: buildCodeSample(workspaceName),
      summary: 'Main shell that hosts the workspace and preview panes',
      updatedAtLabel: 'now',
    },
    'src/features/workspace-preview/state.ts': {
      path: 'src/features/workspace-preview/state.ts',
      title: 'state.ts',
      kind: 'code',
      source: 'mock',
      language: 'ts',
      content: buildStateSample(workspaceName),
      summary: 'Local state bootstrap for the mock workspace pass',
      updatedAtLabel: '2m ago',
    },
    'assets/preview-canvas.png': {
      path: 'assets/preview-canvas.png',
      title: 'preview-canvas.png',
      kind: 'image',
      source: 'mock',
      src: buildImageDataUri(workspaceName, workspacePath),
      summary: 'Generated illustration to simulate a visual artifact preview',
      updatedAtLabel: '6m ago',
    },
    'reports/mock-review.pdf': {
      path: 'reports/mock-review.pdf',
      title: 'mock-review.pdf',
      kind: 'pdf',
      source: 'mock',
      content: buildPdfContent(workspaceName, workspacePath),
      summary: 'One-page review deck for the right panel spike',
      updatedAtLabel: '8m ago',
    },
    'package.json': {
      path: 'package.json',
      title: 'package.json',
      kind: 'code',
      source: 'mock',
      language: 'json',
      content: buildPackageJson(workspaceName),
      summary: 'Package manifest used as a compact text preview sample',
      updatedAtLabel: '11m ago',
    },
    '__changes__/src/features/workspace-preview/workspace-preview-pane.tsx': {
      path: '__changes__/src/features/workspace-preview/workspace-preview-pane.tsx',
      title: 'workspace-preview-pane.tsx.diff',
      kind: 'diff',
      source: 'mock',
      content: buildShellDiff(),
      summary: 'Split the main stage into chat, preview, and workspace columns',
      updatedAtLabel: 'now',
    },
    '__changes__/package.json': {
      path: '__changes__/package.json',
      title: 'package.json.diff',
      kind: 'diff',
      source: 'mock',
      content: buildPaneDiff(),
      summary: 'Add a client-side adapter for mock workspace content',
      updatedAtLabel: '2m ago',
    },
    '__changes__/docs/ditto-right-panel.md': {
      path: '__changes__/docs/ditto-right-panel.md',
      title: 'ditto-right-panel.md.diff',
      kind: 'diff',
      source: 'mock',
      content: buildDocsDiff(),
      summary: 'Document the future API contract needed after the mock pass',
      updatedAtLabel: '9m ago',
    },
    '__changes__/src/features/workspace-preview/legacy-pane.tsx': {
      path: '__changes__/src/features/workspace-preview/legacy-pane.tsx',
      title: 'legacy-pane.tsx.diff',
      kind: 'diff',
      source: 'mock',
      content: buildPaneDiff(),
      summary: 'Remove the legacy preview pane implementation',
      updatedAtLabel: '5m ago',
    },
  }

  const nodes: WorkspaceTreeNode[] = [
    folder('src', 'src', [
      folder('features', 'src/features', [
        folder('workspace-preview', 'src/features/workspace-preview', [
          file(
            'workspace-preview-pane.tsx',
            'src/features/workspace-preview/workspace-preview-pane.tsx',
            'src/features/workspace-preview/workspace-preview-pane.tsx',
            'code'
          ),
          file('state.ts', 'src/features/workspace-preview/state.ts', 'src/features/workspace-preview/state.ts', 'code'),
        ]),
      ]),
    ]),
    folder('docs', 'docs', [file('ditto-right-panel.md', 'docs/ditto-right-panel.md', 'docs/ditto-right-panel.md', 'markdown')]),
    folder('assets', 'assets', [file('preview-canvas.png', 'assets/preview-canvas.png', 'assets/preview-canvas.png', 'image')]),
    folder('reports', 'reports', [file('mock-review.pdf', 'reports/mock-review.pdf', 'reports/mock-review.pdf', 'pdf')]),
    file('package.json', 'package.json', 'package.json', 'code'),
  ]

  const changes: WorkspaceChange[] = [
    {
      id: 'change-chat-shell',
      path: 'src/features/workspace-preview/workspace-preview-pane.tsx',
      status: 'modified',
      summary: documents['__changes__/src/features/workspace-preview/workspace-preview-pane.tsx'].summary || '',
      previewPath: '__changes__/src/features/workspace-preview/workspace-preview-pane.tsx',
    },
    {
      id: 'change-mock-data',
      path: 'package.json',
      status: 'added',
      summary: documents['__changes__/package.json'].summary || '',
      previewPath: '__changes__/package.json',
    },
    {
      id: 'change-contract',
      path: 'docs/ditto-right-panel.md',
      status: 'modified',
      summary: documents['__changes__/docs/ditto-right-panel.md'].summary || '',
      previewPath: '__changes__/docs/ditto-right-panel.md',
    },
    {
      id: 'change-legacy-pane',
      path: 'src/features/workspace-preview/legacy-pane.tsx',
      status: 'deleted',
      summary: documents['__changes__/src/features/workspace-preview/legacy-pane.tsx'].summary || '',
      previewPath: '__changes__/src/features/workspace-preview/legacy-pane.tsx',
    },
  ]

  return {
    workspaceId: workspace.id,
    workspaceName,
    workspacePath,
    nodes,
    documents,
    changes,
    source: 'mock',
  }
}
