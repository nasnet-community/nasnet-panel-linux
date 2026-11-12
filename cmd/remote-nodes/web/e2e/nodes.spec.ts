import { attention, card, chip, expect, reset, test } from './fixtures'

const tooltip = (page: import('@playwright/test').Page) => page.getByRole('tooltip')

test('the Nodes tab shows a node that works, one that is down and one being installed', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('tab', { name: 'Nodes' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('Places subscriptions connect to: this router and your nodes.')).toBeVisible()

  const one = card(page, 'Node 1')
  await expect(one).toContainText('203.0.113.24')
  await expect(one).toContainText('Working')
  await expect(one).toContainText('3 of 6 online')
  await expect(one).toContainText('4.1 GB today')
  await expect(one).toContainText('agent 1.3.0 · Xray 26.10.21')
  await expect(one).not.toContainText('Update available')

  const two = card(page, 'Node 2')
  await expect(two).toContainText('198.51.100.7')
  await expect(two).toContainText('Unreachable')
  // its agent can't say who is online, so it counts subscriptions
  await expect(two).toContainText('4 subscriptions')
  await expect(two).toContainText('0 B today')
  await expect(two).toContainText(/Not reachable since .+ \(22 minutes ago\)/)
  await expect(two).toContainText('agent 1.2.0 · Xray 26.10.18')
  await expect(two).toContainText('Update available')

  const three = card(page, 'Node 3')
  await expect(three).toContainText('192.0.2.41')
  await expect(three).toContainText('Installing')
  await expect(three).toContainText('Step 10 of 15 · Upload Xray 26.10.21 · 62%')
  await expect(three).toContainText('started 1 minute ago')
  await expect(three).not.toContainText('Update available')
})

test('the header shows the version, and the install running in the job tray', async ({ page, isMobile }) => {
  await page.goto('/')
  const tray = page.getByRole('button', { name: 'Installing Node 3' })
  await expect(tray).toBeVisible()
  // phones have room for a count only
  if (isMobile) await expect(tray).toHaveText('1', { useInnerText: true })
  else await expect(page.getByText('v0.1.0', { exact: true })).toBeVisible()
  await tray.click()
  const jobs = page.getByRole('region', { name: 'Jobs' })
  await expect(jobs).toContainText('Installing Node 3')
  await expect(jobs).toContainText(/Step 10 of 15 · \d+:\d\d/)
  await expect(jobs).toContainText('Upload Xray 26.10.21 · 62% · 8.4 of 13.6 MB')
  await expect(jobs).toContainText('Recent')
  await expect(jobs).toContainText('Node 2 added')
  await expect(jobs).toContainText(/3 days ago · took 2:48/)
  await expect(jobs).toContainText('Agent updated · Node 1')
  // Open shows the job's own dialog, steps and all
  await jobs.getByRole('button', { name: 'Open' }).first().click()
  const d = page.getByRole('dialog', { name: 'Installing Node 3' })
  await expect(d).toContainText('Check the server')
  await expect(d).toContainText('62% · 8.4 of 13.6 MB')
  await expect(d).toContainText('You can close this. It keeps going')
})

test('the entry card says the entry is on, with what it rests on', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Entry is on' })).toBeVisible()
  await expect(page.getByText('Checked 2 min ago')).toBeVisible()
  await expect(chip(page, 'Domestic link', 'online')).toBeVisible()
  await expect(chip(page, 'VPN', 'WireGuard')).toBeVisible()
  await expect(chip(page, "Router's Xray", 'running')).toBeVisible()
  await expect(attention(page)).toHaveCount(0)
})

