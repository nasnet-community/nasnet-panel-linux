import type { Page } from '@playwright/test'
import { attention, dialog, expect, reset, test, toast } from './fixtures'
import { confirmDialog, finishJob, menuItem, openMenu, openNode, overflow, panel, row, setNode, verdictChip } from './node-helpers'

// no Latin digit in the prose; machine values (versions, ports, hosts) keep theirs
const LATIN_DIGITS = /[0-9]/

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
})

const text = (page: Page, selector: string) => page.locator(selector).first().innerText()

test.describe('the node page in Persian', () => {
  test('lays out from the right, with the words, digits and buttons of the language', async ({ page }) => {
    await openNode(page, 1)
    const html = page.locator('html')
    await expect(html).toHaveAttribute('lang', 'fa')
    await expect(html).toHaveAttribute('dir', 'rtl')
    await expect(page.getByText('در حال کار', { exact: true })).toBeVisible()
    // machine values keep their Latin digits and read left to right; the uptime and the words are Persian
    await expect(page.getByText('203.0.113.24', { exact: true })).toHaveAttribute('dir', 'ltr')
    await expect(page.getByText('پورت 443', { exact: true })).toBeVisible()
    await expect(page.getByText('agent 1.3.0', { exact: true })).toHaveAttribute('dir', 'ltr')
    await expect(page.getByText('Xray 26.10.21', { exact: true })).toHaveAttribute('dir', 'ltr')
    await expect(page.getByText(/^[۰-۹]+ روز و [۰-۹]+ ساعت روشن$/)).toBeVisible()
    await expect(page.getByRole('button', { name: 'بررسی به‌روزرسانی' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'اقدام‌های بیشتر' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'همهٔ نودها' })).toBeVisible()
    await expect(page.getByRole('group', { name: 'اقدام‌های نود' })).toBeVisible()

    // the name and the badge: the name first, from the right
    const name = (await page.getByRole('heading', { level: 1, name: 'Node 1' }).boundingBox())!
    const badge = (await page.getByText('در حال کار', { exact: true }).boundingBox())!
    expect(name.x).toBeGreaterThan(badge.x)
    // the back arrow points the way back, which is right
    const arrow = page.getByRole('link', { name: 'همهٔ نودها' }).locator('svg')
    expect(await arrow.evaluate((el) => getComputedStyle(el).transform)).toBe('matrix(-1, 0, 0, 1, 0, 0)')
    // the actions sit at the other end of the card from the name
    const check = (await page.getByRole('button', { name: 'بررسی به‌روزرسانی' }).boundingBox())!
    expect(check.x).toBeLessThan(name.x)
  })

  test('moves between the tabs with the arrow keys the way the page reads: the next tab is on the left', async ({ page }) => {
    await openNode(page, 1)
    const tabs = await Promise.all(['اشتراک‌ها', 'وضعیت', 'تنظیمات'].map((n) => page.getByRole('tab', { name: n }).boundingBox()))
    expect(tabs[0]!.x).toBeGreaterThan(tabs[1]!.x)
    expect(tabs[1]!.x).toBeGreaterThan(tabs[2]!.x)
    await page.getByRole('tab', { name: 'اشتراک‌ها' }).focus()
    await page.keyboard.press('ArrowLeft')
    await expect(page).toHaveURL(/#\/nodes\/1\/status$/)
    await expect(page.getByRole('tab', { name: 'وضعیت' })).toBeFocused()
    await page.keyboard.press('ArrowRight')
    await expect(page).toHaveURL(/#\/nodes\/1$/)
  })

  test('says what is wrong, in Persian, with a way out', async ({ page, request }) => {
    // slow enough to read a step off the job's dialog
    await reset(request, { pace: 2 })
    await setNode(request, { id: 1, xrayVersion: '26.10.18' })
    await openNode(page, 1)
    await expect(page.getByText('نیاز به توجه', { exact: true }).first()).toBeVisible()
    const note = attention(page)
    await expect(note).toContainText('تونل خاموش است')
    await expect(note).toContainText('این نود Xray نسخهٔ 26.10.18 را اجرا می‌کند.')
    await expect(note).toContainText('نسخهٔ 26.10.21 که این روتر استفاده می‌کند')
    await expect(page.getByRole('button', { name: 'به‌روزرسانی به 26.10.21', exact: true })).toBeVisible()
    await note.getByRole('button', { name: 'به‌روزرسانی Xray' }).click()
    const confirm = confirmDialog(page, 'Xray در Node 1 به نسخهٔ 26.10.21 به‌روزرسانی شود؟')
    // the count it turns on is in Persian digits too
    await expect(confirm.getByText('۹', { exact: true })).toBeVisible()
    await expect(confirm).toContainText('اتصال باز قطع می‌شود. برنامه‌ها در چند ثانیه خودشان دوباره وصل می‌شوند.')
    await confirm.getByRole('button', { name: 'به‌روزرسانی Xray' }).click()
    const job = dialog(page, /^(در حال به‌روزرسانی Xray|Xray به‌روزرسانی شد)/)
    await expect(job).toBeVisible()
    for (const step of ['دریافت Xray 26.10.21', 'ارسال Xray 26.10.21', 'بررسی Xray 26.10.21']) await expect(job.getByText(step)).toBeVisible()
    await expect(job).toContainText(/مرحله [۰-۹] از ۳/)
    await expect(dialog(page, /^Xray به‌روزرسانی شد/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'Xray به‌روزرسانی شد · Node 1')).toBeVisible()
    await finishJob(page, /^Xray به‌روزرسانی شد/)
    await expect(attention(page)).toHaveCount(0)
  })

  test('says a node can not be reached, with the time in Persian digits, and what is off', async ({ page }) => {
    await openNode(page, 2)
    const note = attention(page)
    await expect(note).toContainText('روتر به این نود نمی‌رسد')
    await expect(note).toContainText(/آخرین دیده‌شدن: ([۰-۹]{2}:[۰-۹]{2}|[۰-۹]+ \S+)، ۲۲ دقیقه پیش\./)
    await expect(note.getByRole('button', { name: 'بررسی دوباره' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'بررسی به‌روزرسانی' })).toBeDisabled()
    await openMenu(page)
    await expect(menuItem(page, 'به‌روزرسانی ایجنت')).toHaveAttribute('aria-disabled', 'true')
    await expect(menuItem(page, 'به‌روزرسانی ایجنت')).toContainText('روتر باید به این نود دسترسی داشته باشد')
    await expect(menuItem(page, 'حذف نود…')).not.toHaveAttribute('aria-disabled', 'true')
    // the menu opens under the button, from its end: the left in this language
    const menu = (await page.getByRole('menu').boundingBox())!
    const trigger = (await page.getByRole('button', { name: 'اقدام‌های بیشتر' }).boundingBox())!
    expect(menu.x).toBeLessThan(trigger.x + trigger.width)
    expect(menu.x).toBeGreaterThanOrEqual(0)
  })
})

test.describe('the Status tab in Persian', () => {
  test('reads in Persian, in Persian digits', async ({ page }) => {
    await openNode(page, 1, 'status')
    await expect(page.getByRole('heading', { level: 2, name: 'همه‌چیز کار می‌کند' })).toBeVisible()
    await expect(page.getByText(/^آخرین گزارش [۰-۹]+ ثانیه پیش$/)).toBeVisible()
    await expect(verdictChip(page, 'پورتال', 'در حال گوش دادن')).toBeVisible()
    await expect(verdictChip(page, 'تونل', /[۰-۹]+ روز و [۰-۹]+ ساعت برقرار/)).toBeVisible()
    await expect(verdictChip(page, 'بریج', 'در حال اجرا')).toBeVisible()
    await expect(verdictChip(page, 'وی پی ان', 'وایرگارد')).toBeVisible()
    // none of the verdict's words has a Latin digit
    expect(await text(page, 'main section')).not.toMatch(LATIN_DIGITS)

    const agent = panel(page, 'ایجنت')
    await expect(row(agent, 'نسخه')).toContainText('1.3.0')
    await expect(row(agent, 'مدت اجرا')).toContainText(/[۰-۹]+ روز و [۰-۹]+ ساعت/)
    await expect(row(agent, 'حافظه')).toContainText('۲۲٫۳ مگابایت')
    await expect(row(agent, 'تنظیمات')).toContainText('اعمال شده')
    const tunnel = panel(page, 'تونل به این روتر')
    await expect(row(tunnel, 'اتصال‌ها')).toContainText('۱ از ۱ برقرار')
    await expect(row(tunnel, 'زمان رفت‌وبرگشت')).toContainText('۲۳ میلی‌ثانیه')
    await expect(row(tunnel, 'اتصال‌های فعلی')).toContainText('۹')
    await expect(row(tunnel, 'دانلود')).toContainText('۱٫۹ ترابایت')
    await expect(row(tunnel, 'آپلود')).toContainText('۴۱۲ گیگابایت')
    await expect(row(tunnel, 'اتصال به اینترنت برقرار نشد')).toContainText('۳')
    await expect(row(tunnel, 'اتصال به اینترنت برقرار نشد')).toContainText('روتر نتوانست اتصالی باز کند. گاه‌به‌گاه چند مورد عادی است.')
    const cert = panel(page, 'گواهی')
    await expect(row(cert, 'اعتبار باقی‌مانده')).toContainText(/[۰-۹]+ روز/)
    await expect(row(cert, 'معتبر تا')).toContainText(/[۰-۹]+ \S+ ۱۴۰[۶۷]/)
    await expect(row(cert, 'تمدید')).toContainText('۳۰ روز پیش از پایان')
    const events = panel(page, 'رویدادهای اخیر').getByRole('listitem')
    await expect(events.nth(0)).toContainText('تونل برقرار شد')
    await expect(events.nth(1)).toContainText('ایجنت به‌روزرسانی شد')
    await expect(events.nth(2)).toContainText('گواهی تمدید شد')
    await expect(events.nth(2)).toContainText('۶ روز پیش')
  })

  test('draws a chart that is still a timeline, with its words and numbers in Persian', async ({ page, isMobile }) => {
    await openNode(page, 1, 'status')
    const traffic = panel(page, 'ترافیک، ۱۵ دقیقهٔ اخیر')
    await expect(traffic).toBeVisible()
    await expect(traffic).toContainText(/دانلود\s*[۰-۹٫]+ (کیلوبایت|مگابایت) بر ثانیه/)
    await expect(traffic).toContainText(/آپلود\s*[۰-۹٫]+ (کیلوبایت|مگابایت) بر ثانیه/)

    const chart = page.getByRole('img', { name: /^دانلود اکنون .+، حداکثر .+ در ۱۵ دقیقهٔ اخیر\. آپلود اکنون .+\.$/ })
    await expect(chart).toBeVisible()
    // the timeline is placed from the left whatever the page's direction: oldest at the left, now at the right
    const oldestLabel = chart.getByText('۱۵ دقیقه پیش', { exact: true })
    const oldest = (await oldestLabel.boundingBox())!
    const now = (await chart.getByText('اکنون', { exact: true }).boundingBox())!
    expect(oldest.x).toBeLessThan(now.x)
    // while the words read the way the page does: in a left-to-right box the digits and the words of
    // "۱۵ دقیقه پیش" would come out in the wrong order
    expect(await oldestLabel.evaluate((el) => getComputedStyle(el).direction)).toBe('rtl')
    if (!isMobile) {
      await expect(chart.getByText('۱۰ دقیقه', { exact: true })).toBeVisible()
      await expect(chart.getByText('۵ دقیقه', { exact: true })).toBeVisible()
    }
    // the axis is at the left of the lines, with the unit written out and in one line
    const axis = chart.locator(':scope > div').first().locator('span')
    await expect(axis.nth(0)).toHaveText(/^[۱۲۵] مگابایت بر ثانیه$/)
    expect(await axis.nth(0).evaluate((el) => getComputedStyle(el).direction)).toBe('rtl')
    await expect(axis.nth(2)).toHaveText('۰')
    const plot = (await chart.locator('svg').boundingBox())!
    const top = (await axis.nth(0).boundingBox())!
    expect(top.x + top.width).toBeLessThanOrEqual(plot.x)
    expect(top.height).toBeLessThan(20)
    // and the lines are drawn the same way: the first point at the left edge, the newest near the right
    const down = (await chart.locator('svg path').nth(1).getAttribute('d'))!
    expect(down.startsWith('M0.0,')).toBe(true)
    expect(Number(/L([\d.]+),[\d.]+$/.exec(down)![1])).toBeGreaterThan(560)
  })

  test('says the router can not reach it, in Persian, with dashes and an empty chart', async ({ page }) => {
    await openNode(page, 2, 'status')
    await expect(page.getByRole('heading', { level: 2, name: 'روتر به این نود نمی‌رسد' })).toBeVisible()
    await expect(page.getByText('آخرین گزارش ۲۲ دقیقه پیش. روتر به این نود نمی‌رسد، پس این عددها قدیمی‌اند.')).toBeVisible()
    for (const label of ['پورتال', 'تونل', 'بریج', 'وی پی ان']) await expect(verdictChip(page, label, '–')).toBeVisible()
    await expect(page.getByText('در ۱۵ دقیقهٔ اخیر داده‌ای ثبت نشده است.')).toBeVisible()
  })
})

test.describe('the Settings tab and its dialogs in Persian', () => {
  test('shows the cards and checks a handshake target in Persian', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await expect(panel(page, 'اتصال')).toContainText('پورت 443')
    const target = panel(page, 'مقصد هندشیک')
    await expect(target).toContainText('www.example.com:443')
    await expect(target).toContainText('کار می‌کند')
    await expect(target).toContainText(/بررسی [۰-۹]+ ساعت پیش/)
    // a key is Latin and reads left to right, with its copy button
    const key = panel(page, 'کلیدها').locator('code').first()
    await expect(key).toHaveAttribute('dir', 'ltr')
    await expect(page.getByRole('button', { name: 'کپی کلید عمومی' })).toBeVisible()
    await expect(panel(page, 'گواهی')).toContainText(/معتبر تا [۰-۹]+ \S+ ۱۴۰[۶۷]/)
    await expect(panel(page, 'گواهی')).toContainText('خودکار ۳۰ روز پیش از پایان تمدید می‌شود')

    await page.getByRole('button', { name: 'تغییر مقصد هندشیک…' }).click()
    const d = dialog(page, 'تغییر مقصد هندشیک')
    await expect(d).toContainText('یک سایت HTTPS که از TLS 1.3 پشتیبانی می‌کند. نود پیش از هر تغییر آن را بررسی می‌کند.')
    // what is typed is a site, which reads left to right
    const field = d.getByRole('textbox', { name: 'مقصد هندشیک', exact: true })
    await expect(field).toHaveAttribute('dir', 'ltr')
    await field.fill('www.cloudflare.com')
    await d.getByRole('button', { name: 'بررسی', exact: true }).click()
    await expect(d.getByText('کار می‌کند', { exact: true })).toBeVisible()
    await expect(d).toContainText('TLS 1.3 · X25519 · هندشیک ۴٫۲ کیلوبایت، بررسی‌شده از این نود')
    await expect(d).toContainText('این کار همهٔ لینک‌های این نود را عوض می‌کند.')
    await d.getByRole('button', { name: 'ذخیره و اعمال' }).click()
    await expect(dialog(page, /^(در حال اعمال تنظیمات|تنظیمات اعمال شد)/)).toBeVisible()
    await expect(dialog(page, /^تنظیمات اعمال شد/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'تنظیمات اعمال شد · Node 1')).toBeVisible()
    await finishJob(page, /^تنظیمات اعمال شد/)
    await expect(panel(page, 'مقصد هندشیک')).toContainText('www.cloudflare.com:443')
  })

  test('says what is wrong with a port, and a failing site, in Persian', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await page.getByRole('button', { name: 'تغییر پورت…' }).click()
    const d = dialog(page, 'تغییر پورت ورودی')
    await expect(d).toContainText(/Node 1 اکنون از پورت 443 استفاده می‌کند\./)
    await d.getByLabel('پورت ورودی').fill('22')
    await expect(d).toContainText('پورت ورودی عددی از ۱ تا ۶۵۵۳۵ است و نباید با پورت SSH یکی باشد.')
    await expect(d.getByRole('button', { name: 'ذخیره و اعمال' })).toBeDisabled()
    await d.getByLabel('پورت ورودی').fill('443')
    await expect(d).toContainText('این همان پورتی است که اکنون استفاده می‌شود.')
    await d.getByRole('button', { name: 'لغو' }).click()

    await page.getByRole('button', { name: 'تغییر مقصد هندشیک…' }).click()
    const h = dialog(page, 'تغییر مقصد هندشیک')
    await h.getByRole('textbox', { name: 'مقصد هندشیک', exact: true }).fill('fail.example.com')
    await h.getByRole('button', { name: 'بررسی', exact: true }).click()
    await expect(h.getByText('ناموفق', { exact: true })).toBeVisible()
    // the hub's reason comes as it is, in its own language, and reads on its own terms
    await expect(h.getByRole('alert')).toContainText('did not complete a TLS 1.3 handshake')
    await expect(h.getByRole('alert')).toHaveAttribute('dir', 'auto')
    await expect(h.getByRole('button', { name: 'ذخیره و اعمال' })).toBeDisabled()
  })

  test('asks before rotating keys and renewing, counting in Persian digits', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await page.getByRole('button', { name: 'تعویض کلیدها…' }).click()
    const confirm = confirmDialog(page, 'کلیدهای Node 1 تعویض شوند؟')
    await expect(confirm.getByText('۶', { exact: true })).toBeVisible()
    await expect(confirm).toContainText('اشتراک لینک تازه می‌گیرد.')
    await confirm.getByRole('button', { name: 'لغو' }).click()
    await page.getByRole('button', { name: 'تمدید اکنون' }).click()
    const renew = confirmDialog(page, 'گواهی Node 1 تمدید شود؟')
    await expect(renew).toContainText('نود از این روتر گواهی تازه‌ای با اعتبار ۱۲ ماه می‌گیرد.')
  })
})

