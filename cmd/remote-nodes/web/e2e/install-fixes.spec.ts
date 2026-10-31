import type { Page, Request } from '@playwright/test'
import { PAGE, dialog, expect, fillAddNode, heapHolds, leaks, openAddNode, reset, stateHolds, test, toast } from './fixtures'
import { overflow } from './node-helpers'

// An install that fails for a reason a person can fix offers the fix in the same dialog: a short
// explanation, what it needs, and Continue, which sends the install again with that one change and
// follows it in the same dialog. The dialog holds the login it was sent while it is open, and nowhere
// else: every path here also looks in the browser for it.

const FAILED = "This server can't be used"
const isInstall = (r: Request) => r.url().endsWith('/api/nodes') && r.method() === 'POST'
const closeButton = (d: ReturnType<typeof dialog>) => d.getByRole('button', { name: 'Close', exact: true }).first()

test.beforeEach(async ({ request }) => {
  await reset(request, { nodes: 2 })
})

/** Opens Add node, fills it in with a host and password, sends it, and gives what the page sent. */
async function send(page: Page, host: string, password = 'a-password', change?: (form: ReturnType<typeof dialog>) => Promise<void>) {
  await openAddNode(page)
  const form = await fillAddNode(page, host, password)
  await change?.(form)
  const sent = page.waitForRequest(isInstall)
  await form.getByRole('button', { name: 'Connect and install' }).click()
  return (await sent).postDataJSON()
}

/** The probes the page makes, for a test that says how many there were. */
function probes(page: Page) {
  const seen: { host: string }[] = []
  page.on('request', (r) => r.url().endsWith('/api/probe') && seen.push(r.postDataJSON()))
  return seen
}

