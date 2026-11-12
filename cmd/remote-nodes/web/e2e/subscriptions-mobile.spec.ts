import type { Locator, Page } from '@playwright/test'
import { dialog, expect, test, toast } from './fixtures'
import { addButton, fillToLimit, flipper, openNodeSubscriptions, openSubscriptions, row } from './subs'

// These only mean something at phone width; the other specs run there too, as the same flows.
test.beforeEach(({ isMobile }) => {
  test.skip(!isMobile, 'phone width only')
})

const overflow = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
const height = async (el: Locator) => (await el.boundingBox())!.height
/** How far the page's own content (not the header) reaches past the right edge of the screen. */
const pastEdge = (page: Page) =>
  page.evaluate(() => {
    const edge = document.documentElement.clientWidth
    return Math.max(0, ...Array.from(document.querySelectorAll('main *')).map((el) => el.getBoundingClientRect().right - edge))
  })

const WORDS = {
  en: { add: 'Add subscription', addTitle: 'Add a subscription', share: 'Share', show: 'Show', replace: 'Replace this link…', edit: 'Edit Sara', remove: 'Delete subscription…' },
  fa: { add: 'افزودن اشتراک', addTitle: 'افزودن اشتراک', share: 'اشتراک‌گذاری', show: 'نمایش', replace: 'جایگزینی این لینک…', edit: 'ویرایش Sara', remove: 'حذف اشتراک…' },
}

for (const lang of ['en', 'fa'] as const) {
  test(`nothing scrolls sideways on either tab, with any dialog open (${lang})`, async ({ page }) => {
    const w = WORDS[lang]
    await page.addInitScript((l) => localStorage.setItem('remote-nodes.language', l), lang)
    for (const open of [() => openSubscriptions(page), () => openNodeSubscriptions(page, 1)]) {
      await open()
      expect(await overflow(page)).toBeLessThanOrEqual(0)

      // the add dialog, with a long name in it
      await page.getByRole('button', { name: w.add }).click()
      const add = dialog(page, w.addTitle)
      await add.locator('input[type="text"], input:not([type])').first().fill('A name that is much longer than the width of any phone could ever show on a single line')
      expect(await overflow(page)).toBeLessThanOrEqual(0)
      await page.keyboard.press('Escape')

      // the share sheet, its link and its question
      await row(page, 'Sara').getByRole('button', { name: w.share, exact: true }).click()
      await expect(page.locator('[data-link]')).toBeVisible()
      expect(await overflow(page)).toBeLessThanOrEqual(0)
      // the whole link, which is long
      await page.getByRole('button', { name: w.show, exact: true }).click()
      expect(await overflow(page)).toBeLessThanOrEqual(0)
      await page.getByRole('button', { name: w.replace }).click()
      await expect(page.getByRole('alertdialog')).toBeVisible()
      expect(await overflow(page)).toBeLessThanOrEqual(0)
      await page.keyboard.press('Escape')
      await page.keyboard.press('Escape')

      // the edit dialog, and the question its delete button asks
      await page.getByRole('button', { name: w.edit, exact: true }).click()
      await expect(page.getByRole('dialog')).toBeVisible()
      expect(await overflow(page)).toBeLessThanOrEqual(0)
      await page.getByRole('button', { name: w.remove }).click()
      await expect(page.getByRole('alertdialog')).toBeVisible()
      expect(await overflow(page)).toBeLessThanOrEqual(0)
      await page.keyboard.press('Escape')
    }
  })
}

