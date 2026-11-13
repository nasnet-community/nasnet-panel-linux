import type { Locator, Page } from '@playwright/test'
import { PAGE, dialog, expect, test, toast } from './fixtures'
import { addButton, isTable, openNodeSubscriptions, openSubscriptions, pick, row } from './subs'

const edit = (page: Page, who: string) => dialog(page, `Edit ${who}`)
const patches = (page: Page) => {
  const seen: unknown[] = []
  page.on('request', (r) => r.method() === 'PATCH' && r.url().includes('/api/subscriptions/') && seen.push(r.postDataJSON()))
  return seen
}

/** A day some days after today in the person's zone, as a date field holds it. */
function inDays(n: number): string {
  const d = new Date()
  d.setDate(d.getDate() + n)
  const pad = (x: number) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** The end-date field: a button that opens a calendar, holding its day in data-value. */
const endField = (d: Locator, label = 'Expires on') => d.getByLabel(label, { exact: true })

/** Picks a day on the end-date calendar: opens it, goes on a month at a time to the day, and clicks it. */
async function pickDay(d: Locator, day: string, labels = { field: 'Expires on', calendar: 'Choose a day', next: 'Next month' }) {
  await endField(d, labels.field).click()
  const cal = d.getByRole('dialog', { name: labels.calendar })
  for (let i = 0; i < 24 && !(await cal.locator(`[data-day="${day}"]`).count()); i += 1) await cal.getByRole('button', { name: labels.next }).click()
  await cal.locator(`[data-day="${day}"]`).click()
  await expect(cal).toHaveCount(0)
}

/** The same day n months on, or that month's last day if it is shorter, as the calendar moves. */
function monthsOn(day: string, n: number): string {
  const [y, m, d] = day.split('-').map(Number)
  const last = new Date(Date.UTC(y, m - 1 + n + 1, 0)).getUTCDate()
  const t = new Date(Date.UTC(y, m - 1 + n, Math.min(d, last)))
  return t.toISOString().slice(0, 10)
}

/** A subscription with no limit and no date: the table's two columns say so, and a card says nothing. */
async function expectNoLimits(page: Page, name: string) {
  const r = row(page, name)
  await expect(r.getByRole('progressbar')).toHaveCount(0)
  if (isTable(page)) {
    await expect(r.getByText('No limit', { exact: true })).toBeVisible()
    await expect(r.getByText('No end date', { exact: true })).toBeVisible()
  } else await expect(r).not.toContainText(/No limit|No end date|days? left|Ended|Ends today/)
}

test('the list has a Traffic limit column and a Days left column, and a card sums both up', async ({ page }) => {
  await openSubscriptions(page)
  const sara = row(page, 'Sara')
  await expect(sara).toContainText('62.0 GB of 100 GB')
  await expect(sara.getByRole('progressbar', { name: 'Traffic limit' })).toHaveAttribute('aria-valuenow', '62')
  // nothing more under the bar: what is left and the end date are not repeated there
  await expect(sara).not.toContainText(/GB left|ends |ended /)
  if (isTable(page)) {
    await expect(page.getByRole('columnheader', { name: 'Traffic limit', exact: true })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: 'Days left', exact: true })).toBeVisible()
    for (const gone of ['Limits', 'Expires', 'Status']) await expect(page.getByRole('columnheader', { name: gone, exact: true })).toHaveCount(0)
    await expect(sara.getByText('41 days', { exact: true })).toBeVisible()
    await expect(sara.getByText(/^\d{1,2} \w{3} 20\d\d$/)).toBeVisible()
  } else await expect(sara.getByText('41 days left', { exact: true })).toBeVisible()
  await expectNoLimits(page, 'Ali')
})

