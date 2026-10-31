import { card, dialog, expect, reset, stateHolds, test, toast, type Reset } from './fixtures'
import { panel, row } from './node-helpers'

const PASS = 'correct horse battery staple'

/** Exports a backup from the page as it is, and returns the file the browser got. */
async function exportBackup(page: import('@playwright/test').Page, passphrase = PASS) {
  await page.goto('/#/settings')
  await page.getByLabel('Passphrase').fill(passphrase)
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export backup' }).click()
  const file = await download
  return { name: file.suggestedFilename(), data: Buffer.concat(await collect(await file.createReadStream())) }
}

async function collect(stream: NodeJS.ReadableStream | null): Promise<Buffer[]> {
  const chunks: Buffer[] = []
  if (stream) for await (const c of stream as AsyncIterable<Buffer>) chunks.push(c)
  return chunks
}

test.describe('the router\'s entry facts and routes', () => {
  test('say what the router reports, and what its three addresses are for', async ({ page, isMobile }) => {
    await page.goto('/#/settings')
    const entry = panel(page, 'Entry')
    await expect(row(entry, 'Domestic link')).toContainText('online')
    await expect(row(entry, 'VPN')).toContainText('WireGuard · online')
    await expect(row(entry, 'Bridge')).toContainText('running')
    await expect(row(entry, 'RouterOS')).toContainText('7.24.5')
    await expect(row(entry, 'RouterOS')).toContainText('Needs 7.20 or newer.')
    await expect(row(entry, 'Checked')).toContainText('2 min ago')

    const routes = panel(page, 'Router routes')
    await expect(routes).toContainText('Each address has its own rule. If a route is missing, that traffic is dropped.')
    const rows = isMobile ? routes.getByRole('list', { name: 'Source addresses this plugin uses' }).getByRole('listitem') : routes.getByRole('table', { name: 'Source addresses this plugin uses' }).locator('tbody tr')
    await expect(rows).toHaveCount(3)
    await expect(rows.nth(0)).toContainText('192.168.50.16')
    await expect(rows.nth(0)).toContainText('Downloads')
    await expect(rows.nth(0)).toContainText('Foreign')
    await expect(rows.nth(1)).toContainText('192.168.50.17')
    await expect(rows.nth(1)).toContainText('Entry exit')
    await expect(rows.nth(1)).toContainText('VPN')
    await expect(rows.nth(2)).toContainText('192.168.50.18')
    await expect(rows.nth(2)).toContainText('Nodes and tunnel')
    await expect(rows.nth(2)).toContainText('Domestic')
    for (let i = 0; i < 3; i++) await expect(rows.nth(i)).toContainText('ready')
    if (!isMobile) {
      // a table with its columns named, for those who read it by column
      for (const h of ['Address', 'Used for', 'Route', 'Status']) await expect(routes.getByRole('columnheader', { name: h })).toBeVisible()
    }
  })

  const CASES: [string, NonNullable<Reset['entry']> | 'fresh', (page: import('@playwright/test').Page) => Promise<void>][] = [
    [
      'without a Domestic link or a VPN',
      'fresh',
      async (page) => {
        const entry = panel(page, 'Entry')
        await expect(row(entry, 'Domestic link')).toContainText('missing')
        await expect(row(entry, 'VPN')).toContainText('missing')
        await expect(row(entry, 'Bridge')).toContainText('stopped')
        // the address with no route says so; the one for downloads does not need one of these
        const routes = panel(page, 'Router routes')
        // the table and the phone's list are both in the page; one of them is shown
        await expect(routes.getByText('missing', { exact: true }).filter({ visible: true })).toHaveCount(2)
        await expect(routes.getByText('ready', { exact: true }).filter({ visible: true })).toHaveCount(1)
      },
    ],
    [
      'with the Domestic link down',
      'domestic-offline',
      async (page) => {
        await expect(row(panel(page, 'Entry'), 'Domestic link')).toContainText('offline')
        await expect(row(panel(page, 'Entry'), 'VPN')).toContainText('WireGuard · online')
      },
    ],
    [
      'with the VPN down',
      'vpn-offline',
      async (page) => {
        await expect(row(panel(page, 'Entry'), 'VPN')).toContainText('WireGuard · offline')
        await expect(row(panel(page, 'Entry'), 'Bridge')).toContainText('waiting')
      },
    ],
    [
      'with RouterOS too old',
      'old-routeros',
      async (page) => {
        const r = row(panel(page, 'Entry'), 'RouterOS')
        await expect(r).toContainText('7.9')
        await expect(r.locator('dd')).toHaveClass(/text-bad-text/)
        await expect(r).toContainText('Needs 7.20 or newer.')
      },
    ],
    [
      'when the router has not reported for a while',
      'stale',
      async (page) => {
        const r = row(panel(page, 'Entry'), 'Checked')
        await expect(r).toContainText('20 min ago')
        await expect(r.locator('dd')).toHaveClass(/text-warn-text/)
        await expect(r).toContainText("The router hasn't reported for a while, so this may be out of date.")
      },
    ],
  ]
  for (const [name, state, check] of CASES) {
    test(`show it ${name}`, async ({ page, request }) => {
      await reset(request, state === 'fresh' ? { fresh: true } : { entry: state })
      await page.goto('/#/settings')
      await expect(panel(page, 'Entry')).toBeVisible()
      await check(page)
    })
  }
})