test.describe('a server with an Xray of its own', () => {
  test('is taken over from the dialog that refused it, and the install goes on in it', async ({ page, request }) => {
    const secret = 'secret-for-the-takeover'
    const seen = probes(page)
    const first = await send(page, 'hasxray.example.com', secret)
    expect(first.takeOverXray).toBeUndefined()
    const d = dialog(page, FAILED)
    await expect(d).toBeVisible({ timeout: 20000 })
    // what was found, and what becomes of it, said before anything is asked
    // the reason is said once: under the title, not again in a box
    await expect(d.getByText('This server already runs an Xray that Remote Nodes did not install. Use a server without one, or take it over.')).toHaveCount(1)
    await expect(d.getByRole('alert')).toHaveCount(0)
    await expect(d).toContainText('Found on the server:')
    await expect(d).toContainText('its program, /usr/local/bin/xray')
    await expect(d).toContainText('its service, xray.service')
    await expect(d).toContainText("It is Xray 26.9.1, not the same build as this plugin's, so its program is replaced.")
    await expect(d).toContainText("Taking it over stops that Xray, removes its config and its service, and keeps no backup. Its program is replaced unless it is the same build as this plugin's.")
    // the 14 steps it never got to are one line, and the step that forgets a node never made isn't there
    await expect(d.getByRole('button', { name: '14 steps not started' })).toBeVisible()
    await expect(d.getByText('Discard the node')).toHaveCount(0)
    // the login is held for the fix, and in no storage
    expect(await stateHolds(page, secret)).toBe(true)
    expect(await leaks(page, secret)).toEqual([])

    const sent = page.waitForRequest(isInstall)
    await d.getByRole('button', { name: 'Take over and continue' }).click()
    const body = (await sent).postDataJSON()
    // the same install, with the one change; the key that was agreed to is the one it holds the server to
    expect(body).toMatchObject({ host: 'hasxray.example.com', name: 'Node 3', sshPort: 22, sshUser: 'root', password: secret, entryPort: 443, takeOverXray: true, hostKey: first.hostKey })
    // the same dialog follows the new job, with the takeover in its steps
    const run = dialog(page, /Installing Node 3|Node added/)
    await expect(run).toBeVisible()
    await expect(page.getByRole('dialog')).toHaveCount(1)
    await expect(run.getByText("Take over the server's Xray")).toBeVisible()
    const done = dialog(page, 'Node added')
    await expect(done).toBeVisible({ timeout: 25000 })
    await expect(done).toContainText('Xray 26.9.1 was stopped, and its service, 1 drop-in, 1 config file, its binary removed. Xray 26.10.21 goes on later.')
    // no new key was read: it is the same server
    expect(seen).toHaveLength(1)
    // the install that was refused is still among the recent jobs, beside the one that worked
    expect(await stateHolds(page, secret)).toBe(false)
    expect(await leaks(page, secret)).toEqual([])
    await closeButton(done).click()
    await page.getByRole('button', { name: 'Recent jobs' }).click()
    const tray = page.getByRole('region', { name: 'Jobs' })
    await expect(tray).toContainText('Installation failed · Node 3')
    await expect(tray).toContainText('Node 3 added')
    // the hub never kept the login
    for (const path of ['api/jobs', 'api/nodes']) expect(await (await request.get(path, { headers: PAGE })).text()).not.toContain(secret)
  })

  test('is kept as it is when it is the build this plugin uses, and its Xray is not sent again', async ({ page }) => {
    await send(page, 'hasxraysame.example.com')
    const d = dialog(page, FAILED)
    await expect(d).toBeVisible({ timeout: 20000 })
    await expect(d).toContainText("It is Xray 26.10.21, the same build as this plugin's, so its program stays as it is.")
    await d.getByRole('button', { name: 'Take over and continue' }).click()
    const done = dialog(page, 'Node added')
    await expect(done).toBeVisible({ timeout: 25000 })
    await expect(done).toContainText('It is the pinned build, so its binary stays.')
    // the steps that move the build stay in the list, as steps with nothing to do
    await expect(done.getByText('Not needed')).toHaveCount(2)
  })

  test('can be left for another server, which is asked for in the same dialog and has its own key read', async ({ page }) => {
    const seen = probes(page)
    await send(page, 'hasxray.example.com')
    const d = dialog(page, FAILED)
    await expect(d).toBeVisible({ timeout: 20000 })
    await expect(d.getByText('Or use another server')).toBeVisible()
    await expect(d.getByRole('button', { name: 'Continue', exact: true })).toBeDisabled()
    await d.getByLabel('Server address').fill('192.0.2.60')
    const sent = page.waitForRequest(isInstall)
    await d.getByRole('button', { name: 'Continue', exact: true }).click()
    const body = (await sent).postDataJSON()
    // another server is another install: its address, its key, and no agreement to take anything over
    expect(body).toMatchObject({ host: '192.0.2.60', name: 'Node 3', password: 'a-password' })
    expect(body.takeOverXray).toBeUndefined()
    expect(seen.map((p) => p.host)).toEqual(['hasxray.example.com', '192.0.2.60'])
    await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
  })

  test('can be left for a server whose key has changed, which is trusted only on purpose, and Back returns to the failure', async ({ page }) => {
    await send(page, 'hasxray.example.com')
    const d = dialog(page, FAILED)
    await expect(d).toBeVisible({ timeout: 20000 })
    await d.getByLabel('Server address').fill('changed.example.com')
    await d.getByRole('button', { name: 'Continue', exact: true }).click()
    // the same question the add dialog asks, in the same dialog
    const ask = dialog(page, 'Add a node')
    await expect(ask).toContainText('changed.example.com shows a different key than last time.')
    await expect(ask).toContainText('SHA256:u3Rq0Q1uV3Jc0mYw7b6o8xk3j2m9Pq5c1f0aZyXwVb8')
    const trust = ask.getByRole('button', { name: 'Trust and install' })
    await expect(trust).toBeDisabled()
    // Back is the failure again, with what it offered
    await ask.getByRole('button', { name: 'Back' }).click()
    await expect(dialog(page, FAILED).getByRole('button', { name: 'Take over and continue' })).toBeVisible()
    await d.getByLabel('Server address').fill('changed.example.com')
    await d.getByRole('button', { name: 'Continue', exact: true }).click()
    await ask.getByLabel('I know why the key changed').check()
    const sent = page.waitForRequest(isInstall)
    await trust.click()
    const body = (await sent).postDataJSON()
    expect(body).toMatchObject({ host: 'changed.example.com', password: 'a-password', hostKey: 'SHA256:u3Rq0Q1uV3Jc0mYw7b6o8xk3j2m9Pq5c1f0aZyXwVb8' })
    expect(body.takeOverXray).toBeUndefined()
    await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
  })

  test('is not taken over a second time without being asked a second time', async ({ page }) => {
    // the takeover goes through, and the install fails after it
    await send(page, 'hasxray-fail.example.com')
    const d = dialog(page, FAILED)
    await expect(d).toBeVisible({ timeout: 20000 })
    await d.getByRole('button', { name: 'Take over and continue' }).click()
    const failed = dialog(page, 'Installation failed')
    await expect(failed).toBeVisible({ timeout: 25000 })
    // what is gone is said plainly, as the hub says it and the page does
    await expect(failed).toContainText("The server's own Xray was stopped and removed, as you asked, and is not restored.")
    await expect(failed).toContainText('The server was cleaned up, and nothing was kept.')
    // Try again sends it as it was, but not the agreement: that is asked again, by the same refusal
    const sent = page.waitForRequest(isInstall)
    await failed.getByRole('button', { name: 'Try again' }).click()
    const again = (await sent).postDataJSON()
    expect(again).toMatchObject({ host: 'hasxray-fail.example.com', password: 'a-password' })
    expect(again.takeOverXray).toBeUndefined()
    await expect(dialog(page, FAILED)).toBeVisible({ timeout: 20000 })
    await expect(dialog(page, FAILED).getByRole('button', { name: 'Take over and continue' })).toBeVisible()
  })

  test('says that an Xray that still runs must be stopped there, and goes on once it is', async ({ page }) => {
    await send(page, 'hasxray-notstopped.example.com')
    const d = dialog(page, FAILED)
    await expect(d).toBeVisible({ timeout: 20000 })
    await d.getByRole('button', { name: 'Take over and continue' }).click()
    const failed = dialog(page, 'Installation failed')
    await expect(failed).toBeVisible({ timeout: 25000 })
    await expect(failed).toContainText('Failed at step 4 of 17')
    await expect(failed).toContainText('An Xray is still running on this server after its service was stopped, for example in a container. Stop it there, or use another server.')
    // the hub's own line adds that the service is running again
    await expect(failed).toContainText('Its service was started again.')
    await expect(failed).toContainText('Nothing was changed on the server, and nothing was kept.')
    await expect(failed).not.toContainText('not restored')
    await expect(failed.getByText('Stop it there, then continue.')).toBeVisible()
    // Continue keeps the takeover that was asked for
    const sent = page.waitForRequest(isInstall)
    await failed.getByRole('button', { name: 'Continue', exact: true }).click()
    expect((await sent).postDataJSON()).toMatchObject({ host: 'hasxray-notstopped.example.com', password: 'a-password', takeOverXray: true })
    await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
  })

  test('reads in Persian, with its digits, and nothing overflows at phone width', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('remote-nodes.language', 'fa'))
    await openAddNode(page, 'افزودن نود')
    const form = await fillAddNode(page, 'hasxray.example.com')
    await form.getByRole('button', { name: 'اتصال و نصب' }).click()
    const d = dialog(page, 'این سرور قابل استفاده نیست')
    await expect(d).toBeVisible({ timeout: 20000 })
    await expect(d).toContainText('روی سرور پیدا شد:')
    await expect(d).toContainText('۱۴ مرحله شروع نشده')
    await expect(d.getByRole('button', { name: 'در اختیار گرفتن و ادامه' })).toBeVisible()
    await expect(d.getByText('یا از سرور دیگری استفاده کنید')).toBeVisible()
    expect(await overflow(page)).toBe(0)
    await d.getByRole('button', { name: 'در اختیار گرفتن و ادامه' }).click()
    await expect(dialog(page, 'نود اضافه شد')).toBeVisible({ timeout: 25000 })
  })
})

