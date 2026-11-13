import type { Page } from '@playwright/test'
import { PAGE, attention, card, chip, dialog, expect, reset, test, toast } from './fixtures'
import { confirmDialog, menuItem, openMenu, overflow, panel, row as statusRow, verdictChip } from './node-helpers'
import { addButton, flipper, isTable, linkText, openSubscriptions, placeTabs, qrPath, row, shownQrPath } from './subs'

// This router: the router's own entry, which subscriptions connect to with no node. The fixture resets
// with it off, so each test here asks for the state it is about.

const CLOUD = 'a1b2c3d4e5f6.sn.mynetname.net'
const KEY = 'Hq3vT9mXb2KdR7wLpN5sYc8ZfA1eJ4uG6oQ0iVtBnMk'

/** The card of the entry while it is off: no link, a heading and a button. */
const offCard = (page: Page) => page.getByRole('heading', { level: 2, name: 'This router' }).locator('xpath=ancestor::div[contains(@class, "rounded-lg")][1]')

type Tab = 'subscriptions' | 'status' | 'settings'

async function openRouter(page: Page, tab: Tab = 'subscriptions') {
  await page.goto(`/#/router${tab === 'subscriptions' ? '' : `/${tab}`}`)
  await expect(page.getByRole('heading', { level: 1, name: 'This router' })).toBeVisible()
}

/** The port in the page's header, once there is one. */
async function headerPort(page: Page): Promise<string> {
  const port = page.getByText(/^port \d+$/)
  await expect(port).toBeVisible()
  return (await port.innerText()).replace('port ', '')
}

test.describe('the card on the Nodes tab', () => {
  test('is first, off until it is switched on, and then waits for the router to open the port', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByText('Places subscriptions connect to: this router and your nodes.')).toBeVisible()
    const off = offCard(page)
    await expect(off).toContainText('Off')
    await expect(off).toContainText('Run Xray on this router.')
    // with nodes beside it, it keeps to the size of a card
    await expect(off).not.toContainText('Add a subscription.')
    await expect(card(page, 'This router')).toHaveCount(0)
    // it comes before the nodes
    await expect(page.locator('main .grid > *').first()).toContainText('This router')

    await off.getByRole('button', { name: 'Turn on' }).click()
    const d = confirmDialog(page, 'Turn on This router?')
    await expect(d).toContainText('The router opens one port on the Domestic link')
    await d.getByRole('button', { name: 'Turn on' }).click()
    await expect(toast(page, 'This router is turning on')).toBeVisible()

    const on = card(page, 'This router')
    await expect(on).toContainText('Starting')
    await expect(on).toContainText('Waiting for the router to open the port. This takes up to a minute.')
    // the router picks the port, and the card has it once it is open
    await expect(on).toContainText('Working', { timeout: 15000 })
    await expect(on).toContainText(new RegExp(`${CLOUD}:\\d{5}`))
    await expect(on).toContainText('0 subscriptions')
  })

  test('on a first run it is the whole page, with the three steps to a first link', async ({ page, request }) => {
    await reset(request, { fresh: true })
    await page.goto('/')
    const off = offCard(page)
    await expect(off).toContainText('Turn it on.')
    await expect(off).toContainText('Add a subscription.')
    await expect(off).toContainText('Share its link.')
    await expect(off.getByRole('button', { name: 'Turn on' })).toBeVisible()
    // a node is still on offer under it
    await expect(page.getByRole('heading', { name: 'No nodes yet' })).toBeVisible()
  })

  test('says who is online and what it moved while it works, and opens its page', async ({ page, request }) => {
    await reset(request, { local: 'on' })
    await page.goto('/')
    const c = card(page, 'This router')
    await expect(c).toContainText(`${CLOUD}:41873`)
    await expect(c).toContainText('Working')
    await expect(c).toContainText('2 of 5 online')
    await expect(c).toContainText(/1\.\d GB today/)
    await expect(c).toContainText('Xray 26.10.21 · no server needed')
    // it is not a node: the count of nodes stays what it was
    await expect(page.getByText('3 of 3 nodes')).toBeVisible()
    await c.click()
    await expect(page).toHaveURL(/#\/router$/)
    await expect(page.getByRole('heading', { level: 1, name: 'This router' })).toBeVisible()
  })

  test('says what is in the way in each state', async ({ page, request }) => {
    const shows = async (state: Parameters<typeof reset>[1], badge: string, reason: string | RegExp) => {
      await reset(request, state)
      await page.goto('/')
      await page.reload()
      const c = card(page, 'This router')
      await expect(c).toContainText(badge)
      await expect(c).toContainText(reason)
    }
    await shows({ local: 'nat' }, 'Working', "Forward port 41873 on the router in front to 192.168.1.20, or links can't reach this router.")
    await shows({ local: 'on', entry: 'vpn-offline' }, 'Paused', 'The VPN is down. Links stop until it is back, so nothing leaves another way.')
    await shows({ local: 'on', entry: 'domestic-offline' }, 'Paused', 'The Domestic link is down. Links stop until it is back.')
    await shows({ local: 'no-port' }, 'Needs attention', 'The router has not opened the port')
    await shows({ local: 'no-address' }, 'Needs attention', 'There is no name to put in links')
    await shows({ local: 'target' }, 'Needs attention', 'The router found no Reality target that works')
    await shows({ local: 'bridge-down' }, 'Needs attention', "The router's Xray is not running")
  })
})

