import { dialog, expect, fillAddNode, openAddNode, reset, test } from './fixtures'
import { overflow } from './node-helpers'

// What a failed install shows: the steps that ran, the steps that never started as one line that opens,
// a step that says nothing left out, and what went wrong said once.

test.beforeEach(async ({ request }) => {
  await reset(request, { nodes: 2 })
})

async function fail(page: import('@playwright/test').Page, host: string, title: string) {
  await openAddNode(page)
  const form = await fillAddNode(page, host)
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const d = dialog(page, title)
  await expect(d).toBeVisible({ timeout: 25000 })
  return d
}

test('the steps that never started are one line, which opens and closes', async ({ page }) => {
  const d = await fail(page, 'fail.example.com', 'Installation failed')
  const line = d.getByRole('button', { name: '8 steps not started' })
  await expect(line).toHaveAttribute('aria-expanded', 'false')
  // the steps that ran are listed; the ones that did not are not
  await expect(d.getByText('Check the agent')).toBeVisible()
  await expect(d.getByText('Connect to the agent')).toHaveCount(0)
  await expect(d.getByText('Save the node')).toHaveCount(0)
  // the cleanup that came after them is
  await expect(d.getByText('Clean up the server')).toBeVisible()
  await line.click()
  await expect(line).toHaveAttribute('aria-expanded', 'true')
  for (const name of ['Connect to the agent', 'Download Xray 26.10.21', 'Upload Xray 26.10.21', 'Check Xray 26.10.21', 'Choose a handshake target', 'Create the keys', 'Apply the configuration', 'Save the node']) {
    await expect(d.getByText(name)).toHaveClass(/text-faint/)
  }
  await line.click()
  await expect(line).toHaveAttribute('aria-expanded', 'false')
  await expect(d.getByText('Save the node')).toHaveCount(0)
})

test('a job that is still running lists all its steps, not started ones included', async ({ page }) => {
  await openAddNode(page)
  const form = await fillAddNode(page, '192.0.2.41')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const d = dialog(page, 'Installing Node 3')
  await expect(d.getByText('Save the node')).toBeVisible()
  await expect(d.getByRole('button', { name: /steps not started/ })).toHaveCount(0)
  await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
  await expect(dialog(page, 'Node added').getByRole('button', { name: /steps not started/ })).toHaveCount(0)
})

test('the step that forgets a node is shown when a cleanup came before it, and not when nothing was made', async ({ page }) => {
  const cleaned = await fail(page, 'late.example.com', 'Installation failed')
  await expect(cleaned.getByText('Clean up the server')).toBeVisible()
  await expect(cleaned.getByText('Discard the node')).toBeVisible()
  await cleaned.getByRole('button', { name: 'Close', exact: true }).first().click()
  const refused = await fail(page, 'hasagent.example.com', "This server can't be used")
  await expect(refused.getByText('Discard the node')).toHaveCount(0)
  await expect(refused.getByText('Clean up the server')).toHaveCount(0)
})

test('what went wrong is said once: by the page when it has words for it, and by the failed step when it has none', async ({ page }) => {
  // no words of the page's: the hub's sentence, on the step, and in no box
  const plain = await fail(page, 'late.example.com', 'Installation failed')
  await expect(plain.getByText("www.example.com:443 didn't answer from this node")).toHaveCount(1)
  await expect(plain.getByRole('alert')).toHaveCount(0)
  await plain.getByRole('button', { name: 'Close', exact: true }).first().click()
  // words of the page's: its sentence in one box, with the hub's detail beside it, and nothing on the step
  const worded = await fail(page, 'unreach.example.com', 'Installation failed')
  await expect(worded.getByRole('alert')).toHaveCount(1)
  await expect(worded.getByRole('alert')).toHaveText("The router can't reach this node. Check that it is on and its address is on the domestic list.")
  await expect(worded.getByText("The agent doesn't answer on TCP port 9090")).toHaveCount(1)
})

test('a refusal is titled so in the tray and on the failed install\'s page, and offers to change the details', async ({ page }) => {
  const d = await fail(page, 'hasagent.example.com', "This server can't be used")
  await d.getByRole('button', { name: 'Close', exact: true }).first().click()
  // the tray offers the same, where a plain failure offers Try again
  await page.getByRole('button', { name: 'Recent jobs' }).click()
  const tray = page.getByRole('region', { name: 'Jobs' })
  await expect(tray.getByRole('button', { name: 'Change details' })).toBeVisible()
  await expect(tray.getByRole('button', { name: 'Try again' })).toHaveCount(0)
  // the node page of the install that was never kept
  await page.goto('/#/nodes/3')
  await expect(page.getByRole('heading', { level: 1, name: "This server can't be used" })).toBeVisible({ timeout: 15000 })
  await expect(page.locator('p').filter({ hasText: 'This server already runs a nasnet-agent that Remote Nodes did not install' })).toBeVisible()
  await expect(page.getByRole('main').getByRole('button', { name: 'Try again' })).toHaveCount(0)
  await page.getByRole('main').getByRole('button', { name: 'Change details' }).click()
  await expect(dialog(page, 'Add a node').getByLabel('Server address')).toHaveValue('hasagent.example.com')
})

test('reads in Persian, with its digits, and does not overflow at phone width', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
  await openAddNode(page, 'افزودن نود')
  const form = await fillAddNode(page, 'fail.example.com')
  await form.getByRole('button', { name: 'اتصال و نصب' }).click()
  const d = dialog(page, 'نصب ناموفق بود')
  await expect(d).toBeVisible({ timeout: 25000 })
  await expect(d.getByRole('button', { name: '۸ مرحله شروع نشده' })).toBeVisible()
  expect(await overflow(page)).toBe(0)
  await d.getByRole('button', { name: '۸ مرحله شروع نشده' }).click()
  expect(await overflow(page)).toBe(0)
})