test.describe('a port the server holds', () => {
  test('is gone round with another entry port, in the same dialog', async ({ page }) => {
    await send(page, 'portbusy.example.com')
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 20000 })
    // which port, and whose
    await expect(d).toContainText('Port 443 on the server is used by nginx.')
    const port = d.getByLabel('Entry port')
    await expect(port).toHaveValue('8443')
    await expect(d.getByText('Pick a free one, or free 443 on the server and try again.')).toBeVisible()
    // not the port that is held, not the one SSH uses, and not nothing
    for (const bad of ['443', '22', '', '70000', 'x']) {
      await port.fill(bad)
      await expect(d.getByRole('button', { name: 'Continue', exact: true })).toBeDisabled()
    }
    await port.fill('8443')
    const sent = page.waitForRequest(isInstall)
    await d.getByRole('button', { name: 'Continue', exact: true }).click()
    expect((await sent).postDataJSON()).toMatchObject({ host: 'portbusy.example.com', name: 'Node 3', password: 'a-password', entryPort: 8443 })
    await expect(page.getByRole('dialog')).toHaveCount(1)
    await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
  })

  test('can be tried again as it was, once freed', async ({ page }) => {
    await send(page, 'portbusy.example.com')
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 20000 })
    const sent = page.waitForRequest(isInstall)
    await d.getByRole('button', { name: 'Try again' }).click()
    expect((await sent).postDataJSON()).toMatchObject({ host: 'portbusy.example.com', entryPort: 443, password: 'a-password' })
    // still held by nginx: the same words, in the same dialog
    await expect(page.getByRole('dialog')).toHaveCount(1)
    await expect(dialog(page, 'Installation failed')).toContainText('Port 443 on the server is used by nginx.', { timeout: 20000 })
  })

  test('is the hub\'s own setting when it is the agent\'s, so nothing is offered but another try', async ({ page }) => {
    await send(page, 'agentbusy.example.com')
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 20000 })
    await expect(d).toContainText("Port 9090, which the agent uses, is used by python3 on the server. The agent's port is a setting of this hub and can't be set for one node.")
    await expect(d.getByLabel('Entry port')).toHaveCount(0)
    await expect(d.getByRole('region', { name: 'What you can do' })).toHaveCount(0)
    await expect(d.getByRole('button', { name: 'Try again' })).toBeVisible()
    await expect(d.getByRole('button', { name: 'Change details' })).toBeVisible()
  })
})

