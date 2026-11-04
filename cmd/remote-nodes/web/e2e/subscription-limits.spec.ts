import type { Page } from '@playwright/test'
import { PAGE, dialog, expect, test, toast } from './fixtures'
import { isTable, openNodeSubscriptions, openSubscriptions, pick, row } from './subs'

const limit = (page: Page, who: string) => dialog(page, `Limits for ${who}`)
const patches = (page: Page) => {
  const seen: unknown[] = []
  page.on('request', (r) => r.method() === 'PATCH' && r.url().includes('/api/subscriptions/') && seen.push(r.postDataJSON()))
  return seen
}

/** A subscription with no limit and no date: the table says so in its Limits column, and a card says nothing. */
async function expectNoLimits(page: Page, name: string) {
  const r = row(page, name)
  await expect(r.getByRole('progressbar')).toHaveCount(0)
  if (isTable(page)) {
    await expect(r).toContainText('No limit')
    await expect(r).toContainText('No end date')
  } else {
    await expect(r).not.toContainText(/No limit|No end date|Expires/)
  }
}

/** The day after today in the person's zone, as a date field holds it. */
function tomorrow(): string {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

test('a limit shows how much of it is used, and an end date how long is left', async ({ page }) => {
  await openSubscriptions(page)
  const sara = row(page, 'Sara')
  await expect(sara).toContainText('62.0 GB of 100 GB')
  await expect(sara.getByRole('progressbar', { name: 'Traffic limit' })).toHaveAttribute('aria-valuenow', '62')
  await expect(sara).toContainText(/Expires .+ · in 41 days/)
  // the rest have neither
  await expectNoLimits(page, 'Ali')
})

test('a limit below what was used stops the subscription, and a higher one starts it again', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Ali', 'Limits and expiry')
  const d = limit(page, 'Ali')
  await expect(d.getByLabel('Traffic limit')).toHaveValue('')
  await expect(d.getByLabel('Expires on')).toHaveValue('')
  await expect(d).toContainText('Used so far: 21.0 GB')

  await d.getByLabel('Traffic limit').fill('20')
  const sent = page.waitForRequest((r) => r.method() === 'PATCH' && r.url().endsWith('/api/subscriptions/2'))
  await d.getByRole('button', { name: 'Save' }).click()
  // only what changed is sent
  expect((await sent).postDataJSON()).toEqual({ trafficLimit: 20_000_000_000 })
  await expect(toast(page, 'Limits of Ali saved')).toBeVisible()
  await expect(row(page, 'Ali')).toContainText('Limit reached')
  await expect(row(page, 'Ali')).toContainText('21.0 GB of 20.0 GB used')
  await expect(row(page, 'Ali').getByRole('progressbar', { name: 'Traffic limit' })).toHaveAttribute('aria-valuenow', '100')

  await pick(page, 'Ali', 'Limits and expiry')
  await expect(limit(page, 'Ali').getByLabel('Traffic limit')).toHaveValue('20')
  await limit(page, 'Ali').getByLabel('Traffic limit').fill('40')
  await limit(page, 'Ali').getByRole('button', { name: 'Save' }).click()
  await expect(row(page, 'Ali')).toContainText('Online')
  await expect(row(page, 'Ali')).not.toContainText('Limit reached')
})

test('the limit field takes a comma, and refuses what is not a number above zero without asking the hub', async ({ page }) => {
  await openSubscriptions(page)
  const seen = patches(page)
  await pick(page, 'Ali', 'Limits and expiry')
  const d = limit(page, 'Ali')
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
  await pick(page, 'Mina', 'Limits and expiry')
  const d = limit(page, 'Mina')
  await d.getByLabel('Expires on').fill('2020-01-01')
  await d.getByRole('button', { name: 'Save' }).click()
  await expect(d.getByText('Pick a day that has not passed yet.')).toBeVisible()
  expect(seen).toEqual([])

  const day = tomorrow()
  await d.getByLabel('Expires on').fill(day)
  const sent = page.waitForRequest((r) => r.method() === 'PATCH' && r.url().endsWith('/api/subscriptions/4'))
  await d.getByRole('button', { name: 'Save' }).click()
  const body = (await sent).postDataJSON() as { expiresAt: string }
  expect(Object.keys(body)).toEqual(['expiresAt'])
  const at = new Date(body.expiresAt)
  expect([at.getHours(), at.getMinutes(), at.getSeconds()]).toEqual([23, 59, 59])
  expect(at.getDate()).toBe(Number(day.slice(8)))
  await expect(row(page, 'Mina')).toContainText(/Expires .+ · in \d+ days?/)
})

