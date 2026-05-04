import { expect, test } from '@playwright/test'

import { installMockBackend } from './support/mock-backend'

test('shares only explicitly selected workspace files and previews them from the public page', async ({ page }) => {
  const state = await installMockBackend(page, {
    workspaceFiles: [
      {
        path: 'src/lib/request.ts',
        name: 'request.ts',
        isDir: false,
      },
      {
        path: 'docs/guide.md',
        name: 'guide.md',
        isDir: false,
      },
      {
        path: 'src/secret.txt',
        name: 'secret.txt',
        isDir: false,
      },
    ],
    sessions: [
      {
        id: 'sess-share',
        title: 'Share Selection',
        activeAgent: 'claude',
        workspaceId: 'ws-1',
        messageCount: 2,
        createdAt: Date.UTC(2026, 3, 24, 3, 0, 0),
        updatedAt: Date.UTC(2026, 3, 24, 3, 30, 0),
      },
    ],
    sessionDetails: {
      'sess-share': {
        id: 'sess-share',
        title: 'Share Selection',
        activeAgent: 'claude',
        workspaceId: 'ws-1',
        createdAt: Date.UTC(2026, 3, 24, 3, 0, 0),
        updatedAt: Date.UTC(2026, 3, 24, 3, 30, 0),
        messages: [
          {
            role: 'user',
            content: 'Please review the share selection.',
            files: [
              {
                name: 'request.ts',
                path: 'src/lib/request.ts',
                size: 1024,
              },
              {
                name: 'secret.txt',
                path: 'src/secret.txt',
                size: 1024,
              },
            ],
          },
          {
            role: 'assistant',
            content: 'Share selection is ready.',
            agent: 'claude',
          },
        ],
      },
    },
  })

  await page.goto('/c/sess-share')

  await expect(page.getByText('Please review the share selection.')).toBeVisible()
  await expect(page.getByText('Share selection is ready.')).toBeVisible()

  await page.getByRole('button', { name: 'Share', exact: true }).click()
  const shareDialog = page.getByRole('dialog', { name: 'Share' })
  await expect(shareDialog.getByText('Shared workspace files')).toHaveCount(0)
  await expect(shareDialog.getByText(/selected/)).toHaveCount(0)
  await expect(shareDialog.getByLabel('src/lib/request.ts')).toHaveCount(0)
  await expect(shareDialog.getByPlaceholder('Search files')).toHaveCount(0)

  await shareDialog.getByRole('switch', { name: 'Enable sharing' }).click()
  await expect(shareDialog.locator('input[readonly]')).toHaveValue(/share-token-1/)
  expect(state.shareCreateRequests).toEqual([
    {
      conversationId: 'sess-share',
      files: [],
    },
  ])

  await shareDialog.getByRole('button', { name: 'lib' }).click()
  const requestFile = shareDialog.getByLabel('src/lib/request.ts')
  await expect(requestFile).toBeVisible()
  await expect(requestFile).not.toBeChecked()
  await requestFile.check()
  await shareDialog.getByLabel('docs/guide.md').check()
  await shareDialog.getByRole('button', { name: 'Update share' }).click()
  await expect.poll(() => state.shareCreateRequests.length).toBe(2)
  expect(state.shares[0]?.token).toBe('share-token-1')
  expect(state.shares[0]?.files).toEqual([
    { path: 'docs/guide.md' },
    { path: 'src/lib/request.ts' },
  ])

  await page.goto('/share?token=share-token-1')

  await expect(page.getByText('Please review the share selection.')).toBeVisible()
  await expect(page.getByText('Share selection is ready.')).toBeVisible()
  const filesPanel = page.locator('section').filter({ hasText: 'Files' })
  await expect(filesPanel.getByRole('button', { name: /request\.ts/ })).toBeVisible()
  await expect(filesPanel.getByRole('button', { name: /guide\.md/ })).toBeVisible()
  await expect(page.getByText('secret.txt')).toHaveCount(0)

  const unauthorizedStatus = await page.evaluate(async () => {
    const response = await fetch(
      '/api/public/shares/conversations/share-token-1/file-meta?fileId=src%2Fsecret.txt'
    )
    return response.status
  })
  expect(unauthorizedStatus).toBe(404)

  await filesPanel.getByRole('button', { name: /request\.ts/ }).click()
  await expect(page.getByRole('dialog', { name: 'request.ts' })).toBeVisible()
  await expect(page.getByText(/loaded from mock backend/)).toBeVisible()
})