test('the days left warn in the last week, and an end date that passed says Ended', async ({ page, request }) => {
  const at = (days: number) => new Date(Date.now() + days * 86400e3).toISOString()
  expect((await request.patch('/api/subscriptions/2', { headers: PAGE, data: { expiresAt: at(3) } })).ok()).toBe(true)
  expect((await request.patch('/api/subscriptions/4', { headers: PAGE, data: { expiresAt: at(60) } })).ok()).toBe(true)
  await openSubscriptions(page)
  const table = isTable(page)
  const soon = row(page, 'Ali')
  await expect(soon.getByText(table ? '3 days' : '3 days left', { exact: true })).toHaveClass(/text-warn-text/)
  // an end date alone has no bar
  await expect(soon.getByRole('progressbar')).toHaveCount(0)
  if (table) await expect(soon.getByText('No limit', { exact: true })).toBeVisible()
  await expect(row(page, 'Mina').getByText(table ? '60 days' : '60 days left', { exact: true })).not.toHaveClass(/text-warn-text|text-bad-text/)

  // a day that has passed: the hub refuses to set one, so the list is told of one that ran out
  await page.route('**/api/subscriptions', async (route) => {
    const subs = (await (await route.fetch()).json()) as { id: number }[]
    await route.fulfill({ json: subs.map((x) => (x.id === 2 ? { ...x, expiresAt: at(-2), stopped: 'expired' } : x)) })
  })
  await page.reload()
  await expect(row(page, 'Ali').getByText(table ? 'Ended' : /^Ended \d{1,2} \w{3} 20\d\d$/)).toHaveClass(/text-bad-text/)
})

test('each row has an Edit button that opens the dialog with what is set', async ({ page }) => {
  await openSubscriptions(page)
  await row(page, 'Sara').getByRole('button', { name: 'Edit Sara', exact: true }).click()
  const d = edit(page, 'Sara')
  await expect(d.getByLabel('Name')).toHaveValue('Sara')
  await expect(d.getByLabel('Traffic limit')).toHaveValue('100')
  await expect(endField(d)).toHaveAttribute('data-value', inDays(41))
  await expect(d).toContainText('Used since the last reset: 62.0 GB')
  // what the row's menu used to hold is here: resetting the usage, and deleting
  await expect(d.getByRole('button', { name: 'Reset usage…' })).toBeVisible()
  await expect(d.getByRole('button', { name: 'Delete subscription…' })).toBeVisible()
  await d.getByRole('button', { name: 'Cancel' }).click()
  // the row itself has Share and Edit, and no menu
  await expect(row(page, 'Sara').getByRole('button')).toHaveCount(2)
  await expect(page.getByRole('button', { name: /^Actions for/ })).toHaveCount(0)
})

test('a limit below what was used stops the subscription, and a higher one starts it again', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Ali', 'Edit')
  const d = edit(page, 'Ali')
  await expect(d.getByLabel('Traffic limit')).toHaveValue('')
  await expect(endField(d)).toHaveAttribute('data-value', '')
  await expect(d).toContainText('Used since the last reset: 21.0 GB')

  await d.getByLabel('Traffic limit').fill('20')
  const sent = page.waitForRequest((r) => r.method() === 'PATCH' && r.url().endsWith('/api/subscriptions/2'))
  await d.getByRole('button', { name: 'Save' }).click()
  // only what changed is sent
  expect((await sent).postDataJSON()).toEqual({ trafficLimit: 20_000_000_000 })
  await expect(toast(page, 'Ali saved')).toBeVisible()
  await expect(row(page, 'Ali').locator('[data-status]')).toHaveText('Stopped · limit reached')
  // a stopped subscription is connected nowhere
  await expect(row(page, 'Ali').getByText('connected here')).toHaveCount(0)
  await expect(row(page, 'Ali')).toContainText('21.0 GB of 20.0 GB')
  await expect(row(page, 'Ali').getByRole('progressbar', { name: 'Traffic limit' })).toHaveAttribute('aria-valuenow', '100')

  // the dialog says why it stopped and what starts it again
  await pick(page, 'Ali', 'Edit')
  await expect(edit(page, 'Ali')).toContainText('Ali has used up its traffic limit, so it is stopped.')
  await expect(edit(page, 'Ali').getByLabel('Traffic limit')).toHaveValue('20')
  await edit(page, 'Ali').getByLabel('Traffic limit').fill('40')
  await edit(page, 'Ali').getByRole('button', { name: 'Save' }).click()
  await expect(row(page, 'Ali').locator('[data-status]')).toHaveText('Online on Node 1 · 2 connections')
})

