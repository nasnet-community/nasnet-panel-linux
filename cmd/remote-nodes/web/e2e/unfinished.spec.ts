import { card, dialog, expect, fillAddNode, openAddNode, reset, test, toast } from './fixtures'

// An install that fails cleans the server up and leaves no node. One whose cleanup failed too is kept as
// an unfinished install: listed apart from the nodes, not counted, and taken out with Clean up or Forget.

const UNFINISHED = "Installs that didn't finish"

test.describe('a refused server', () => {
  test.beforeEach(async ({ request }) => {
    await reset(request, { nodes: 2 })
  })

  test('one that runs its own Xray is refused before anything changes, in words of its own', async ({ page }) => {
    await openAddNode(page)
    const form = await fillAddNode(page, 'hasxray.example.com')
    await form.getByRole('button', { name: 'Connect and install' }).click()
    const failed = dialog(page, 'Installation failed')
    await expect(failed).toBeVisible({ timeout: 20000 })
    await expect(failed).toContainText('Failed at step 1 of 16')
    // the step has the hub's sentence, with what it found, and the page says it in its own words
    await expect(failed).toContainText('(/usr/local/bin/xray, an xray.service unit)')
    // the router reached the server to find it, so the domestic list is not the matter
    await expect(failed).not.toContainText('Domestic link')
    await expect(failed.getByRole('alert')).toHaveText('This server already runs an Xray that Remote Nodes did not install. Use a server without one.')
    await expect(failed).toContainText('Nothing was changed on the server, and nothing was kept.')
    await expect(failed.getByText('Clean up the server')).toHaveCount(0)
    await expect(failed.getByText('Discard the node')).toBeVisible()
    await expect(page.getByText('2 of 3 nodes')).toBeVisible()
  })

  test('one that runs another agent is refused the same way', async ({ page }) => {
    await openAddNode(page)
    const form = await fillAddNode(page, 'hasagent.example.com')
    await form.getByRole('button', { name: 'Connect and install' }).click()
    const failed = dialog(page, 'Installation failed')
    await expect(failed).toBeVisible({ timeout: 20000 })
    await expect(failed.getByRole('alert')).toHaveText(
      "This server already runs an agent that Remote Nodes did not install, such as a Linux panel's node. Use a server without one.",
    )
    await expect(failed).toContainText('Nothing was changed on the server, and nothing was kept.')
  })
})

