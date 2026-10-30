import { attention, card, chip, dialog, expect, fillAddNode, reset, test, toast, type Reset } from './fixtures'

test('before setup the entry is off, the page says what is missing, and nodes can still be added', async ({ page, request }) => {
  await reset(request, { fresh: true })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Entry is off' })).toBeVisible()
  await expect(page.getByText('Checked 2 min ago')).toBeVisible()
  await expect(chip(page, 'Domestic link', 'missing')).toBeVisible()
  await expect(chip(page, 'VPN', 'missing')).toBeVisible()
  await expect(chip(page, 'Bridge', 'stopped')).toBeVisible()

  // one item for each thing that is missing, each with a way to the setup
  const items = attention(page)
  await expect(items).toContainText('This router has no Domestic link')
  await expect(items).toContainText("Subscriptions can't reach their exit until it's set up. You can still add and update nodes. Add a Domestic link in NasNet Panel's setup.")
  await expect(items).toContainText('This router has no VPN')
  await expect(items).toContainText("Traffic from subscriptions leaves through the VPN. Add one in NasNet Panel's setup.")
  const setup = items.getByRole('link', { name: 'Open setup' })
  await expect(setup).toHaveCount(2)
  for (const link of await setup.all()) {
    await expect(link).toHaveAttribute('target', '_blank')
    await expect(link).toHaveAttribute('rel', /noopener/)
  }

  // the empty state, with the count and a button that works
  await expect(page.getByText('0 of 3 nodes')).toBeVisible()
  await expect(page.getByText('Limit reached')).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'No nodes yet' })).toBeVisible()
  await expect(page.getByText('Add a Linux server over SSH. The login is used once, never stored.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add node' })).not.toHaveAttribute('aria-disabled', 'true')
  await page.getByRole('button', { name: 'Add your first node' }).click()
  const form = dialog(page, 'Add a node')
  await expect(form.getByLabel('Name', { exact: true })).toHaveAttribute('placeholder', 'Node 1')

  // a node installs with the entry off; the dialog says its tunnel waits
  await fillAddNode(page, '192.0.2.41')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const done = dialog(page, 'Node added')
  await expect(done).toBeVisible({ timeout: 20000 })
  await expect(done).toContainText('The agent is running. Its tunnel starts when entry is on.')
  await expect(toast(page, 'Node 1 added')).toBeVisible()
  await expect(card(page, 'Node 1')).toContainText('Working')
  await expect(page.getByRole('heading', { name: 'No nodes yet' })).toHaveCount(0)
  await expect(page.getByText('1 of 3 nodes')).toBeVisible()
})

const CASES: [string, Reset['entry'], { heading: string; chips: [string, string][]; items?: string[]; note?: string }][] = [
  [
    'the Domestic link is down',
    'domestic-offline',
    { heading: 'Entry is off', chips: [['Domestic link', 'offline'], ['VPN', 'WireGuard']], items: ['The Domestic link is offline'] },
  ],
  [
    'the VPN is down',
    'vpn-offline',
    { heading: 'Entry is off', chips: [['Domestic link', 'online'], ['VPN', 'offline'], ['Bridge', 'waiting']], items: ['The VPN is offline'] },
  ],
  [
    'RouterOS is too old',
    'old-routeros',
    {
      heading: 'Entry is off',
      chips: [['Domestic link', 'online'], ['VPN', 'WireGuard']],
      items: ['RouterOS 7.9 is too old', 'Remote Nodes needs RouterOS 7.11 or newer. Update RouterOS, then reload this page.'],
    },
  ],
  [
    'the router has not reported for a while',
    'stale',
    { heading: 'Entry is on', chips: [['Domestic link', 'online'], ['Bridge', 'running']], note: 'Checked 20 min ago, may be out of date' },
  ],
]

for (const [name, entry, want] of CASES) {
  test(`entry when ${name}`, async ({ page, request }) => {
    await reset(request, { entry })
    await page.goto('/')
    await expect(page.getByRole('heading', { name: want.heading })).toBeVisible()
    for (const [label, value] of want.chips) await expect(chip(page, label, value)).toBeVisible()
    if (want.items) for (const text of want.items) await expect(attention(page)).toContainText(text)
    else await expect(attention(page)).toHaveCount(0)
    if (want.note) {
      // a warning, not just text
      await expect(page.getByText(want.note)).toHaveClass(/text-warn-text/)
    }
    // the nodes are listed all the same
    await expect(card(page, 'Node 1')).toBeVisible()
  })
}

test('with the entry off, a node that works is still working', async ({ page, request }) => {
  await reset(request, { entry: 'vpn-offline' })
  await page.goto('/')
  await expect(card(page, 'Node 1')).toContainText('Working')
})
