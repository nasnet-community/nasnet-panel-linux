import type { Page } from '@playwright/test'
import { expect, reset, test, toast } from './fixtures'
import { confirmDialog, openNode, panel } from './node-helpers'
import { obRow, openOutbounds } from './outbounds'

const card = (page: Page) => panel(page, 'Leaves through')
const choice = (page: Page) => card(page).getByRole('combobox', { name: 'Leaves through' })

async function openRouterSettings(page: Page) {
  await page.goto('/#/router/settings')
  await expect(page.getByRole('heading', { level: 1, name: 'This router' })).toBeVisible()
}

test.describe("a node's Settings tab", () => {
  test('shows the outbound it leaves through and how that is doing', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await expect(card(page)).toContainText('Where traffic from this node goes out to the internet.')
    await expect(choice(page)).toHaveValue('1')
    await expect(choice(page).locator('option:checked')).toHaveText('Frankfurt · VLESS · TCP · REALITY')
    await expect(card(page)).toContainText(/Up · \d+ ms/)
    await expect(card(page)).toContainText('de.example.com:443')
    await expect(card(page).getByRole('button', { name: 'Save…' })).toBeDisabled()
    // the router's VPN first, then the outbounds by name
    await expect(choice(page).locator('option')).toHaveText([
      "Router's VPN",
      'Amsterdam · Trojan · WebSocket · TLS',
      'Frankfurt · VLESS · TCP · REALITY',
      'Helsinki · Shadowsocks',
      'Office · SOCKS',
      'Paris · VMess · WebSocket · TLS',
    ])
  })

  test("goes back to the router's VPN after asking, with what the restart does", async ({ page }) => {
    await openNode(page, 1, 'settings')
    await choice(page).selectOption({ label: "Router's VPN" })
    await card(page).getByRole('button', { name: 'Save…' }).click()
    const ask = confirmDialog(page, 'Change where Node 1 leaves through?')
    await expect(ask).toContainText("Its traffic will leave through Router's VPN.")
    await expect(ask).toContainText("The router's Xray restarts. Open connections drop for a few seconds, and apps reconnect on their own.")
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/nodes/1/route') && r.method() === 'PUT')
    await ask.getByRole('button', { name: 'Change' }).click()
    expect((await sent).postDataJSON()).toEqual({ outboundId: 0 })
    await expect(toast(page, "Node 1 now leaves through Router's VPN")).toBeVisible()
    await expect(ask).toBeHidden()
    await expect(choice(page)).toHaveValue('0')
    await expect(card(page).getByRole('button', { name: 'Save…' })).toBeDisabled()

    // the outbound has nobody left on it
    await openOutbounds(page)
    await expect(obRow(page, 'Frankfurt').locator('[data-state="idle"]')).toBeVisible()
  })

  test('Cancel leaves it as it was', async ({ page, request }) => {
    await openNode(page, 1, 'settings')
    await choice(page).selectOption('4')
    await card(page).getByRole('button', { name: 'Save…' }).click()
    await confirmDialog(page, 'Change where Node 1 leaves through?').getByRole('button', { name: 'Cancel' }).click()
    const node = await (await request.get('/api/nodes/1', { headers: { 'X-RemoteNodes-UI': '1' } })).json()
    expect(node.outboundId).toBe(1)
  })

  test('to another outbound: not checked at first, then how it is doing', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await choice(page).selectOption({ label: 'Helsinki · Shadowsocks' })
    await card(page).getByRole('button', { name: 'Save…' }).click()
    await confirmDialog(page, 'Change where Node 1 leaves through?').getByRole('button', { name: 'Change' }).click()
    await expect(toast(page, 'Node 1 now leaves through Helsinki')).toBeVisible()
    await expect(card(page)).toContainText('198.51.100.61:8388')
    await expect(card(page)).toContainText(/Not checked yet|Up · \d+ ms/)
    await expect(card(page)).toContainText(/Up · \d+ ms/, { timeout: 15000 })
  })

  test('says what happens to the traffic while its outbound is down, with or without the fallback', async ({ page }) => {
    // Node 2 can't be reached, but where its traffic leaves is the router's business, so it still changes
    await openNode(page, 2, 'settings')
    await expect(card(page)).toContainText("Down · on the router's VPN")
    await expect(card(page)).toContainText("Amsterdam doesn't answer, so this traffic leaves through the router's VPN for now.")
    await expect(choice(page)).toBeEnabled()
    await choice(page).selectOption({ label: 'Paris · VMess · WebSocket · TLS' })
    await card(page).getByRole('button', { name: 'Save…' }).click()
    await confirmDialog(page, 'Change where Node 2 leaves through?').getByRole('button', { name: 'Change' }).click()
    await expect(card(page)).toContainText("Paris doesn't answer, so this traffic stops until it does.", { timeout: 15000 })
  })

  test('with no outbounds, points to where they are added', async ({ page, request }) => {
    await reset(request, { outbounds: false })
    await openNode(page, 1, 'settings')
    await expect(choice(page).locator('option')).toHaveText(["Router's VPN"])
    await expect(card(page)).toContainText('Add an outbound to send this traffic another way.')
    await card(page).getByRole('link', { name: 'Open Outbounds' }).click()
    await expect(page).toHaveURL(/#\/outbounds$/)
    await expect(page.getByRole('heading', { name: 'No outbounds yet' })).toBeVisible()
  })

  test("a refusal stays in the question in the page's words", async ({ page }) => {
    await openNode(page, 1, 'settings')
    await page.route('**/api/nodes/1/route', (r) => r.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ code: 'not_found', error: 'no such outbound' }) }))
    await choice(page).selectOption('5')
    await card(page).getByRole('button', { name: 'Save…' }).click()
    const ask = confirmDialog(page, 'Change where Node 1 leaves through?')
    await ask.getByRole('button', { name: 'Change' }).click()
    await expect(ask.getByRole('alert')).toHaveText('It no longer exists. Reload the page.')
  })
})