test.describe("the router's page", () => {
  test.beforeEach(async ({ request }) => {
    await reset(request, { local: 'on' })
  })

  test('has a header with its address, and the three tabs of a node', async ({ page }) => {
    await openRouter(page)
    const main = page.locator('main')
    await expect(main).toContainText(CLOUD)
    await expect(main).toContainText('port 41873')
    await expect(main).toContainText('Xray 26.10.21')
    await expect(main).toContainText(/up 6d 4h/)
    for (const name of ['Subscriptions', 'Status', 'Settings']) await expect(page.getByRole('tab', { name })).toBeVisible()
    await expect(page.getByRole('tab', { name: 'Subscriptions' })).toHaveAttribute('aria-selected', 'true')
    await expect(attention(page)).toHaveCount(0)
    await page.getByRole('tab', { name: 'Status' }).click()
    await expect(page).toHaveURL(/#\/router\/status$/)
    await page.getByRole('link', { name: 'All nodes' }).click()
    await expect(page).toHaveURL(/#\/$/)
  })

  test('lists every subscription with its switch for the router', async ({ page }) => {
    await openRouter(page)
    await expect(flipper(page, 'Sara', 'This router')).toHaveAttribute('aria-checked', 'true')
    await expect(page.locator('main')).toContainText('On this router')
    await expect(page.locator('main')).toContainText('5 of 7')
    await expect(row(page, 'Mina')).toContainText('Online')
    await expect(row(page, 'Mina')).toContainText('2 connections')
    await expect(row(page, 'Kian').locator('[data-status]')).toHaveText('Off')
    if (isTable(page)) await expect(page.getByRole('columnheader')).toHaveText(['Subscription', 'On this router', 'Traffic', 'Limits', 'Actions'])

    await flipper(page, 'Kian', 'This router').click()
    await expect(toast(page, 'Kian switched on for This router')).toBeVisible()
    await expect(flipper(page, 'Kian', 'This router')).toHaveAttribute('aria-checked', 'true')
    await expect(page.locator('main')).toContainText('6 of 7')

    // off again, with a way back
    await flipper(page, 'Kian', 'This router').click()
    const off = toast(page, 'Kian switched off on This router')
    await expect(off).toBeVisible()
    await off.getByRole('button', { name: 'Undo' }).click()
    await expect(flipper(page, 'Kian', 'This router')).toHaveAttribute('aria-checked', 'true')
  })

  test("shares a link that carries the router's name and port, and replaces it", async ({ page }) => {
    await openRouter(page)
    await row(page, 'Sara').getByRole('button', { name: 'Share' }).click()
    const d = dialog(page, "Share Sara's link")
    await expect(d.locator('[data-link]')).toBeVisible()
    const first = await linkText(page)
    expect(first).toMatch(new RegExp(`^vless://[0-9a-f-]{36}@${CLOUD}:41873\\?.*security=reality.*pbk=${KEY}.*#Sara$`))
    expect(await shownQrPath(page)).toBe(qrPath(first))

    await d.getByRole('button', { name: 'Replace this link…' }).click()
    await confirmDialog(page, "Replace Sara's link?").getByRole('button', { name: 'Replace link' }).click()
    await expect(toast(page, 'Sara has a new link')).toBeVisible()
    await expect.poll(() => linkText(page)).not.toBe(first)
    expect(await linkText(page)).toContain(`@${CLOUD}:41873?`)
  })

  test('says how it is doing on the Status tab', async ({ page }) => {
    await openRouter(page, 'status')
    await expect(page.getByRole('heading', { name: 'Everything is working' })).toBeVisible()
    await expect(page.getByText(/Last report \d+ s ago/)).toBeVisible()
    await expect(verdictChip(page, 'Port', 'open')).toBeVisible()
    await expect(verdictChip(page, 'Reality target', 'set')).toBeVisible()
    await expect(verdictChip(page, "Router's Xray", 'running')).toBeVisible()
    await expect(verdictChip(page, 'VPN', 'WireGuard')).toBeVisible()

    const address = panel(page, 'Address')
    await expect(statusRow(address, 'Name in links')).toContainText(CLOUD)
    await expect(statusRow(address, 'Name comes from')).toContainText('IP Cloud')
    await expect(statusRow(address, 'Port')).toContainText('41873')
    await expect(statusRow(address, 'Public address')).toContainText('198.51.100.24')
    await expect(statusRow(address, 'In front of it')).toContainText('no other router')

    const xray = panel(page, 'Xray')
    await expect(statusRow(xray, 'State')).toContainText('running')
    await expect(statusRow(xray, 'Version')).toContainText('26.10.21')
    await expect(statusRow(xray, 'Leaves through')).toContainText("the router's VPN")

    const use = panel(page, 'In use')
    await expect(statusRow(use, 'Online now')).toContainText('2 of 5')
    await expect(statusRow(use, 'Connections now')).toContainText('3')

    const events = panel(page, 'Recent events')
    await expect(events).toContainText('Subscription switched on')
    await expect(events).toContainText('Port opened')
    await expect(events).toContainText('Turned on')
  })

  test('shows what can be changed on the Settings tab, each with its own button', async ({ page }) => {
    await openRouter(page, 'settings')
    const address = panel(page, 'Address')
    await expect(address).toContainText(CLOUD)
    await expect(address).toContainText('from IP Cloud')
    await expect(address.getByRole('button', { name: 'Use my own name…' })).toBeEnabled()
    await expect(panel(page, 'Port')).toContainText('Port 41873')
    await expect(panel(page, 'Port')).toContainText('The router picked it at random.')
    await expect(panel(page, 'Reality target')).toContainText('www.example.com:443')
    const keys = panel(page, 'Keys')
    await expect(keys.getByText(KEY, { exact: true })).toBeVisible()
    await expect(keys.getByText('9f3a1c7e', { exact: true })).toBeVisible()
    await expect(panel(page, 'Turn off This router')).toContainText('Subscriptions keep their switches and keys')
    await expect(page.getByRole('button', { name: /^Advanced/ })).toHaveAttribute('aria-expanded', 'false')
  })

  test('takes a name of its own for the links, and goes back to the IP Cloud name', async ({ page }) => {
    await openRouter(page, 'settings')
    await panel(page, 'Address').getByRole('button', { name: 'Use my own name…' }).click()
    const d = dialog(page, 'Name in links')
    await expect(d).toContainText("It must point at this router's address on the Domestic link, 198.51.100.24.")
    await expect(d).toContainText('This changes every link for This router.')
    const save = d.getByRole('button', { name: 'Save and apply' })
    await expect(save).toBeDisabled()
    await d.getByLabel('Name', { exact: true }).fill('not a name')
    await expect(d).toContainText('Give a host name, such as home.example.com.')
    await expect(save).toBeDisabled()
    await d.getByLabel('Name', { exact: true }).fill('Home.Example.com')
    await save.click()
    await expect(toast(page, 'Links now carry home.example.com')).toBeVisible()
    await expect(d).toBeHidden()
    const address = panel(page, 'Address')
    await expect(address).toContainText('home.example.com')
    await expect(address).toContainText('your own name')

    // a link carries it from then on
    await page.getByRole('tab', { name: 'Subscriptions' }).click()
    await row(page, 'Sara').getByRole('button', { name: 'Share' }).click()
    await expect(page.locator('[data-link]')).toBeVisible()
    expect(await linkText(page)).toContain('@home.example.com:41873?')
    await dialog(page, "Share Sara's link").getByRole('button', { name: 'Close' }).click()

    await page.getByRole('tab', { name: 'Settings' }).click()
    await panel(page, 'Address').getByRole('button', { name: 'Use my own name…' }).click()
    await expect(d).toContainText(`Or go back to the router's IP Cloud name, ${CLOUD}.`)
    await d.getByRole('button', { name: 'Use the IP Cloud name' }).click()
    await expect(toast(page, "Links now carry the router's IP Cloud name")).toBeVisible()
    await expect(panel(page, 'Address')).toContainText('from IP Cloud')
  })

  test('asks the router for a new port, which every link then carries', async ({ page }) => {
    await openRouter(page, 'settings')
    await panel(page, 'Port').getByRole('button', { name: 'New port…' }).click()
    const d = confirmDialog(page, 'Ask the router for a new port?')
    // five subscriptions are on it, and each gets a new link
    await expect(d).toContainText('5')
    await expect(d).toContainText('subscriptions get new links. Each needs its new link shared again.')
    await d.getByRole('button', { name: 'New port' }).click()
    await expect(toast(page, 'The router is opening a new port')).toBeVisible()
    await expect(attention(page)).toContainText('This router is starting')
    await expect(page.getByText('This router is starting')).toHaveCount(0, { timeout: 15000 })
    const port = await headerPort(page)
    expect(port).not.toBe('41873')
    await expect(panel(page, 'Port')).toContainText(`Port ${port}`)
    // a link carries the new port
    await page.getByRole('tab', { name: 'Subscriptions' }).click()
    await row(page, 'Sara').getByRole('button', { name: 'Share' }).click()
    await expect(page.locator('[data-link]')).toBeVisible()
    expect(await linkText(page)).toContain(`@${CLOUD}:${port}?`)
  })

  test('changes the Reality target once the router has checked it, or leaves the choice to the router', async ({ page }) => {
    await openRouter(page, 'settings')
    const open = () => panel(page, 'Reality target').getByRole('button', { name: 'Change Reality target…' }).click()
    await open()
    const d = dialog(page, 'Change the Reality target')
    const apply = d.getByRole('button', { name: 'Save and apply' })
    await expect(d).toContainText('The router checks it before anything changes.')
    await expect(apply).toBeDisabled()

    await d.getByLabel('Reality target').fill('fail.example.com:443')
    await d.getByRole('button', { name: 'Check', exact: true }).click()
    await expect(d.getByRole('status')).toContainText('failing')
    await expect(apply).toBeDisabled()

    await d.getByLabel('Reality target').fill('www.example.net')
    await d.getByRole('button', { name: 'Check', exact: true }).click()
    await expect(d.getByRole('status')).toContainText('working')
    await expect(d.getByRole('status')).toContainText('checked from this router')
    await apply.click()
    await expect(toast(page, 'Reality target changed to www.example.net:443')).toBeVisible()
    await expect(panel(page, 'Reality target')).toContainText('www.example.net:443')

    await open()
    await d.getByRole('button', { name: 'Let the router choose' }).click()
    await expect(toast(page, 'The router is choosing a Reality target')).toBeVisible()
    await expect(panel(page, 'Reality target')).toContainText('www.example.org:443')
  })

  test('rotates the keys from the menu, after saying how many links change', async ({ page }) => {
    await openRouter(page, 'settings')
    await openMenu(page)
    await menuItem(page, 'Rotate keys').click()
    const d = confirmDialog(page, 'Rotate the keys on This router?')
    await expect(d).toContainText('subscriptions get new links.')
    await d.getByRole('button', { name: 'Rotate keys' }).click()
    await expect(toast(page, 'Keys rotated on This router')).toBeVisible()
    await expect(panel(page, 'Keys').getByText(KEY, { exact: true })).toHaveCount(0)
  })

  test('takes a server name of its own, after a confirmation', async ({ page }) => {
    await openRouter(page, 'settings')
    await page.getByRole('button', { name: /^Advanced/ }).click()
    await page.getByLabel('Same as the Reality target').uncheck()
    await page.getByLabel('Server name', { exact: true }).fill('cdn.example.com')
    await page.getByRole('button', { name: 'Save server name…' }).click()
    const d = confirmDialog(page, 'Change the server name on This router?')
    await expect(d).toContainText('The server name becomes cdn.example.com.')
    await d.getByRole('button', { name: 'Change server name' }).click()
    await expect(toast(page, 'Server name changed on This router')).toBeVisible()
    // the form starts again from what the entry has now
    await expect(page.getByLabel('Same as the Reality target')).not.toBeChecked()
    await expect(page.getByLabel('Server name', { exact: true })).toHaveValue('cdn.example.com')
  })

  test('turns off and on again with the same port, keys and switches', async ({ page }) => {
    await openRouter(page, 'settings')
    await panel(page, 'Turn off This router').getByRole('button', { name: 'Turn off…' }).click()
    const d = confirmDialog(page, 'Turn off This router?')
    await expect(d).toContainText('subscriptions lose their links here until This router is on again.')
    await expect(d).toContainText('turning it on again brings back the same links')
    await d.getByRole('button', { name: 'Turn off' }).click()
    await expect(toast(page, 'This router is off')).toBeVisible()

    // off, it has no tabs: only what it is and the way to turn it on
    await expect(page.getByRole('tab')).toHaveCount(0)
    await expect(page.locator('main')).toContainText('Run Xray on this router.')
    await page.getByRole('button', { name: 'Turn on' }).click()
    await confirmDialog(page, 'Turn on This router?').getByRole('button', { name: 'Turn on' }).click()
    await expect(page.getByRole('tab', { name: 'Subscriptions' })).toBeVisible()
    expect(await headerPort(page)).toBe('41873')
    await page.getByRole('tab', { name: 'Subscriptions' }).click()
    await expect(flipper(page, 'Sara', 'This router')).toHaveAttribute('aria-checked', 'true')
    await expect(flipper(page, 'Kian', 'This router')).toHaveAttribute('aria-checked', 'false')
    await page.getByRole('tab', { name: 'Settings' }).click()
    await expect(panel(page, 'Keys').getByText(KEY, { exact: true })).toBeVisible()
  })
})

test.describe('what is in the way', () => {
  test('a router with no name offers to set one', async ({ page, request }) => {
    await reset(request, { local: 'no-address' })
    await openRouter(page)
    const note = attention(page)
    await expect(note).toContainText('There is no name to put in links')
    await expect(note).toContainText('IP Cloud has given the router no name.')
    await note.getByRole('button', { name: 'Set a name…' }).click()
    const d = dialog(page, 'Name in links')
    await d.getByLabel('Name', { exact: true }).fill('home.example.com')
    await d.getByRole('button', { name: 'Save and apply' }).click()
    await expect(attention(page)).toHaveCount(0)
    await expect(page.locator('main')).toContainText('home.example.com')
  })

  test('a router that found no Reality target offers to set one', async ({ page, request }) => {
    await reset(request, { local: 'target' })
    await openRouter(page, 'status')
    const note = attention(page)
    await expect(note).toContainText('The router found no Reality target that works')
    await expect(note).toContainText('None of the 6 sites tried works as a Reality target from this router. It tries again in a few minutes.')
    await expect(page.getByRole('heading', { name: "Subscriptions can't connect right now" })).toBeVisible()
    await expect(verdictChip(page, 'Reality target', 'none works')).toBeVisible()
    await note.getByRole('button', { name: 'Set a Reality target…' }).click()
    const d = dialog(page, 'Change the Reality target')
    await d.getByLabel('Reality target').fill('www.example.net:443')
    await d.getByRole('button', { name: 'Check', exact: true }).click()
    await d.getByRole('button', { name: 'Save and apply' }).click()
    await expect(attention(page)).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Everything is working' })).toBeVisible()
  })

  test('a paused router points at Settings, where entry says what is missing', async ({ page, request }) => {
    await reset(request, { local: 'on', entry: 'vpn-offline' })
    await openRouter(page)
    const note = attention(page)
    await expect(note).toContainText('This router is paused')
    await expect(note).toContainText('The VPN is down.')
    // who is online isn't known while nothing gets through
    await expect(row(page, 'Mina')).toContainText('this router is not working')
    await note.getByRole('button', { name: 'Open Settings' }).click()
    await expect(page).toHaveURL(/#\/settings$/)
  })

  test("a router whose port isn't open keeps a switch for later, and has no link to share yet", async ({ page, request }) => {
    await reset(request, { local: 'no-port' })
    await openRouter(page)
    await expect(attention(page)).toContainText('The router has not opened the port')
    await expect(attention(page)).toContainText("Check that the Domestic link is up and that this plugin's script is still on the router.")
    // a switch that is on waits for the router, and says so
    await expect(row(page, 'Sara')).toContainText('when it is back')
    await flipper(page, 'Kian', 'This router').click()
    await expect(toast(page, 'Kian switched on for This router. It takes effect when it is working again.')).toBeVisible()
    await expect(row(page, 'Kian')).toContainText('when it is back')
    await row(page, 'Sara').getByRole('button', { name: 'Share' }).click()
    const d = dialog(page, "Share Sara's link")
    await expect(d.getByRole('alert')).toContainText('This router has no port or no name to put in a link yet.')
    await expect(d.getByRole('button', { name: 'Copy link' })).toBeDisabled()
  })

  test('behind another router it works, and says which port to forward', async ({ page, request }) => {
    await reset(request, { local: 'nat' })
    await openRouter(page, 'status')
    await expect(attention(page)).toContainText('This router sits behind another one')
    await expect(page.getByRole('heading', { name: 'Working, with 1 warning' })).toBeVisible()
    await expect(statusRow(panel(page, 'Address'), 'In front of it')).toContainText('another router')
    await expect(statusRow(panel(page, 'Address'), 'On the Domestic link')).toContainText('192.168.1.20')
  })
})

test.describe('the Subscriptions tab', () => {
  test.beforeEach(async ({ request }) => {
    await reset(request, { local: 'on' })
  })

  test('has a switch for the router before the nodes, and counts who is online on it', async ({ page }) => {
    await openSubscriptions(page)
    await expect(flipper(page, 'Sara', 'This router')).toHaveAttribute('aria-checked', 'true')
    await expect(flipper(page, 'Kian', 'This router')).toHaveAttribute('aria-checked', 'false')
    // under the one heading for the places, the router comes first
    if (isTable(page)) await expect(page.getByRole('columnheader')).toHaveText(['Subscription', 'Switched on for', 'Traffic', 'Limits', 'Actions', 'This router', 'Node 1', /^Node 2/])
    await expect(page.locator('main')).toContainText('on This router and Node 1')
    await expect(row(page, 'Mina')).toContainText('on This router')

    await flipper(page, 'Kian', 'This router').click()
    await expect(toast(page, 'Kian switched on for This router')).toBeVisible()
    await expect(flipper(page, 'Kian', 'This router')).toHaveAttribute('aria-checked', 'true')
    // the node's switch beside it is as it was
    await expect(flipper(page, 'Kian', 'Node 1')).toHaveAttribute('aria-checked', 'false')
  })

  test("shares the router's link first, beside the nodes'", async ({ page }) => {
    await openSubscriptions(page)
    await row(page, 'Sara').getByRole('button', { name: 'Share' }).click()
    const d = dialog(page, "Share Sara's links")
    const places = placeTabs(d)
    await expect(places).toHaveText(['This router', /^Node 1/, /^Node 2/])
    await expect(places.first()).toHaveAttribute('aria-selected', 'true')
    await expect(d.locator('[data-link]')).toBeVisible()
    expect(await linkText(page)).toContain(`@${CLOUD}:41873?`)
    // the router's link works at home too, and the dialog says so for it alone
    await expect(d).toContainText('Works away from home and on your own Wi-Fi.')
    await places.nth(1).click()
    await expect.poll(() => linkText(page)).toContain('@203.0.113.24:443?')
    await expect(d).not.toContainText('Works away from home')
    await expect(d.getByRole('button', { name: 'Copy all 3 links' })).toBeVisible()
  })

  test('shows where each subscription is connected, in its line and beside the switch of that place', async ({ page }) => {
    await openSubscriptions(page)
    await expect(row(page, 'Mina').locator('[data-status]')).toHaveText('Online on This router · 2 connections')
    await expect(row(page, 'Sara').locator('[data-status]')).toHaveText('Online on Node 1 · 3 connections')
    // one green dot each, beside the switch of the place it is connected at
    for (const name of ['Mina', 'Sara']) await expect(row(page, name).getByText('connected here')).toHaveCount(1)
    if (isTable(page)) {
      await expect(page.getByRole('columnheader')).toHaveText(['Subscription', 'Switched on for', 'Traffic', 'Limits', 'Actions', 'This router', 'Node 1', /^Node 2/])
      const cells = row(page, 'Mina').getByRole('cell')
      await expect(cells.nth(0).getByText('connected here')).toHaveCount(1)
      await expect(cells.nth(1).getByText('connected here')).toHaveCount(0)
    } else {
      // on a phone the router and the nodes sit two to a row
      const cells = await Promise.all((await row(page, 'Mina').locator('ul > li').all()).map((li) => li.boundingBox()))
      expect(cells).toHaveLength(3)
      expect(Math.round(cells[0]!.y)).toBe(Math.round(cells[1]!.y))
      expect(cells[2]!.y).toBeGreaterThan(cells[0]!.y)
    }
  })

  test('adds a subscription that connects through the router, and shares its link at once', async ({ page, request }) => {
    await openSubscriptions(page)
    await page.getByRole('button', { name: 'Add subscription' }).click()
    const d = dialog(page, 'Add a subscription')
    const through = d.getByRole('group', { name: 'Connects through' })
    await expect(through.getByLabel(/^This router/)).toBeChecked()
    await expect(through).toContainText('no server needed')
    await expect(through).toContainText('Each place gives it its own link.')
    // only the router: the nodes are left out
    await through.getByLabel(/^Node 1/).uncheck()
    await d.getByLabel('Name').fill('Tara')
    await expect(addButton(d)).toHaveText('Add and share')
    await addButton(d).click()
    await expect(toast(page, 'Tara added')).toBeVisible()
    const share = dialog(page, "Share Tara's link")
    await expect(share.locator('[data-link]')).toBeVisible()
    expect(await linkText(page)).toMatch(new RegExp(`@${CLOUD}:41873\\?.*#Tara$`))

    const list = await (await request.get('/api/subscriptions', { headers: PAGE })).json()
    const tara = list.find((x: { name: string }) => x.name === 'Tara')
    expect(tara.local.enabled).toBe(true)
    expect(tara.nodes.filter((n: { enabled: boolean }) => n.enabled)).toEqual([])
  })

  test('leaves the router out of a new subscription while it is not working', async ({ page, request }) => {
    await reset(request, { local: 'no-port' })
    await openSubscriptions(page)
    await page.getByRole('button', { name: 'Add subscription' }).click()
    const through = dialog(page, 'Add a subscription').getByRole('group', { name: 'Connects through' })
    await expect(through.getByLabel(/^This router/)).not.toBeChecked()
    await expect(through).toContainText('This router not working')
    await expect(through.getByLabel(/^Node 1/)).toBeChecked()
  })

  test('offers the router from a node page too', async ({ page }) => {
    await page.goto('/#/nodes/1')
    await expect(flipper(page, 'Sara', 'Node 1')).toBeVisible()
    await page.getByRole('button', { name: 'Add subscription' }).click()
    await expect(dialog(page, 'Add a subscription').getByRole('group', { name: 'Connects through' }).getByLabel(/^This router/)).toBeChecked()
  })

  test('has no column and no choice for a router that is switched off', async ({ page, request }) => {
    await reset(request)
    await openSubscriptions(page)
    await expect(page.getByRole('switch', { name: /on This router$/ })).toHaveCount(0)
    await page.getByRole('button', { name: 'Add subscription' }).click()
    const d = dialog(page, 'Add a subscription')
    await expect(d.getByRole('group', { name: 'On these nodes' })).toBeVisible()
    await expect(d).not.toContainText('This router')
  })
})

test.describe('the rest of the page', () => {
  test.beforeEach(async ({ request }) => {
    await reset(request, { local: 'on' })
  })

  test('reads in Persian', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
    await page.goto('/')
    const c = page.getByRole('link', { name: /^این روتر/ })
    await expect(c).toContainText('در حال کار')
    await expect(c).toContainText('۲ از ۵ آنلاین')
    await expect(c).toContainText('بدون نیاز به سرور')
    await c.click()
    await expect(page.getByRole('heading', { level: 1, name: 'این روتر' })).toBeVisible()
    await expect(page.locator('main')).toContainText('پورت 41873')
    await page.getByRole('tab', { name: 'تنظیمات' }).click()
    await expect(page.locator('main')).toContainText('نامی که همهٔ لینک‌های این روتر دارند.')
    await expect(page.locator('main')).toContainText('از IP Cloud')
    await page.getByRole('tab', { name: 'وضعیت' }).click()
    await expect(page.getByRole('heading', { name: 'همه‌چیز کار می‌کند' })).toBeVisible()
    await expect(page.locator('main')).toContainText('وی پی ان روتر')
  })

  test('fits the window on every tab', async ({ page }) => {
    for (const tab of ['subscriptions', 'status', 'settings'] as const) {
      await openRouter(page, tab)
      expect(await overflow(page), tab).toBe(0)
    }
    await page.goto('/')
    await expect(card(page, 'This router')).toBeVisible()
    expect(await overflow(page)).toBe(0)
  })

  test('leaves the entry card as it was', async ({ page }) => {
    await page.goto('/')
    await expect(chip(page, "Router's Xray", 'running')).toBeVisible()
  })
})
