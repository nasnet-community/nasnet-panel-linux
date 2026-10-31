import { card, chip, dialog, expect, fillAddNode, openAddNode, reset, test, toast } from './fixtures'

test('the language button switches the whole page to Persian: right to left, Persian digits and words', async ({ page }) => {
  await page.goto('/')
  await expect(card(page, 'Node 1')).toBeVisible()
  const html = page.locator('html')
  await expect(html).toHaveAttribute('lang', 'en')
  await expect(html).toHaveAttribute('dir', 'ltr')

  await page.getByRole('button', { name: 'فارسی' }).click()
  await expect(html).toHaveAttribute('lang', 'fa')
  await expect(html).toHaveAttribute('dir', 'rtl')
  expect(await page.evaluate(() => localStorage.getItem('remote-nodes.language'))).toBe('fa')

  // the entry card
  await expect(page.getByRole('heading', { name: 'ورودی روشن است' })).toBeVisible()
  await expect(page.getByText('بررسی ۲ دقیقه پیش')).toBeVisible()
  await expect(chip(page, 'آپ‌لینک داخلی', 'آنلاین')).toBeVisible()
  await expect(chip(page, 'وی پی ان', 'وایرگارد')).toBeVisible()
  await expect(chip(page, 'بریج', 'در حال اجرا')).toBeVisible()
  // tabs and the nodes header
  await expect(page.getByRole('tab', { name: 'نودها' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('tab', { name: 'اشتراک‌ها' })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'تنظیمات' })).toBeVisible()
  await expect(page.getByText('هر نود سروری است که اشتراک‌ها به آن وصل می‌شوند.')).toBeVisible()
  await expect(page.getByText('۳ از ۳ نود')).toBeVisible()
  await expect(page.getByText('به حد مجاز رسید')).toBeVisible()

  // the cards: digits, sizes and times in Persian; versions and addresses stay Latin
  const one = card(page, 'Node 1')
  await expect(one).toContainText('در حال کار')
  await expect(one).toContainText('۳ از ۶ آنلاین')
  await expect(one).toContainText('۴٫۱ گیگابایت امروز')
  await expect(one).toContainText('203.0.113.24')
  await expect(one).toContainText('agent 1.3.0 · Xray 26.10.21')
  const two = card(page, 'Node 2')
  await expect(two).toContainText('در دسترس نیست')
  await expect(two).toContainText('۴ اشتراک')
  await expect(two).toContainText('۰ بایت امروز')
  await expect(two).toContainText(/از (ساعت [۰-۹]{2}:[۰-۹]{2}|[۰-۹]+ \S+) در دسترس نیست \(۲۲ دقیقه پیش\)/)
  await expect(two).toContainText('به‌روزرسانی موجود است')
  const three = card(page, 'Node 3')
  await expect(three).toContainText('در حال نصب')
  await expect(three).toContainText('مرحله ۱۰ از ۱۵ · ارسال Xray 26.10.21 · ۶۲٪')
  await expect(three).toContainText('شروع شد ۱ دقیقه پیش')
  await expect(page.getByRole('button', { name: 'در حال نصب Node 3' })).toBeVisible()
  // no Latin digit is left in prose
  await expect(page.getByRole('heading', { name: 'ورودی روشن است' }).locator('xpath=..')).not.toContainText(/[0-9]/)

  // the chevron that points on mirrors, and the page lays out from the right
  const arrow = one.locator('svg').last()
  expect(await arrow.evaluate((el) => getComputedStyle(el).transform)).toBe('matrix(-1, 0, 0, 1, 0, 0)')
  const tabs = await Promise.all(['نودها', 'اشتراک‌ها', 'تنظیمات'].map((name) => page.getByRole('tab', { name }).boundingBox()))
  expect(tabs[0]!.x).toBeGreaterThan(tabs[1]!.x)
  expect(tabs[1]!.x).toBeGreaterThan(tabs[2]!.x)
  const brand = (await page.getByRole('link', { name: /Remote Nodes/ }).boundingBox())!
  expect(brand.x).toBeGreaterThan(page.viewportSize()!.width / 2)
  // Persian text uses the Persian font when the device has it, else the system's
  expect(await page.evaluate(() => getComputedStyle(document.body).fontFamily)).toMatch(/^Vazirmatn/)

  // it is remembered, and the button now offers English
  await page.reload()
  await expect(html).toHaveAttribute('dir', 'rtl')
  await expect(page.getByRole('heading', { name: 'ورودی روشن است' })).toBeVisible()
  await page.getByRole('button', { name: 'English' }).click()
  await expect(html).toHaveAttribute('lang', 'en')
  await expect(html).toHaveAttribute('dir', 'ltr')
  await expect(page.getByRole('heading', { name: 'Entry is on' })).toBeVisible()
  await expect(card(page, 'Node 1')).toContainText('3 of 6 online')
  expect(await arrow.evaluate((el) => getComputedStyle(el).transform)).toBe('none')
})