test('supports sharing with no files and revoking the same link', async ({ page }) => {
  const state = await installMockBackend(page, {
    sessions: [
      {
        id: 'sess-no-files',
        title: 'No File Share',
        activeAgent: 'claude',
        workspaceId: 'ws-1',
        messageCount: 2,
        createdAt: Date.UTC(2026, 3, 24, 4, 0, 0),
        updatedAt: Date.UTC(2026, 3, 24, 4, 20, 0),
      },
    ],
    sessionDetails: {
      'sess-no-files': {
        id: 'sess-no-files',
        title: 'No File Share',
        activeAgent: 'claude',
        workspaceId: 'ws-1',
        createdAt: Date.UTC(2026, 3, 24, 4, 0, 0),
        updatedAt: Date.UTC(2026, 3, 24, 4, 20, 0),
        messages: [
          {
            role: 'user',
            content: 'Share the conversation text only.',
          },
          {
            role: 'assistant',
            content: 'No files are needed.',
            agent: 'claude',
          },
        ],
      },
    },
  })

  await page.goto('/c/sess-no-files')
  await page.getByRole('button', { name: 'Share', exact: true }).click()
  const shareDialog = page.getByRole('dialog', { name: 'Share' })
  await expect(shareDialog.getByLabel('src/components/ChatPanel.tsx')).toHaveCount(0)
  await expect(shareDialog.getByText(/selected/)).toHaveCount(0)

  await shareDialog.getByRole('switch', { name: 'Enable sharing' }).click()
  await expect(shareDialog.locator('input[readonly]')).toHaveValue(/share-token-1/)
  await expect(shareDialog.getByRole('button', { name: 'src' })).toBeVisible()
  expect(state.shareCreateRequests).toEqual([
    {
      conversationId: 'sess-no-files',
      files: [],
    },
  ])

  await page.goto('/share?token=share-token-1')
  await expect(page.getByText('Share the conversation text only.')).toBeVisible()
  await expect(page.getByText('No files are needed.')).toBeVisible()
  await expect(page.locator('section').filter({ hasText: /^Files$/ })).toHaveCount(0)

  await page.goto('/c/sess-no-files')
  await page.getByRole('button', { name: 'Share', exact: true }).click()
  const reopenedShareDialog = page.getByRole('dialog', { name: 'Share' })
  await expect(reopenedShareDialog.getByRole('switch', { name: 'Enable sharing' })).toBeChecked()

  await reopenedShareDialog.getByRole('switch', { name: 'Enable sharing' }).click()
  await expect(reopenedShareDialog.locator('input[readonly]')).toHaveCount(0)
  expect(state.shareDeleteRequests).toEqual(['sess-no-files'])
  expect(state.shares).toEqual([])
})

