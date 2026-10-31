// The real page against a real hub: no mock. Run it with `npm run e2e:real` (see playwright.real.config.ts).
//
// What it needs, all from the environment:
//   RN_BASE_URL       where the hub is, with its base path when it has one (default http://127.0.0.1:8080/)
//   RN_NODE_NAME      the name of the node that is installed already (default node-a)
//   RN_NODE_HOST      its address, for installing it again (RN_DESTRUCTIVE only)
//   RN_NODE_PASSWORD  its root password (RN_DESTRUCTIVE only)
//   RN_DESTRUCTIVE=1  also remove that node through the page and install it again
//   RN_SHOTS=dir      where screenshots go (default test-results-real/shots)
//
// The tests run in order and share the hub. They leave nothing of their own behind, except that the
// destructive ones end with the node installed again, under the same name.
import { writeFileSync, mkdirSync } from 'node:fs'
import { test as base, expect, type Page } from '@playwright/test'
import { PAGE, card, chip, dialog, toast } from '../e2e/fixtures'
import { menuItem, openMenu } from '../e2e/node-helpers'
import { qrPath, row, shownQrPath } from '../e2e/subs'

const HOST = process.env.RN_NODE_HOST ?? ''
const PASSWORD = process.env.RN_NODE_PASSWORD ?? ''
const NODE = process.env.RN_NODE_NAME ?? 'node-a'
const SHOTS = process.env.RN_SHOTS ?? 'test-results-real/shots'
const SUB = 'spec-bob'

// What the browser and the hub said while a test ran. A page error or a console error fails the
// test; so does a response of 400 or more that the test did not expect.
const test = base.extend<{ seen: { errors: string[]; bad: string[]; warnings: string[] } }>({
  seen: [
    async ({ page }, use, info) => {
      const seen = { errors: [] as string[], bad: [] as string[], warnings: [] as string[] }
      page.on('pageerror', (e) => seen.errors.push(`pageerror: ${e.message}`))
      page.on('console', (m) => {
        if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) seen.errors.push(`console.error: ${m.text()}`)
        if (m.type() === 'warning') seen.warnings.push(m.text())
      })
      page.on('response', (r) => {
        if (r.status() >= 400) seen.bad.push(`${r.status()} ${r.request().method()} ${new URL(r.url()).pathname}`)
      })
      await use(seen)
      // shown, not failed: the page handles a 404 for the link of a switched-off subscription
      if (seen.bad.length) console.log(`   [${info.project.name}] ${info.title}: responses of 400 or more: ${seen.bad.join('; ')}`)
      if (seen.warnings.length) console.log(`   [${info.project.name}] ${info.title}: console warnings: ${seen.warnings.join('; ')}`)
      expect(seen.errors, 'the page logged errors').toEqual([])
    },
    { auto: true },
  ],
})

test.describe.configure({ mode: 'serial' })

const api = (page: Page, path: string) => page.request.get(path, { headers: PAGE })

async function nodeInfo(page: Page, name = NODE): Promise<{ id: number; host: string }> {
  const nodes: { id: number; name: string; host: string }[] = await (await api(page, 'api/nodes')).json()
  const n = nodes.find((x) => x.name === name)
  expect(n, `node ${name} is listed`).toBeTruthy()
  return n!
}

const nodeId = async (page: Page, name = NODE) => (await nodeInfo(page, name)).id