test.describe('an install whose cleanup fails too', () => {
  test.beforeEach(async ({ request }) => {
    await reset(request, { nodes: 2 })
  })

  test('is kept as an unfinished install: apart from the nodes, not counted, and no node page', async ({ page }) => {
    await openAddNode(page)
    const form = await fillAddNode(page, 'stuck.example.com')
    await form.getByLabel('Name', { exact: true }).fill('Stuck')
    await form.getByRole('button', { name: 'Connect and install' }).click()
    const failed = dialog(page, 'Installation failed')
    await expect(failed).toBeVisible({ timeout: 20000 })
    await expect(failed).toContainText("Couldn't upload the agent: the connection broke.")
    await expect(failed).toContainText('Cleaning up the server failed too, so the install is kept as unfinished. Clean it up or forget it on the Nodes tab.')
    // its server waits for a cleanup, so there is no Try again
    await expect(failed.getByRole('button', { name: 'Try again' })).toHaveCount(0)
    await expect(page.getByRole('region', { name: UNFINISHED })).toContainText('Stuck')
    // it isn't a node: no card (a card's link says more than the name), and the count is the two nodes there are
    await expect(page.getByRole('link', { name: /^Stuck\s/ })).toHaveCount(0)
    await expect(page.getByText('2 of 3 nodes')).toBeVisible()
    await failed.getByRole('button', { name: 'Clean up or forget' }).click()
    await expect(page).toHaveURL(/#\/nodes\/3$/)
    await expect(page.getByRole('heading', { level: 1, name: "Stuck's install didn't finish" })).toBeVisible()
    await expect(page.getByRole('tab')).toHaveCount(0)
    await expect(page.getByText('Stopped at Upload the agent')).toBeVisible()
    await expect(page.getByText('Why the cleanup failed')).toBeVisible()
    await expect(page.getByText("Can't reach SSH on stuck.example.com port 22")).toBeVisible()

    // and another install on its server waits until it is taken out
    await page.goto('/')
    await page.getByRole('button', { name: 'Add node' }).click()
    const again = await fillAddNode(page, 'stuck.example.com')
    await again.getByRole('button', { name: 'Connect and install' }).click()
    await expect(again.getByRole('alert')).toContainText("An install on that server didn't finish. Clean it up or forget it on the Nodes tab first.")
  })
})

test.describe('an unfinished install', () => {
  test.beforeEach(async ({ request }) => {
    await reset(request, { unfinished: true })
  })

  test('is a compact row beside a full router, which still counts three nodes', async ({ page }) => {
    await page.goto('/')
    const list = page.getByRole('region', { name: UNFINISHED })
    await expect(list).toContainText("These aren't nodes. Clean up each server, or forget the install.")
    await expect(list).toContainText('Old install')
    await expect(list).toContainText('192.0.2.77')
    await expect(list).toContainText('Stopped at Upload the agent')
    await expect(page.getByText('3 of 3 nodes')).toBeVisible()
    await expect(page.getByRole('link', { name: /^Old install\s/ })).toHaveCount(0)
    await expect(card(page, 'Node 1')).toBeVisible()
  })

  test('is cleaned up with a login, its host key checked, and is gone', async ({ page }) => {
    await page.goto('/')
    const list = page.getByRole('region', { name: UNFINISHED })
    await list.getByRole('button', { name: 'Clean up…' }).click()
    const clean = dialog(page, 'Clean up 192.0.2.77?')
    await expect(clean).toContainText('This removes what the install put on the server')
    // only the login: forgetting has its own button
    await expect(clean.getByRole('button', { name: 'Forget without removing' })).toHaveCount(0)
    await clean.locator('input[type="password"]').fill('a-password')
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/nodes/9/remove'))
    await clean.getByRole('button', { name: 'Log in and clean up' }).click()
    const body = (await sent).postDataJSON()
    expect(body.login.hostKey).toBe('SHA256:Old1nstallKeyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA')
    expect(body.force).toBeUndefined()
    await expect(toast(page, 'Old install cleaned up')).toBeVisible({ timeout: 15000 })
    await expect(page.getByRole('region', { name: UNFINISHED })).toHaveCount(0)
  })

  test('is forgotten after a line that says what may still be on the server', async ({ page }) => {
    await page.goto('/#/nodes/9')
    await expect(page.getByRole('heading', { level: 1, name: "Old install's install didn't finish" })).toBeVisible()
    await page.getByRole('button', { name: 'Forget…' }).click()
    const forget = page.getByRole('alertdialog', { name: 'Forget Old install?' })
    await expect(forget).toContainText(
      "Only this record is deleted. The server isn't touched, so the agent, Xray and their services from this install may still be on it.",
    )
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/nodes/9/remove'))
    await forget.getByRole('button', { name: 'Forget', exact: true }).click()
    expect((await sent).postDataJSON()).toEqual({ force: true })
    await expect(toast(page, 'Old install forgotten')).toBeVisible()
    await expect(page).toHaveURL(/#\/nodes$|#\/$|\/$/)
    await expect(page.getByRole('region', { name: UNFINISHED })).toHaveCount(0)
  })

  test('is never offered as a node for a subscription', async ({ page }) => {
    await page.goto('/#/subscriptions')
    await page.getByRole('button', { name: 'Add subscription' }).click()
    const add = dialog(page, 'Add a subscription')
    await expect(add.getByRole('checkbox', { name: /Node 1/ })).toBeVisible()
    await expect(add.getByRole('checkbox', { name: /Old install/ })).toHaveCount(0)
    await expect(add).not.toContainText('Old install')
  })

  test('reads in Persian', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
    await page.goto('/')
    const list = page.getByRole('region', { name: 'نصب‌های ناتمام' })
    await expect(list).toContainText('این‌ها نود نیستند. سرور هر کدام را پاک‌سازی کنید یا نصب را فراموش کنید.')
    await expect(list).toContainText('متوقف در ارسال ایجنت')
    await expect(list.getByRole('button', { name: 'پاک‌سازی…' })).toBeVisible()
    await expect(list.getByRole('button', { name: 'فراموش کردن…' })).toBeVisible()
  })
})

test.describe('Try again', () => {
  test.beforeEach(async ({ request }) => {
    await reset(request, { nodes: 2 })
  })

  test('is in the job tray after a reload, and fills the form in again without the password', async ({ page }) => {
    await openAddNode(page)
    const form = await fillAddNode(page, 'late.example.com')
    await form.getByLabel('Name', { exact: true }).fill('Later')
    await form.getByLabel('Username').fill('admin')
    await form.getByRole('button', { name: 'Connect and install' }).click()
    await expect(dialog(page, 'Installation failed')).toBeVisible({ timeout: 20000 })

    await page.reload()
    await page.getByRole('button', { name: 'Recent jobs' }).click()
    const tray = page.getByRole('region', { name: 'Jobs' })
    await expect(tray).toContainText('Installation failed · Later')
    await tray.getByRole('button', { name: 'Try again' }).click()
    const again = dialog(page, 'Add a node')
    await expect(again.getByLabel('Server address')).toHaveValue('late.example.com')
    await expect(again.getByLabel('Name', { exact: true })).toHaveValue('Later')
    await expect(again.getByLabel('Username')).toHaveValue('admin')
    await expect(again.locator('input[type="password"]')).toHaveValue('')
  })

  test("is on the failed install's page too, where its progress was", async ({ page }) => {
    await openAddNode(page)
    const form = await fillAddNode(page, 'fail.example.com')
    await form.getByRole('button', { name: 'Connect and install' }).click()
    await expect(dialog(page, 'Installing Node 3')).toBeVisible()
    await page.goto('/#/nodes/3')
    await expect(page.getByRole('heading', { level: 1, name: 'Installation failed · Node 3' })).toBeVisible({ timeout: 20000 })
    await expect(page.getByText('This install failed, and no node was kept.')).toBeVisible()
    await expect(page.getByText('The server was cleaned up, and nothing was kept.')).toBeVisible()
    await page.getByRole('button', { name: 'Try again' }).click()
    await expect(page).toHaveURL(/#\/nodes$|#\/$|\/$/)
    await expect(dialog(page, 'Add a node').getByLabel('Server address')).toHaveValue('fail.example.com')
  })
})