test('the controls are big enough to tap', async ({ page }) => {
  await openSubscriptions(page)
  const controls: [string, Locator][] = [
    ['a switch', flipper(page, 'Sara', 'Node 1')],
    ['the unreachable one', flipper(page, 'Sara', 'Node 2')],
    ['Share', row(page, 'Sara').getByRole('button', { name: 'Share', exact: true })],
    ['Edit', row(page, 'Sara').getByRole('button', { name: 'Edit Sara', exact: true })],
    ['Add subscription', page.getByRole('button', { name: 'Add subscription' })],
  ]
  for (const [what, c] of controls) expect(await height(c), what).toBeGreaterThanOrEqual(44)
  // every switch sits in a cell as tall as a finger, two to a row
  for (const li of await row(page, 'Sara').locator('ul > li').all()) expect(await height(li)).toBeGreaterThanOrEqual(44)

  await page.getByRole('button', { name: 'Add subscription' }).click()
  const add = dialog(page, 'Add a subscription')
  const sheet = (await add.boundingBox())!
  const view = page.viewportSize()!
  // a sheet from the bottom, as wide as the screen
  expect(sheet.width).toBeGreaterThanOrEqual(view.width - 2)
  expect(sheet.y + sheet.height).toBeGreaterThanOrEqual(view.height - 2)
  for (const [what, c] of [
    ['the name', add.getByLabel('Name')],
    ['a node to tick', add.locator('label').filter({ hasText: 'Node 1' })],
    ['a node that is down', add.locator('label').filter({ hasText: 'Node 2' })],
    ['Cancel', add.getByRole('button', { name: 'Cancel' })],
    ['the add button', addButton(add)],
    ['close', add.getByRole('button', { name: 'Close', exact: true })],
  ] as [string, Locator][])
    expect(await height(c), what).toBeGreaterThanOrEqual(44)
  await page.keyboard.press('Escape')

  await row(page, 'Sara').getByRole('button', { name: 'Share', exact: true }).click()
  const share = dialog(page, "Share Sara's link")
  await expect(share.locator('[data-link]')).toBeVisible()
  for (const [what, c] of [
    ['the tabs', share.getByRole('tablist', { name: 'Place' })],
    ['Show', share.getByRole('button', { name: 'Show', exact: true })],
    ['Copy link', share.getByRole('button', { name: 'Copy link' })],
    ['Save QR image', share.getByRole('button', { name: 'Save QR image' })],
    ['Copy all links', share.getByRole('button', { name: 'Copy all 2 links' })],
    ['Replace this link', share.getByRole('button', { name: 'Replace this link…' })],
  ] as [string, Locator][])
    expect(await height(c), what).toBeGreaterThanOrEqual(44)
  await page.keyboard.press('Escape')

  // the edit dialog holds what the row's menu did
  await row(page, 'Sara').getByRole('button', { name: 'Edit Sara', exact: true }).click()
  const edit = dialog(page, 'Edit Sara')
  for (const [what, c] of [
    ['Reset usage', edit.getByRole('button', { name: 'Reset usage…' })],
    ['Delete subscription', edit.getByRole('button', { name: 'Delete subscription…' })],
    ['Save', edit.getByRole('button', { name: 'Save' })],
  ] as [string, Locator][])
    expect(await height(c), what).toBeGreaterThanOrEqual(44)
})

test("a node's tab has its controls big enough too", async ({ page }) => {
  await openNodeSubscriptions(page, 1)
  const controls: [string, Locator][] = [
    ['a switch', flipper(page, 'Sara', 'Node 1')],
    ['Share', row(page, 'Sara').getByRole('button', { name: 'Share', exact: true })],
    ['Edit', row(page, 'Sara').getByRole('button', { name: 'Edit Sara', exact: true })],
    ['All subscriptions', page.getByRole('link', { name: 'All subscriptions' })],
    ['Add subscription', page.getByRole('button', { name: 'Add subscription' })],
  ]
  for (const [what, c] of controls) expect(await height(c), what).toBeGreaterThanOrEqual(44)
})

test('cards stack in one column, the switches of each two to a row, the tiles two by two', async ({ page }) => {
  await openSubscriptions(page)
  const boxes = await Promise.all(['Ali', 'Dana', 'Kian'].map((n) => row(page, n).boundingBox()))
  expect(new Set(boxes.map((b) => Math.round(b!.x))).size).toBe(1)
  expect(boxes[1]!.y).toBeGreaterThan(boxes[0]!.y + boxes[0]!.height - 1)
  expect(boxes[2]!.y).toBeGreaterThan(boxes[1]!.y + boxes[1]!.height - 1)
  // the places of a card: Node 1 and Node 2 side by side, each with its switch, and the status line above them
  const cells = await Promise.all((await row(page, 'Sara').locator('ul > li').all()).map((li) => li.boundingBox()))
  expect(cells).toHaveLength(2)
  expect(Math.round(cells[0]!.y)).toBe(Math.round(cells[1]!.y))
  expect(cells[1]!.x).toBeGreaterThan(cells[0]!.x + cells[0]!.width - 1)
  await expect(row(page, 'Sara').locator('[data-status]')).toHaveText('Online on Node 1 · 3 connections')
  // the node that is down says so to a screen reader, and under its name when a switch waits for it
  await expect(row(page, 'Sara').locator('ul > li').nth(1)).toContainText(/Node 2\s*unreachable\s*when it is back/)
  await expect(row(page, 'Ali').locator('ul > li').nth(1)).not.toContainText('when it is back')
  // one Share button and one Edit button to a card
  await expect(row(page, 'Sara').getByRole('button')).toHaveCount(2)
  const tiles = await page.locator('main .grid > div').all()
  expect(tiles).toHaveLength(4)
  const t = await Promise.all(tiles.map((x) => x.boundingBox()))
  expect(Math.round(t[0]!.y)).toBe(Math.round(t[1]!.y))
  expect(Math.round(t[2]!.y)).toBe(Math.round(t[3]!.y))
  expect(t[2]!.y).toBeGreaterThan(t[0]!.y + t[0]!.height - 1)
  // nothing in a tile is cut off: its text fits
  for (const x of tiles) expect(await x.evaluate((el) => Array.from(el.querySelectorAll('div')).every((d) => d.scrollWidth <= d.clientWidth + 1))).toBe(true)
})