test('serves the page, the health check and the API guard', async ({ page, request, baseURL }) => {
  const res = await request.get('./')
  expect(res.status()).toBe(200)
  expect(res.headers()['content-type']).toContain('text/html')
  const html = await res.text()
  expect(html).toContain('<div id="root"')
  // a base path lands in the page as a <base> tag, so the relative api/ calls work
  const base = new URL(baseURL!).pathname.replace(/\/$/, '')
  if (base) expect(html).toContain(`<base href="${base}/">`)
  else expect(html).not.toContain('<base href="/api/plugin')
  expect(res.headers()['cache-control']).toBe('no-store')

  const health = await request.get('health')
  expect(health.ok()).toBe(true)
  expect(await health.json()).toMatchObject({ ok: true })
  // no header, no API
  const bare = await request.get('api/config')
  expect(bare.status()).toBe(403)
  expect(await bare.json()).toMatchObject({ code: 'forbidden' })
  const cfg = await request.get('api/config', { headers: PAGE })
  expect(cfg.status()).toBe(200)

  await page.goto('./')
  await expect(page.getByRole('heading', { name: 'Entry is on' })).toBeVisible()
  await expect(chip(page, 'Domestic link', 'online')).toBeVisible()
  await expect(chip(page, 'VPN', 'WireGuard')).toBeVisible()
  await expect(chip(page, 'Bridge', 'running')).toBeVisible()
  mkdirSync(SHOTS, { recursive: true })
  await page.screenshot({ path: `${SHOTS}/${test.info().project.name}-root.png`, fullPage: true })
})

test('the Nodes tab shows the installed node, working, with its address and versions', async ({ page }) => {
  await page.goto('./')
  const c = card(page, NODE)
  await expect(c).toBeVisible()
  await expect(c).toContainText((await nodeInfo(page)).host)
  await expect(c).toContainText('Working')
  await expect(c).toContainText(/agent 1\.3\.\d · Xray 26\.10\.21/)
})

test('adds a subscription, and its link and QR code are the hub\'s', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  // a run that stopped half way may have left it; the name is free again afterwards
  for (const s of await (await api(page, 'api/subscriptions')).json()) {
    if (s.name === SUB) await page.request.delete(`api/subscriptions/${s.id}`, { headers: PAGE })
  }
  await page.goto('./#/subscriptions')
  await expect(page.getByRole('button', { name: 'Add subscription' })).toBeVisible()
  await page.getByRole('button', { name: 'Add subscription' }).click()
  const d = dialog(page, 'Add a subscription')
  await d.getByLabel('Name').fill(SUB)
  await d.getByRole('button', { name: 'Add subscription' }).click()
  await expect(toast(page, `${SUB} added`)).toBeVisible()
  // its link is ready, so the page opens the share dialog on it
  const share = dialog(page, `Share ${SUB}'s link`)
  await expect(share).toBeVisible()
  const link = share.locator('[data-link]')
  const host = (await nodeInfo(page)).host.replace(/\./g, '\\.')
  await expect(link).toHaveText(new RegExp(`^vless://[0-9a-f-]{36}@${host}:\\d+\\?.*#${SUB}$`))
  await expect(link).toHaveAttribute('dir', 'ltr')
  const text = await link.innerText()
  // the code on the screen draws exactly this link
  expect(await shownQrPath(page)).toBe(qrPath(text))
  await expect(share.getByRole('img', { name: new RegExp(`QR code of ${SUB}'s link on ${NODE}`) })).toBeVisible()
  // the hub gives the same link
  const id = (await (await api(page, 'api/subscriptions')).json()).find((s: { name: string }) => s.name === SUB).id
  const nid = await nodeId(page)
  const viaApi = await (await api(page, `api/subscriptions/${id}/nodes/${nid}/link`)).json()
  expect(viaApi.vless).toBe(text)
  await share.getByRole('button', { name: 'Copy link' }).click()
  await expect(share.getByRole('button', { name: 'Copied' })).toBeVisible()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(text)
  // kept for the script that tries the link from a real client
  mkdirSync('test-results-real', { recursive: true })
  writeFileSync('test-results-real/spec-link.txt', `${text}\n`, { mode: 0o600 })
  await page.screenshot({ path: `${SHOTS}/${test.info().project.name}-share.png` })
  await share.getByRole('button', { name: 'Close', exact: true }).click()
  await expect(share).toHaveCount(0)
  // and the same dialog opens again from the row
  await row(page, SUB).getByRole('button', { name: 'Share', exact: true }).click()
  await expect(share).toBeVisible()
  await expect(share.locator('[data-link]')).toHaveText(text)
  await share.getByRole('button', { name: 'Close', exact: true }).click()
})

