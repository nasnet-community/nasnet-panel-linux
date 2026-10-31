import type { Page } from '@playwright/test'
import { attention, dialog, expect, test } from './fixtures'
import { menuItem, openMenu, openNode, overflow, panel, setNode } from './node-helpers'

// These only mean something at phone width; the other specs run there too, as the same flows.
test.beforeEach(({ isMobile }) => {
  test.skip(!isMobile, 'phone width only')
})

const height = async (page: Page, name: string | RegExp, role: 'button' | 'link' | 'tab' | 'textbox' | 'checkbox' = 'button') => (await page.getByRole(role, { name }).first().boundingBox())!.height

test('no page of the node or the router settings scrolls sideways, in either language and in trouble', async ({ page, request }) => {
  const paths = ['/#/nodes/1', '/#/nodes/1/status', '/#/nodes/1/settings', '/#/nodes/2', '/#/nodes/2/status', '/#/nodes/2/settings', '/#/nodes/3', '/#/settings']
  for (const lang of ['en', 'fa']) {
    await page.addInitScript((l) => localStorage.setItem('remote-nodes.language', l), lang)
    for (const path of paths) {
      await page.goto(path)
      await page.reload()
      await expect(page.getByRole('tablist').first()).toBeVisible()
      expect(await overflow(page), `${path} in ${lang}`).toBeLessThanOrEqual(0)
    }
    await setNode(request, { id: 1, xrayVersion: '26.10.18', crash: true, notApplied: 'xray -test failed: a rather long message about a port that is already in use on the node' })
    for (const path of ['/#/nodes/1', '/#/nodes/1/status', '/#/nodes/1/settings']) {
      await page.goto(path)
      await page.reload()
      await expect(page.getByRole('tablist').first()).toBeVisible()
      expect(await overflow(page), `${path} in trouble in ${lang}`).toBeLessThanOrEqual(0)
    }
  }
})

test('a name as long as a name can be does not push the page or its dialogs sideways', async ({ page }) => {
  // the longest name the hub takes
  const long = 'A'.repeat(64)
  await openNode(page, 1)
  await openMenu(page)
  await menuItem(page, 'Rename').click()
  await dialog(page, 'Rename node').getByLabel('Name').fill(long)
  await dialog(page, 'Rename node').getByRole('button', { name: 'Save' }).click()
  await expect(page.getByRole('heading', { level: 1, name: long })).toBeVisible()
  expect(await overflow(page)).toBeLessThanOrEqual(0)
  for (const tab of ['Status', 'Settings']) {
    await page.getByRole('tab', { name: tab }).click()
    expect(await overflow(page), tab).toBeLessThanOrEqual(0)
  }
  // a confirmation that carries the name
  await page.getByRole('button', { name: 'Rotate keys…' }).click()
  const confirm = page.getByRole('alertdialog')
  await expect(confirm).toContainText(long)
  expect(await overflow(page)).toBeLessThanOrEqual(0)
  const box = (await confirm.boundingBox())!
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width + 1)
  // and the title stays inside the sheet
  const title = (await confirm.getByRole('heading').boundingBox())!
  expect(title.x + title.width).toBeLessThanOrEqual(box.x + box.width + 1)
})

