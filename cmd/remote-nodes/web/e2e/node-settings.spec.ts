import { dialog, expect, test, toast } from './fixtures'
import { confirmDialog, finishJob, openNode, panel, setNode } from './node-helpers'

const KEY = 'x7QnLw4Rk2VtB9yD0sEoUj3HaFcN6mXz8PqIg1KvTbA'

// a renewed certificate has its whole year again
const row365 = (page: import('@playwright/test').Page) => panel(page, 'Certificate').getByText('365 days', { exact: true })

const settingsSent = (page: import('@playwright/test').Page) => page.waitForRequest((r) => r.url().endsWith('/api/nodes/1/settings') && r.method() === 'PATCH')

test.describe('the Settings tab', () => {
  test('shows what can be changed, each with its own button', async ({ page }) => {
    await openNode(page, 1, 'settings')
    const conn = panel(page, 'Connection')
    await expect(conn).toContainText('How subscriptions reach this node.')
    await expect(conn).toContainText('Port 443')
    await expect(conn.getByRole('button', { name: 'Change port…' })).toBeEnabled()

    const target = panel(page, 'Reality target')
    await expect(target).toContainText('The site this node uses for the TLS handshake.')
    await expect(target).toContainText('www.example.com:443')
    await expect(target.getByText('working', { exact: true })).toBeVisible()
    await expect(target).toContainText('checked 2 hours ago')
    await expect(target.getByRole('button', { name: 'Change Reality target…' })).toBeEnabled()

    const keys = panel(page, 'Keys')
    await expect(keys).toContainText('Every link for this node carries these.')
    await expect(keys.getByText(KEY, { exact: true })).toBeVisible()
    await expect(keys.getByText('a1b2c3d4', { exact: true })).toBeVisible()
    await expect(keys).toContainText('Rotating keys changes every link for this node. Each subscription needs its new link.')
    await expect(keys.getByRole('button', { name: 'Rotate keys…' })).toBeEnabled()

    const cert = panel(page, 'Certificate')
    await expect(cert).toContainText('How this router and the node trust each other.')
    await expect(cert).toContainText(/Valid until \d{1,2} \w{3} 20\d\d · renews by itself 30 days before/)
    await expect(cert.getByRole('button', { name: 'Renew now' })).toBeEnabled()

    // the server name is for those who look for it
    const advanced = page.getByRole('button', { name: /^Advanced/ })
    await expect(advanced).toHaveAttribute('aria-expanded', 'false')
    await expect(page.getByLabel('Server name', { exact: true })).toHaveCount(0)
  })

  test('copies a key', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await openNode(page, 1, 'settings')
    await page.getByRole('button', { name: 'Copy public key' }).click()
    await expect(page.getByText('Copied', { exact: true })).toBeAttached()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(KEY)
    await page.getByRole('button', { name: 'Copy short ID' }).click()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('a1b2c3d4')
  })

  test('is off, and says why, while the router can not reach the node', async ({ page }) => {
    await openNode(page, 2, 'settings')
    await expect(page.getByText('Settings can change again once the router can reach this node.')).toBeVisible()
    for (const name of ['Change port…', 'Change Reality target…', 'Rotate keys…', 'Renew now']) {
      const b = page.getByRole('button', { name })
      await expect(b).toBeDisabled()
      await expect(b).toHaveAttribute('title', 'Needs the router to reach this node')
    }
    await page.getByRole('button', { name: /^Advanced/ }).click()
    await expect(page.getByLabel('Same as the Reality target')).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Save server name…' })).toBeDisabled()
    // what the node has is still shown
    await expect(panel(page, 'Keys').getByText(KEY, { exact: true })).toBeVisible()
  })
})

