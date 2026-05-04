import { expect, test } from '@playwright/test'

import { installMockBackend } from './support/mock-backend'

test('redirects chat traffic to setup when dependencies are not ready', async ({ page }) => {
  await installMockBackend(page, {
    setupReady: false,
    setupStatusSequence: [false],
    setupSubscribeEvents: [
      {
        ready: false,
        environment: [
          {
            name: 'Node.js',
            command: 'node -v',
            status: 'missing',
            message: 'Required',
          },
        ],
        agents: [],
        acpPackages: [],
      },
    ],
  })

  await page.goto('/c')

  await expect(page).toHaveURL(/\/setup$/)
  await expect(page.getByRole('heading', { name: 'Lumi Setup' })).toBeVisible()
  await expect(page.getByText('Node.js')).toBeVisible()
})

test('enters chat when the setup event stream reports ready', async ({ page }) => {
  await installMockBackend(page, {
    setupReady: true,
    setupSubscribeEvents: [
      {
        ready: true,
        environment: [
          {
            name: 'Node.js',
            command: 'node -v',
            status: 'ready',
            message: 'v22.0.0',
          },
        ],
        agents: [
          {
            name: 'Claude Code',
            command: 'npx -y @agentclientprotocol/claude-agent-acp@0.30.0',
            status: 'ready',
            message: 'Installed',
          },
        ],
        acpPackages: [],
      },
    ],
  })

  await page.goto('/setup')

  const continueButton = page.getByRole('button', { name: 'Continue to Chat' })
  await expect(continueButton).toBeVisible()
  if (/\/setup$/.test(page.url())) {
    await continueButton.click({ timeout: 2000 }).catch(() => {})
  }
  await expect(page).toHaveURL(/\/c$/)
  await expect(page.getByText('Start chatting!')).toBeVisible()
})