test('the name, the limit and the date go in one save', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Mina', 'Edit')
  const d = edit(page, 'Mina')
  await d.getByLabel('Name').fill('Minoo')
  await d.getByLabel('Traffic limit').fill('25')
  await pickDay(d, inDays(10))
  const sent = page.waitForRequest((r) => r.method() === 'PATCH' && r.url().endsWith('/api/subscriptions/4'))
  await d.getByRole('button', { name: 'Save' }).click()
  const body = (await sent).postDataJSON() as Record<string, unknown>
  expect(Object.keys(body).sort()).toEqual(['expiresAt', 'name', 'trafficLimit'])
  expect(body).toMatchObject({ name: 'Minoo', trafficLimit: 25_000_000_000 })
  await expect(toast(page, 'Minoo saved')).toBeVisible()
  await expect(row(page, 'Minoo')).toContainText('9.4 GB of 25.0 GB')
  // it ends with its day, ten days on
  await expect(row(page, 'Minoo').getByText(/^10 days( left)?$/)).toBeVisible()
})

test('the limit field takes a comma, and refuses what is not a number above zero without asking the hub', async ({ page }) => {
  await openSubscriptions(page)
  const seen = patches(page)
  await pick(page, 'Ali', 'Edit')
  const d = edit(page, 'Ali')
  for (const bad of ['0', '-3', 'ten', '1e3']) {
    await d.getByLabel('Traffic limit').fill(bad)
    await d.getByRole('button', { name: 'Save' }).click()
    await expect(d.getByText('Enter a number of gigabytes above zero, or leave it empty.')).toBeVisible()
  }
  expect(seen).toEqual([])

  await d.getByLabel('Traffic limit').fill('12,5')
  const sent = page.waitForRequest((r) => r.method() === 'PATCH')
  await d.getByRole('button', { name: 'Save' }).click()
  expect((await sent).postDataJSON()).toEqual({ trafficLimit: 12_500_000_000 })
})

test('the calendar can\'t pick a day that has passed, and a picked day ends at its end', async ({ page }) => {
  await openSubscriptions(page)
  const seen = patches(page)
  await pick(page, 'Mina', 'Edit')
  const d = edit(page, 'Mina')
  await endField(d).click()
  const cal = d.getByRole('dialog', { name: 'Choose a day' })
  const yesterday = inDays(-1)
  if (!(await cal.locator(`[data-day="${yesterday}"]`).count())) await cal.getByRole('button', { name: 'Previous month' }).click()
  const past = cal.locator(`[data-day="${yesterday}"]`)
  await expect(past).toHaveAttribute('aria-disabled', 'true')
  // a click on it does nothing (force: Playwright itself won't click a disabled button)
  await past.click({ force: true })
  await expect(cal).toBeVisible()
  await expect(endField(d)).toHaveAttribute('data-value', '')
  // today is still to come: it ends tonight
  await expect(cal.locator(`[data-day="${inDays(0)}"]`)).not.toHaveAttribute('aria-disabled', 'true')
  await expect(cal.locator(`[data-day="${inDays(0)}"]`)).toHaveAttribute('aria-current', 'date')
  await page.keyboard.press('Escape')
  expect(seen).toEqual([])

  const day = inDays(1)
  await pickDay(d, day)
  await expect(endField(d)).toHaveAttribute('data-value', day)
  const sent = page.waitForRequest((r) => r.method() === 'PATCH' && r.url().endsWith('/api/subscriptions/4'))
  await d.getByRole('button', { name: 'Save' }).click()
  const body = (await sent).postDataJSON() as { expiresAt: string }
  expect(Object.keys(body)).toEqual(['expiresAt'])
  const at = new Date(body.expiresAt)
  expect([at.getHours(), at.getMinutes(), at.getSeconds()]).toEqual([23, 59, 59])
  expect(at.getDate()).toBe(Number(day.slice(8)))
  await expect(row(page, 'Mina')).toContainText(/in \d+ (day|days|hours?)/)
})