test('at the limit, the count, a badge and a tooltip on the disabled button say why', async ({ page, isMobile }) => {
  await page.goto('/')
  await expect(page.getByText('3 of 3 nodes')).toBeVisible()
  await expect(page.getByText('Limit reached')).toBeVisible()
  const add = page.getByRole('button', { name: 'Add node' })
  // aria-disabled, not disabled: it can take focus, so keyboards and touch can reach the reason
  await expect(add).toHaveAttribute('aria-disabled', 'true')
  await expect(add).toHaveAttribute('aria-describedby', 'cap-tip')
  await expect(tooltip(page)).toBeHidden()

  if (!isMobile) {
    await add.hover()
    await expect(tooltip(page)).toBeVisible()
    await expect(tooltip(page)).toContainText("This router runs at most 3 nodes. Each node's tunnel uses router memory. Remove one to add another.")
    await page.mouse.move(2, 2)
    await expect(tooltip(page)).toBeHidden()
  }

  // by keyboard: Tab from the last tab lands on the button
  await page.getByRole('tab', { name: 'Settings' }).focus()
  await page.keyboard.press('Tab')
  await expect(add).toBeFocused()
  await expect(tooltip(page)).toBeVisible()
  await expect(add).toHaveAccessibleDescription(/This router runs at most 3 nodes/)
  await page.keyboard.press('Tab')
  await expect(tooltip(page)).toBeHidden()

  // pressing it does nothing but keep the reason in view
  await add.click({ force: true })
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(tooltip(page)).toBeVisible()

  // the tooltip stays on the screen, whatever the width
  const box = (await tooltip(page).boundingBox())!
  const width = page.viewportSize()!.width
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(width)
})

test('with room left the button is live and the badge is gone', async ({ page, request }) => {
  await reset(request, { nodes: 2 })
  await page.goto('/')
  await expect(page.getByText('2 of 3 nodes')).toBeVisible()
  await expect(page.getByText('Limit reached')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Add node' })).not.toHaveAttribute('aria-disabled', 'true')
})

test('a router capped at one node says so in the singular', async ({ page, request }) => {
  await reset(request, { nodes: 1, maxNodes: 1 })
  await page.goto('/')
  await expect(page.getByText('1 of 1 node', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Add node' }).focus()
  await expect(tooltip(page)).toContainText('This router runs at most 1 node.')
})

test('opens a node from its card, switches its tabs and comes back', async ({ page }) => {
  await page.goto('/')
  await card(page, 'Node 2').click()
  await expect(page).toHaveURL(/#\/nodes\/2$/)
  await expect(page.getByRole('heading', { name: 'Node 2' })).toBeVisible()
  await expect(page.getByText('198.51.100.7', { exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Subscriptions' })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('tab', { name: 'Status' }).click()
  await expect(page).toHaveURL(/#\/nodes\/2\/status$/)
  await page.getByRole('tab', { name: 'Settings' }).click()
  await expect(page).toHaveURL(/#\/nodes\/2\/settings$/)
  // the address works as a link: straight to a tab
  await page.goto('/#/nodes/1/status')
  await expect(page.getByRole('heading', { name: 'Node 1' })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Status' })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('link', { name: 'All nodes' }).click()
  await expect(page).toHaveURL(/#\/$/)
  await expect(card(page, 'Node 1')).toBeVisible()
})

test('a node that does not exist says so', async ({ page }) => {
  await page.goto('/#/nodes/99')
  await expect(page.getByRole('alert')).toContainText('It no longer exists. Reload the page.')
})

test('the root tabs go by address', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('tab', { name: 'Subscriptions' }).click()
  await expect(page).toHaveURL(/#\/subscriptions$/)
  await expect(page.getByRole('tab', { name: 'Subscriptions' })).toHaveAttribute('aria-selected', 'true')
  await page.reload()
  await expect(page.getByRole('tab', { name: 'Subscriptions' })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('tab', { name: 'Settings' }).click()
  await expect(page).toHaveURL(/#\/settings$/)
  await page.getByRole('link', { name: /V2ray\/Xray/ }).click()
  await expect(page.getByRole('tab', { name: 'Nodes' })).toHaveAttribute('aria-selected', 'true')
  // the entry card stays above every tab
  await expect(page.getByRole('heading', { name: 'Entry is on' })).toBeVisible()
})

test('arrow keys move between the tabs', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('tab', { name: 'Nodes' }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(page).toHaveURL(/#\/subscriptions$/)
  await expect(page.getByRole('tab', { name: 'Subscriptions' })).toBeFocused()
  await page.keyboard.press('End')
  await expect(page).toHaveURL(/#\/settings$/)
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab', { name: 'Nodes' })).toHaveAttribute('aria-selected', 'true')
})
