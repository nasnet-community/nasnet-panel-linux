import type { Page } from '@playwright/test'
import { PAGE, dialog, expect, test, toast } from './fixtures'
import { isTable, openNodeSubscriptions, openSubscriptions, pick, row } from './subs'

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

/** A subscription with no limit and no date: the table says so in its two columns, and a card says nothing. */
async function expectNoLimits(page: Page, name: string) {
  const r = row(page, name)
  await expect(r.getByRole('progressbar')).toHaveCount(0)
  if (isTable(page)) {
    await expect(r).toContainText('No limit')
    await expect(r).toContainText('Never')
  } else {
    await expect(r).not.toContainText(/No limit|No end date|Expires/)
  }
}

test('the list has a Traffic limit and an Expires column, and a card sums both up', async ({ page }) => {
  await openSubscriptions(page)
  const sara = row(page, 'Sara')
  await expect(sara).toContainText('62.0 GB of 100 GB')
  await expect(sara).toContainText('38.0 GB left')
  await expect(sara.getByRole('progressbar', { name: 'Traffic limit' })).toHaveAttribute('aria-valuenow', '62')
  await expect(sara).toContainText(/in 41 days/)
  if (isTable(page)) {
    const heads = page.getByRole('columnheader')
    await expect(heads.nth(5)).toHaveText('Traffic limit')
    await expect(heads.nth(6)).toHaveText('Expires')
  } else {
    await expect(sara).toContainText(/Expires .+ · in 41 days/)
  }
  await expectNoLimits(page, 'Ali')
})

test('each row has an Edit button that opens the dialog with what is set', async ({ page }) => {
  await openSubscriptions(page)
  await row(page, 'Sara').getByRole('button', { name: 'Edit Sara', exact: true }).click()
  const d = edit(page, 'Sara')
  await expect(d.getByLabel('Name')).toHaveValue('Sara')
  await expect(d.getByLabel('Traffic limit')).toHaveValue('100')
  await expect(d.getByLabel('Expires on')).toHaveValue(inDays(41))
  await expect(d).toContainText('Used since the last reset: 62.0 GB')
  // the menu has it too, first after Share
  await d.getByRole('button', { name: 'Cancel' }).click()
  await page.getByRole('button', { name: 'Actions for Sara', exact: true }).click()
  await expect(page.getByRole('menuitem')).toHaveText(['Share link', 'Edit', 'Reset usage', 'Delete subscription'])
})

test('a limit below what was used stops the subscription, and a higher one starts it again', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Ali', 'Edit')
  const d = edit(page, 'Ali')
  await expect(d.getByLabel('Traffic limit')).toHaveValue('')
  await expect(d.getByLabel('Expires on')).toHaveValue('')
  await expect(d).toContainText('Used since the last reset: 21.0 GB')

  await d.getByLabel('Traffic limit').fill('20')
  const sent = page.waitForRequest((r) => r.method() === 'PATCH' && r.url().endsWith('/api/subscriptions/2'))
  await d.getByRole('button', { name: 'Save' }).click()
  // only what changed is sent
  expect((await sent).postDataJSON()).toEqual({ trafficLimit: 20_000_000_000 })
  await expect(toast(page, 'Ali saved')).toBeVisible()
  await expect(row(page, 'Ali')).toContainText('Limit reached')
  await expect(row(page, 'Ali')).toContainText('21.0 GB of 20.0 GB used')
  await expect(row(page, 'Ali')).toContainText('used up')
  await expect(row(page, 'Ali').getByRole('progressbar', { name: 'Traffic limit' })).toHaveAttribute('aria-valuenow', '100')

  // the dialog says why it stopped and what starts it again
  await pick(page, 'Ali', 'Edit')
  await expect(edit(page, 'Ali')).toContainText('Ali has used up its traffic limit, so it is stopped.')
  await expect(edit(page, 'Ali').getByLabel('Traffic limit')).toHaveValue('20')
  await edit(page, 'Ali').getByLabel('Traffic limit').fill('40')
  await edit(page, 'Ali').getByRole('button', { name: 'Save' }).click()
  await expect(row(page, 'Ali')).toContainText('Online')
  await expect(row(page, 'Ali')).not.toContainText('Limit reached')
})