test.describe('a login the server does not accept', () => {
  test('is typed again in the dialog, and the secrets are never shown or kept', async ({ page, request }) => {
    const wrong = 'wrong'
    await send(page, '192.0.2.41', wrong)
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 20000 })
    await expect(d).toContainText("The server didn't accept the login. Check the user name and the password or key.")
    // the hub's own sentence says the same, so it isn't said twice
    await expect(d.getByText("The server didn't accept the login.")).toHaveCount(1)
    expect(await stateHolds(page, wrong)).toBe(true)
    // the port and user it had, and nothing in the secret fields
    await expect(d.getByLabel('SSH port')).toHaveValue('22')
    await expect(d.getByLabel('Username')).toHaveValue('root')
    await expect(d.locator('input[type="password"]')).toHaveValue('')
    await d.locator('input[type="password"]').fill('the-right-password')
    const sent = page.waitForRequest(isInstall)
    await d.getByRole('button', { name: 'Continue', exact: true }).click()
    const body = (await sent).postDataJSON()
    expect(body).toMatchObject({ host: '192.0.2.41', name: 'Node 3', sshUser: 'root', password: 'the-right-password' })
    expect(JSON.stringify(body)).not.toContain(wrong)
    await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
    for (const secret of [wrong, 'the-right-password']) {
      expect(await stateHolds(page, secret)).toBe(false)
      expect(await leaks(page, secret)).toEqual([])
      for (const path of ['api/jobs', 'api/nodes']) expect(await (await request.get(path, { headers: PAGE })).text()).not.toContain(secret)
    }
  })

  test('can be a key instead, which replaces the password whole', async ({ page }) => {
    await send(page, '192.0.2.41', 'wrong')
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 20000 })
    await d.getByRole('radio', { name: 'Private key' }).click()
    await d.getByLabel('Private key').fill('-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----')
    const sent = page.waitForRequest(isInstall)
    await d.getByRole('button', { name: 'Continue', exact: true }).click()
    const body = (await sent).postDataJSON()
    expect(body.privateKey).toContain('BEGIN OPENSSH PRIVATE KEY')
    expect(body.password).toBeUndefined()
    await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
  })

  test('asks for nothing it was not given: an empty password stops before the hub is asked', async ({ page }) => {
    await send(page, '192.0.2.41', 'wrong')
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 20000 })
    let sent = 0
    page.on('request', (r) => isInstall(r) && sent++)
    await d.getByRole('button', { name: 'Continue', exact: true }).click()
    const password = d.locator('input[type="password"]')
    await expect.poll(() => password.evaluate((el: HTMLInputElement) => el.validity.valueMissing)).toBe(true)
    await page.waitForTimeout(300)
    expect(sent).toBe(0)
    await expect(page.getByRole('dialog')).toHaveCount(1)
    // a key that is no more than blanks is said too, in the page's words
    await d.getByRole('radio', { name: 'Private key' }).click()
    await d.getByLabel('Private key').evaluate((el: HTMLTextAreaElement) => (el.required = false))
    await d.getByLabel('Private key').fill('   ')
    await d.getByRole('button', { name: 'Continue', exact: true }).click()
    await expect(d.getByRole('alert').filter({ hasText: 'Paste the private key.' })).toBeVisible()
    expect(sent).toBe(0)
  })

  test('that cannot act as root is another user, and says why', async ({ page }) => {
    await send(page, 'nosudo.example.com', 'a-password', (form) => form.getByLabel('Username').fill('admin'))
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 20000 })
    await expect(d).toContainText("This login can't act as root: the server has no sudo, or sudo didn't accept the password. Use root, or a user that can use sudo.")
    // the hub's own sentence says which of the two it was
    await expect(d).toContainText("This login isn't root, and the server has no sudo.")
    await expect(d.getByLabel('Username')).toHaveValue('admin')
    await d.getByLabel('Username').fill('root')
    await d.locator('input[type="password"]').fill('root-password')
    const sent = page.waitForRequest(isInstall)
    await d.getByRole('button', { name: 'Continue', exact: true }).click()
    expect((await sent).postDataJSON()).toMatchObject({ host: 'nosudo.example.com', sshUser: 'root', password: 'root-password' })
    await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
  })
})