test('the first-run page, the limit tooltip and the empty state in Persian', async ({ page, request }) => {
  await reset(request, { fresh: true })
  await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'ورودی خاموش است' })).toBeVisible()
  await expect(chip(page, 'آپ‌لینک داخلی', 'ندارد')).toBeVisible()
  await expect(page.getByRole('region', { name: 'نیاز به توجه' })).toContainText('این روتر آپ‌لینک داخلی ندارد')
  await expect(page.getByRole('region', { name: 'نیاز به توجه' })).toContainText('این روتر وی پی ان ندارد')
  await expect(page.getByRole('link', { name: 'باز کردن راه‌اندازی' })).toHaveCount(2)
  await expect(page.getByRole('heading', { name: 'هنوز نودی نیست' })).toBeVisible()
  await expect(page.getByText('۰ از ۳ نود')).toBeVisible()
  await expect(page.getByRole('button', { name: 'افزودن اولین نود' })).toBeVisible()
})

test('the limit tooltip in Persian', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
  await page.goto('/')
  const add = page.getByRole('button', { name: 'افزودن نود' })
  await expect(page.getByText('۳ از ۳ نود')).toBeVisible()
  await add.focus()
  const tip = page.getByRole('tooltip')
  await expect(tip).toBeVisible()
  await expect(tip).toContainText('این روتر حداکثر ۳ نود اجرا می‌کند. تونل هر نود از حافظهٔ روتر استفاده می‌کند. برای افزودن نود تازه یکی را حذف کنید.')
  // it hangs from the button's end, which is the left in a right-to-left page
  const box = (await tip.boundingBox())!
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width)
})

test('the add dialog and its install job in Persian', async ({ page, request }) => {
  await reset(request, { nodes: 2 })
  await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
  await openAddNode(page, 'افزودن نود')
  const form = dialog(page, 'افزودن نود')
  await expect(form).toContainText('ورود فقط یک بار برای نصب استفاده می‌شود و ذخیره نمی‌شود.')
  await expect(form.getByLabel('نام', { exact: true })).toHaveAttribute('placeholder', 'نود ۳')
  await expect(form.getByLabel('پورت SSH')).toHaveValue('22')
  // what is typed in Latin reads left to right
  await expect(form.getByLabel('آدرس سرور')).toHaveAttribute('dir', 'ltr')
  await expect(form.getByLabel('نام کاربری')).toHaveAttribute('dir', 'ltr')
  await expect(form.getByRole('radio', { name: 'رمز عبور' })).toBeVisible()
  await expect(form.getByRole('radio', { name: 'کلید خصوصی' })).toBeVisible()
  await fillAddNode(page, '192.0.2.41')
  await form.getByRole('button', { name: 'اتصال و نصب' }).click()

  const job = dialog(page, 'در حال نصب نود ۳')
  await expect(job).toBeVisible()
  await expect(job).toContainText(/مرحله [۰-۹]+ از ۱۵ · [۰-۹]:[۰-۹]{2} تاکنون/)
  await expect(job.getByText('بررسی سرور')).toBeVisible()
  await expect(job.getByText('ارسال Xray 26.10.21')).toBeVisible()
  await expect(job).toContainText('می‌توانید این را ببندید. نصب ادامه می‌یابد')
  const done = dialog(page, 'نود اضافه شد')
  await expect(done).toBeVisible({ timeout: 20000 })
  await expect(done).toContainText(/[۰-۹]+:[۰-۹]{2} طول کشید/)
  await expect(done).toContainText('ایجنت در حال اجراست و تونلش برقرار است.')
  await expect(toast(page, 'نود ۳ اضافه شد')).toBeVisible()
  await expect(card(page, 'نود ۳')).toContainText('در حال کار')
})