test.describe('about', () => {
  test('gives the version, the commit, the licence, and where the source is', async ({ page }) => {
    await page.goto('/#/settings')
    const about = panel(page, 'About')
    await expect(row(about, 'Version')).toContainText('Remote Nodes 0.1.0')
    await expect(row(about, 'Commit')).toContainText('a1b2c3d')
    await expect(row(about, 'License')).toContainText('AGPL-3.0')
    await expect(about).toContainText('Update this plugin from the Plugins page in NasNet Panel.')
    const source = about.getByRole('link', { name: 'Source code' })
    await expect(source).toHaveAttribute('href', 'https://github.com/nasnet-community/nasnet-panel-linux/tree/a1b2c3d')
    const notices = about.getByRole('link', { name: 'Notices' })
    // relative, so it follows the page's base path to the hub's own /licenses
    await expect(notices).toHaveAttribute('href', 'licenses')
    for (const link of [source, notices]) {
      await expect(link).toHaveAttribute('target', '_blank')
      await expect(link).toHaveAttribute('rel', /noopener noreferrer/)
    }
  })

  test('opens the notices the hub serves, as text, in a tab of their own', async ({ page, context }) => {
    await page.goto('/#/settings')
    const [tab] = await Promise.all([context.waitForEvent('page'), panel(page, 'About').getByRole('link', { name: 'Notices' }).click()])
    await expect(tab).toHaveURL(/\/licenses$/)
    await expect(tab.locator('body')).toContainText('Sample licence text for LICENSE')
    await expect(tab.locator('body')).toContainText('Sample licence text for web-notices.txt')
    expect(await tab.evaluate(() => document.contentType)).toBe('text/plain')
    // the page is still where it was
    await expect(page).toHaveURL(/#\/settings$/)
    await expect(panel(page, 'About')).toBeVisible()
    await tab.close()
  })
})

test.describe('the backup', () => {
  test('warns until the first one, and asks for a passphrase', async ({ page }) => {
    await page.goto('/#/settings')
    const backup = panel(page, 'Backup')
    await expect(backup).toContainText('The backup holds the certificate authority every node trusts. Keep it safe.')
    await expect(backup).toContainText("No backup yet. If this router's data is lost, every node has to be installed again.")
    await expect(backup).toContainText("You need it to import the backup. It can't be recovered.")
    await expect(page.getByLabel('Passphrase')).toHaveAttribute('type', 'password')
    await expect(backup.getByText('Bring nodes back on a fresh install.')).toBeVisible()
    // no passphrase, no file
    let asked = false
    page.on('request', (r) => r.url().endsWith('/api/backup/export') && (asked = true))
    await backup.getByRole('button', { name: 'Export backup' }).click()
    await expect(backup.getByText('Enter a passphrase.')).toBeVisible()
    expect(asked).toBe(false)
  })

  test('exports a file under the name the hub gives, and then it is no longer "no backup yet"', async ({ page }) => {
    await page.goto('/#/settings')
    await page.getByLabel('Passphrase').fill(PASS)
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/backup/export') && r.method() === 'POST')
    const download = page.waitForEvent('download')
    await page.getByRole('button', { name: 'Export backup' }).click()
    const file = await download
    expect((await sent).postDataJSON()).toEqual({ passphrase: PASS })
    expect(file.suggestedFilename()).toMatch(/^remote-nodes-backup-\d{4}-\d\d-\d\d\.rnbackup$/)
    const text = Buffer.concat(await collect(await file.createReadStream())).toString()
    expect(text.startsWith('RNBK1\n')).toBe(true)
    await expect(toast(page, `Backup saved as ${file.suggestedFilename()}`)).toBeVisible()
    // the passphrase has done its work and is gone
    await expect(page.getByLabel('Passphrase')).toHaveValue('')
    expect(await stateHolds(page, PASS)).toBe(false)
    const backup = panel(page, 'Backup')
    await expect(backup).toContainText(/Last backup (just now|\d+ minutes? ago)\./)
    await expect(backup.getByText('No backup yet.')).toHaveCount(0)
    // the hub keeps the date, so a reload still knows
    await page.reload()
    await expect(panel(page, 'Backup')).toContainText(/Last backup (just now|\d+ minutes? ago)\./)
  })

  test('says what went wrong when the hub can not make the file', async ({ page }) => {
    await page.route('**/api/backup/export', (route) => route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 'internal', error: 'disk is full' }) }))
    await page.goto('/#/settings')
    await page.getByLabel('Passphrase').fill(PASS)
    await page.getByRole('button', { name: 'Export backup' }).click()
    await expect(panel(page, 'Backup').getByText('disk is full')).toBeVisible()
    // nothing was saved, so it is still no backup, and the passphrase stays to try again
    await expect(panel(page, 'Backup')).toContainText('No backup yet.')
    await expect(page.getByLabel('Passphrase')).toHaveValue(PASS)
  })
})

