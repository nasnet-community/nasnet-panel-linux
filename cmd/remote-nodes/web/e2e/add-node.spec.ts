import { card, dialog, expect, fillAddNode, openAddNode, reset, stateHolds, test, toast } from './fixtures'

const STEPS = [
  'Fetch the signed release',
  'Check signature and hashes',
  'Send the agent',
  'Start the agent',
  'Send Xray 26.10.21',
  'Choose a handshake target',
  'Apply the config',
  'Start the tunnel',
]

test.beforeEach(async ({ request }) => {
  // two nodes of the three, so there is room for one more
  await reset(request, { nodes: 2 })
})

test('adds a node: the form, then the install job with its steps, then the finished node', async ({ page }) => {
  await openAddNode(page)
  const form = dialog(page, 'Add a node')
  await expect(form).toContainText('The login is used once for the install and never stored.')
  // the name starts as the next free "Node N", and the port as 22 with the user root
  await expect(form.getByLabel('Name', { exact: true })).toHaveAttribute('placeholder', 'Node 3')
  await expect(form.getByLabel('SSH port')).toHaveValue('22')
  await expect(form.getByLabel('Username')).toHaveValue('root')
  await fillAddNode(page, '192.0.2.41')

  // the first host key is pinned as it is shown, with no question
  const sent = page.waitForRequest((r) => r.url().endsWith('/api/nodes') && r.method() === 'POST')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const body = (await sent).postDataJSON()
  expect(body).toMatchObject({ host: '192.0.2.41', name: 'Node 3', sshPort: 22, sshUser: 'root', password: 'a-password', entryPort: 443 })
  expect(body.hostKey).toMatch(/^SHA256:/)
  expect(body.privateKey).toBeUndefined()

  const job = dialog(page, 'Installing Node 3')
  await expect(job).toBeVisible()
  // every step is listed from the start, the later ones not started
  for (const step of STEPS) await expect(job.getByText(step)).toBeVisible()
  await expect(job).toContainText(/Step \d of 8 · \d+:\d\d so far/)
  // the upload reports how far it has got (a short window, so look often)
  await expect.poll(() => job.innerText(), { intervals: [40], timeout: 15000 }).toMatch(/\d+% · [\d.]+ of 18\.0 MB/)
  await expect(job).toContainText("You can close this. The install keeps going, and the job tray at the top shows it until it's done.")
  // the header has it too, and the list shows the node as installing
  await expect(page.getByRole('button', { name: 'Installing Node 3' })).toBeVisible()
  await expect(card(page, 'Node 3')).toContainText('Installing')

  const done = dialog(page, 'Node added')
  await expect(done).toBeVisible({ timeout: 20000 })
  await expect(done).toContainText(/Took \d+:\d\d/)
  await expect(done).toContainText('The agent is running and its tunnel is up.')
  for (const step of STEPS) await expect(done.getByText(step)).toBeVisible()
  await expect(done).toContainText('Connected over mTLS')
  await expect(done).toContainText('www.example.com:443')
  await expect(toast(page, 'Node 3 added')).toBeVisible()
  // the list and the cap follow
  await expect(page.getByText('3 of 3 nodes')).toBeVisible()
  await expect(page.getByText('Limit reached')).toBeVisible()
  await expect(card(page, 'Node 3')).toContainText('Working')

  await done.getByRole('button', { name: 'Open node' }).click()
  await expect(page).toHaveURL(/#\/nodes\/3$/)
  await expect(page.getByRole('heading', { name: 'Node 3' })).toBeVisible()
})

test('a name typed in is the name the node gets, and a key can stand in for the password', async ({ page }) => {
  await openAddNode(page)
  const form = dialog(page, 'Add a node')
  await form.getByLabel('Server address').fill('192.0.2.50')
  await form.getByLabel('Name', { exact: true }).fill('Tehran 1')
  await form.getByRole('radio', { name: 'Private key' }).click()
  await form.getByLabel('Private key').fill('-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----')
  await form.getByRole('button', { name: 'Advanced' }).click()
  await form.getByLabel('Entry port').fill('8443')
  const sent = page.waitForRequest((r) => r.url().endsWith('/api/nodes') && r.method() === 'POST')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const body = (await sent).postDataJSON()
  expect(body).toMatchObject({ host: '192.0.2.50', name: 'Tehran 1', entryPort: 8443 })
  expect(body.privateKey).toContain('BEGIN OPENSSH PRIVATE KEY')
  // only the chosen secret goes
  expect(body.password).toBeUndefined()
  await expect(dialog(page, 'Installing Tehran 1')).toBeVisible()
  await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 20000 })
  await expect(card(page, 'Tehran 1')).toBeVisible()
})

test('closing the dialog does not stop the install: the tray shows it and a toast says when it is done', async ({ page }) => {
  await openAddNode(page)
  await fillAddNode(page, '192.0.2.41')
  await dialog(page, 'Add a node').getByRole('button', { name: 'Connect and install' }).click()
  const job = dialog(page, 'Installing Node 3')
  await expect(job).toBeVisible()
  // the close button says it keeps going
  await job.getByRole('button', { name: 'Close; it keeps going' }).first().click()
  await expect(job).toBeHidden()
  await expect(page.getByRole('button', { name: 'Installing Node 3' })).toBeVisible()
  await expect(toast(page, 'Node 3 added')).toBeVisible({ timeout: 20000 })
  await expect(card(page, 'Node 3')).toContainText('Working')
  await expect(page.getByRole('button', { name: 'Installing Node 3' })).toHaveCount(0)
  // the tray keeps the result for a while
  await page.getByRole('button', { name: 'Recent jobs' }).click()
  await expect(page.getByRole('region', { name: 'Jobs' })).toContainText('Node 3 added')
})