test('the switch turns the subscription off and on, and the link stays the same', async ({ page }) => {
  await page.goto('./#/subscriptions')
  const sw = page.getByRole('switch', { name: `${SUB} on ${NODE}`, exact: true })
  await expect(sw).toHaveAttribute('aria-checked', 'true')
  const id = (await (await api(page, 'api/subscriptions')).json()).find((s: { name: string }) => s.name === SUB).id
  const nid = await nodeId(page)
  const before = (await (await api(page, `api/subscriptions/${id}/nodes/${nid}/link`)).json()).vless

  await sw.click()
  await expect(sw).toHaveAttribute('aria-checked', 'false')
  await expect(toast(page, /switched off/)).toBeVisible()
  const off = await api(page, `api/subscriptions/${id}/nodes/${nid}/link`)
  expect(off.status(), 'a switched-off subscription has no link').toBe(404)

  await sw.click()
  await expect(sw).toHaveAttribute('aria-checked', 'true')
  await expect(toast(page, /switched on/)).toBeVisible()
  const after = (await (await api(page, `api/subscriptions/${id}/nodes/${nid}/link`)).json()).vless
  expect(after).toBe(before)
})

test('the node page has its tabs, and the status tab says the tunnel is up', async ({ page }) => {
  const id = await nodeId(page)
  await page.goto(`./#/nodes/${id}`)
  await expect(page.getByRole('heading', { level: 1, name: NODE })).toBeVisible()
  for (const name of ['Subscriptions', 'Status', 'Settings']) await expect(page.getByRole('tab', { name })).toBeVisible()
  await expect(page.getByRole('switch', { name: `${SUB} on ${NODE}`, exact: true })).toBeVisible()
  await page.getByRole('tab', { name: 'Status' }).click()
  await expect(page.getByText(/tunnel/i).first()).toBeVisible()
  await page.screenshot({ path: `${SHOTS}/${test.info().project.name}-node-status.png`, fullPage: true })
  await page.getByRole('tab', { name: 'Settings' }).click()
  await expect(page.getByRole('heading', { level: 2 }).first()).toBeVisible()
  await openMenu(page)
  await expect(menuItem(page, 'Remove node…')).toBeVisible()
  await page.keyboard.press('Escape')
})

test('the spec subscription is deleted again', async ({ page }) => {
  await page.goto('./#/subscriptions')
  await page.getByRole('button', { name: `Actions for ${SUB}`, exact: true }).click()
  await page.getByRole('menuitem', { name: /Delete/ }).click()
  const confirm = page.getByRole('alertdialog')
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: /Delete/ }).click()
  await expect(page.getByRole('switch', { name: `${SUB} on ${NODE}`, exact: true })).toHaveCount(0)
  const left = await (await api(page, 'api/subscriptions')).json()
  expect(left.find((s: { name: string }) => s.name === SUB)).toBeUndefined()
})

test.describe('Persian, right to left, at the width this project gives', () => {
  test('the language button flips the page and nothing runs off the side', async ({ page, isMobile }) => {
    await page.goto('./')
    await expect(card(page, NODE)).toBeVisible()
    await page.getByRole('button', { name: 'فارسی' }).click()
    const html = page.locator('html')
    await expect(html).toHaveAttribute('lang', 'fa')
    await expect(html).toHaveAttribute('dir', 'rtl')
    await expect(page.getByRole('heading', { name: 'ورودی روشن است' })).toBeVisible()
    for (const name of ['نودها', 'اشتراک‌ها', 'تنظیمات']) await expect(page.getByRole('tab', { name })).toBeVisible()
    // Persian digits in the counts
    await expect(page.getByText(/[۰-۹]+ از [۰-۹]+ نود/)).toBeVisible()
    // the tabs run from the right
    const tabs = await Promise.all(['نودها', 'اشتراک‌ها', 'تنظیمات'].map((name) => page.getByRole('tab', { name }).boundingBox()))
    expect(tabs[0]!.x).toBeGreaterThan(tabs[1]!.x)
    expect(tabs[1]!.x).toBeGreaterThan(tabs[2]!.x)
    await expectNoSideScroll(page)
    await page.screenshot({ path: `${SHOTS}/${test.info().project.name}-fa-root.png`, fullPage: true })

    for (const [route, label] of [['#/subscriptions', 'اشتراک‌ها'], ['#/settings', 'تنظیمات']] as const) {
      await page.goto(`./${route}`)
      await expect(page.getByRole('tab', { name: label })).toHaveAttribute('aria-selected', 'true')
      await expectNoSideScroll(page)
      await page.screenshot({ path: `${SHOTS}/${test.info().project.name}-fa-${route.slice(2)}.png`, fullPage: true })
    }
    const id = await nodeId(page)
    await page.goto(`./#/nodes/${id}`)
    await expect(page.getByRole('heading', { level: 1, name: NODE })).toBeVisible()
    await expectNoSideScroll(page)
    await page.screenshot({ path: `${SHOTS}/${test.info().project.name}-fa-node.png`, fullPage: true })
    if (isMobile) expect(page.viewportSize()!.width).toBeLessThan(500)
    // leave the language as it was found
    await page.getByRole('button', { name: 'English' }).click()
    await expect(html).toHaveAttribute('lang', 'en')
  })
})