test('the calendar works from the keyboard, and Escape closes it and not the dialog', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Kian', 'Edit')
  const d = edit(page, 'Kian')
  await endField(d).click()
  const cal = d.getByRole('dialog', { name: 'Choose a day' })
  // it opens on today, the earliest it can pick
  await expect(cal.locator(`[data-day="${inDays(0)}"]`)).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await expect(cal.locator(`[data-day="${inDays(1)}"]`)).toBeFocused()
  await page.keyboard.press('ArrowDown')
  await expect(cal.locator(`[data-day="${inDays(8)}"]`)).toBeFocused()
  await page.keyboard.press('PageDown')
  const target = monthsOn(inDays(8), 1)
  await expect(cal.locator(`[data-day="${target}"]`)).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(cal).toHaveCount(0)
  await expect(endField(d)).toHaveAttribute('data-value', target)
  await expect(endField(d)).toBeFocused()

  await endField(d).click()
  await expect(cal.locator(`[data-day="${target}"]`)).toBeFocused()
  await expect(cal.locator(`[data-day="${target}"]`).locator('xpath=..')).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('Escape')
  await expect(cal).toHaveCount(0)
  await expect(d).toBeVisible()
})

test('the quick days set the end date from today, and count on from a date already set', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Kian', 'Edit')
  const d = edit(page, 'Kian')
  const presets = d.getByRole('group', { name: 'Set the end date' })
  await presets.getByRole('button', { name: '+30 days' }).click()
  await expect(endField(d)).toHaveAttribute('data-value', inDays(30))
  await presets.getByRole('button', { name: '+30 days' }).click()
  await expect(endField(d)).toHaveAttribute('data-value', inDays(60))
  // and the same button for no end date at all
  await presets.getByRole('button', { name: 'No end date' }).click()
  await expect(endField(d)).toHaveAttribute('data-value', '')
  await expect(presets.getByRole('button', { name: 'No end date' })).toHaveCount(0)
  await presets.getByRole('button', { name: '+90 days' }).click()
  await expect(endField(d)).toHaveAttribute('data-value', inDays(90))
  await presets.getByRole('button', { name: '+1 year' }).click()
  const sent = page.waitForRequest((r) => r.method() === 'PATCH' && r.url().endsWith('/api/subscriptions/7'))
  await d.getByRole('button', { name: 'Save' }).click()
  const at = new Date(((await sent).postDataJSON() as { expiresAt: string }).expiresAt)
  const want = new Date()
  want.setDate(want.getDate() + 90)
  want.setFullYear(want.getFullYear() + 1)
  expect([at.getFullYear(), at.getMonth(), at.getDate()]).toEqual([want.getFullYear(), want.getMonth(), want.getDate()])
})

test('an expired subscription is renewed from today, and says why it stopped', async ({ page, request }) => {
  // Dana ends tomorrow; the mock then moves her date into the past, the way time would
  expect((await request.patch('/api/subscriptions/6', { headers: PAGE, data: { expiresAt: new Date(Date.now() + 2000).toISOString() } })).ok()).toBe(true)
  await page.waitForTimeout(2500)
  await openSubscriptions(page)
  await expect(row(page, 'Dana').locator('[data-status]')).toHaveText(/^Stopped · expired \d{1,2} \w{3} 20\d\d$/)
  await expect(row(page, 'Dana').getByText(/^Ended( \d{1,2} \w{3} 20\d\d)?$/)).toBeVisible()
  await pick(page, 'Dana', 'Edit')
  const d = edit(page, 'Dana')
  await expect(d).toContainText(/Dana ended on .+, so it is stopped\. Pick a later date to start it again\./)
  await d.getByRole('group', { name: 'Set the end date' }).getByRole('button', { name: '+30 days' }).click()
  await expect(endField(d)).toHaveAttribute('data-value', inDays(30))
  await d.getByRole('button', { name: 'Save' }).click()
  await expect(toast(page, 'Dana saved')).toBeVisible()
  await expect(row(page, 'Dana').locator('[data-status]')).not.toContainText('Stopped')
  await expect(row(page, 'Dana').getByText(/^30 days( left)?$/)).toBeVisible()
})