test.describe("This router's Settings tab", () => {
  test.beforeEach(async ({ request }) => {
    await reset(request, { local: 'on' })
  })

  test('changes where This router leaves through, after asking', async ({ page }) => {
    await openRouterSettings(page)
    await expect(card(page)).toContainText('Where traffic from This router goes out to the internet.')
    await expect(choice(page)).toHaveValue('3')
    await expect(card(page)).toContainText("Paris doesn't answer, so this traffic stops until it does.")
    await choice(page).selectOption({ label: 'Frankfurt · VLESS · TCP · REALITY' })
    await card(page).getByRole('button', { name: 'Save…' }).click()
    const ask = confirmDialog(page, 'Change where This router leaves through?')
    await expect(ask).toContainText('Its traffic will leave through Frankfurt.')
    await expect(ask).toContainText('Open connections drop for a few seconds')
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/local/route') && r.method() === 'PUT')
    await ask.getByRole('button', { name: 'Change' }).click()
    expect((await sent).postDataJSON()).toEqual({ outboundId: 1 })
    await expect(toast(page, 'This router now leaves through Frankfurt')).toBeVisible()
    await expect(choice(page)).toHaveValue('1')

    // its Status tab says it too
    await page.getByRole('tab', { name: 'Status' }).click()
    const row = page.locator('dt').filter({ hasText: /^Leaves through$/ }).locator('xpath=..')
    await expect(row).toContainText('Frankfurt')
  })

  test("its Status tab says the router's VPN when it leaves through that", async ({ page }) => {
    await openRouterSettings(page)
    await choice(page).selectOption('0')
    await card(page).getByRole('button', { name: 'Save…' }).click()
    await confirmDialog(page, 'Change where This router leaves through?').getByRole('button', { name: 'Change' }).click()
    await expect(toast(page, "This router now leaves through Router's VPN")).toBeVisible()
    await page.getByRole('tab', { name: 'Status' }).click()
    await expect(page.locator('dt').filter({ hasText: /^Leaves through$/ }).locator('xpath=..')).toContainText("the router's VPN")
    await page.getByRole('button', { name: 'فارسی' }).click()
    await expect(page.locator('dt').filter({ hasText: /^خروج از$/ }).locator('xpath=..')).toContainText('وی پی ان روتر')
  })
})
