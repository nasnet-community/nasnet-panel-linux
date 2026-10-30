import type { Page } from '@playwright/test'
import { PAGE, dialog, expect, test, toast } from './fixtures'
import { addViaApi, fillToLimit, flipper, openNodeSubscriptions, pick, row } from './subs'

const tile = (page: Page, label: string) => page.locator('main .grid > div').filter({ hasText: label })
const add = (page: Page) => page.getByRole('button', { name: 'Add subscription' })

test("a working node's tab shows how many subscriptions are on it, who is online, its traffic, and every subscription", async ({ page }) => {
  await openNodeSubscriptions(page, 1)
  await expect(page.getByRole('tab', { name: 'Subscriptions' })).toHaveAttribute('aria-selected', 'true')

  await expect(tile(page, 'On this node')).toHaveText('On this node 6 of 7 Kian is off', { useInnerText: true })
  await expect(tile(page, 'Online now')).toHaveText('Online now 3 on 9 connections', { useInnerText: true })
  await expect(tile(page, 'Today')).toHaveText('Today 4.1 GB ↑ 1.0 GB · ↓ 3.1 GB', { useInnerText: true })
  await expect(tile(page, 'Last 30 days')).toHaveText('Last 30 days 122 GB ↑ 28.0 GB · ↓ 94.0 GB', { useInnerText: true })

  await expect(page.getByText(/^Updated \d+ s ago$/)).toBeVisible()
  await expect(add(page)).toBeVisible()
  await expect(add(page)).not.toHaveAttribute('aria-disabled', 'true')
  // the count belongs to the root tab: here there is room for the link back to it
  await expect(page.getByText(/\d+ of \d+ subscriptions/)).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'All subscriptions' })).toHaveAttribute('href', '#/subscriptions')

  const rows: [name: string, status: string, sub: string, today: string, month: string][] = [
    ['Ali', 'Online', '2 connections', '640 MB', '21.0 GB in 30 days'],
    ['Dana', 'Active', 'last seen 2 days ago', '0 B', '600 MB in 30 days'],
    ['Kian', 'Off', 'off on this node', '0 B', '5.2 GB in 30 days'],
    ['Mina', 'Active', 'last seen 3 hours ago', '310 MB', '9.4 GB in 30 days'],
    ['Parsa', 'Active', 'last seen yesterday', '0 B', '1.1 GB in 30 days'],
    ['Reza', 'Online', '4 connections', '1.9 GB', '52.0 GB in 30 days'],
    ['Sara', 'Online', '3 connections', '1.2 GB', '38.0 GB in 30 days'],
  ]
  for (const [name, status, sub, today, month] of rows) {
    const r = row(page, name)
    await expect(r, name).toBeVisible()
    await expect(r).toContainText(status)
    await expect(r).toContainText(sub)
    await expect(r).toContainText(`${today} today`)
    await expect(r).toContainText(month)
    await expect(flipper(page, name, 'Node 1')).toHaveAttribute('aria-checked', String(name !== 'Kian'))
    await expect(page.getByRole('button', { name: `Actions for ${name}`, exact: true })).toBeVisible()
  }
  await expect(page.getByText('Showing 7 of 7 · sorted by name')).toBeVisible()

  // one that is off here is muted, and has no link to share
  await expect(row(page, 'Kian')).toHaveClass(/text-muted/)
  await expect(row(page, 'Kian').getByRole('button', { name: 'Share', exact: true })).toHaveCount(0)
  await expect(row(page, 'Sara')).not.toHaveClass(/text-muted/)
  await expect(row(page, 'Sara').getByRole('button', { name: 'Share', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Actions for Kian' }).click()
  const item = page.getByRole('menuitem', { name: /Share link/ })
  await expect(item).toHaveAttribute('aria-disabled', 'true')
  await expect(item).toContainText('Switch it on for this node first')
})

test("the unreachable node's tab says what it can't know", async ({ page }) => {
  await openNodeSubscriptions(page, 2)
  await expect(tile(page, 'On this node')).toHaveText('On this node 4 of 7 3 are off', { useInnerText: true })
  await expect(tile(page, 'Online now')).toHaveText('Online now – unknown while unreachable', { useInnerText: true })
  for (const name of ['Sara', 'Reza', 'Dana', 'Kian']) {
    await expect(flipper(page, name, 'Node 2')).toHaveAttribute('aria-checked', 'true')
    await expect(row(page, name)).toContainText('Unknown')
    await expect(row(page, name)).toContainText('node unreachable')
    await expect(row(page, name)).not.toContainText('Online')
  }
  for (const name of ['Ali', 'Mina', 'Parsa']) await expect(row(page, name)).toContainText('off on this node')
  // a switch is accepted, and the toast says when it takes effect
  await flipper(page, 'Ali', 'Node 2').click()
  await expect(toast(page, 'Ali switched on for Node 2. It takes effect when the node is back.')).toBeVisible()
  await expect(tile(page, 'On this node')).toContainText('5 of 7')
})

test('a switch turns a subscription off for this node only, with an Undo; the tile follows', async ({ page }) => {
  await openNodeSubscriptions(page, 1)
  const sent = page.waitForRequest((r) => r.method() === 'PUT' && r.url().endsWith('/api/subscriptions/1/nodes/1'))
  await flipper(page, 'Sara', 'Node 1').click()
  expect((await sent).postDataJSON()).toEqual({ enabled: false })
  const note = toast(page, 'Sara switched off on Node 1')
  await expect(note).toBeVisible()
  await expect(tile(page, 'On this node')).toContainText('5 of 7')
  await expect(tile(page, 'On this node')).toContainText('2 are off')
  await expect(row(page, 'Sara')).toContainText('Off')
  await expect(row(page, 'Sara')).toContainText('off on this node')
  await expect(row(page, 'Sara')).toHaveClass(/text-muted/)
  await expect(row(page, 'Sara').getByRole('button', { name: 'Share', exact: true })).toHaveCount(0)
  await expect(tile(page, 'Online now')).toContainText('2')

  await note.getByRole('button', { name: 'Undo' }).click()
  await expect(toast(page, 'Sara switched on for Node 1')).toBeVisible()
  await expect(tile(page, 'On this node')).toContainText('6 of 7')
  await expect(tile(page, 'On this node')).toContainText('Kian is off')
  await expect(row(page, 'Sara').getByRole('button', { name: 'Share', exact: true })).toBeVisible()

  // everybody on: the tile says so
  await flipper(page, 'Kian', 'Node 1').click()
  const on = toast(page, 'Kian switched on for Node 1')
  await expect(on).toBeVisible()
  await expect(on.getByRole('button')).toHaveCount(0)
  await expect(tile(page, 'On this node')).toContainText('7 of 7')
  await expect(tile(page, 'On this node')).toContainText('all on')
  // the root tab sees the same switch
  await page.getByRole('link', { name: 'All subscriptions' }).click()
  await expect(page).toHaveURL(/#\/subscriptions$/)
  await expect(page.getByRole('tab', { name: 'Subscriptions' })).toHaveAttribute('aria-selected', 'true')
  await expect(flipper(page, 'Kian', 'Node 1')).toHaveAttribute('aria-checked', 'true')
})

test('adds a subscription from the node, which then shows in the list and the count', async ({ page }) => {
  await openNodeSubscriptions(page, 1)
  await add(page).click()
  const d = dialog(page, 'Add a subscription')
  await expect(d).toContainText("3 places left. Its link is ready as soon as it's added.")
  await expect(d.getByRole('checkbox', { name: 'Node 1', exact: true })).toBeChecked()
  await expect(d.getByRole('checkbox', { name: 'Node 2 unreachable' })).not.toBeChecked()
  await expect(d.getByRole('checkbox', { name: 'Node 3 installing' })).toBeDisabled()
  await d.getByLabel('Name').fill('Nika')
  await d.getByRole('button', { name: 'Add subscription' }).click()
  await expect(toast(page, 'Nika added')).toBeVisible()
  const share = dialog(page, "Share Nika's link")
  await expect(share.locator('[data-link]')).toHaveText(/@203\.0\.113\.24:443\?.*#Nika$/)
  await share.getByRole('button', { name: 'Close', exact: true }).click()
  await expect(flipper(page, 'Nika', 'Node 1')).toHaveAttribute('aria-checked', 'true')
  await expect(tile(page, 'On this node')).toContainText('7 of 8')
  await expect(page.getByText('Showing 8 of 8 · sorted by name')).toBeVisible()
  // a name already used is refused here too
  await add(page).click()
  await dialog(page, 'Add a subscription').getByLabel('Name').fill('Nika')
  await page.keyboard.press('Enter')
  await expect(dialog(page, 'Add a subscription')).toContainText('That name is already used.')
})

test('renames and deletes from the node, and the tiles follow', async ({ page }) => {
  await openNodeSubscriptions(page, 1)
  await pick(page, 'Reza', 'Rename')
  await dialog(page, 'Rename Reza').getByLabel('Name').fill('Rezvan')
  await page.keyboard.press('Enter')
  await expect(toast(page, 'Renamed to Rezvan')).toBeVisible()
  await expect(row(page, 'Rezvan')).toContainText('4 connections')
  await expect(row(page, 'Reza')).toHaveCount(0)

  await pick(page, 'Rezvan', 'Delete subscription')
  await page.getByRole('alertdialog', { name: 'Delete Rezvan?' }).getByRole('button', { name: 'Delete subscription' }).click()
  await expect(toast(page, 'Rezvan deleted')).toBeVisible()
  await expect(row(page, 'Rezvan')).toHaveCount(0)
  await expect(tile(page, 'On this node')).toContainText('5 of 6')
  await expect(tile(page, 'Online now')).toContainText('2')
  await expect(tile(page, 'Online now')).toContainText('on 5 connections')
  await expect(page.getByText('Showing 6 of 6 · sorted by name')).toBeVisible()
})

test('at the limit the button is disabled with the same badge and tooltip, and the count is left to the root tab', async ({ page, request }) => {
  await fillToLimit(request)
  await openNodeSubscriptions(page, 1)
  await expect(page.getByText('Limit reached')).toBeVisible()
  await expect(add(page)).toHaveAttribute('aria-disabled', 'true')
  await expect(page.getByRole('tooltip')).toBeHidden()
  await add(page).focus()
  await expect(page.getByRole('tooltip')).toBeVisible()
  await expect(page.getByRole('tooltip')).toContainText('This router holds at most 10 subscriptions.')
  await add(page).click({ force: true })
  await expect(page.getByRole('dialog')).toHaveCount(0)
  // the link to all of them stays
  await expect(page.getByRole('link', { name: 'All subscriptions' })).toBeVisible()
  const box = (await page.getByRole('tooltip').boundingBox())!
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width)
})

test('with no subscriptions at all, the tab offers the first one', async ({ page, request }) => {
  for (let id = 1; id <= 7; id += 1) expect((await request.delete(`/api/subscriptions/${id}`, { headers: PAGE })).ok()).toBe(true)
  await page.goto('/#/nodes/1')
  await expect(page.getByRole('heading', { name: 'No subscriptions yet' })).toBeVisible()
  await expect(page.getByText('Add a node first')).toHaveCount(0)
  await expect(tile(page, 'On this node')).toContainText('0 of 0')
  await expect(page.getByText(/^Showing/)).toHaveCount(0)
  await page.getByRole('button', { name: 'Add your first subscription' }).click()
  await dialog(page, 'Add a subscription').getByLabel('Name').fill('Nika')
  await page.keyboard.press('Enter')
  await expect(toast(page, 'Nika added')).toBeVisible()
  await expect(dialog(page, "Share Nika's link")).toBeVisible()
})

test('a subscription added elsewhere turns up at the next load, in its place by name', async ({ page, request }) => {
  await openNodeSubscriptions(page, 1)
  await addViaApi(request, 'Bahar')
  await page.reload()
  await expect(flipper(page, 'Bahar', 'Node 1')).toBeVisible()
  const names = await page.getByRole('switch').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label')!.replace(/ on Node \d$/, '')))
  expect(names).toEqual(['Ali', 'Bahar', 'Dana', 'Kian', 'Mina', 'Parsa', 'Reza', 'Sara'])
})