test('an install that fails, in Persian, keeps the hub\'s English where it has no words of its own', async ({ page, request }) => {
  await reset(request, { nodes: 2 })
  await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
  await openAddNode(page, 'افزودن نود')
  await fillAddNode(page, 'fail.example.com')
  await dialog(page, 'افزودن نود').getByRole('button', { name: 'اتصال و نصب' }).click()
  const failed = dialog(page, 'نصب ناموفق بود')
  await expect(failed).toBeVisible({ timeout: 20000 })
  await expect(failed).toContainText('ناموفق در مرحله ۷ از ۱۵')
  await expect(failed).toContainText("The agent doesn't answer on TCP port 9090 (dial tcp fail.example.com:9090: i/o timeout).")
  await expect(failed).toContainText('این روتر از طریق آپ‌لینک داخلی به نود می‌رسد، پس آدرس آن باید در فهرست داخلی باشد.')
  // the hub keeps the node, so the way on is to open it
  await expect(failed.getByRole('button', { name: 'باز کردن نود' })).toBeVisible()
})

test('arrow keys follow the reading direction in the tabs', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
  await page.goto('/')
  await page.getByRole('tab', { name: 'نودها' }).focus()
  // the next tab is to the left
  await page.keyboard.press('ArrowLeft')
  await expect(page).toHaveURL(/#\/subscriptions$/)
  await expect(page.getByRole('tab', { name: 'اشتراک‌ها' })).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab', { name: 'نودها' })).toHaveAttribute('aria-selected', 'true')
})

test.describe('which language it starts in', () => {
  test.describe('a browser set to Persian', () => {
    test.use({ locale: 'fa-IR' })
    test('starts in Persian, and a choice of English sticks', async ({ page }) => {
      await page.goto('/')
      await expect(page.locator('html')).toHaveAttribute('dir', 'rtl')
      await expect(page.getByRole('heading', { name: 'ورودی روشن است' })).toBeVisible()
      await page.getByRole('button', { name: 'English' }).click()
      await page.reload()
      await expect(page.locator('html')).toHaveAttribute('lang', 'en')
      await expect(page.getByRole('heading', { name: 'Entry is on' })).toBeVisible()
    })
  })

  test('follows the panel when it has chosen Persian', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('nasnet-panel.language', 'fa'))
    await page.goto('/')
    await expect(page.locator('html')).toHaveAttribute('lang', 'fa')
    // its own choice wins once it has one
    await page.getByRole('button', { name: 'English' }).click()
    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  })

  test.describe('storage blocked', () => {
    test.use({ colorScheme: 'dark' })
    test('still works, for as long as the page is open', async ({ page }) => {
      await page.addInitScript(() => {
        Object.defineProperty(window, 'localStorage', {
          get() {
            throw new Error('denied')
          },
        })
      })
      await page.goto('/')
      await expect(page.getByRole('heading', { name: 'Entry is on' })).toBeVisible()
      await page.getByRole('button', { name: 'فارسی' }).click()
      await expect(page.getByRole('heading', { name: 'ورودی روشن است' })).toBeVisible()
      await page.getByRole('button', { name: 'تغییر به پوستهٔ روشن' }).click()
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
    })
  })
})
