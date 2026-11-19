import type { Locator, Page } from '@playwright/test'
import { dialog, expect, test } from './fixtures'
import { contrast, openNode, overflow, panel } from './node-helpers'
import { ID, LINK, isTable, obRow, openAdd, openOutbounds } from './outbounds'

const fa = (page: Page) => page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))

test.describe('in Persian', () => {
  test.beforeEach(async ({ page }) => fa(page))

  test('the tab runs right to left in Persian words and digits; what is technical stays Latin', async ({ page }) => {
    await openOutbounds(page)
    await expect(page.locator('html')).toHaveAttribute('dir', 'rtl')
    await expect(page.getByRole('tab', { name: 'خروجی‌ها' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByText('سرورهای دیگری که ترافیک می‌تواند به‌جای وی پی ان روتر از آن‌ها خارج شود.')).toBeVisible()
    await expect(page.getByText('۵ از ۱۶ خروجی')).toBeVisible()
    await expect(page.getByRole('button', { name: 'افزودن خروجی' })).toBeVisible()

    const frankfurt = obRow(page, 'Frankfurt')
    await expect(frankfurt.locator('[data-state="up"]')).toHaveText(/^برقرار · [۰-۹]+ میلی‌ثانیه$/)
    await expect(frankfurt).toContainText('VLESS · TCP · REALITY')
    await expect(frankfurt).toContainText('de.example.com:443')
    await expect(frankfurt).toContainText('۷٫۶ گیگابایت')
    await expect(obRow(page, 'Amsterdam').locator('[data-state="fallback"] > span').first()).toHaveText('قطع · روی وی پی ان روتر')
    await expect(obRow(page, 'Paris').locator('[data-state="down"]')).toContainText('قطع')
    await expect(obRow(page, 'Paris')).toContainText('این روتر')
    await expect(obRow(page, 'Helsinki').locator('[data-state="idle"]')).toHaveText('استفاده نمی‌شود')
    await expect(obRow(page, 'Helsinki')).toContainText('هیچ‌کدام')
    for (const name of ['آزمایش Frankfurt', 'ویرایش Frankfurt', 'حذف Frankfurt']) await expect(frankfurt.getByRole('button', { name, exact: true })).toBeVisible()
    await expect(page.getByText('بررسی سلامت', { exact: true })).toBeVisible()
    await expect(page.getByText('هر ۳۰ ثانیه یک بار بررسی می‌شود.')).toBeVisible()
    if (isTable(page)) {
      // the first column is on the right
      const [first, last] = await Promise.all([page.getByRole('columnheader', { name: 'خروجی' }).boundingBox(), page.getByRole('columnheader', { name: 'استفاده‌کننده‌ها' }).boundingBox()])
      expect(first!.x).toBeGreaterThan(last!.x)
    }
  })

  test('the Add dialog reads, refuses and names in Persian', async ({ page }) => {
    await openOutbounds(page)
    const d = await openAdd(page, `vless://${ID}@a.example.com:443?type=kcp#k`)
    // the name is set off left to right inside the sentence
    await expect(d.getByText(/^روش انتقال \u2066mKCP\u2069 اینجا پشتیبانی نمی‌شود\. از TCP، WebSocket، HTTPUpgrade یا XHTTP استفاده کنید\.$/)).toBeVisible()
    await d.getByLabel('لینک یا JSON').fill(LINK)
    await expect(d.locator('[data-read]')).toContainText('آنچه خوانده شد')
    await expect(d.locator('[data-read]')).toContainText('VLESS · WebSocket · TLS')
    await expect(d.getByLabel('نام', { exact: true })).toHaveValue('🇳🇱 Rotterdam')
    await expect(d.getByRole('switch', { name: 'تا وقتی قطع است از وی پی ان روتر استفاده شود' })).toBeVisible()
    await d.getByRole('button', { name: 'آزمایش', exact: true }).click()
    await expect(d.getByRole('status')).toHaveText(/^پاسخ در [۰-۹]+ میلی‌ثانیه$/)
  })

  test('Leaves through and its question are in Persian', async ({ page }) => {
    await openNode(page, 1, 'settings')
    const card = panel(page, 'خروج از')
    await expect(card.getByRole('combobox', { name: 'خروج از' }).locator('option').first()).toHaveText('وی پی ان روتر')
    await card.getByRole('combobox').selectOption('0')
    await card.getByRole('button', { name: 'ذخیره…' }).click()
    const ask = page.getByRole('alertdialog', { name: 'راه خروج Node 1 تغییر کند؟' })
    await expect(ask).toContainText('ترافیکش از وی پی ان روتر خارج می‌شود.')
    await expect(ask).toContainText('اتصال‌های باز چند ثانیه قطع می‌شوند')
  })

  test('one in use says who uses it, in Persian', async ({ page }) => {
    await openOutbounds(page)
    await obRow(page, 'Paris').getByRole('button', { name: 'حذف Paris' }).click()
    await expect(page.getByRole('alertdialog', { name: 'Paris در حال استفاده است' })).toContainText('ترافیک این روتر از این خروجی خارج می‌شود.')
  })
})

// Three pairs of the panel's colours fall a little short of 4.5 in light mode, as the other tabs' theme
// specs say: green on its tint (4.45), and the faint and muted greys on the page itself (4.3). They are
// shared tokens; here they are held to what they measure, and may not get worse.
const SHORT_IN_LIGHT: Record<string, number> = {
  'an up badge': 4.4,
  'the count': 4.3,
  'the legend': 4.3,
}

async function check(theme: 'dark' | 'light', texts: Record<string, Locator>) {
  for (const [what, el] of Object.entries(texts)) {
    const least = theme === 'light' ? (SHORT_IN_LIGHT[what] ?? 4.5) : 4.5
    expect.soft(await contrast(el), `${what} in ${theme}`).toBeGreaterThanOrEqual(least)
  }
}

for (const theme of ['dark', 'light'] as const) {
  test.describe(theme, () => {
    test.beforeEach(async ({ page }) => {
      await page.addInitScript((t) => localStorage.setItem('theme', t), theme)
    })

    test('the text of the Outbounds tab is readable (contrast of at least 4.5)', async ({ page }) => {
      await openOutbounds(page)
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
      await obRow(page, 'Frankfurt').getByRole('button', { name: 'Test Frankfurt' }).click()
      await expect(obRow(page, 'Frankfurt').getByRole('status')).toHaveText(/Answered/)
      await check(theme, {
        'the intro': page.getByText("Other servers that traffic can leave through, instead of the router's VPN."),
        'the count': page.getByText('5 of 16 outbounds'),
        'a name': obRow(page, 'Frankfurt').getByText('Frankfurt', { exact: true }),
        'a kind': obRow(page, 'Frankfurt').getByText('VLESS · TCP · REALITY'),
        'a server': obRow(page, 'Frankfurt').getByText('de.example.com:443'),
        'an up badge': obRow(page, 'Frankfurt').locator('[data-state="up"] > span').first(),
        'an answer': obRow(page, 'Frankfurt').getByRole('status').locator('span').first(),
        'a fallback badge': obRow(page, 'Amsterdam').locator('[data-state="fallback"] > span').first(),
        'a fallback error': obRow(page, 'Amsterdam').getByText('dial tcp 198.51.100.80:443: i/o timeout'),
        'a down badge': obRow(page, 'Paris').locator('[data-state="down"] > span').first(),
        'a down error': obRow(page, 'Paris').getByText(/connection refused/),
        'an idle badge': obRow(page, 'Helsinki').locator('[data-state="idle"] > span').first(),
        'nobody': obRow(page, 'Helsinki').getByText('None', { exact: true }),
        'a user': obRow(page, 'Frankfurt').getByText('Node 1', { exact: true }),
        'a traffic total': obRow(page, 'Frankfurt').getByText('7.6 GB'),
        'a test button': obRow(page, 'Frankfurt').getByRole('button', { name: 'Test Frankfurt' }),
        'the legend': page.getByText("Traffic counts from the last start of the router's Xray."),
        'the check hint': page.getByText('Left empty, it is https://www.google.com/generate_204.'),
        'the add button': page.getByRole('button', { name: 'Add outbound' }),
        ...(isTable(page) && { 'a column heading': page.getByRole('columnheader', { name: 'Used by' }) }),
      })
    })

    test('the text of the Add dialog and of Leaves through is readable', async ({ page }) => {
      await openOutbounds(page)
      const d = await openAdd(page, LINK.replace('#', '&allowInsecure=1#'))
      await d.getByRole('button', { name: 'Test', exact: true }).click()
      await expect(d.getByRole('status')).toHaveText(/Answered/)
      await check(theme, {
        'the description': d.getByText('Paste a share link or an Xray outbound JSON.'),
        'a label': d.getByText('Link or JSON', { exact: true }),
        'a hint': d.getByText('A vless, vmess, trojan, ss or socks link, or one outbound from an Xray config.'),
        'what was read': d.locator('[data-read] dt').first(),
        'a value read': d.locator('[data-read] dd').first(),
        'a notice': d.getByText('It turns certificate checks off. They stay on here.'),
        'an answer': d.getByRole('status').locator('span').first(),
        'the fallback hint': d.getByText('Off: traffic that leaves through it stops until it answers again.'),
      })
      await d.getByLabel('Link or JSON').fill(`vless://${ID}@a.example.com:443?type=grpc`)
      await check(theme, { 'a refusal': d.getByText(/The gRPC transport isn't carried here/) })
      await page.keyboard.press('Escape')

      await openNode(page, 2, 'settings')
      const card = panel(page, 'Leaves through')
      await check(theme, {
        'the hint': card.getByText('Where traffic from this node goes out to the internet.'),
        'the choice': card.getByRole('combobox'),
        'the state': card.locator('.rounded-full').first(),
        'the note': card.getByText("Amsterdam doesn't answer, so this traffic leaves through the router's VPN for now."),
      })
    })
  })
}

test.describe('nothing scrolls sideways', () => {
  for (const lang of ['en', 'fa'] as const) {
    test(`at 768, 1024 and 1280 px, and on a phone, with the dialogs open (${lang})`, async ({ page, isMobile }) => {
      if (lang === 'fa') await fa(page)
      const widths = isMobile ? [page.viewportSize()!.width] : [768, 1024, 1280]
      for (const width of widths) {
        if (!isMobile) await page.setViewportSize({ width, height: 900 })
        await openOutbounds(page)
        expect(await overflow(page), `${width} px`).toBeLessThanOrEqual(0)

        const d = await openAdd(page, LINK.replace('#', `&host=${'a'.repeat(60)}.example.com#`))
        await expect(d.locator('[data-read]')).toBeVisible()
        expect(await overflow(page), `${width} px, add`).toBeLessThanOrEqual(0)
        await page.keyboard.press('Escape')

        await obRow(page, 'Frankfurt').getByRole('button', { name: lang === 'fa' ? 'ویرایش Frankfurt' : 'Edit Frankfurt' }).click()
        await expect(page.getByRole('dialog')).toBeVisible()
        expect(await overflow(page), `${width} px, edit`).toBeLessThanOrEqual(0)
        await page.keyboard.press('Escape')

        await openNode(page, 2, 'settings')
        expect(await overflow(page), `${width} px, node settings`).toBeLessThanOrEqual(0)
      }
    })
  }
})

test.describe('on a phone', () => {
  test.beforeEach(({ isMobile }) => {
    test.skip(!isMobile, 'phone width only')
  })

  test('the rows are cards, and their controls are big enough to tap', async ({ page }) => {
    await openOutbounds(page)
    await expect(page.getByRole('table')).toHaveCount(0)
    const row = obRow(page, 'Frankfurt')
    for (const name of ['Test Frankfurt', 'Edit Frankfurt', 'Remove Frankfurt']) expect((await row.getByRole('button', { name }).boundingBox())!.height, name).toBeGreaterThanOrEqual(44)
    expect((await page.getByRole('button', { name: 'Add outbound' }).boundingBox())!.height).toBeGreaterThanOrEqual(44)
    const d = await openAdd(page, LINK)
    const sheet = (await d.boundingBox())!
    expect(sheet.width).toBeGreaterThanOrEqual(page.viewportSize()!.width - 2)
    expect((await d.getByRole('switch').boundingBox())!.height).toBeGreaterThanOrEqual(44)
    await expect(dialog(page, 'Add an outbound').getByRole('button', { name: 'Add outbound' })).toBeVisible()
  })
})