test('the share sheet fits the screen, and everything in it can be reached', async ({ page }) => {
  await openSubscriptions(page)
  await row(page, 'Sara').getByRole('button', { name: 'Share', exact: true }).click()
  const d = dialog(page, "Share Sara's link")
  await expect(d.locator('[data-link]')).toBeVisible()
  const view = page.viewportSize()!
  const box = (await d.boundingBox())!
  expect(box.height).toBeLessThanOrEqual(view.height * 0.92 + 2)
  expect(box.y).toBeGreaterThanOrEqual(0)
  // the long link stays inside the sheet, on one line and shown whole
  for (const show of [false, true]) {
    if (show) await d.getByRole('button', { name: 'Show', exact: true }).click()
    const link = (await d.locator('[data-link]').boundingBox())!
    expect(link.x).toBeGreaterThanOrEqual(box.x)
    expect(link.x + link.width).toBeLessThanOrEqual(box.x + box.width)
  }
  // Replace is at the foot: scroll to it, and it is on the screen
  const replace = d.getByRole('button', { name: 'Replace this link…' })
  await replace.scrollIntoViewIfNeeded()
  const r = (await replace.boundingBox())!
  expect(r.y + r.height).toBeLessThanOrEqual(view.height)
  expect(r.x).toBeGreaterThanOrEqual(0)
  expect(r.x + r.width).toBeLessThanOrEqual(view.width)
})

test('a toast moves up while a sheet is open, clear of its buttons, and sits at the bottom when none is', async ({ page }) => {
  await openSubscriptions(page)
  // no sheet: the bottom
  await flipper(page, 'Kian', 'Node 1').click()
  const first = toast(page, 'Kian switched on for Node 1')
  await expect(first).toBeVisible()
  const view = page.viewportSize()!
  const b = (await first.boundingBox())!
  expect(b.y).toBeGreaterThan(view.height / 2)
  expect(b.y + b.height).toBeLessThanOrEqual(view.height - 12)

  // a sheet: the top, above it
  await page.getByRole('button', { name: 'Add subscription' }).click()
  const add = dialog(page, 'Add a subscription')
  await add.getByLabel('Name').fill('Nika')
  await addButton(add).click()
  const note = toast(page, 'Nika added')
  await expect(note).toBeVisible()
  const share = dialog(page, "Share Nika's link")
  await expect(share).toBeVisible()
  const t = (await note.boundingBox())!
  const sheet = (await share.boundingBox())!
  expect(t.y + t.height).toBeLessThanOrEqual(sheet.y)
})

for (const width of [412, 360, 320]) {
  for (const lang of ['en', 'fa'] as const) {
    test(`at the limit the toolbar fits a ${width} px phone, in both tabs (${lang})`, async ({ page, request }) => {
      const w = WORDS[lang]
      await fillToLimit(request)
      await page.setViewportSize({ width, height: 800 })
      await page.addInitScript((l) => localStorage.setItem('remote-nodes.language', l), lang)
      for (const open of [() => openSubscriptions(page), () => openNodeSubscriptions(page, 1)]) {
        await open()
        const add = page.getByRole('button', { name: w.add })
        await expect(add).toHaveAttribute('aria-disabled', 'true')
        // (the header's buttons touch the edge of a 320 px screen, so this looks at what is under it)
        expect(await pastEdge(page), page.url()).toBeLessThanOrEqual(0)
        // everything in the toolbar is on the screen
        for (const c of [add, page.getByText(lang === 'en' ? 'Limit reached' : 'به حد مجاز رسید')]) {
          const b = (await c.boundingBox())!
          expect(b.x).toBeGreaterThanOrEqual(0)
          expect(b.x + b.width).toBeLessThanOrEqual(width)
        }
        await add.focus()
        const tip = (await page.getByRole('tooltip').boundingBox())!
        expect(tip.x).toBeGreaterThanOrEqual(0)
        expect(tip.x + tip.width).toBeLessThanOrEqual(width)
      }
    })
  }
}

test('the limit tooltip opens under the button, in the width of the row, on a tap', async ({ page, request }) => {
  await fillToLimit(request)
  await openSubscriptions(page)
  await expect(page.getByText('Limit reached')).toBeVisible()
  const button = page.getByRole('button', { name: 'Add subscription' })
  await button.tap({ force: true })
  const tip = page.getByRole('tooltip')
  await expect(tip).toBeVisible()
  const box = (await tip.boundingBox())!
  const view = page.viewportSize()!
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(view.width)
  const b = (await button.boundingBox())!
  expect(box.y).toBeGreaterThanOrEqual(b.y + b.height)
  expect(box.width).toBeGreaterThan(view.width * 0.7)
  expect(await overflow(page)).toBeLessThanOrEqual(0)
})