test('the controls of the node page are big enough to tap', async ({ page, request }) => {
  await setNode(request, { id: 1, xrayVersion: '26.10.18' })
  await openNode(page, 1)
  expect(await height(page, 'Check for updates')).toBeGreaterThanOrEqual(44)
  expect(await height(page, 'More actions')).toBeGreaterThanOrEqual(44)
  for (const name of ['Subscriptions', 'Status', 'Settings']) expect(await height(page, name, 'tab')).toBeGreaterThanOrEqual(44)
  expect(await attention(page).getByRole('button', { name: 'Update Xray' }).boundingBox().then((b) => b!.height)).toBeGreaterThanOrEqual(44)
  // the update chip looks small but its touch area reaches 44 px: a point 9 px above it still lands on it
  const chip = page.getByRole('button', { name: 'Update to 26.10.21', exact: true })
  const b = (await chip.boundingBox())!
  expect(b.height).toBeLessThan(30)
  const lands = await page.evaluate(([x, y]) => document.elementFromPoint(x, y)?.closest('button')?.textContent ?? '', [b.x + b.width / 2, b.y - 9])
  expect(lands).toContain('Update to 26.10.21')

  await page.getByRole('tab', { name: 'Settings' }).click()
  for (const name of ['Change port…', 'Change handshake target…', 'Rotate keys…', 'Renew now']) expect(await height(page, name)).toBeGreaterThanOrEqual(44)
  for (const name of ['Copy public key', 'Copy short ID']) {
    const copy = (await page.getByRole('button', { name }).boundingBox())!
    expect(copy.height).toBeGreaterThanOrEqual(44)
    expect(copy.width).toBeGreaterThanOrEqual(44)
  }
  expect(await height(page, /^Advanced/)).toBeGreaterThanOrEqual(44)
  await page.getByRole('button', { name: /^Advanced/ }).click()
  expect((await page.getByLabel('Same as the handshake target').locator('xpath=..').boundingBox())!.height).toBeGreaterThanOrEqual(44)
  expect(await height(page, 'Server name', 'textbox')).toBeGreaterThanOrEqual(44)
})

test('the router settings controls are big enough to tap too', async ({ page }) => {
  await page.goto('/#/settings')
  await expect(panel(page, 'Backup')).toBeVisible()
  expect(await height(page, 'Export backup')).toBeGreaterThanOrEqual(44)
  expect(await height(page, 'Import…')).toBeGreaterThanOrEqual(44)
  expect(await height(page, 'Passphrase', 'textbox')).toBeGreaterThanOrEqual(44)
})

test('stacks the header card and the notes, and the buttons are as wide as the room', async ({ page, request }) => {
  await setNode(request, { id: 1, xrayVersion: '26.10.18' })
  await openNode(page, 1)
  const title = (await page.getByRole('heading', { level: 1, name: 'Node 1' }).boundingBox())!
  const check = (await page.getByRole('button', { name: 'Check for updates' }).boundingBox())!
  // the actions are under the name, with the menu's button at the end of the row
  expect(check.y).toBeGreaterThan(title.y + title.height)
  const view = page.viewportSize()!
  expect(check.width).toBeGreaterThan(view.width / 2)
  const note = (await attention(page).locator('> div').first().boundingBox())!
  const button = (await attention(page).getByRole('button', { name: 'Update Xray' }).boundingBox())!
  expect(button.y).toBeGreaterThan(note.y + note.height / 2)
  expect(button.width).toBeGreaterThan(note.width / 2)
  expect(await overflow(page)).toBeLessThanOrEqual(0)
})

test('opens the menu inside the screen, with items you can reach', async ({ page }) => {
  await openNode(page, 1)
  await openMenu(page)
  const menu = (await page.getByRole('menu').boundingBox())!
  const view = page.viewportSize()!
  expect(menu.x).toBeGreaterThanOrEqual(0)
  expect(menu.x + menu.width).toBeLessThanOrEqual(view.width)
  // and ends where the button ends
  const trigger = (await page.getByRole('button', { name: 'More actions' }).boundingBox())!
  expect(Math.abs(menu.x + menu.width - (trigger.x + trigger.width))).toBeLessThanOrEqual(10)
})

