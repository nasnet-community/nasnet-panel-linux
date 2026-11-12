import type { Locator, Page } from '@playwright/test'
import { PAGE, dialog, expect, test, toast } from './fixtures'
import { addButton, addViaApi, fillToLimit, flipper, listed, openNodeSubscriptions, openSubscriptions, pick, placeTabs, qrPath, row, shownQrPath } from './subs'

const LRM = '‎'

// Persian is the language of these tests, from the first load
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
})

const tile = (page: Page, label: string) => page.locator('main .grid > div').filter({ hasText: label })
const add = (page: Page) => page.getByRole('button', { name: 'افزودن اشتراک' })
const share = (page: Page, name: string) => row(page, name).getByRole('button', { name: 'اشتراک‌گذاری', exact: true })
const direction = (el: Locator) => el.evaluate((node) => getComputedStyle(node).direction)

test('the tab in Persian: right to left, Persian digits and separators, the words of the design', async ({ page }) => {
  await openSubscriptions(page)
  await expect(page.locator('html')).toHaveAttribute('dir', 'rtl')

  await expect(tile(page, 'اشتراک‌ها')).toHaveText('اشتراک‌ها ۷ از ۱۰ ۳ جای خالی', { useInnerText: true })
  await expect(tile(page, 'اکنون آنلاین')).toHaveText('اکنون آنلاین ۳ روی Node 1', { useInnerText: true })
  await expect(tile(page, 'امروز')).toHaveText(`امروز ۴٫۳ گیگابایت ↑ ${LRM}۱٫۱ گیگابایت · ↓ ${LRM}۳٫۲ گیگابایت`, { useInnerText: true })
  await expect(tile(page, '۳۰ روز اخیر')).toHaveText(`۳۰ روز اخیر ۱۲۷ گیگابایت ↑ ${LRM}۳۱٫۸ گیگابایت · ↓ ${LRM}۹۵٫۵ گیگابایت`, { useInnerText: true })
  // no Latin digit is left in the tiles that count and weigh
  for (const label of ['اشتراک‌ها', 'امروز', '۳۰ روز اخیر']) await expect(tile(page, label)).not.toContainText(/[0-9]/)

  await expect(page.getByText(/^به‌روز شده [۰-۹]+ ثانیه پیش$/)).toBeVisible()
  await expect(page.getByText('۷ از ۱۰ اشتراک')).toBeVisible()
  await expect(add(page)).toBeVisible()
  await expect(page.getByText('Node 3 در حال نصب است. وقتی آماده شد اینجا نشان داده می‌شود.')).toBeVisible()

  // every row, in the design's words: the line under the name says how it is doing
  const rows: [string, string, string, string][] = [
    ['Sara', 'آنلاین روی Node 1 · ۳ اتصال', '۱٫۲ گیگابایت امروز', '۳۸٫۰ گیگابایت در ۳۰ روز'],
    ['Ali', 'آنلاین روی Node 1 · ۲ اتصال', '۶۴۰ مگابایت امروز', '۲۱٫۰ گیگابایت در ۳۰ روز'],
    ['Mina', 'آفلاین · آخرین بازدید ۳ ساعت پیش', '۳۱۰ مگابایت امروز', '۹٫۴ گیگابایت در ۳۰ روز'],
    ['Parsa', 'آفلاین · آخرین بازدید دیروز', '۰ بایت امروز', '۱٫۱ گیگابایت در ۳۰ روز'],
    ['Dana', 'آفلاین · آخرین بازدید ۲ روز پیش', '۰ بایت امروز', '۶۰۰ مگابایت در ۳۰ روز'],
    ['Kian', 'آفلاین · آخرین بازدید ۲۵ دقیقه پیش', '۲۱۰ مگابایت امروز', '۵٫۲ گیگابایت در ۳۰ روز'],
  ]
  for (const [name, status, today, month] of rows) {
    const r = row(page, name)
    await expect(r.locator('[data-status]'), name).toContainText(status)
    await expect(r).toContainText(today)
    await expect(r).toContainText(month)
    await expect(r.getByRole('button', { name: 'اشتراک‌گذاری', exact: true })).toBeVisible()
    await expect(r.getByRole('button', { name: `ویرایش ${name}`, exact: true })).toBeVisible()
    await expect(page.getByRole('switch', { name: `${name} روی Node 1`, exact: true })).toBeVisible()
  }
  // the note under the list, and what a switch for the node that is down says
  await expect(page.getByText('نقطهٔ سبز کنار هر کلید نشان می‌دهد اکنون به کجا وصل است.')).toBeVisible()
  await expect(row(page, 'Sara')).toContainText('وقتی برگردد')
})