test.describe('changing the Reality target', () => {
  test('checks a site from the node, and applies it only after it has passed', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await page.getByRole('button', { name: 'Change Reality target…' }).click()
    const d = dialog(page, 'Change the Reality target')
    const field = d.getByRole('textbox', { name: 'Reality target', exact: true })
    const apply = d.getByRole('button', { name: 'Save and apply' })
    await expect(field).toHaveValue('www.example.com:443')
    await expect(d).toContainText('An HTTPS site that supports TLS 1.3. The node checks it before anything changes.')
    await expect(d).toContainText('This changes every link for this node.')
    await expect(d).toContainText('Each subscription needs its new link.')
    // nothing to apply yet, and nothing has been checked
    await expect(apply).toBeDisabled()
    await expect(d.getByText('working')).toHaveCount(0)

    // the node's own target passes, and is the target now
    await d.getByRole('button', { name: 'Check', exact: true }).click()
    await expect(d.getByText('working', { exact: true })).toBeVisible()
    // a check that has come back is announced
    await expect(d.getByRole('status')).toContainText('working')
    await expect(d).toContainText('TLS 1.3 · X25519 · 4.2 KB handshake, checked from this node')
    await expect(d).toContainText('That is the target now.')
    await expect(apply).toBeDisabled()

    // a bare host is taken as host:443, and what was checked is what was typed
    await field.fill('WWW.Cloudflare.com')
    await expect(d.getByText('working')).toHaveCount(0)
    await expect(apply).toBeDisabled()
    const checked = page.waitForRequest((r) => r.url().endsWith('/api/nodes/1/handshake-check') && r.method() === 'POST')
    await d.getByRole('button', { name: 'Check', exact: true }).click()
    expect((await checked).postDataJSON()).toEqual({ target: 'www.cloudflare.com:443' })
    await expect(d.getByText('working', { exact: true })).toBeVisible()
    await expect(apply).toBeEnabled()
    // typing again takes the pass back: it was for another site
    await field.fill('www.cloudflare.org')
    await expect(d.getByText('working')).toHaveCount(0)
    await expect(apply).toBeDisabled()
    // the site that passed, typed again, still has its pass
    await field.fill('www.cloudflare.com')
    await expect(d.getByText('working', { exact: true })).toBeVisible()
    await expect(d.getByRole('button', { name: 'Check again' })).toBeVisible()
    await expect(apply).toBeEnabled()

    const sent = settingsSent(page)
    await apply.click()
    expect((await sent).postDataJSON()).toEqual({ handshakeTarget: 'www.cloudflare.com:443' })
    const job = dialog(page, /^(Applying settings|Settings applied)/)
    await expect(job.getByText('Choose a Reality target')).toBeVisible()
    await expect(job.getByText('Apply the configuration')).toBeVisible()
    await expect(job.getByText('Save the node')).toBeVisible()
    await expect(dialog(page, /^Settings applied/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'Settings applied · Node 1')).toBeVisible()
    await finishJob(page, /^Settings applied/)
    const card = panel(page, 'Reality target')
    await expect(card).toContainText('www.cloudflare.com:443')
    await expect(card).toContainText('working')
    await expect(card).toContainText('checked just now')
  })

  test('says why a site fails, and will not apply it', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await page.getByRole('button', { name: 'Change Reality target…' }).click()
    const d = dialog(page, 'Change the Reality target')
    const field = d.getByRole('textbox', { name: 'Reality target', exact: true })
    const apply = d.getByRole('button', { name: 'Save and apply' })

    await field.fill('fail.example.com')
    await d.getByRole('button', { name: 'Check', exact: true }).click()
    await expect(d.getByText('failing', { exact: true })).toBeVisible()
    await expect(d.getByRole('alert')).toContainText('fail.example.com:443 did not complete a TLS 1.3 handshake')
    await expect(apply).toBeDisabled()

    // a handshake too big for the tunnel is told by its size
    await field.fill('big.example.com:8443')
    await d.getByRole('button', { name: 'Check', exact: true }).click()
    await expect(d.getByText('failing', { exact: true })).toBeVisible()
    await expect(d.getByRole('alert')).toContainText('the handshake is 9.4 KB, over the 8 KB limit')
    await expect(apply).toBeDisabled()
    // a second look is "Check again"
    await expect(d.getByRole('button', { name: 'Check again' })).toBeVisible()
  })

  test('says what is not a site before asking the node', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await page.getByRole('button', { name: 'Change Reality target…' }).click()
    const d = dialog(page, 'Change the Reality target')
    const field = d.getByRole('textbox', { name: 'Reality target', exact: true })
    for (const bad of ['not a site', '1.2.3.4', 'example.com:0', 'example.com:99999']) {
      await field.fill(bad)
      await expect(d).toContainText('Give a site as host:port, such as www.example.com:443.')
      await expect(d.getByRole('button', { name: 'Check', exact: true })).toBeDisabled()
    }
    await field.fill('')
    await expect(d.getByRole('button', { name: 'Check', exact: true })).toBeDisabled()
  })

  test('keeps the old target when the node can not apply the new one', async ({ page, request }) => {
    await setNode(request, { id: 1, failNext: 'change-settings' })
    await openNode(page, 1, 'settings')
    await page.getByRole('button', { name: 'Change Reality target…' }).click()
    const d = dialog(page, 'Change the Reality target')
    await d.getByRole('textbox', { name: 'Reality target', exact: true }).fill('www.cloudflare.com')
    await d.getByRole('button', { name: 'Check', exact: true }).click()
    await d.getByRole('button', { name: 'Save and apply' }).click()
    const failed = dialog(page, /^Applying settings failed/)
    await expect(failed).toBeVisible({ timeout: 20000 })
    await expect(failed).toContainText('xray -test failed on the node: port already in use')
    await expect(toast(page, 'Applying settings failed · Node 1')).toBeVisible()
    await failed.getByRole('button', { name: 'Done' }).click()
    await expect(panel(page, 'Reality target')).toContainText('www.example.com:443')
  })
})

