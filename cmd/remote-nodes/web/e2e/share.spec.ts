import type { Page } from '@playwright/test'
import { PAGE, dialog, expect, test, toast } from './fixtures'
import { addViaApi, linkText, openNodeSubscriptions, openSubscriptions, pick, placeTabs, qrPath, row, shownQrPath } from './subs'

// Sara's link on Node 1, as the design board shows it
const BOARD_LINK =
  'vless://3f0c9a52-7d1e-4b8a-9c44-0e5b2a1d6f73@203.0.113.24:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=x7QnLw4Rk2VtB9yD0sEoUj3HaFcN6mXz8PqIg1KvTbA&sid=a1b2c3d4&type=tcp#Sara'

const shareButton = (page: Page, name: string) => row(page, name).getByRole('button', { name: 'Share', exact: true })

test('shows a subscription on a node as a QR code and a link, and copies the link', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  const asked: string[] = []
  page.on('request', (r) => /\/link$/.test(r.url()) && asked.push(new URL(r.url()).pathname.replace(/^.*subscriptions\//, '')))
  await openSubscriptions(page)
  await shareButton(page, 'Sara').click()
  // she is on two nodes, so there are two links, and the dialog says links
  const d = dialog(page, "Share Sara's links")
  await expect(d).toContainText('One link per place. Anyone with a link can use this subscription.')

  // a tab per node it is on; the one that can't be reached carries a red dot, which a screen reader hears as a word
  await expect(placeTabs(d)).toHaveText(['Node 1', 'Node 2 unreachable'])
  await expect(d.getByRole('tab', { name: 'Node 1', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(d.getByRole('tab', { name: 'Node 2 unreachable' })).toHaveAttribute('aria-selected', 'false')
  await expect(d.getByRole('tab', { name: 'Node 2 unreachable' }).locator('[aria-hidden]')).toHaveClass(/bg-bad/)
  await expect(d.getByRole('tab', { name: 'Node 1', exact: true }).locator('[aria-hidden]')).toHaveCount(0)

  const link = d.locator('[data-link]')
  await expect(link).toHaveText(BOARD_LINK)
  // a link reads left to right in any language
  await expect(link).toHaveAttribute('dir', 'ltr')
  // it is long, so it shows as one line until it is asked for whole
  const oneLine = (await link.boundingBox())!.height
  expect(oneLine).toBeLessThan(40)
  await d.getByRole('button', { name: 'Show', exact: true }).click()
  expect((await link.boundingBox())!.height).toBeGreaterThan(oneLine * 2)
  await d.getByRole('button', { name: 'Hide', exact: true }).click()
  expect((await link.boundingBox())!.height).toBe(oneLine)
  await expect(d.getByRole('img', { name: "QR code of Sara's link on Node 1" })).toBeVisible()
  // the code holds exactly this link
  expect(await shownQrPath(page)).toBe(qrPath(BOARD_LINK))
  await expect(d).toContainText('Any app that opens vless:// links can use it.')
  await expect(d).toContainText('The old link stops working once it is replaced.')

  await d.getByRole('button', { name: 'Copy link' }).click()
  await expect(d.getByRole('button', { name: 'Copied' })).toBeVisible()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(BOARD_LINK)

  // Node 2 has its own link, address and id; going back needs no second call
  await d.getByRole('tab', { name: 'Node 2 unreachable' }).click()
  await expect(link).toHaveText(/^vless:\/\/[0-9a-f-]{36}@198\.51\.100\.7:443\?.*#Sara$/)
  await expect(d.getByRole('img', { name: "QR code of Sara's link on Node 2" })).toBeVisible()
  const two = await linkText(page)
  expect(two).not.toBe(BOARD_LINK)
  expect(await shownQrPath(page)).toBe(qrPath(two))
  await expect(d.getByRole('tab', { name: 'Node 2 unreachable' })).toHaveAttribute('aria-selected', 'true')
  await d.getByRole('tab', { name: 'Node 1', exact: true }).click()
  await expect(link).toHaveText(BOARD_LINK)
  expect(asked).toEqual(['1/nodes/1/link', '1/nodes/2/link'])
})

test('a subscription on one node has one link: no tabs, and nothing to copy all of', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Ali', 'Share link')
  const d = dialog(page, "Share Ali's link")
  await expect(d).toContainText('Anyone with this link can use this subscription.')
  await expect(d.getByRole('tablist')).toHaveCount(0)
  await expect(d.getByRole('button', { name: /Copy all/ })).toHaveCount(0)
  await expect(d.locator('[data-link]')).toHaveText(/^vless:\/\/[0-9a-f-]{36}@203\.0\.113\.24:443\?.*#Ali$/)
})

test('saves the QR code as an image named after the subscription and the node', async ({ page }) => {
  await openSubscriptions(page)
  await shareButton(page, 'Sara').click()
  const d = dialog(page, "Share Sara's links")
  await expect(d.locator('[data-link]')).toHaveText(BOARD_LINK)
  const saved = page.waitForEvent('download')
  await d.getByRole('button', { name: 'Save QR image' }).click()
  const file = await saved
  expect(file.suggestedFilename()).toBe('Sara - Node 1.png')
  // a PNG, square, and big enough to be read off another screen
  const bytes = await (await import('node:fs/promises')).readFile(await file.path())
  expect(bytes.subarray(0, 8).toString('hex')).toBe('89504e470d0a1a0a')
  const [width, height] = [bytes.readUInt32BE(16), bytes.readUInt32BE(20)]
  expect(width).toBe(height)
  expect(width).toBeGreaterThanOrEqual(720)
})

test('copies the links of every place at once, one to a line', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await openSubscriptions(page)
  await shareButton(page, 'Sara').click()
  const d = dialog(page, "Share Sara's links")
  await expect(d.locator('[data-link]')).toHaveText(BOARD_LINK)
  await expect(d).toContainText('One paste adds every link, so the app can use another place.')
  await d.getByRole('button', { name: 'Copy all 2 links' }).click()
  await expect(toast(page, '2 links copied')).toBeVisible()
  const [one, two, ...more] = (await page.evaluate(() => navigator.clipboard.readText())).split('\n')
  expect(one).toBe(BOARD_LINK)
  expect(two).toMatch(/^vless:\/\/[0-9a-f-]{36}@198\.51\.100\.7:443\?.*#Sara$/)
  expect(more).toEqual([])
  // what was fetched for it is kept: the other tab opens without another call
  const asked: string[] = []
  page.on('request', (r) => /\/link$/.test(r.url()) && asked.push(r.url()))
  await d.getByRole('tab', { name: 'Node 2 unreachable' }).click()
  await expect(d.locator('[data-link]')).toHaveText(two)
  expect(asked).toEqual([])
})

test('keeps its focus inside, and Escape closes it and gives the focus back', async ({ page, isMobile }) => {
  test.skip(isMobile, 'keyboard focus is for wide screens')
  await openSubscriptions(page)
  await shareButton(page, 'Sara').click()
  const d = dialog(page, "Share Sara's link")
  await expect(d.getByRole('tab', { name: 'Node 1', exact: true })).toBeFocused()
  await expect(d.getByRole('button', { name: 'Copy link' })).toBeEnabled()
  // Tab goes round the dialog and never out of it
  const seen = new Set<string>()
  for (let i = 0; i < 9; i += 1) {
    await page.keyboard.press('Tab')
    seen.add(await page.evaluate(() => (document.activeElement as HTMLElement).getAttribute('aria-label') ?? (document.activeElement as HTMLElement).textContent!.trim()))
    expect(await d.evaluate((el) => el.contains(document.activeElement))).toBe(true)
  }
  expect([...seen]).toEqual(expect.arrayContaining(['Node 2 unreachable', 'Show', 'Copy link', 'Save QR image', 'Copy all 2 links', 'Replace this link…', 'Close', 'Node 1']))
  await page.keyboard.press('Escape')
  await expect(d).toHaveCount(0)
  await expect(shareButton(page, 'Sara')).toBeFocused()
})

test.describe('replacing a link', () => {
  test('asks first; Cancel and Escape change nothing; confirming shows the new link, for that node only', async ({ page }) => {
    await openSubscriptions(page)
    await shareButton(page, 'Sara').click()
    const d = dialog(page, "Share Sara's link")
    const old = BOARD_LINK
    await expect(d.locator('[data-link]')).toHaveText(old)
    // Node 2's link, to see it stay as it is
    await d.getByRole('tab', { name: 'Node 2 unreachable' }).click()
    await expect(d.locator('[data-link]')).toHaveText(/@198\.51\.100\.7:/)
    const two = await linkText(page)
    await d.getByRole('tab', { name: 'Node 1', exact: true }).click()

    const replace = d.getByRole('button', { name: 'Replace this link…' })
    await replace.click()
    const c = page.getByRole('alertdialog', { name: "Replace Sara's link?" })
    await expect(c).toContainText("The old link stops working. You'll share the new link with them.")
    // on two nodes, it says which one this is about
    await expect(c).toContainText('This only changes the link for Node 1.')
    await expect(c.getByRole('button', { name: 'Replace link' })).not.toHaveClass(/bg-bad/)

    await c.getByRole('button', { name: 'Cancel' }).click()
    await expect(c).toHaveCount(0)
    expect(await linkText(page)).toBe(old)
    await replace.click()
    await page.keyboard.press('Escape')
    await expect(c).toHaveCount(0)
    // only the question closed
    await expect(d).toBeVisible()
    expect(await linkText(page)).toBe(old)

    await replace.click()
    const sent = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/api/subscriptions/1/nodes/1/replace-link'))
    await c.getByRole('button', { name: 'Replace link' }).click()
    await sent
    await expect(toast(page, 'Sara has a new link')).toBeVisible()
    await expect(c).toHaveCount(0)
    const fresh = await linkText(page)
    expect(fresh).not.toBe(old)
    // only the id changed
    expect(fresh.replace(/^vless:\/\/[^@]+/, '')).toBe(old.replace(/^vless:\/\/[^@]+/, ''))
    expect(await shownQrPath(page)).toBe(qrPath(fresh))
    // the share dialog stays, with the question's button focused again
    await expect(d).toBeVisible()
    await expect(replace).toBeFocused()
    await d.getByRole('tab', { name: 'Node 2 unreachable' }).click()
    expect(await linkText(page)).toBe(two)
    await d.getByRole('button', { name: 'Close', exact: true }).click()

    // the hub keeps the new one
    await shareButton(page, 'Sara').click()
    expect(await linkText(page)).toBe(fresh)
  })

  test('on one node it asks without the line about which', async ({ page }) => {
    await openSubscriptions(page)
    await shareButton(page, 'Ali').click()
    await dialog(page, "Share Ali's link").getByRole('button', { name: 'Replace this link…' }).click()
    const c = page.getByRole('alertdialog', { name: "Replace Ali's link?" })
    await expect(c).toContainText("The old link stops working. You'll share the new link with them.")
    await expect(c).not.toContainText('This only changes')
  })

  test('a subscription that is gone says so in the question, and keeps the old link on show', async ({ page, request }) => {
    await openSubscriptions(page)
    await shareButton(page, 'Sara').click()
    const d = dialog(page, "Share Sara's link")
    await expect(d.locator('[data-link]')).toHaveText(BOARD_LINK)
    expect((await request.delete('/api/subscriptions/1', { headers: PAGE })).ok()).toBe(true)
    await d.getByRole('button', { name: 'Replace this link…' }).click()
    const c = page.getByRole('alertdialog', { name: "Replace Sara's link?" })
    await c.getByRole('button', { name: 'Replace link' }).click()
    await expect(c.getByRole('alert')).toHaveText('It no longer exists. Reload the page.')
    await expect(toast(page, 'Sara has a new link')).toHaveCount(0)
    await c.getByRole('button', { name: 'Cancel' }).click()
    await expect(d.locator('[data-link]')).toHaveText(BOARD_LINK)
  })
})

test('a link that cannot be fetched says why, and offers nothing to copy', async ({ page, request }) => {
  await openSubscriptions(page)
  // switched off on Node 1 behind the page's back: it has no link there any more
  expect((await request.put('/api/subscriptions/1/nodes/1', { headers: PAGE, data: { enabled: false } })).ok()).toBe(true)
  await shareButton(page, 'Sara').click()
  const d = dialog(page, "Share Sara's link")
  await expect(d.getByRole('alert')).toHaveText('It no longer exists. Reload the page.')
  await expect(d.locator('[data-link]')).toHaveCount(0)
  await expect(d.getByRole('img')).toHaveCount(0)
  await expect(d.getByRole('button', { name: 'Copy link' })).toBeDisabled()
  await expect(d.getByRole('button', { name: 'Save QR image' })).toBeDisabled()
  await expect(d.getByRole('button', { name: 'Replace this link…' })).toBeDisabled()
  // the other node still works
  await d.getByRole('tab', { name: 'Node 2 unreachable' }).click()
  await expect(d.locator('[data-link]')).toHaveText(/@198\.51\.100\.7:/)
  await expect(d.getByRole('alert')).toHaveCount(0)
})

test.describe('a name that is not Latin', () => {
  for (const [name, fragment] of [
    ['سارا', '#%D8%B3%D8%A7%D8%B1%D8%A7'],
    ['Mom سارا', '#Mom%20%D8%B3%D8%A7%D8%B1%D8%A7'],
  ]) {
    test(`${name}: the link's fragment survives in the text, the clipboard and the QR code`, async ({ page, context, request }) => {
      await context.grantPermissions(['clipboard-read', 'clipboard-write'])
      expect((await addViaApi(request, name)).ok()).toBe(true)
      await openSubscriptions(page)
      await shareButton(page, name).click()
      const d = dialog(page, `Share ${name}'s link`)
      const shown = await d.locator('[data-link]').innerText()
      expect(shown.endsWith(fragment)).toBe(true)
      // the name comes back out of the fragment
      expect(decodeURIComponent(shown.slice(shown.indexOf('#') + 1))).toBe(name)
      expect(shown).toMatch(/^[\x00-\x7f]*$/)
      await expect(d.getByRole('img', { name: `QR code of ${name}'s link on Node 1` })).toBeVisible()
      expect(await shownQrPath(page)).toBe(qrPath(shown))
      await d.getByRole('button', { name: 'Copy link' }).click()
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(shown)
    })
  }
})

test("a node's own tab shares the subscription's link on that node", async ({ page }) => {
  await openNodeSubscriptions(page, 1)
  await shareButton(page, 'Sara').click()
  const d = dialog(page, "Share Sara's link")
  // one place, so nothing to choose
  await expect(d.getByRole('tablist')).toHaveCount(0)
  await expect(d.locator('[data-link]')).toHaveText(BOARD_LINK)
})