test('the name, the limit and the date go in one save', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Mina', 'Edit')
  const d = edit(page, 'Mina')
  await d.getByLabel('Name').fill('Minoo')
  await d.getByLabel('Traffic limit').fill('25')
  await d.getByLabel('Expires on').fill(inDays(10))
  const sent = page.waitForRequest((r) => r.method() === 'PATCH' && r.url().endsWith('/api/subscriptions/4'))
  await d.getByRole('button', { name: 'Save' }).click()
  const body = (await sent).postDataJSON() as Record<string, unknown>
  expect(Object.keys(body).sort()).toEqual(['expiresAt', 'name', 'trafficLimit'])
  expect(body).toMatchObject({ name: 'Minoo', trafficLimit: 25_000_000_000 })
  await expect(toast(page, 'Minoo saved')).toBeVisible()
  await expect(row(page, 'Minoo')).toContainText('9.4 GB of 25.0 GB')
  await expect(row(page, 'Minoo')).toContainText(/in (9|10) days/)
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

test('an end date has to be a day that has not passed, and ends the subscription at the end of that day', async ({ page }) => {
  await openSubscriptions(page)
  const seen = patches(page)
  await pick(page, 'Mina', 'Edit')
  const d = edit(page, 'Mina')
  await d.getByLabel('Expires on').fill('2020-01-01')
  await d.getByRole('button', { name: 'Save' }).click()
  await expect(d.getByText('Pick a day that has not passed yet.')).toBeVisible()
  expect(seen).toEqual([])

  const day = inDays(1)
  await d.getByLabel('Expires on').fill(day)
  const sent = page.waitForRequest((r) => r.method() === 'PATCH' && r.url().endsWith('/api/subscriptions/4'))
  await d.getByRole('button', { name: 'Save' }).click()
  const body = (await sent).postDataJSON() as { expiresAt: string }
  expect(Object.keys(body)).toEqual(['expiresAt'])
  const at = new Date(body.expiresAt)
  expect([at.getHours(), at.getMinutes(), at.getSeconds()]).toEqual([23, 59, 59])
  expect(at.getDate()).toBe(Number(day.slice(8)))
  await expect(row(page, 'Mina')).toContainText(/in \d+ (day|days|hours?)/)
})

test('the quick days set the end date from today, and count on from a date already set', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Kian', 'Edit')
  const d = edit(page, 'Kian')
  const presets = d.getByRole('group', { name: 'Set the end date' })
  await presets.getByRole('button', { name: '+30 days' }).click()
  await expect(d.getByLabel('Expires on')).toHaveValue(inDays(30))
  await presets.getByRole('button', { name: '+30 days' }).click()
  await expect(d.getByLabel('Expires on')).toHaveValue(inDays(60))
  // and the same button for no end date at all
  await presets.getByRole('button', { name: 'No end date' }).click()
  await expect(d.getByLabel('Expires on')).toHaveValue('')
  await expect(presets.getByRole('button', { name: 'No end date' })).toHaveCount(0)
  await presets.getByRole('button', { name: '+90 days' }).click()
  await expect(d.getByLabel('Expires on')).toHaveValue(inDays(90))
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
  await expect(row(page, 'Dana')).toContainText('Expired')
  await pick(page, 'Dana', 'Edit')
  const d = edit(page, 'Dana')
  await expect(d).toContainText(/Dana ended on .+, so it is stopped\. Pick a later date to start it again\./)
  await d.getByRole('group', { name: 'Set the end date' }).getByRole('button', { name: '+30 days' }).click()
  await expect(d.getByLabel('Expires on')).toHaveValue(inDays(30))
  await d.getByRole('button', { name: 'Save' }).click()
  await expect(toast(page, 'Dana saved')).toBeVisible()
  await expect(row(page, 'Dana')).not.toContainText('Expired')
})