test('the table lays out from the right, with the unreachable node marked in Persian', async ({ page, isMobile }) => {
  test.skip(isMobile, 'the table is for wide screens')
  await openSubscriptions(page)
  const heads = page.getByRole('columnheader')
  await expect(heads).toHaveText(['اشتراک', 'روشن برای', 'ترافیک', 'محدودیت‌ها', 'کارها', 'Node 1', /^Node 2\s*در دسترس نیست$/])
  const x = await Promise.all([0, 1, 2, 3].map(async (i) => (await heads.nth(i).boundingBox())!.x))
  // the first column is the right-most, and each next one is further left
  for (let i = 1; i < x.length; i += 1) expect(x[i]).toBeLessThan(x[i - 1])
  // the nodes under their heading run the same way
  expect((await heads.nth(6).boundingBox())!.x).toBeLessThan((await heads.nth(5).boundingBox())!.x)
  // the action buttons sit at the left end, the name at the right end
  const table = (await page.getByRole('table').boundingBox())!
  const name = (await row(page, 'Sara').getByRole('rowheader').boundingBox())!
  const edit = (await page.getByRole('button', { name: 'ویرایش Sara', exact: true }).boundingBox())!
  expect(name.x + name.width).toBeGreaterThan(table.x + table.width - 40)
  expect(edit.x).toBeLessThan(table.x + 40)
})

test('a switch knob moves to the end it points at: left when on in Persian, right when on in English', async ({ page }) => {
  await openSubscriptions(page)
  const knobSide = async (name: string, node: string, on: string) => {
    const sw = flipper(page, name, node, on)
    const track = (await sw.locator('> span').boundingBox())!
    const knob = (await sw.locator('> span > span').boundingBox())!
    return knob.x + knob.width / 2 < track.x + track.width / 2 ? 'left' : 'right'
  }
  // Sara is on Node 1; Ali is off on Node 2
  expect(await knobSide('Sara', 'Node 1', 'روی')).toBe('left')
  expect(await knobSide('Ali', 'Node 2', 'روی')).toBe('right')
  await page.getByRole('button', { name: 'English' }).click()
  await expect(page.locator('html')).toHaveAttribute('dir', 'ltr')
  expect(await knobSide('Sara', 'Node 1', 'on')).toBe('right')
  expect(await knobSide('Ali', 'Node 2', 'on')).toBe('left')
})

test('names in Persian are sorted in the Persian alphabet, ahead of Latin names in a Persian page', async ({ page, request }) => {
  for (const name of ['علی', 'پارسا', 'سارا']) expect((await addViaApi(request, name)).ok()).toBe(true)
  await openSubscriptions(page)
  expect(await listed(page, 'Node 1', 'روی')).toEqual(['پارسا', 'سارا', 'علی', 'Ali', 'Dana', 'Kian', 'Mina', 'Parsa', 'Reza', 'Sara'])
  // the language button re-sorts at once: Latin names first in English
  await page.getByRole('button', { name: 'English' }).click()
  expect(await listed(page, 'Node 1')).toEqual(['Ali', 'Dana', 'Kian', 'Mina', 'Parsa', 'Reza', 'Sara', 'پارسا', 'سارا', 'علی'])
})