test('shows a safe read-only error page when a sandbox share cannot start', async ({ page }) => {
  await installMockBackend(page, {
    publicShareError: 'sandbox_unavailable',
    shares: [
      {
        id: 'share-1',
        token: 'share-token-1',
        conversationId: 'sess-sandbox-share',
        files: [],
        createdAt: Date.UTC(2026, 3, 24, 5, 0, 0),
        updatedAt: Date.UTC(2026, 3, 24, 5, 0, 0),
      },
    ],
    sessions: [
      {
        id: 'sess-sandbox-share',
        title: 'Sandbox Share',
        activeAgent: 'claude',
        workspaceId: 'ws-sandbox',
        messageCount: 2,
        createdAt: Date.UTC(2026, 3, 24, 5, 0, 0),
        updatedAt: Date.UTC(2026, 3, 24, 5, 5, 0),
      },
    ],
    sessionDetails: {
      'sess-sandbox-share': {
        id: 'sess-sandbox-share',
        title: 'Sandbox Share',
        activeAgent: 'claude',
        workspaceId: 'ws-sandbox',
        createdAt: Date.UTC(2026, 3, 24, 5, 0, 0),
        updatedAt: Date.UTC(2026, 3, 24, 5, 5, 0),
        messages: [
          { role: 'user', content: 'Open the shared output.' },
          { role: 'assistant', content: 'The sandbox needs to start first.', agent: 'claude' },
        ],
      },
    },
    workspaces: [
      {
        id: 'ws-sandbox',
        name: 'Sandbox Workspace',
        path: '/Users/me/sandbox-app',
        kind: 'sandbox',
        sandboxStatus: 'failed',
        sandboxReady: false,
        sandboxError: 'sandbox_unavailable',
      },
    ],
  })

  await page.goto('/share?token=share-token-1')

  await expect(page.getByText('Sandbox runtime is unavailable')).toBeVisible()
  await expect(
    page.getByText('This content belongs to a sandbox workspace, but the runtime could not be started.'),
  ).toBeVisible()
  await expect(page.getByText('Error: sandbox_unavailable')).toBeVisible()
})

test('shows a sandbox-specific preview error for shared files when runtime access fails', async ({ page }) => {
  await installMockBackend(page, {
    publicShareFileErrors: {
      'src/lib/request.ts': 'sandbox_unavailable',
    },
    workspaceFiles: [
      {
        path: 'src/lib/request.ts',
        name: 'request.ts',
        isDir: false,
      },
    ],
    shares: [
      {
        id: 'share-1',
        token: 'share-token-1',
        conversationId: 'sess-sandbox-file',
        files: [{ path: 'src/lib/request.ts' }],
        createdAt: Date.UTC(2026, 3, 24, 6, 0, 0),
        updatedAt: Date.UTC(2026, 3, 24, 6, 0, 0),
      },
    ],
    sessions: [
      {
        id: 'sess-sandbox-file',
        title: 'Sandbox File Share',
        activeAgent: 'claude',
        workspaceId: 'ws-sandbox',
        messageCount: 2,
        createdAt: Date.UTC(2026, 3, 24, 6, 0, 0),
        updatedAt: Date.UTC(2026, 3, 24, 6, 5, 0),
      },
    ],
    sessionDetails: {
      'sess-sandbox-file': {
        id: 'sess-sandbox-file',
        title: 'Sandbox File Share',
        activeAgent: 'claude',
        workspaceId: 'ws-sandbox',
        createdAt: Date.UTC(2026, 3, 24, 6, 0, 0),
        updatedAt: Date.UTC(2026, 3, 24, 6, 5, 0),
        messages: [
          {
            role: 'user',
            content: 'Please inspect the request file.',
            files: [
              {
                name: 'request.ts',
                path: 'src/lib/request.ts',
                size: 1024,
              },
            ],
          },
          { role: 'assistant', content: 'Open the file preview.', agent: 'claude' },
        ],
      },
    },
    workspaces: [
      {
        id: 'ws-sandbox',
        name: 'Sandbox Workspace',
        path: '/Users/me/sandbox-app',
        kind: 'sandbox',
        sandboxStatus: 'failed',
        sandboxReady: false,
        sandboxError: 'sandbox_unavailable',
      },
    ],
  })

  await page.goto('/share?token=share-token-1')
  const filesPanel = page.locator('section').filter({ hasText: 'Files' })
  await filesPanel.getByRole('button', { name: /request\.ts/ }).click()

  const dialog = page.getByRole('dialog', { name: 'request.ts' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('Sandbox runtime is unavailable')).toBeVisible()
  await expect(dialog.getByText('sandbox_unavailable')).toBeVisible()
})