test.describe('importing a backup', () => {
  test('is not for an install that has nodes, and says so', async ({ page }) => {
    await page.goto('/#/settings')
    await page.getByRole('button', { name: 'Import…' }).click()
    const d = dialog(page, 'Import a backup')
    await expect(d).toContainText('The nodes come back and reconnect on their own, with no SSH login.')
    await expect(d.getByText('This install already has nodes. A backup can only be imported on an empty one.')).toBeVisible()
    await expect(d.getByRole('button', { name: 'Import backup' })).toBeDisabled()
    await expect(d.getByLabel('Backup file')).toBeDisabled()
    await d.getByRole('button', { name: 'Cancel' }).click()
    await expect(d).toBeHidden()
  })

  test('brings the nodes back on a fresh install, and tells a wrong passphrase from a wrong file', async ({ page, request }) => {
    const backup = await exportBackup(page)
    expect(backup.data.length).toBeGreaterThan(100)

    await reset(request, { fresh: true })
    await page.goto('/#/settings')
    await page.reload()
    await page.getByRole('button', { name: 'Import…' }).click()
    const d = dialog(page, 'Import a backup')
    const go = d.getByRole('button', { name: 'Import backup' })
    await expect(d.getByText('This install already has nodes.')).toHaveCount(0)

    // nothing chosen, nothing sent
    let sent = 0
    page.on('request', (r) => r.url().endsWith('/api/backup/import') && sent++)
    await go.click()
    await expect(d.getByRole('alert')).toContainText('Choose the backup file.')
    await d.getByLabel('Backup file').setInputFiles({ name: backup.name, mimeType: 'application/octet-stream', buffer: backup.data })
    await go.click()
    await expect(d.getByRole('alert')).toContainText('Enter a passphrase.')
    expect(sent).toBe(0)

    // a passphrase that does not open it
    await d.getByLabel('Passphrase').fill('not the passphrase')
    await go.click()
    await expect(d.getByRole('alert')).toContainText("That passphrase doesn't open this backup.")
    expect(sent).toBe(1)

    // a file that is not a backup at all
    await d.getByLabel('Backup file').setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('shopping list') })
    await go.click()
    await expect(d.getByRole('alert')).toContainText("This file isn't a Remote Nodes backup.")

    // a file too big to be one is turned away without being sent
    await d.getByLabel('Backup file').setInputFiles({ name: 'movie.bak', mimeType: 'application/octet-stream', buffer: Buffer.alloc(3 * 1024 * 1024) })
    const before = sent
    await go.click()
    await expect(d.getByRole('alert')).toContainText("This file isn't a Remote Nodes backup.")
    expect(sent).toBe(before)

    // the right file with the right passphrase
    await d.getByLabel('Backup file').setInputFiles({ name: backup.name, mimeType: 'application/octet-stream', buffer: backup.data })
    await d.getByLabel('Passphrase').fill(PASS)
    const body = page.waitForRequest((r) => r.url().endsWith('/api/backup/import') && r.method() === 'POST')
    await go.click()
    const posted = (await body).postDataJSON()
    expect(posted.passphrase).toBe(PASS)
    expect(Buffer.from(posted.data, 'base64').toString().startsWith('RNBK1\n')).toBe(true)
    const done = dialog(page, 'Imported 3 nodes')
    await expect(done).toBeVisible()
    await expect(done).toContainText('They reconnect on their own. Open the Nodes tab to see them.')
    await expect(toast(page, 'Imported 3 nodes')).toBeVisible()
    // neither the passphrase nor the file is kept
    expect(await stateHolds(page, PASS)).toBe(false)
    await done.getByRole('button', { name: 'Show nodes' }).click()
    await expect(page).toHaveURL(/#\/$/)
    for (const name of ['Node 1', 'Node 2', 'Node 3']) await expect(card(page, name)).toBeVisible()
    await expect(page.getByText('3 of 3 nodes')).toBeVisible()
    // and they came back working, since they trust this router already
    await expect(card(page, 'Node 1')).toContainText('Working')
  })

  test('says the install is not empty when the hub says so, in the hub\'s words', async ({ page }) => {
    const backup = await exportBackup(page)
    await page.route('**/api/backup/import', (route) =>
      route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ code: 'bad_request', error: 'a backup can only be imported on an empty install, with no nodes' }) }),
    )
    // a page that thinks it is empty: the config says no nodes
    await page.route('**/api/config', async (route) => {
      const res = await route.fetch()
      const json = await res.json()
      await route.fulfill({ response: res, json: { ...json, nodeCount: 0 } })
    })
    await page.reload()
    await page.getByRole('button', { name: 'Import…' }).click()
    const d = dialog(page, 'Import a backup')
    await d.getByLabel('Backup file').setInputFiles({ name: backup.name, mimeType: 'application/octet-stream', buffer: backup.data })
    await d.getByLabel('Passphrase').fill(PASS)
    await d.getByRole('button', { name: 'Import backup' }).click()
    await expect(d.getByRole('alert')).toContainText('a backup can only be imported on an empty install, with no nodes')
  })
})