test('emptying a field takes the limit or the date away', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Sara', 'Edit')
  const d = edit(page, 'Sara')
  await d.getByLabel('Traffic limit').fill('')
  await d.getByRole('group', { name: 'Set the end date' }).getByRole('button', { name: 'No end date' }).click()
  await expect(endField(d)).toHaveAttribute('data-value', '')
  const sent = page.waitForRequest((r) => r.method() === 'PATCH' && r.url().endsWith('/api/subscriptions/1'))
  await d.getByRole('button', { name: 'Save' }).click()
  expect((await sent).postDataJSON()).toEqual({ trafficLimit: null, expiresAt: null })
  await expectNoLimits(page, 'Sara')
})

test('saving without a change asks nothing of the hub', async ({ page }) => {
  await openSubscriptions(page)
  const seen = patches(page)
  await pick(page, 'Sara', 'Edit')
  await edit(page, 'Sara').getByRole('button', { name: 'Save' }).click()
  await expect(edit(page, 'Sara')).toHaveCount(0)
  expect(seen).toEqual([])
})

test('resetting the usage starts the count again, from the edit dialog', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Reza', 'Edit')
  await edit(page, 'Reza').getByLabel('Traffic limit').fill('50')
  await edit(page, 'Reza').getByRole('button', { name: 'Save' }).click()
  await expect(row(page, 'Reza').locator('[data-status]')).toHaveText('Stopped · limit reached')

  // the question takes the dialog's place
  await pick(page, 'Reza', 'Edit')
  await edit(page, 'Reza').getByRole('button', { name: 'Reset usage…' }).click()
  const d = page.getByRole('alertdialog', { name: 'Reset the usage of Reza?' })
  await expect(edit(page, 'Reza')).toHaveCount(0)
  await expect(d).toContainText('starts again from zero')
  const sent = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/api/subscriptions/3/reset-usage'))
  await d.getByRole('button', { name: 'Reset usage' }).click()
  await sent
  await expect(toast(page, 'Usage of Reza reset')).toBeVisible()
  await expect(row(page, 'Reza').locator('[data-status]')).not.toContainText('Stopped')
  await expect(row(page, 'Reza')).toContainText('0 B of 50.0 GB')

  // the same for one that isn't stopped
  await pick(page, 'Sara', 'Reset usage')
  await page.getByRole('alertdialog', { name: 'Reset the usage of Sara?' }).getByRole('button', { name: 'Reset usage' }).click()
  await expect(row(page, 'Sara')).toContainText('0 B of 100 GB')
})