test('the add dialog in Persian: right to left, a Persian name goes through, its link keeps the name', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await openSubscriptions(page)
  await add(page).click()
  const d = dialog(page, 'افزودن اشتراک')
  expect(await direction(d)).toBe('rtl')
  await expect(d).toContainText('۳ جای خالی. لینک آن به‌محض افزودن آماده است.')
  await expect(d.getByLabel('نام')).toHaveAttribute('dir', 'auto')
  await expect(d).toContainText('برنامه این نام را هم نشان می‌دهد.')
  await expect(d.getByRole('group', { name: 'روی این نودها' }).or(d.locator('fieldset'))).toContainText('روی این نودها')
  await expect(d.getByRole('checkbox', { name: 'Node 1', exact: true })).toBeChecked()
  await expect(d.getByRole('checkbox', { name: 'Node 2 در دسترس نیست' })).not.toBeChecked()
  await expect(d.getByRole('checkbox', { name: 'Node 3 در حال نصب' })).toBeDisabled()
  await expect(d).toContainText('هر نود یک لینک جدا برای آن می‌دهد.')
  // the title is at the right and the close button at the left
  const title = (await d.getByRole('heading').boundingBox())!
  const close = (await d.getByRole('button', { name: 'بستن', exact: true }).boundingBox())!
  expect(close.x).toBeLessThan(title.x)

  await d.getByLabel('نام').fill('نیکا')
  const sent = page.waitForRequest((r) => r.url().endsWith('/api/subscriptions') && r.method() === 'POST')
  await expect(addButton(d)).toHaveText('افزودن و اشتراک‌گذاری')
  await addButton(d).click()
  expect((await sent).postDataJSON()).toEqual({ name: 'نیکا', nodeIds: [1] })
  await expect(toast(page, 'نیکا اضافه شد')).toBeVisible()

  const s = dialog(page, 'اشتراک‌گذاری لینک نیکا')
  expect(await direction(s)).toBe('rtl')
  await expect(s).toContainText('هر کسی که این لینک را داشته باشد می‌تواند از این اشتراک استفاده کند.')
  const shown = await s.locator('[data-link]').innerText()
  expect(shown.endsWith(`#${encodeURIComponent('نیکا')}`)).toBe(true)
  expect(decodeURIComponent(shown.slice(shown.indexOf('#') + 1))).toBe('نیکا')
  // the link and its QR code read the same, whatever the page's direction
  await expect(s.locator('[data-link]')).toHaveAttribute('dir', 'ltr')
  expect(await direction(s.locator('[data-link]'))).toBe('ltr')
  await expect(s.getByRole('img', { name: 'کد QR لینک نیکا روی Node 1' })).toBeVisible()
  expect(await shownQrPath(page)).toBe(qrPath(shown))
  await expect(s).toContainText('هر برنامه‌ای که لینک‌های vless:// را باز کند می‌تواند از آن استفاده کند.')
  await s.getByRole('button', { name: 'کپی لینک' }).click()
  await expect(s.getByRole('button', { name: 'کپی شد' })).toBeVisible()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(shown)
  await s.getByRole('button', { name: 'بستن', exact: true }).click()

  // the new name leads the list, ahead of the Latin ones
  expect((await listed(page, 'Node 1', 'روی'))[0]).toBe('نیکا')
  await expect(tile(page, 'اشتراک‌ها')).toContainText('۸ از ۱۰')
})

test('a name already used, and one that is missing, in Persian', async ({ page }) => {
  await openSubscriptions(page)
  await add(page).click()
  const d = dialog(page, 'افزودن اشتراک')
  await addButton(d).click()
  await expect(d).toContainText('برای اشتراک یک نام وارد کنید.')
  await d.getByLabel('نام').fill('Sara')
  await addButton(d).click()
  await expect(d).toContainText('این نام قبلاً استفاده شده است.')
})