test.describe('the router\'s Settings tab in Persian', () => {
  test('reads in Persian, with the routes and the backup', async ({ page }) => {
    await page.goto('/#/settings')
    await expect(panel(page, 'ورودی')).toBeVisible()
    const entry = panel(page, 'ورودی')
    await expect(row(entry, 'آپ‌لینک داخلی')).toContainText('آنلاین')
    await expect(row(entry, 'وی پی ان')).toContainText('وایرگارد · آنلاین')
    await expect(row(entry, 'RouterOS')).toContainText('7.24.5')
    await expect(row(entry, 'RouterOS')).toContainText('به نسخهٔ 7.20 یا جدیدتر نیاز دارد.')
    await expect(row(entry, 'بررسی')).toContainText('۲ دقیقه پیش')
    const routes = panel(page, 'مسیرهای روتر')
    await expect(routes).toContainText('هر آدرس قاعدهٔ خودش را دارد. اگر مسیری نباشد، آن ترافیک مسدود می‌شود.')
    await expect(routes.getByText('دانلودها').filter({ visible: true })).toBeVisible()
    await expect(routes.getByText('نودها و تونل').filter({ visible: true })).toBeVisible()
    await expect(routes.getByText('آماده', { exact: true }).filter({ visible: true })).toHaveCount(3)
    const about = panel(page, 'درباره')
    await expect(about).toContainText('این افزونه را از صفحهٔ افزونه‌ها در نسنت پنل به‌روزرسانی کنید.')
    await expect(about.getByRole('link', { name: 'کد منبع' })).toBeVisible()
    await expect(about.getByRole('link', { name: 'اطلاعیه‌های مجوز' })).toBeVisible()
    // the version, commit and licence are what they are in any language
    await expect(row(about, 'نسخه')).toContainText('Remote Nodes 0.1.0')
    await expect(row(about, 'مجوز')).toContainText('AGPL-3.0')
  })

  test('opens the notices in a tab of their own: the hub serves the same text in any language', async ({ page, context }) => {
    await page.goto('/#/settings')
    const notices = panel(page, 'درباره').getByRole('link', { name: 'اطلاعیه‌های مجوز' })
    await expect(notices).toHaveAttribute('href', 'licenses')
    await expect(notices).toHaveAttribute('target', '_blank')
    await expect(notices).toHaveAttribute('rel', /noopener noreferrer/)
    const [tab] = await Promise.all([context.waitForEvent('page'), notices.click()])
    await expect(tab).toHaveURL(/\/licenses$/)
    await expect(tab.locator('body')).toContainText('Sample licence text for NOTICES')
    expect(await tab.evaluate(() => document.contentType)).toBe('text/plain')
    await expect(page).toHaveURL(/#\/settings$/)
    await tab.close()
  })

  test('exports and imports a backup, with its warnings and errors in Persian', async ({ page, request }) => {
    await page.goto('/#/settings')
    const backup = panel(page, 'پشتیبان')
    await expect(backup).toContainText('هنوز پشتیبانی نیست.')
    await backup.getByRole('button', { name: 'گرفتن پشتیبان' }).click()
    await expect(backup.getByText('رمز عبور را وارد کنید.')).toBeVisible()
    await page.getByLabel('رمز عبور').fill('correct horse')
    const download = page.waitForEvent('download')
    await backup.getByRole('button', { name: 'گرفتن پشتیبان' }).click()
    const file = await download
    await expect(toast(page, `پشتیبان با نام ${file.suggestedFilename()} ذخیره شد`)).toBeVisible()
    await expect(backup).toContainText(/آخرین پشتیبان (همین الان|[۰-۹]+ دقیقه پیش)\./)

    // a wrong passphrase on a fresh install
    const chunks: Buffer[] = []
    for await (const c of (await file.createReadStream()) as AsyncIterable<Buffer>) chunks.push(c)
    await reset(request, { fresh: true })
    await page.reload()
    await page.getByRole('button', { name: 'بازیابی…' }).click()
    const d = dialog(page, 'بازیابی پشتیبان')
    await d.getByLabel('فایل پشتیبان').setInputFiles({ name: file.suggestedFilename(), mimeType: 'application/octet-stream', buffer: Buffer.concat(chunks) })
    await d.getByLabel('رمز عبور').fill('wrong')
    await d.getByRole('button', { name: 'بازیابی پشتیبان' }).click()
    await expect(d.getByRole('alert')).toContainText('این رمز عبور، پشتیبان را باز نمی‌کند.')
    await d.getByLabel('رمز عبور').fill('correct horse')
    await d.getByRole('button', { name: 'بازیابی پشتیبان' }).click()
    await expect(dialog(page, '۳ نود بازیابی شد')).toBeVisible()
  })
})

test.describe('removing a node in Persian', () => {
  test('asks for the login first, and says why, in Persian', async ({ page }) => {
    await openNode(page, 2)
    await openMenu(page)
    await menuItem(page, 'حذف نود…').click()
    const d = dialog(page, 'Node 2 حذف شود؟')
    await expect(d).toContainText('ایجنت و Xray از نود پاک می‌شوند و لینک همهٔ اشتراک‌ها به آن از کار می‌افتد.')
    // no confirmation in between: the login form is what opens, with the reason for it
    await expect(d.getByText('حذف ایجنت و Xray از سرور به دسترسی SSH نیاز دارد. برای این کار وارد شوید.')).toBeVisible()
    await expect(d.getByRole('button', { name: 'ورود و حذف' })).toBeVisible()
    await expect(d.getByText('ورود ندارید؟ بعداً ایجنت را دستی پاک کنید.')).toBeVisible()
    await d.getByRole('button', { name: 'فراموش کردن بدون حذف' }).click()
    // one short step: the job may be over before its dialog is up
    await expect(dialog(page, /^(در حال حذف نود|نود حذف شد)/)).toBeVisible()
    await expect(dialog(page, /^نود حذف شد/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'نود حذف شد · Node 2')).toBeVisible()
  })
})

test('no page of the node or the router\'s settings scrolls sideways in Persian', async ({ page, request }) => {
  for (const path of ['/#/nodes/1', '/#/nodes/1/status', '/#/nodes/1/settings', '/#/nodes/2/status', '/#/nodes/3', '/#/settings']) {
    await page.goto(path)
    await page.reload()
    await expect(page.getByRole('tablist').first()).toBeVisible()
    expect(await overflow(page), path).toBeLessThanOrEqual(0)
  }
  await setNode(request, { id: 1, xrayVersion: '26.10.18' })
  await page.goto('/#/nodes/1/status')
  await page.reload()
  await expect(attention(page)).toBeVisible()
  expect(await overflow(page)).toBeLessThanOrEqual(0)
})