test('adding a subscription can set a limit and a date at once', async ({ page }) => {
  await openSubscriptions(page)
  await page.getByRole('button', { name: 'Add subscription' }).click()
  const d = dialog(page, 'Add a subscription')
  await d.getByLabel('Name').fill('Nima')
  await d.getByLabel('Traffic limit').fill('abc')
  await addButton(d).click()
  await expect(d.getByText('Enter a number of gigabytes above zero, or leave it empty.')).toBeVisible()

  await d.getByLabel('Traffic limit').fill('5')
  await d.getByRole('group', { name: 'Set the end date' }).getByRole('button', { name: '+30 days' }).click()
  await expect(endField(d)).toHaveAttribute('data-value', inDays(30))
  const sent = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/api/subscriptions'))
  await addButton(d).click()
  const body = (await sent).postDataJSON() as { name: string; nodeIds: number[]; trafficLimit: number; expiresAt: string }
  expect(body).toMatchObject({ name: 'Nima', trafficLimit: 5_000_000_000 })
  expect(Date.parse(body.expiresAt)).toBeGreaterThan(Date.now() + 29 * 86400e3)
  await expect(toast(page, 'Nima added')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(row(page, 'Nima')).toContainText('0 B of 5.0 GB')
})

test('a plain add sends no limits', async ({ page }) => {
  await openSubscriptions(page)
  await page.getByRole('button', { name: 'Add subscription' }).click()
  const d = dialog(page, 'Add a subscription')
  await d.getByLabel('Name').fill('Nima')
  const sent = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/api/subscriptions'))
  await addButton(d).click()
  const body = (await sent).postDataJSON() as Record<string, unknown>
  expect(Object.keys(body).sort()).toEqual(['name', 'nodeIds'])
})

test('a node\'s own list shows the limit columns, a stopped subscription, and edits it the same way', async ({ page, request }) => {
  expect((await request.patch('/api/subscriptions/2', { headers: PAGE, data: { trafficLimit: 20_000_000_000 } })).ok()).toBe(true)
  await openNodeSubscriptions(page, 1)
  await expect(row(page, 'Ali').locator('[data-status]')).toHaveText('Stopped · limit reached')
  await expect(row(page, 'Ali')).toContainText('21.0 GB of 20.0 GB')
  await expect(page.getByRole('switch', { name: 'Ali on Node 1', exact: true })).toHaveAttribute('aria-checked', 'true')
  if (isTable(page)) await expect(page.getByRole('columnheader', { name: 'Traffic limit', exact: true })).toBeVisible()
  await row(page, 'Ali').getByRole('button', { name: 'Edit Ali', exact: true }).click()
  const d = edit(page, 'Ali')
  await expect(d.getByLabel('Traffic limit')).toHaveValue('20')
  await d.getByLabel('Traffic limit').fill('30')
  await d.getByRole('button', { name: 'Save' }).click()
  await expect(toast(page, 'Ali saved')).toBeVisible()
  await expect(row(page, 'Ali').locator('[data-status]')).toHaveText('Online · 2 connections')
})

test('the hub\'s own refusal shows in the dialog', async ({ page }) => {
  await openSubscriptions(page)
  // what the page lets through is checked by the page; a script (or a newer page) can send what it would not
  await page.route('**/api/subscriptions/2', async (route) => {
    if (route.request().method() !== 'PATCH') return route.fallback()
    await route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ code: 'bad_request', error: 'The traffic limit is too large.' }) })
  })
  await pick(page, 'Ali', 'Edit')
  await edit(page, 'Ali').getByLabel('Traffic limit').fill('5')
  await edit(page, 'Ali').getByRole('button', { name: 'Save' }).click()
  await expect(edit(page, 'Ali').getByRole('alert')).toContainText('The traffic limit is too large.')
})

test('the edit dialog in Persian: right to left, its words, and the quick days', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
  await openSubscriptions(page)
  await row(page, 'Sara').getByRole('button', { name: 'ویرایش Sara', exact: true }).click()
  const d = dialog(page, 'ویرایش Sara')
  expect(await d.evaluate((el) => getComputedStyle(el).direction)).toBe('rtl')
  await expect(d.getByLabel('سقف ترافیک')).toHaveValue('100')
  await expect(endField(d, 'تاریخ پایان')).toHaveAttribute('data-value', inDays(41))
  // the calendar is the Persian one: Saturday first, Persian names and digits, and right to left on the keys
  await endField(d, 'تاریخ پایان').click()
  const cal = d.getByRole('dialog', { name: 'انتخاب روز' })
  await expect(cal.getByRole('columnheader').first()).toHaveText('ش')
  await expect(cal.getByRole('grid')).toHaveAttribute('aria-label', /آذر|دی|بهمن|اسفند|فروردین|اردیبهشت|خرداد|تیر|مرداد|شهریور|مهر|آبان/)
  const chosen = cal.locator(`[data-day="${inDays(41)}"]`)
  await expect(chosen).toHaveText(/^[۰-۹]+$/)
  await expect(chosen).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(cal.locator(`[data-day="${inDays(42)}"]`)).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(cal).toHaveCount(0)
  await expect(d.getByRole('group', { name: 'تعیین تاریخ پایان' }).getByRole('button')).toHaveText(['+۳۰ روز', '+۹۰ روز', '+۱ سال', 'بدون تاریخ پایان'])
  await expect(d).toContainText('مصرف از آخرین صفر کردن')
})