test('edit, delete and replace-link questions in Persian', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Sara', 'Edit', 'fa')
  const rename = dialog(page, 'ویرایش Sara')
  expect(await direction(rename)).toBe('rtl')
  await rename.getByLabel('نام').fill('Sarah')
  await rename.getByRole('button', { name: 'ذخیره' }).click()
  await expect(toast(page, 'نام به Sarah تغییر کرد')).toBeVisible()

  // deleting starts in the Edit dialog
  await pick(page, 'Sarah', 'Delete subscription', 'fa')
  const del = page.getByRole('alertdialog', { name: 'حذف Sarah؟' })
  await expect(del).toContainText('اشتراک از همه‌جا حذف می‌شود و لینک‌هایش از کار می‌افتند.')
  await expect(del.getByRole('button', { name: 'حذف اشتراک' })).toHaveClass(/bg-bad/)
  await del.getByRole('button', { name: 'لغو' }).click()

  await share(page, 'Ali').click()
  await dialog(page, 'اشتراک‌گذاری لینک Ali').getByRole('button', { name: 'جایگزینی این لینک…' }).click()
  const replace = page.getByRole('alertdialog', { name: 'لینک Ali جایگزین شود؟' })
  await expect(replace).toContainText('لینک قدیمی از کار می‌افتد. لینک تازه را باید دوباره برای آن‌ها بفرستید.')
  await replace.getByRole('button', { name: 'جایگزینی لینک' }).click()
  await expect(toast(page, 'لینک تازه‌ای برای Ali ساخته شد')).toBeVisible()
})

test('the node picker, with a node that is down, and the question that names the node, in Persian', async ({ page }) => {
  await openSubscriptions(page)
  await share(page, 'Sara').click()
  // two nodes, two links: the title says links, and each has a tab
  const d = dialog(page, 'اشتراک‌گذاری لینک‌های Sara')
  await expect(d).toContainText('برای هر جا یک لینک.')
  await expect(placeTabs(d)).toHaveText(['Node 1', 'Node 2 در دسترس نیست'])
  // the first node is at the right end
  const one = (await d.getByRole('tab', { name: 'Node 1', exact: true }).boundingBox())!
  const two = (await d.getByRole('tab', { name: /Node 2/ }).boundingBox())!
  expect(one.x).toBeGreaterThan(two.x)
  // the rest of the dialog's words
  for (const name of ['نمایش', 'کپی لینک', 'ذخیرهٔ تصویر QR', 'کپی هر ۲ لینک']) await expect(d.getByRole('button', { name, exact: true })).toBeVisible()
  await d.getByRole('button', { name: 'جایگزینی این لینک…' }).click()
  await expect(page.getByRole('alertdialog', { name: 'لینک Sara جایگزین شود؟' })).toContainText('این فقط لینک Node 1 را تغییر می‌دهد.')
})

test('toasts of the switches in Persian, with Undo', async ({ page }) => {
  await openSubscriptions(page)
  await flipper(page, 'Sara', 'Node 1', 'روی').click()
  const off = toast(page, 'Sara روی Node 1 خاموش شد')
  await expect(off).toBeVisible()
  await off.getByRole('button', { name: 'واگرد' }).click()
  await expect(toast(page, 'Sara روی Node 1 روشن شد')).toBeVisible()
  await flipper(page, 'Ali', 'Node 2', 'روی').click()
  await expect(toast(page, 'Ali روی Node 2 روشن شد. وقتی نود برگردد اعمال می‌شود.')).toBeVisible()
})

