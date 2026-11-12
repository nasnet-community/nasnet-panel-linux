import { expect, reset, test } from './fixtures'
import { openNode, panel, row, setNode, verdictChip } from './node-helpers'

const title = (page: import('@playwright/test').Page, text: string) => expect(page.getByRole('heading', { level: 2, name: text })).toBeVisible()

test.describe('the Status tab of a node that works', () => {
  test('says all is well, with the four things it rests on', async ({ page }) => {
    await openNode(page, 1, 'status')
    await title(page, 'Everything is working')
    await expect(page.getByText(/^Last report \d+ s ago$/)).toBeVisible()
    await expect(verdictChip(page, 'Portal', 'listening')).toBeVisible()
    await expect(verdictChip(page, 'Tunnel', /^.*up 6d [34]h$/)).toBeVisible()
    await expect(verdictChip(page, "Router's Xray", 'running')).toBeVisible()
    await expect(verdictChip(page, 'VPN', 'WireGuard')).toBeVisible()
    await expect(page.locator('main ul').first().getByRole('listitem')).toHaveCount(4)
  })

  test('draws the last 15 minutes of traffic, download over a fill and upload under it', async ({ page, isMobile }) => {
    const answered = page.waitForResponse((r) => r.url().endsWith('/api/nodes/1/traffic'))
    await openNode(page, 1, 'status')
    const body = await (await answered).json()
    expect(body.stepSec).toBe(15)
    expect(body.samples).toHaveLength(61)

    await title(page, 'Traffic, last 15 minutes')
    // the legend has what is flowing now, both ways
    const traffic = panel(page, 'Traffic, last 15 minutes')
    await expect(traffic).toContainText(/Download\s*\d+(\.\d)? (KB|MB)\/s/)
    await expect(traffic).toContainText(/Upload\s*\d+(\.\d)? (KB|MB)\/s/)

    // one sentence stands in for the drawing
    const chart = page.getByRole('img', { name: /^Download .+ now, at most .+ in the last 15 minutes\. Upload .+ now\.$/ })
    await expect(chart).toBeVisible()
    // 15 minutes of 15 s steps is 61 points: a line, the area under it, and the other line
    const paths = chart.locator('svg path')
    await expect(paths).toHaveCount(3)
    const down = (await paths.nth(1).getAttribute('d'))!
    // the page's clock ticks each second and the samples sit on 15 s marks, so the oldest
    // point can start a hair inside the left edge, or have just dropped out of view
    expect([59, 60]).toContain(down.match(/L/g)?.length)
    expect(Number(/^M([\d.]+),/.exec(down)![1])).toBeLessThan(10)
    // the newest sample is at the right edge, give or take the age of the reading
    expect(Number(/L([\d.]+),[\d.]+$/.exec(down)![1])).toBeGreaterThan(560)
    expect((await paths.nth(0).getAttribute('d'))!.endsWith('Z')).toBe(true)
    // the axis tops out at a round number, and the time runs from 15 minutes ago to now
    const axis = chart.locator(':scope > div').first().locator('span')
    await expect(axis).toHaveCount(3)
    await expect(axis.nth(0)).toHaveText(/^(1|2|5) MB\/s$/)
    await expect(axis.nth(1)).toHaveText(/^(500 KB\/s|1 MB\/s|2\.5 MB\/s)$/)
    await expect(axis.nth(2)).toHaveText('0')
    // in English the labels read left to right
    expect(await chart.getByText('15 min ago', { exact: true }).evaluate((el) => getComputedStyle(el).direction)).toBe('ltr')
    // a phone has room for the two ends of the timeline only
    for (const label of isMobile ? ['15 min ago', 'now'] : ['15 min ago', '10 min', '5 min', 'now']) await expect(chart.getByText(label, { exact: true })).toBeVisible()
  })

  test('has a card of rows for the agent, Xray, the tunnel and the certificate', async ({ page }) => {
    await openNode(page, 1, 'status')
    const agent = panel(page, 'Agent')
    await expect(row(agent, 'Version')).toContainText('1.3.0')
    await expect(row(agent, 'Running for')).toContainText(/^Running for6d [34]h$/)
    await expect(row(agent, 'Memory')).toContainText('22.3 MB')
    await expect(row(agent, 'Settings')).toContainText('applied')

    const xray = panel(page, 'Xray')
    await expect(row(xray, 'State')).toContainText('running')
    await expect(row(xray, 'Version')).toContainText('26.10.21')
    await expect(row(xray, 'Restarts')).toContainText('0')
    await expect(xray.getByText('needed')).toHaveCount(0)

    const tunnel = panel(page, 'Tunnel to this router')
    await expect(row(tunnel, 'Connections')).toContainText('1 of 1 up')
    await expect(row(tunnel, 'Round trip')).toContainText('23 ms')
    await expect(row(tunnel, 'Connections now')).toContainText('9')
    await expect(row(tunnel, 'Download')).toContainText('1.9 TB')
    await expect(row(tunnel, 'Upload')).toContainText('412 GB')
    // a number that is off says what it means
    await expect(row(tunnel, "Couldn't reach the internet")).toContainText('3')
    await expect(row(tunnel, "Couldn't reach the internet")).toContainText('The router could not open a connection. A few now and then is normal.')
    await expect(tunnel.getByText("Totals count from the plugin's last start.")).toBeVisible()

    const cert = panel(page, 'Certificate')
    await expect(row(cert, 'Valid for')).toContainText(/\d+ days/)
    await expect(row(cert, 'Valid until')).toContainText(/\d{1,2} \w{3} 20\d\d/)
    await expect(row(cert, 'Renews')).toContainText('30 days before it ends')
  })

  test('shows a dash for what the hub has no source for, and not a row of zeros', async ({ page, request }) => {
    await setNode(request, { id: 1, unmeasured: true })
    await openNode(page, 1, 'status')
    const agent = panel(page, 'Agent')
    await expect(row(agent, 'Running for')).toContainText(/^Running for–$/)
    await expect(row(agent, 'Memory')).toContainText(/^Memory–$/)
    // no news of the settings is not news that they are waiting
    await expect(row(agent, 'Settings')).toContainText(/^Settings–$/)
    await expect(row(agent, 'Settings').locator('dd')).not.toHaveClass(/text-warn-text/)
    await expect(row(panel(page, 'Tunnel to this router'), 'Round trip')).toContainText(/^Round trip–$/)
    // the node's header has no "up 0s" either, though it still has the agent's version
    await expect(page.getByText('up 0s', { exact: true })).toHaveCount(0)
    await expect(page.getByText('agent 1.3.0', { exact: true })).toBeVisible()
  })

  test('lists what happened to the node, newest first, in the language of the page', async ({ page }) => {
    await openNode(page, 1, 'status')
    const events = panel(page, 'Recent events').getByRole('listitem')
    await expect(events).toHaveCount(4)
    await expect(events.nth(0)).toContainText('Tunnel up')
    await expect(events.nth(0)).toContainText('after 2 s')
    await expect(events.nth(1)).toContainText('Agent updated')
    await expect(events.nth(1)).toContainText('1.2.0 to 1.3.0')
    await expect(events.nth(2)).toContainText('Certificate renewed')
    await expect(events.nth(3)).toContainText('Tunnel up')
    // when each was, for the ones that fit beside the words
    await expect(events.nth(0)).toContainText('2 hours ago')
    await expect(events.nth(1)).toContainText('1 day ago')
    await expect(events.nth(2)).toContainText('6 days ago')
  })
})