test.describe('a host key that is not the one that was agreed to', () => {
  test('is shown with the one before it, and trusted only after the same tick the add dialog wants', async ({ page }) => {
    const sentFirst = await send(page, 'keymoved.example.com')
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 20000 })
    await expect(d).toContainText("The node's SSH key changed. Check it before you go on.")
    // both keys, so they can be compared
    await expect(d.getByText('Agreed to')).toBeVisible()
    await expect(d.getByText(sentFirst.hostKey)).toBeVisible()
    await expect(d.getByText('Shows now')).toBeVisible()
    await expect(d.getByText('SHA256:u3Rq0Q1uV3Jc0mYw7b6o8xk3j2m9Pq5c1f0aZyXwVb8')).toBeVisible()
    const trust = d.getByRole('button', { name: 'Trust the new key and continue' })
    await expect(trust).toBeDisabled()
    await d.getByLabel('I know why the key changed').check()
    await expect(trust).toBeEnabled()
    const sent = page.waitForRequest(isInstall)
    await trust.click()
    // held to the key it shows now, and to that alone
    expect((await sent).postDataJSON()).toMatchObject({ host: 'keymoved.example.com', password: 'a-password', hostKey: 'SHA256:u3Rq0Q1uV3Jc0mYw7b6o8xk3j2m9Pq5c1f0aZyXwVb8' })
    await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
  })
})