test('emptying a field takes the limit or the date away', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Sara', 'Limits and expiry')
  const d = limit(page, 'Sara')
  await expect(d.getByLabel('Traffic limit')).toHaveValue('100')
  await expect(d.getByLabel('Expires on')).not.toHaveValue('')
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
  await pick(page, 'Sara', 'Limits and expiry')
  await limit(page, 'Sara').getByRole('button', { name: 'Save' }).click()
  await expect(limit(page, 'Sara')).toHaveCount(0)
  expect(seen).toEqual([])
})

test('resetting the usage starts the count again, and a subscription stopped at its limit runs again', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Reza', 'Limits and expiry')
  await limit(page, 'Reza').getByLabel('Traffic limit').fill('50')
  await limit(page, 'Reza').getByRole('button', { name: 'Save' }).click()
  await expect(row(page, 'Reza')).toContainText('Limit reached')

  await pick(page, 'Reza', 'Reset usage')
  const d = page.getByRole('alertdialog', { name: 'Reset the usage of Reza?' })
  await expect(d).toContainText('starts again from zero')
  const sent = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/api/subscriptions/3/reset-usage'))
  await d.getByRole('button', { name: 'Reset usage' }).click()
  await sent
  await expect(toast(page, 'Usage of Reza reset')).toBeVisible()
  await expect(row(page, 'Reza')).not.toContainText('Limit reached')
  await expect(row(page, 'Reza')).toContainText('0 B of 50.0 GB')
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
  await d.getByLabel('Expires on').fill(tomorrow())
  const sent = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/api/subscriptions'))
  await d.getByRole('button', { name: 'Add subscription' }).click()
  const body = (await sent).postDataJSON() as { name: string; nodeIds: number[]; trafficLimit: number; expiresAt: string }
  expect(body).toMatchObject({ name: 'Nima', trafficLimit: 5_000_000_000 })
  expect(Date.parse(body.expiresAt)).toBeGreaterThan(Date.now())
  await expect(toast(page, 'Nima added')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(row(page, 'Nima')).toContainText('0 B of 5.00 GB'.replace('5.00', '5.0'))
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

test('a node\'s own list shows a stopped subscription too, with its switch left as it was', async ({ page, request }) => {
  const r = await request.patch('/api/subscriptions/2', { headers: PAGE, data: { trafficLimit: 20_000_000_000 } })
  expect(r.ok()).toBe(true)
  await openNodeSubscriptions(page, 1)
  await expect(row(page, 'Ali')).toContainText('Limit reached')
  await expect(page.getByRole('switch', { name: 'Ali on Node 1', exact: true })).toHaveAttribute('aria-checked', 'true')
  // the limits are the subscription's, edited on its own tab
  await page.getByRole('button', { name: 'Actions for Ali', exact: true }).click()
  await expect(page.getByRole('menuitem', { name: 'Limits and expiry' })).toHaveCount(0)
})

test('the hub\'s own refusal of a limit shows in the dialog', async ({ page }) => {
  await openSubscriptions(page)
  // what the page lets through is checked by the page; a script (or a newer page) can send what it would not
  await page.route('**/api/subscriptions/2', async (route) => {
    if (route.request().method() !== 'PATCH') return route.fallback()
    await route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ code: 'bad_request', error: 'The traffic limit is too large.' }) })
  })
  await pick(page, 'Ali', 'Limits and expiry')
  await limit(page, 'Ali').getByLabel('Traffic limit').fill('5')
  await limit(page, 'Ali').getByRole('button', { name: 'Save' }).click()
  await expect(limit(page, 'Ali').getByRole('alert')).toContainText('The traffic limit is too large.')
})