test.describe('changing the entry port', () => {
  test('refuses a port that can not work, then applies one that can', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await page.getByRole('button', { name: 'Change port…' }).click()
    const d = dialog(page, 'Change the entry port')
    await expect(d).toContainText('Node 1 uses port 443 now.')
    const port = d.getByLabel('Entry port')
    const apply = d.getByRole('button', { name: 'Save and apply' })
    await expect(apply).toBeDisabled()
    await expect(d).toContainText('The port subscriptions connect to. It must be free on the node.')
    await expect(d).toContainText('This changes every link for this node.')
    for (const [bad, why] of [
      ['22', 'The entry port is a number from 1 to 65535, and not the SSH port.'],
      ['0', 'The entry port is a number from 1 to 65535, and not the SSH port.'],
      ['70000', 'The entry port is a number from 1 to 65535, and not the SSH port.'],
      ['44a', 'The entry port is a number from 1 to 65535, and not the SSH port.'],
      ['443', 'That is the port it uses now.'],
    ] as const) {
      await port.fill(bad)
      await expect(d).toContainText(why)
      await expect(apply).toBeDisabled()
    }
    await port.fill('8443')
    await expect(apply).toBeEnabled()
    const sent = settingsSent(page)
    await apply.click()
    expect((await sent).postDataJSON()).toEqual({ entryPort: 8443 })
    await expect(dialog(page, /^(Applying settings|Settings applied)/)).toBeVisible()
    await expect(dialog(page, /^Settings applied/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'Settings applied · Node 1')).toBeVisible()
    await finishJob(page, /^Settings applied/)
    await expect(panel(page, 'Connection')).toContainText('Port 8443')
    // the header has it too
    await expect(page.getByText('port 8443', { exact: true })).toBeVisible()
  })
})