test('a changed host key stops the install until it is trusted on purpose', async ({ page }) => {
  await openAddNode(page)
  const form = await fillAddNode(page, 'changed.example.com')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const stop = dialog(page, 'Add a node')
  await expect(stop).toContainText('changed.example.com shows a different key than last time.')
  await expect(stop).toContainText("If you didn't reinstall it, don't continue.")
  await expect(stop).toContainText('SHA256:u3Rq0Q1uV3Jc0mYw7b6o8xk3j2m9Pq5c1f0aZyXwVb8')
  const trust = stop.getByRole('button', { name: 'Trust and install' })
  await expect(trust).toBeDisabled()
  // Back keeps what was typed, the login included
  await stop.getByRole('button', { name: 'Back' }).click()
  await expect(stop.getByLabel('Server address')).toHaveValue('changed.example.com')
  await expect(stop.locator('input[type="password"]')).toHaveValue('a-password')
  await stop.getByRole('button', { name: 'Connect and install' }).click()
  await stop.getByLabel('I know why the key changed').check()
  await expect(trust).toBeEnabled()
  const sent = page.waitForRequest((r) => r.url().endsWith('/api/nodes') && r.method() === 'POST')
  await trust.click()
  // the key it was shown is the key that is sent
  expect((await sent).postDataJSON().hostKey).toBe('SHA256:u3Rq0Q1uV3Jc0mYw7b6o8xk3j2m9Pq5c1f0aZyXwVb8')
  await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 20000 })
})

test('an install that fails shows the step, the reason and a hint, and leaves no node behind', async ({ page }) => {
  await openAddNode(page)
  const form = await fillAddNode(page, 'fail.example.com')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const failed = dialog(page, 'Installation failed')
  await expect(failed).toBeVisible({ timeout: 20000 })
  await expect(failed).toContainText('Failed at step 4 of 8')
  await expect(failed.getByText('dial fail.example.com:8443: i/o timeout')).toBeVisible()
  // the steps after it were never started
  await expect(failed.getByText('Send Xray 26.10.21')).toHaveClass(/text-faint/)
  await expect(failed).toContainText('This router reaches the node over its Domestic link, so the address must be on the domestic list.')
  await expect(toast(page, 'Installation failed')).toBeVisible()
  await expect(page.getByText('2 of 3 nodes')).toBeVisible()
  await expect(card(page, 'Node 3')).toHaveCount(0)

  // Try again is the form again, without the password it had
  await failed.getByRole('button', { name: 'Try again' }).click()
  await expect(dialog(page, 'Add a node')).toBeVisible()
  await expect(dialog(page, 'Add a node').locator('input[type="password"]')).toHaveValue('')
})

test('an install that fails late keeps the node, which can then be opened', async ({ page }) => {
  await openAddNode(page)
  const form = await fillAddNode(page, 'late.example.com')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const failed = dialog(page, 'Installation failed')
  await expect(failed).toBeVisible({ timeout: 20000 })
  await expect(failed).toContainText('Failed at step 6 of 8')
  await expect(failed).toContainText("www.example.com:443 didn't answer from this node")
  await expect(failed).toContainText('The node is saved. Open it to see where it stands.')
  // the node is saved, so there is no starting over; the domestic hint belongs to the first steps
  await expect(failed).not.toContainText('Domestic link')
  await expect(failed.getByRole('button', { name: 'Try again' })).toHaveCount(0)
  await failed.getByRole('button', { name: 'Open node' }).click()
  await expect(page).toHaveURL(/#\/nodes\/3$/)
})

test('a job whose stream is refused is followed by polling', async ({ page }) => {
  await openAddNode(page)
  const form = await fillAddNode(page, 'poll.example.com')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  await expect(dialog(page, 'Installing Node 3')).toBeVisible()
  await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 25000 })
  await expect(toast(page, 'Node 3 added')).toBeVisible()
})

test('says what is wrong with the form before asking the hub', async ({ page }) => {
  await openAddNode(page)
  const form = await fillAddNode(page, '192.0.2.41')
  await form.getByRole('button', { name: 'Advanced' }).click()
  await form.getByLabel('Entry port').fill('22')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  await expect(form.getByRole('alert')).toContainText('The entry port is a number from 1 to 65535, and not the SSH port.')
  await form.getByLabel('Entry port').fill('')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  await expect(form.getByRole('alert')).toContainText('The entry port is a number from 1 to 65535')
})

test('shows what the hub says when it cannot reach the node or refuses the login', async ({ page }) => {
  await openAddNode(page)
  const form = await fillAddNode(page, 'offline.example.com')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  // a code the page has its own words for
  await expect(form.getByRole('alert')).toContainText("The router can't reach this node. Check that it is on and its address is on the domestic list.")
  // one it hasn't: the hub's sentence
  await form.getByLabel('Server address').fill('192.0.2.41')
  await form.locator('input[type="password"]').fill('wrong')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  await expect(form.getByRole('alert')).toContainText('ssh: unable to authenticate: the password or key was not accepted')
  // still the form, with what was typed
  await expect(form.getByLabel('Server address')).toHaveValue('192.0.2.41')
})

test('the password is not kept once the install has started', async ({ page }) => {
  const secret = 'hunter2-not-a-real-password'
  await openAddNode(page)
  const form = await fillAddNode(page, '192.0.2.41', secret)
  // typed, so the walk over the page's state does find it
  expect(await stateHolds(page, secret)).toBe(true)
  await form.getByRole('button', { name: 'Connect and install' }).click()
  await expect(dialog(page, 'Installing Node 3')).toBeVisible()
  expect(await stateHolds(page, secret)).toBe(false)
  await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 20000 })
  expect(await stateHolds(page, secret)).toBe(false)
})