test.describe('a server that has an agent, or is a node already', () => {
  test('goes on another server, asked for in the same dialog', async ({ page }) => {
    await send(page, 'hasagent.example.com')
    const d = dialog(page, FAILED)
    await expect(d).toBeVisible({ timeout: 20000 })
    // nothing to take over: only another server
    await expect(d.getByRole('button', { name: 'Take over and continue' })).toHaveCount(0)
    await d.getByLabel('Server address').fill('192.0.2.62')
    const sent = page.waitForRequest(isInstall)
    await d.getByRole('button', { name: 'Continue', exact: true }).click()
    expect((await sent).postDataJSON()).toMatchObject({ host: '192.0.2.62', name: 'Node 3', password: 'a-password' })
    await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
  })

  test('that is a node here names it, opens it, or goes on another server', async ({ page }) => {
    await send(page, 'isnode.example.com')
    const d = dialog(page, FAILED)
    await expect(d).toBeVisible({ timeout: 20000 })
    await expect(d).toContainText('This server is already the node Node 1 here, under another address.')
    await d.getByRole('button', { name: 'Open that node' }).click()
    await expect(page).toHaveURL(/#\/nodes\/1$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Node 1' })).toBeVisible()
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })
})

test.describe('any other failure', () => {
  test('is tried again as it was, or changed, and keeps no more of the login than it needs', async ({ page }) => {
    const secret = 'secret-for-another-try'
    await send(page, 'fail.example.com', secret)
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 20000 })
    // nothing in particular to fix: no panel, and the two ways on
    await expect(d.getByRole('region', { name: 'What you can do' })).toHaveCount(0)
    const sent = page.waitForRequest(isInstall)
    await d.getByRole('button', { name: 'Try again' }).click()
    expect((await sent).postDataJSON()).toMatchObject({ host: 'fail.example.com', name: 'Node 3', password: secret })
    await expect(page.getByRole('dialog')).toHaveCount(1)
    const again = dialog(page, 'Installation failed')
    await expect(again.getByRole('button', { name: 'Change details' })).toBeVisible({ timeout: 25000 })
    // both attempts are among the recent jobs; the dialog is the second's
    await again.getByRole('button', { name: 'Change details' }).click()
    const form = dialog(page, 'Add a node')
    await expect(form.getByLabel('Server address')).toHaveValue('fail.example.com')
    await expect(form.locator('input[type="password"]')).toHaveValue('')
    expect(await stateHolds(page, secret)).toBe(false)
    expect(await leaks(page, secret)).toEqual([])
    await closeButton(form).click()
    await page.getByRole('button', { name: 'Recent jobs' }).click()
    await expect(page.getByRole('region', { name: 'Jobs' }).getByText('Installation failed · Node 3')).toHaveCount(2)
  })

  test('says one sentence once, and the hub\'s own words beside it only for what they add', async ({ page }) => {
    await send(page, 'unreach.example.com')
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 20000 })
    // the page's sentence for the code, and the hub's detail with what it found, which the sentence doesn't say
    await expect(d.getByText("The router can't reach this node. Check that it is on and its address is on the domestic list.")).toHaveCount(1)
    await expect(d.getByText("The agent doesn't answer on TCP port 9090")).toHaveCount(1)
    // and the failed step itself says nothing over again
    await expect(d.getByRole('alert')).toHaveCount(1)
  })
})

test.describe('a fix the hub will not take', () => {
  test('says why, in the dialog, and the install can still be changed', async ({ page, request }) => {
    await send(page, 'fail.example.com')
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 25000 })
    // the router fills up while the dialog is open
    const other = await request.post('api/nodes', {
      headers: PAGE,
      data: { name: 'Other', host: '192.0.2.99', sshPort: 22, sshUser: 'root', password: 'x', hostKey: 'SHA256:otherotherotherotherotherotherotherother1', entryPort: 443 },
    })
    expect(other.status()).toBe(202)
    await d.getByRole('button', { name: 'Try again' }).click()
    await expect(d.getByRole('alert').filter({ hasText: 'This router has reached its limit of nodes. Remove one to add another.' })).toBeVisible()
    // still the failure, still with its ways on
    await expect(d.getByRole('button', { name: 'Change details' })).toBeVisible()
    await expect(page.getByRole('dialog')).toHaveCount(1)
  })
})

test.describe('an install that kept its server for a cleanup', () => {
  test('has nothing to fix in place: the cleanup comes first', async ({ page }) => {
    await send(page, 'stuck.example.com')
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 25000 })
    await expect(d.getByRole('button', { name: 'Clean up or forget' })).toBeVisible()
    // the details can still be changed, but the same install can't go again until the cleanup
    await expect(d.getByRole('button', { name: 'Change details' })).toBeVisible()
    await expect(d.getByRole('button', { name: 'Try again' })).toHaveCount(0)
    await expect(d.getByRole('region', { name: 'What you can do' })).toHaveCount(0)
  })
})