test('the limit, in Persian: the count, the badge, the tooltip and the error', async ({ page, request }) => {
  await fillToLimit(request, 9)
  await openSubscriptions(page)
  await add(page).click()
  const d = dialog(page, 'افزودن اشتراک')
  await expect(d).toContainText('۱ جای خالی.')
  await d.getByLabel('نام').fill('نیکا')
  expect((await addViaApi(request, 'Taken')).ok()).toBe(true)
  await addButton(d).click()
  await expect(d.getByRole('alert')).toHaveText('این روتر به حداکثر تعداد اشتراک رسیده است. برای افزودن اشتراک تازه یکی را حذف کنید.')
  await d.getByRole('button', { name: 'لغو' }).click()

  await page.reload()
  await expect(page.getByText('۱۰ از ۱۰ اشتراک')).toBeVisible()
  await expect(page.getByText('به حد مجاز رسید')).toBeVisible()
  await add(page).focus()
  const tip = page.getByRole('tooltip')
  await expect(tip).toContainText('این روتر حداکثر ۱۰ اشتراک نگه می‌دارد. هر اشتراک روی هر نودی که روشن است یک کلاینت است. برای افزودن اشتراک تازه یکی را حذف کنید.')
  // it hangs from the button's end, which is the left in a right-to-left page
  const box = (await tip.boundingBox())!
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width)
})

test('the empty state, in Persian', async ({ page, request }) => {
  for (let id = 1; id <= 7; id += 1) expect((await request.delete(`/api/subscriptions/${id}`, { headers: PAGE })).ok()).toBe(true)
  await page.goto('/#/subscriptions')
  await expect(page.getByRole('heading', { name: 'هنوز اشتراکی نیست' })).toBeVisible()
  await expect(page.getByText('اشتراک یک نام است با یک لینک برای هر جایی که روی آن روشن است. یکی اضافه کنید و لینکش را به اشتراک بگذارید.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'افزودن اولین اشتراک' })).toBeVisible()
})

test("a node's tab in Persian", async ({ page }) => {
  await openNodeSubscriptions(page, 1)
  await expect(tile(page, 'روی این نود')).toHaveText('روی این نود ۶ از ۷ Kian خاموش است', { useInnerText: true })
  await expect(tile(page, 'اکنون آنلاین')).toHaveText('اکنون آنلاین ۳ با ۹ اتصال', { useInnerText: true })
  await expect(tile(page, 'امروز')).toHaveText(`امروز ۴٫۱ گیگابایت ↑ ${LRM}۱٫۰ گیگابایت · ↓ ${LRM}۳٫۱ گیگابایت`, { useInnerText: true })
  await expect(tile(page, '۳۰ روز اخیر')).toHaveText(`۳۰ روز اخیر ۱۲۲ گیگابایت ↑ ${LRM}۲۸٫۰ گیگابایت · ↓ ${LRM}۹۴٫۰ گیگابایت`, { useInnerText: true })
  await expect(page.getByRole('link', { name: 'همهٔ اشتراک‌ها' })).toBeVisible()
  await expect(add(page)).toBeVisible()
  await expect(row(page, 'Ali').locator('[data-status]')).toHaveText('آنلاین · ۲ اتصال')
  await expect(row(page, 'Sara').locator('[data-status]')).toHaveText('آنلاین · ۳ اتصال')
  await expect(row(page, 'Dana').locator('[data-status]')).toHaveText('آفلاین · آخرین بازدید ۲ روز پیش')
  await expect(row(page, 'Kian').locator('[data-status]')).toHaveText('خاموش')
  await expect(row(page, 'Kian').getByRole('button', { name: 'اشتراک‌گذاری', exact: true })).toHaveCount(0)
  await expect(page.getByText('۷ از ۷ نمایش داده می‌شود · مرتب‌شده بر اساس نام')).toBeVisible()
  await expect(flipper(page, 'Kian', 'Node 1', 'روی')).toHaveAttribute('aria-checked', 'false')
  // one that is off here has Edit and nothing else, and the dialog it opens holds delete and reset
  await expect(row(page, 'Kian').getByRole('button')).toHaveCount(1)
  await pick(page, 'Kian', 'Edit', 'fa')
  const d = dialog(page, 'ویرایش Kian')
  await expect(d.getByRole('button', { name: 'صفر کردن مصرف…' })).toBeVisible()
  await expect(d.getByRole('button', { name: 'حذف اشتراک…' })).toBeVisible()
})