test('puts the dialogs of the node page at the bottom as sheets, with their buttons easy to hit', async ({ page }) => {
  await openNode(page, 1, 'settings')
  await page.getByRole('button', { name: 'Change handshake target…' }).click()
  const d = dialog(page, 'Change the handshake target')
  const sheet = (await d.boundingBox())!
  const view = page.viewportSize()!
  expect(sheet.width).toBeGreaterThanOrEqual(view.width - 2)
  expect(sheet.y + sheet.height).toBeGreaterThanOrEqual(view.height - 2)
  for (const c of [d.getByRole('textbox', { name: 'Handshake target', exact: true }), d.getByRole('button', { name: 'Check', exact: true }), d.getByRole('button', { name: 'Cancel' }), d.getByRole('button', { name: 'Save and apply' }), d.getByRole('button', { name: 'Close', exact: true })]) {
    expect((await c.boundingBox())!.height).toBeGreaterThanOrEqual(44)
  }
  expect(await overflow(page)).toBeLessThanOrEqual(0)
  // the check's result, once it is there, fits the sheet too
  await d.getByRole('button', { name: 'Check', exact: true }).click()
  await expect(d.getByText('working', { exact: true })).toBeVisible()
  expect(await overflow(page)).toBeLessThanOrEqual(0)
})

test('lists the router routes as two lines each instead of a table', async ({ page }) => {
  await page.goto('/#/settings')
  const routes = panel(page, 'Router routes')
  await expect(routes.getByRole('table')).toHaveCount(0)
  const list = routes.getByRole('list', { name: 'Source addresses this plugin uses' })
  await expect(list.getByRole('listitem')).toHaveCount(3)
  const first = list.getByRole('listitem').first()
  await expect(first).toContainText('192.168.50.16')
  await expect(first).toContainText('Downloads · Foreign')
  await expect(first.getByText('ready')).toBeVisible()
  expect(await overflow(page)).toBeLessThanOrEqual(0)
})

test('stacks the cards of the Status tab in one column', async ({ page }) => {
  await openNode(page, 1, 'status')
  const boxes = await Promise.all(['Agent', 'Xray', 'Tunnel to this router', 'Certificate'].map((t) => panel(page, t).boundingBox()))
  expect(new Set(boxes.map((b) => Math.round(b!.x))).size).toBe(1)
  for (let i = 1; i < boxes.length; i++) expect(boxes[i]!.y).toBeGreaterThan(boxes[i - 1]!.y + boxes[i - 1]!.height - 1)
})

for (const lang of ['en', 'fa'] as const) {
  test(`names the two ends of the chart's timeline and nothing that could run into them, on the phone and a narrower one (${lang})`, async ({ page }) => {
    await page.addInitScript((l) => localStorage.setItem('remote-nodes.language', l), lang)
    const labels = { en: ['15 min ago', '10 min', '5 min', 'now'], fa: ['۱۵ دقیقه پیش', '۱۰ دقیقه', '۵ دقیقه', 'اکنون'] }[lang]
    for (const width of [412, 360]) {
      await page.setViewportSize({ width, height: 800 })
      await openNode(page, 1, 'status')
      const chart = page.getByRole('img', { name: lang === 'en' ? /^Download/ : /^دانلود/ })
      await expect(chart).toBeVisible()
      const shown: { x: number; w: number }[] = []
      for (const label of labels) {
        const el = chart.getByText(label, { exact: true })
        if (!(await el.isVisible())) continue
        const b = (await el.boundingBox())!
        shown.push({ x: b.x, w: b.width })
      }
      // the first and the last; the two in the middle are for wider screens
      expect(shown, `${width}px`).toHaveLength(2)
      shown.sort((a, b) => a.x - b.x)
      expect(shown[1].x, `${width}px`).toBeGreaterThan(shown[0].x + shown[0].w)
      // and the axis keeps to a line, inside the card
      const axis = (await chart.locator(':scope > div').first().locator('span').first().boundingBox())!
      expect(axis.height, `${width}px axis`).toBeLessThan(20)
      expect(axis.x, `${width}px axis`).toBeGreaterThanOrEqual(0)
      expect(await overflow(page)).toBeLessThanOrEqual(0)
    }
  })
}