test.describe('a failure seen after the dialog that sent it is gone', () => {
  test('is only explained, since the login went with the dialog, and Change details types it again', async ({ page }) => {
    const secret = 'secret-for-a-closed-dialog'
    await send(page, 'hasxray.example.com', secret)
    const d = dialog(page, FAILED)
    await expect(d).toBeVisible({ timeout: 20000 })
    await closeButton(d).click()
    expect(await stateHolds(page, secret)).toBe(false)
    await page.getByRole('button', { name: 'Recent jobs' }).click()
    await page.getByRole('region', { name: 'Jobs' }).getByRole('button', { name: 'Open' }).click()
    const again = dialog(page, FAILED)
    await expect(again).toBeVisible()
    // what was found is still said; there is nothing to send it with
    await expect(again).toContainText("It is Xray 26.9.1, not the same build as this plugin's")
    await expect(again).toContainText("The login isn't kept once the dialog it was typed in is closed")
    await expect(again.getByRole('button', { name: 'Take over and continue' })).toHaveCount(0)
    await again.getByRole('button', { name: 'Change details' }).click()
    const form = dialog(page, 'Add a node')
    await expect(form.getByLabel('Server address')).toHaveValue('hasxray.example.com')
    await expect(form.locator('input[type="password"]')).toHaveValue('')
    expect(await leaks(page, secret)).toEqual([])
  })
})

test('a refusal says so in the title in a toast too, and the node count is untouched', async ({ page }) => {
  await send(page, 'hasagent.example.com')
  await expect(dialog(page, FAILED)).toBeVisible({ timeout: 20000 })
  await expect(toast(page, 'Installation failed')).toBeVisible()
  await expect(page.getByText('2 of 3 nodes')).toBeVisible()
})

// What the page's own heap holds, whatever holds it: the login is there while its dialog is open and its
// install can still fail, and nowhere once it is closed or the install worked. The check is the browser's
// own heap snapshot, which also sees a closure that a long-lived part of the page might have kept.
test.describe('the login in the page\'s memory', () => {
  // gone within a frame or two of the dialog going: the page flushes the last of what it queued then
  const gone = (page: Page, text: string) => expect.poll(() => heapHolds(page, text), { timeout: 5000, intervals: [100, 250, 500] }).toBe(false)

  test('is there while a failure can be fixed, and gone when the dialog closes', async ({ page }) => {
    const secret = 'heap-secret-closing-0001'
    await send(page, 'hasxray.example.com', secret)
    const d = dialog(page, FAILED)
    await expect(d).toBeVisible({ timeout: 20000 })
    expect(await heapHolds(page, secret)).toBe(true)
    await closeButton(d).click()
    await expect(d).toBeHidden()
    await gone(page, secret)
  })

  test('is gone once the install it was sent for has worked', async ({ page }) => {
    const secret = 'heap-secret-success-0002'
    await send(page, 'portbusy.example.com', secret)
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 20000 })
    expect(await heapHolds(page, secret)).toBe(true)
    await d.getByRole('button', { name: 'Continue', exact: true }).click()
    await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
    await gone(page, secret)
  })

  test('is gone when another dialog takes the place of the one that held it', async ({ page }) => {
    const secret = 'heap-secret-replaced-0003'
    await send(page, 'fail.example.com', secret)
    const d = dialog(page, 'Installation failed')
    await expect(d).toBeVisible({ timeout: 25000 })
    expect(await heapHolds(page, secret)).toBe(true)
    // Change details goes back to the form, with no login in it, and the dialog no longer holds the install
    await d.getByRole('button', { name: 'Change details' }).click()
    await expect(dialog(page, 'Add a node')).toBeVisible()
    await gone(page, secret)
  })

  test('is gone for an install that worked first time, the whole way through', async ({ page }) => {
    const secret = 'heap-secret-plain-0004'
    await send(page, '192.0.2.41', secret)
    await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
    await gone(page, secret)
    await closeButton(dialog(page, 'Node added')).click()
    await gone(page, secret)
  })
})