test('emptying a field takes the limit or the date away', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Sara', 'Edit')
  const d = edit(page, 'Sara')
  await d.getByLabel('Traffic limit').fill('')
  await d.getByLabel('Expires on').fill('')
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

test('resetting the usage starts the count again, from the menu or from the edit dialog', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Reza', 'Edit')
  await edit(page, 'Reza').getByLabel('Traffic limit').fill('50')
  await edit(page, 'Reza').getByRole('button', { name: 'Save' }).click()
  await expect(row(page, 'Reza')).toContainText('Limit reached')

  // from the dialog: the question takes its place
  await pick(page, 'Reza', 'Edit')
  await edit(page, 'Reza').getByRole('button', { name: 'Reset usage…' }).click()
  const d = page.getByRole('alertdialog', { name: 'Reset the usage of Reza?' })
  await expect(edit(page, 'Reza')).toHaveCount(0)
  await expect(d).toContainText('starts again from zero')
  const sent = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/api/subscriptions/3/reset-usage'))
  await d.getByRole('button', { name: 'Reset usage' }).click()
  await sent
  await expect(toast(page, 'Usage of Reza reset')).toBeVisible()
  await expect(row(page, 'Reza')).not.toContainText('Limit reached')
  await expect(row(page, 'Reza')).toContainText('0 B of 50.0 GB')

  // from the menu
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
  await d.getByRole('button', { name: 'Add subscription' }).click()
  await expect(d.getByText('Enter a number of gigabytes above zero, or leave it empty.')).toBeVisible()

  await d.getByLabel('Traffic limit').fill('5')
  await d.getByRole('group', { name: 'Set the end date' }).getByRole('button', { name: '+30 days' }).click()
  await expect(d.getByLabel('Expires on')).toHaveValue(inDays(30))
  const sent = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/api/subscriptions'))
  await d.getByRole('button', { name: 'Add subscription' }).click()
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
  await d.getByRole('button', { name: 'Add subscription' }).click()
  const body = (await sent).postDataJSON() as Record<string, unknown>
  expect(Object.keys(body).sort()).toEqual(['name', 'nodeIds'])
})

test('a node\'s own list shows the two columns, a stopped subscription, and edits it the same way', async ({ page, request }) => {
  expect((await request.patch('/api/subscriptions/2', { headers: PAGE, data: { trafficLimit: 20_000_000_000 } })).ok()).toBe(true)
  await openNodeSubscriptions(page, 1)
  await expect(row(page, 'Ali')).toContainText('Limit reached')
  await expect(row(page, 'Ali')).toContainText('21.0 GB of 20.0 GB')
  await expect(page.getByRole('switch', { name: 'Ali on Node 1', exact: true })).toHaveAttribute('aria-checked', 'true')
  if (isTable(page)) {
    const heads = page.getByRole('columnheader')
    await expect(heads.nth(3)).toHaveText('Traffic limit')
    await expect(heads.nth(4)).toHaveText('Expires')
  }
  await row(page, 'Ali').getByRole('button', { name: 'Edit Ali', exact: true }).click()
  const d = edit(page, 'Ali')
  await expect(d.getByLabel('Traffic limit')).toHaveValue('20')
  await d.getByLabel('Traffic limit').fill('30')
  await d.getByRole('button', { name: 'Save' }).click()
  await expect(toast(page, 'Ali saved')).toBeVisible()
  await expect(row(page, 'Ali')).not.toContainText('Limit reached')
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
  await expect(d.getByLabel('تاریخ پایان', { exact: true })).toHaveValue(inDays(41))
  await expect(d.getByRole('group', { name: 'تعیین تاریخ پایان' }).getByRole('button')).toHaveText(['+۳۰ روز', '+۹۰ روز', '+۱ سال', 'بدون تاریخ پایان'])
  await expect(d).toContainText('مصرف از آخرین صفر کردن')
})