test.describe('the Status tab of a node in trouble', () => {
  test('with the wrong Xray: the tunnel is off, the bridge waits, and the card says what is needed', async ({ page, request }) => {
    await setNode(request, { id: 1, xrayVersion: '26.10.18' })
    await openNode(page, 1, 'status')
    await title(page, "Subscriptions can't connect right now")
    await expect(verdictChip(page, 'Portal', 'listening')).toBeVisible()
    await expect(verdictChip(page, 'Tunnel', 'off')).toBeVisible()
    await expect(verdictChip(page, "Router's Xray", 'waiting')).toBeVisible()
    await expect(verdictChip(page, 'VPN', 'WireGuard')).toBeVisible()
    const xray = panel(page, 'Xray')
    await expect(row(xray, 'Version')).toContainText('26.10.18')
    await expect(row(xray, 'Version')).toContainText('26.10.21 needed')
    const tunnel = panel(page, 'Tunnel to this router')
    await expect(row(tunnel, 'Connections')).toContainText('0 of 1 up')
    await expect(row(tunnel, 'Connections')).toContainText('The portal answers, but the tunnel stays off until Xray matches.')
    await expect(row(tunnel, 'Round trip')).toContainText('–')
    // nothing flows through a tunnel that is off
    await expect(panel(page, 'Traffic, last 15 minutes')).toContainText(/Download\s*0 B\/s/)
    const events = panel(page, 'Recent events').getByRole('listitem')
    await expect(events.nth(0)).toContainText("Tunnel off")
    await expect(events.nth(0)).toContainText("Xray 26.10.18 doesn't match 26.10.21")
    await expect(events.nth(1)).toContainText('Tunnel up')
    await expect(events.nth(2)).toContainText('Xray changed')
    await expect(events.nth(2)).toContainText('on the node')
  })

  test("with the router unable to reach it: dashes, old numbers said to be old, and no traffic to draw", async ({ page }) => {
    await openNode(page, 2, 'status')
    await title(page, "The router can't reach this node")
    await expect(page.getByText("Last report 22 minutes ago. The router can't reach this node, so these numbers are old.")).toBeVisible()
    for (const label of ['Portal', 'Tunnel', "Router's Xray", 'VPN']) await expect(verdictChip(page, label, '–')).toBeVisible()
    await expect(page.getByText('No readings in the last 15 minutes.')).toBeVisible()
    await expect(panel(page, 'Traffic, last 15 minutes')).toContainText(/Download\s*–/)
    // what it last said is still there, with the update it is due
    await expect(row(panel(page, 'Agent'), 'Version')).toContainText('1.2.0')
    await expect(row(panel(page, 'Agent'), 'Version')).toContainText('1.3.0 available')
    await expect(row(panel(page, 'Xray'), 'Version')).toContainText('26.10.18')
    await expect(row(panel(page, 'Xray'), 'Version')).toContainText('26.10.21 available')
    await expect(row(panel(page, 'Tunnel to this router'), 'Connections')).toContainText('0 of 1 up')
    const events = panel(page, 'Recent events').getByRole('listitem')
    await expect(events.nth(0)).toContainText('Node unreachable')
    await expect(events.nth(0)).toContainText('no answer over the Domestic link')
    await expect(events.nth(0)).toContainText('22 minutes ago')
  })

  test('with Xray crashing: the portal is down, the restarts are counted and the last exit is shown', async ({ page, request }) => {
    await setNode(request, { id: 1, crash: true })
    await openNode(page, 1, 'status')
    await title(page, "Subscriptions can't connect right now")
    await expect(verdictChip(page, 'Portal', 'not running')).toBeVisible()
    await expect(verdictChip(page, 'Tunnel', 'down')).toBeVisible()
    const xray = panel(page, 'Xray')
    await expect(row(xray, 'State')).toContainText('keeps crashing')
    await expect(row(xray, 'Running for')).toContainText('not running')
    await expect(row(xray, 'Restarts')).toContainText('14')
    await expect(row(xray, 'Restarts')).toContainText('It restarts on its own and waits longer each time.')
    await expect(row(xray, 'Last exit')).toContainText('exit status 1: config: address already in use')
    await expect(row(panel(page, 'Tunnel to this router'), 'Connections')).toContainText("The router can't open the tunnel. It keeps trying.")
  })

  test('with settings the agent could not apply, or a certificate near its end, counts warnings', async ({ page, request }) => {
    await setNode(request, { id: 1, notApplied: 'xray -test failed: port in use' })
    await openNode(page, 1, 'status')
    await title(page, 'Working, with 1 warning')
    await expect(row(panel(page, 'Agent'), 'Settings')).toContainText('xray -test failed: port in use')
    await expect(row(panel(page, 'Agent'), 'Settings').locator('dd')).toHaveClass(/text-warn-text/)

    await setNode(request, { id: 1, certDays: 9 })
    await page.reload()
    await title(page, 'Working, with 2 warnings')
    const days = row(panel(page, 'Certificate'), 'Valid for').locator('dd')
    await expect(days).toHaveText('9 days')
    await expect(days).toHaveClass(/text-warn-text/)
    await setNode(request, { id: 1, certDays: 2 })
    await page.reload()
    await expect(row(panel(page, 'Certificate'), 'Valid for').locator('dd')).toHaveClass(/text-bad-text/)
  })

  test('with entry off it is a node that works, waiting for the router', async ({ page, request }) => {
    await reset(request, { entry: 'vpn-offline' })
    await openNode(page, 1, 'status')
    await title(page, 'The tunnel is waiting for entry to turn on')
    await expect(verdictChip(page, 'Portal', 'listening')).toBeVisible()
    await expect(verdictChip(page, 'Tunnel', 'off')).toBeVisible()
    await expect(verdictChip(page, "Router's Xray", 'waiting')).toBeVisible()
    await expect(verdictChip(page, 'VPN', 'offline')).toBeVisible()
    await expect(row(panel(page, 'Tunnel to this router'), 'Connections')).toContainText('The tunnel starts when entry is on.')
  })

  test('refreshes by itself, so the page follows the node', async ({ page, request }) => {
    await openNode(page, 1, 'status')
    await title(page, 'Everything is working')
    await setNode(request, { id: 1, xrayVersion: '26.10.18' })
    // the node is polled every few seconds
    await expect(page.getByRole('heading', { level: 2, name: "Subscriptions can't connect right now" })).toBeVisible({ timeout: 12000 })
    await expect(page.getByRole('region', { name: 'Needs attention' })).toContainText('The tunnel is off')
  })
})