async function expectNoSideScroll(page: Page) {
  const over = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }))
  expect(over.scroll, 'the page is wider than the window').toBeLessThanOrEqual(over.client + 1)
}

test.describe('removing the node through the page, then installing it again', () => {
  test.skip(!process.env.RN_DESTRUCTIVE, 'RN_DESTRUCTIVE=1 removes and reinstalls the node')

  test('removes the node with its SSH login, and the card is gone', async ({ page }) => {
    const id = await nodeId(page)
    await page.goto(`./#/nodes/${id}`)
    await expect(page.getByRole('heading', { level: 1, name: NODE })).toBeVisible()
    await openMenu(page)
    await menuItem(page, 'Remove node…').click()
    const d = dialog(page, `Remove ${NODE}?`)
    await expect(d.getByText('Removing the agent and Xray from the server needs SSH access. Log in to do it.')).toBeVisible()
    await d.locator('input[type="password"]').fill(PASSWORD)
    await d.getByRole('button', { name: 'Log in and remove' }).click()
    const job = dialog(page, /^(Removing the node|Node removed)/)
    for (const step of ['Check the server', 'Stop the agent', 'Remove the agent', 'Forget the node']) await expect(job.getByText(step)).toBeVisible()
    await expect(dialog(page, /^Node removed/)).toBeVisible({ timeout: 60000 })
    await expect(toast(page, new RegExp(`Node removed · ${NODE}`))).toBeVisible()
    await page.screenshot({ path: `${SHOTS}/${test.info().project.name}-removed.png` })
  })

  test('installs it again: the form, the fifteen steps, the finished node', async ({ page }) => {
    await page.goto('./')
    await expect(page.getByText(/\d+ of \d+ nodes?/)).toBeVisible()
    await page.getByRole('button', { name: 'Add node' }).click()
    const form = dialog(page, 'Add a node')
    await form.getByLabel('Server address').fill(HOST)
    await form.getByLabel('Name', { exact: true }).fill(NODE)
    await form.locator('input[type="password"]').fill(PASSWORD)
    await form.getByRole('button', { name: 'Connect and install' }).click()
    const job = dialog(page, `Installing ${NODE}`)
    await expect(job).toBeVisible()
    for (const step of ['Check the server', 'Download the agent', 'Create the certificate', 'Upload the agent', 'Check the agent', 'Apply the configuration', 'Save the node']) {
      await expect(job.getByText(step)).toBeVisible()
    }
    const done = dialog(page, 'Node added')
    await expect(done).toBeVisible({ timeout: 180000 })
    await expect(done).toContainText('The agent is running and its tunnel is up.')
    await expect(done).toContainText('managed profile')
    await page.screenshot({ path: `${SHOTS}/${test.info().project.name}-installed.png` })
    await done.getByRole('button', { name: 'Open node' }).click()
    await expect(page.getByRole('heading', { level: 1, name: NODE })).toBeVisible()
  })
})
