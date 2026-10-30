import { attention, card, dialog, expect, fillAddNode, openAddNode, reset, test, toast } from './fixtures'

// These only mean something at phone width; the other specs run there too, as the same flows.
test.beforeEach(({ isMobile }) => {
  test.skip(!isMobile, 'phone width only')
})

const overflow = (page: import('@playwright/test').Page) =>
  page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)

test('no page scrolls sideways, in either language', async ({ page }) => {
  for (const lang of ['en', 'fa']) {
    await page.addInitScript((l) => localStorage.setItem('remote-nodes.language', l), lang)
    for (const path of ['/', '/#/subscriptions', '/#/settings', '/#/nodes/1', '/#/nodes/2/status', '/#/nodes/3/settings']) {
      await page.goto(path)
      await page.reload()
      await expect(page.getByRole('tablist').first()).toBeVisible()
      expect(await overflow(page), `${path} in ${lang}`).toBeLessThanOrEqual(0)
    }
  }
})

test('the first-run page, with its two attention items, fits as well', async ({ page, request }) => {
  await reset(request, { fresh: true })
  await page.goto('/')
  await expect(attention(page).getByRole('link', { name: 'Open setup' })).toHaveCount(2)
  expect(await overflow(page)).toBeLessThanOrEqual(0)
  // the items stack: text above, button below, the button as wide as the item
  const items = attention(page).locator('> div')
  const item = (await items.first().boundingBox())!
  const button = (await items.first().getByRole('link').boundingBox())!
  expect(button.y).toBeGreaterThan(item.y + item.height / 2)
  expect(button.width).toBeGreaterThan(item.width / 2)
})

test('the controls are big enough to tap', async ({ page }) => {
  await page.goto('/')
  await expect(card(page, 'Node 1')).toBeVisible()
  const controls = [
    page.getByRole('button', { name: 'Installing Node 3' }),
    page.getByRole('button', { name: 'فارسی' }),
    page.getByRole('button', { name: /^Switch to (light|dark) theme$/ }),
    page.getByRole('button', { name: 'Add node' }),
    ...(await page.getByRole('tab').all()),
  ]
  for (const c of controls) {
    const box = (await c.boundingBox())!
    expect(box.height, (await c.getAttribute('aria-label')) ?? (await c.textContent()) ?? '').toBeGreaterThanOrEqual(44)
  }
  // a card is a link as big as the card
  expect((await card(page, 'Node 1').boundingBox())!.height).toBeGreaterThanOrEqual(100)
})

test('the add dialog is a sheet from the bottom, its fields and buttons easy to hit', async ({ page, request }) => {
  await reset(request, { nodes: 2 })
  await openAddNode(page)
  const d = dialog(page, 'Add a node')
  const sheet = (await d.boundingBox())!
  const view = page.viewportSize()!
  expect(sheet.width).toBeGreaterThanOrEqual(view.width - 2)
  expect(sheet.y + sheet.height).toBeGreaterThanOrEqual(view.height - 2)
  for (const c of [
    d.getByLabel('Server address'),
    d.getByLabel('SSH port'),
    d.getByLabel('Username'),
    d.locator('input[type="password"]'),
    d.getByLabel('Name', { exact: true }),
    // the two choices share one control, which is the 44 px tall thing to hit
    d.getByRole('radiogroup'),
    d.getByRole('button', { name: 'Advanced' }),
    d.getByRole('button', { name: 'Cancel' }),
    d.getByRole('button', { name: 'Connect and install' }),
    d.getByRole('button', { name: 'Close', exact: true }),
  ]) {
    expect((await c.boundingBox())!.height).toBeGreaterThanOrEqual(44)
  }
  expect(await overflow(page)).toBeLessThanOrEqual(0)
})

test('a toast moves up while a sheet is open, clear of its buttons', async ({ page, request }) => {
  await reset(request, { nodes: 2 })
  await openAddNode(page)
  await fillAddNode(page, '192.0.2.41')
  await dialog(page, 'Add a node').getByRole('button', { name: 'Connect and install' }).click()
  const done = dialog(page, 'Node added')
  await expect(done).toBeVisible({ timeout: 20000 })
  const note = toast(page, 'Node 3 added')
  await expect(note).toBeVisible()
  const t = (await note.boundingBox())!
  const sheet = (await done.boundingBox())!
  expect(t.y + t.height).toBeLessThanOrEqual(sheet.y)
})

test('a toast sits at the bottom when no sheet is open', async ({ page, request }) => {
  await reset(request, { nodes: 2 })
  await openAddNode(page)
  await fillAddNode(page, '192.0.2.41')
  await dialog(page, 'Add a node').getByRole('button', { name: 'Connect and install' }).click()
  await dialog(page, 'Installing Node 3').getByRole('button', { name: 'Close; it keeps going' }).first().click()
  const note = toast(page, 'Node 3 added')
  await expect(note).toBeVisible({ timeout: 20000 })
  const box = (await note.boundingBox())!
  const view = page.viewportSize()!
  expect(box.y).toBeGreaterThan(view.height / 2)
  expect(box.y + box.height).toBeLessThanOrEqual(view.height - 12)
})

test('a tap on the disabled button shows why, in the width of the row', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Limit reached')).toBeVisible()
  await page.getByRole('button', { name: 'Add node' }).tap({ force: true })
  const tip = page.getByRole('tooltip')
  await expect(tip).toBeVisible()
  const box = (await tip.boundingBox())!
  const view = page.viewportSize()!
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(view.width)
  // it opens under the button, and is wide enough to read
  const button = (await page.getByRole('button', { name: 'Add node' }).boundingBox())!
  expect(box.y).toBeGreaterThanOrEqual(button.y + button.height)
  expect(box.width).toBeGreaterThan(view.width * 0.7)
  expect(await overflow(page)).toBeLessThanOrEqual(0)
})

test('the job tray opens as a panel across the screen', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Installing Node 3' }).tap()
  const jobs = page.getByRole('region', { name: 'Jobs' })
  await expect(jobs).toBeVisible()
  const box = (await jobs.boundingBox())!
  const view = page.viewportSize()!
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(view.width)
  expect(box.width).toBeGreaterThan(view.width * 0.9)
  await expect(jobs).toContainText('Installing Node 3')
  expect(await overflow(page)).toBeLessThanOrEqual(0)
})

test('the cards stack in one column, the numbers of each in a row', async ({ page }) => {
  await page.goto('/')
  const boxes = await Promise.all(['Node 1', 'Node 2', 'Node 3'].map((n) => card(page, n).boundingBox()))
  expect(new Set(boxes.map((b) => Math.round(b!.x))).size).toBe(1)
  expect(boxes[1]!.y).toBeGreaterThan(boxes[0]!.y + boxes[0]!.height - 1)
  expect(boxes[2]!.y).toBeGreaterThan(boxes[1]!.y + boxes[1]!.height - 1)
})