test.describe('keys and the certificate', () => {
  test('rotating the keys says how many links change, and shows the new keys', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await page.getByRole('button', { name: 'Rotate keys…' }).click()
    const confirm = confirmDialog(page, 'Rotate the keys on Node 1?')
    // six subscriptions are on this node, and each gets a new link
    await expect(confirm.getByText('6', { exact: true })).toBeVisible()
    await expect(confirm).toContainText('subscriptions get new links. Each needs its new link shared again.')
    await expect(confirm).toContainText('The node switches to a new key pair. Nothing else on it changes.')
    await confirm.getByRole('button', { name: 'Rotate keys' }).click()
    const job = dialog(page, /^(Rotating keys|Keys rotated)/)
    for (const step of ['Create the keys', 'Apply the configuration', 'Save the node']) await expect(job.getByText(step)).toBeVisible()
    await expect(dialog(page, /^Keys rotated/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'Keys rotated · Node 1')).toBeVisible()
    await finishJob(page, /^Keys rotated/)
    const keys = panel(page, 'Keys')
    await expect(keys.getByText(KEY, { exact: true })).toHaveCount(0)
    await expect(keys.getByText('a1b2c3d4', { exact: true })).toHaveCount(0)
    await expect(keys.locator('code').nth(0)).toHaveText(/^[A-Za-z0-9_-]{43}$/)
    await expect(keys.locator('code').nth(1)).toHaveText(/^[0-9a-f]{8}$/)
    await page.getByRole('tab', { name: 'Status' }).click()
    await expect(panel(page, 'Recent events')).toContainText('Keys rotated')
  })

  test('a node with nothing on it has no links to change, and says so', async ({ page, request }) => {
    await setNode(request, { id: 1, noSubscriptions: true })
    await openNode(page, 1, 'settings')
    await page.getByRole('button', { name: 'Rotate keys…' }).click()
    const confirm = confirmDialog(page, 'Rotate the keys on Node 1?')
    await expect(confirm.getByText('0', { exact: true })).toBeVisible()
    await expect(confirm).toContainText('No subscription is on this node, so no link changes.')
    await confirm.getByRole('button', { name: 'Cancel' }).click()
    await expect(confirm).toBeHidden()
  })

  test('renews the certificate as a job, and its date moves on', async ({ page }) => {
    await openNode(page, 1, 'settings')
    const cert = panel(page, 'Certificate')
    const before = await cert.innerText()
    await cert.getByRole('button', { name: 'Renew now' }).click()
    const confirm = confirmDialog(page, 'Renew the certificate on Node 1?')
    await expect(confirm).toContainText('The node gets a new certificate from this router, valid for 12 months. Subscriptions keep their links.')
    await confirm.getByRole('button', { name: 'Renew certificate' }).click()
    await expect(dialog(page, /^(Renewing the certificate|Certificate renewed)/)).toBeVisible()
    await expect(dialog(page, /^Certificate renewed/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'Certificate renewed · Node 1')).toBeVisible()
    await finishJob(page, /^Certificate renewed/)
    await expect(cert).not.toHaveText(before)
    await page.getByRole('tab', { name: 'Status' }).click()
    await expect(row365(page)).toBeVisible()
    await expect(panel(page, 'Recent events')).toContainText('Certificate renewed')
  })
})

test.describe('the server name', () => {
  test('is the Reality target\'s unless one is set, and setting one asks first', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await page.getByRole('button', { name: /^Advanced/ }).click()
    const same = page.getByLabel('Same as the Reality target')
    const name = page.getByRole('textbox', { name: 'Server name', exact: true })
    const save = page.getByRole('button', { name: 'Save server name…' })
    await expect(same).toBeChecked()
    await expect(name).toBeDisabled()
    await expect(name).toHaveValue('www.example.com')
    await expect(save).toBeDisabled()

    await same.uncheck()
    await expect(name).toBeEnabled()
    await name.fill('not a host')
    await expect(page.getByText('Give a host name, such as cdn.example.com.')).toBeVisible()
    await expect(save).toBeDisabled()
    await name.fill('cdn.example.com')
    await expect(save).toBeEnabled()
    await save.click()
    const confirm = confirmDialog(page, 'Change the server name on Node 1?')
    await expect(confirm).toContainText('The server name becomes cdn.example.com.')
    await expect(confirm.getByText('6', { exact: true })).toBeVisible()
    const sent = settingsSent(page)
    await confirm.getByRole('button', { name: 'Change server name' }).click()
    expect((await sent).postDataJSON()).toEqual({ sni: 'cdn.example.com' })
    await expect(dialog(page, /^Settings applied/)).toBeVisible({ timeout: 20000 })
    await finishJob(page, /^Settings applied/)

    // it is what the node has now, so the form starts from it
    await expect(page.getByLabel('Same as the Reality target')).not.toBeChecked()
    await expect(page.getByRole('textbox', { name: 'Server name', exact: true })).toHaveValue('cdn.example.com')
    await expect(page.getByRole('button', { name: 'Save server name…' })).toBeDisabled()

    // and back to the target's own, which is an empty name to the hub
    await page.getByLabel('Same as the Reality target').check()
    await expect(page.getByRole('button', { name: 'Save server name…' })).toBeEnabled()
    await page.getByRole('button', { name: 'Save server name…' }).click()
    const again = confirmDialog(page, 'Change the server name on Node 1?')
    await expect(again).toContainText("The server name becomes the Reality target's: www.example.com.")
    const sentAgain = settingsSent(page)
    await again.getByRole('button', { name: 'Change server name' }).click()
    expect((await sentAgain).postDataJSON()).toEqual({ sni: '' })
    await expect(dialog(page, /^Settings applied/)).toBeVisible({ timeout: 20000 })
    await finishJob(page, /^Settings applied/)
    await expect(page.getByLabel('Same as the Reality target')).toBeChecked()
  })
})
