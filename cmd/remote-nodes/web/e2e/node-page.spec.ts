import { attention, card, dialog, expect, liftLimit, reset, stateHolds, test, toast } from './fixtures'
import { confirmDialog, finishJob, menuItem, openMenu, openNode, setNode, verdictChip } from './node-helpers'

const chipButton = (page: import('@playwright/test').Page, name: string) => page.getByRole('button', { name, exact: true })

test.describe('the header card and the list of notes', () => {
  test('a node that works has its facts, no notes, and nothing due', async ({ page }) => {
    await openNode(page, 1)
    await expect(page.getByText('Working', { exact: true })).toBeVisible()
    await expect(page.getByText('203.0.113.24', { exact: true })).toBeVisible()
    await expect(page.getByText('port 443', { exact: true })).toBeVisible()
    await expect(page.getByText('agent 1.3.0', { exact: true })).toBeVisible()
    await expect(page.getByText('Xray 26.10.21', { exact: true })).toBeVisible()
    await expect(page.getByText(/^up 6d [34]h$/)).toBeVisible()
    await expect(attention(page)).toHaveCount(0)
    await expect(page.getByRole('button', { name: /^Update (to|available)/ })).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'All nodes' })).toBeVisible()
    await expect(page.getByRole('group', { name: 'Node actions' })).toBeVisible()
  })

  test('the more-actions menu lists what can be done, and says why an update is not on offer', async ({ page }) => {
    await openNode(page, 1)
    await openMenu(page)
    for (const name of ['Update agent', 'Update Xray']) {
      await expect(menuItem(page, name)).toHaveAttribute('aria-disabled', 'true')
      await expect(menuItem(page, name)).toContainText('Already up to date')
    }
    for (const name of ['Renew certificate', 'Rotate keys', 'Rename', 'Remove node…']) await expect(menuItem(page, name)).not.toHaveAttribute('aria-disabled', 'true')
    await expect(menuItem(page, 'Rotate keys')).toContainText('Every link for this node changes')
    // removing is set apart and in the danger colour
    await expect(menuItem(page, 'Remove node…')).toHaveClass(/text-bad-text/)
    await page.keyboard.press('Escape')
    await expect(page.getByRole('menu')).toHaveCount(0)
  })

  test('Check for updates says what it found', async ({ page, request }) => {
    await openNode(page, 1)
    await page.getByRole('button', { name: 'Check for updates' }).click()
    await expect(toast(page, 'Agent and Xray are up to date')).toBeVisible()
    await setNode(request, { id: 1, agentVersion: '1.2.0', xrayVersion: '26.10.18' })
    await page.getByRole('button', { name: 'Check for updates' }).click()
    await expect(toast(page, 'Update found: agent 1.3.0 · Xray 26.10.21')).toBeVisible()
    // both chips are there now, and the menu offers both
    await expect(chipButton(page, 'Update to 1.3.0')).toBeVisible()
    await expect(chipButton(page, 'Update to 26.10.21')).toBeVisible()
    await openMenu(page)
    await expect(menuItem(page, 'Update agent to 1.3.0')).not.toHaveAttribute('aria-disabled', 'true')
    await expect(menuItem(page, 'Update Xray to 26.10.21')).not.toHaveAttribute('aria-disabled', 'true')
  })

  test('a node with the wrong Xray says its tunnel is off, and the note updates it', async ({ page, request }) => {
    // slow enough to catch the upload's percentage
    await reset(request, { pace: 2 })
    await setNode(request, { id: 1, xrayVersion: '26.10.18' })
    await openNode(page, 1)
    await expect(page.getByText('Needs attention', { exact: true })).toBeVisible()
    await expect(page.getByText('Xray 26.10.18', { exact: true })).toBeVisible()
    const note = attention(page)
    await expect(note).toContainText('The tunnel is off')
    await expect(note).toContainText('This node runs Xray 26.10.18. Its tunnel stays off until it matches 26.10.21, the version this router uses.')

    await note.getByRole('button', { name: 'Update Xray' }).click()
    const confirm = confirmDialog(page, 'Update Xray on Node 1 to 26.10.21?')
    // the connections it cuts off, as the count it turns on
    await expect(confirm.getByText('9', { exact: true })).toBeVisible()
    await expect(confirm).toContainText('open connections drop. Apps reconnect on their own in a few seconds.')
    await expect(confirm).toContainText('It takes a minute or two. The job tray at the top shows how it goes.')
    await confirm.getByRole('button', { name: 'Update Xray' }).click()

    const job = dialog(page, /^Updating Xray/)
    await expect(job).toBeVisible()
    for (const step of ['Download Xray 26.10.21', 'Upload Xray 26.10.21', 'Check Xray 26.10.21']) await expect(job.getByText(step)).toBeVisible()
    await expect.poll(() => job.innerText(), { intervals: [40], timeout: 15000 }).toMatch(/\d+% · [\d.]+ of 13\.6 MB/)
    await expect(dialog(page, /^Xray updated/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'Xray updated · Node 1')).toBeVisible()
    await finishJob(page, /^Xray updated/)

    // the page has the node as it is now: no note, no chip, working
    await expect(attention(page)).toHaveCount(0)
    await expect(page.getByText('Working', { exact: true })).toBeVisible()
    await expect(page.getByText('Xray 26.10.21', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: /^Update to/ })).toHaveCount(0)
  })

  test('an old agent has a chip that opens its update, and the update runs as a job', async ({ page, request }) => {
    await reset(request, { pace: 2 })
    await setNode(request, { id: 1, agentVersion: '1.2.0' })
    await openNode(page, 1)
    await expect(page.getByText('agent 1.2.0', { exact: true })).toBeVisible()
    await chipButton(page, 'Update to 1.3.0').click()
    const confirm = confirmDialog(page, 'Update the agent on Node 1 to 1.3.0?')
    await confirm.getByRole('button', { name: 'Update agent' }).click()
    const job = dialog(page, /^Updating the agent/)
    for (const step of ['Download the agent', 'Upload the agent', 'Restart the agent', 'Check the agent']) await expect(job.getByText(step)).toBeVisible()
    await expect.poll(() => job.innerText(), { intervals: [40], timeout: 15000 }).toMatch(/\d+% · [\d.]+ of 18\.0 MB/)
    await expect(dialog(page, /^Agent updated/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'Agent updated · Node 1')).toBeVisible()
    await finishJob(page, /^Agent updated/)
    await expect(page.getByText('agent 1.3.0', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: /^Update to/ })).toHaveCount(0)
    // and the node's events say so
    await page.getByRole('tab', { name: 'Status' }).click()
    await expect(page.getByText('1.2.0 to 1.3.0').first()).toBeVisible()
  })

  test('a node the router can not reach says since when, and what needs its agent is off', async ({ page }) => {
    await openNode(page, 2)
    await expect(page.getByText('Unreachable', { exact: true })).toBeVisible()
    const note = attention(page)
    await expect(note).toContainText("The router can't reach this node")
    await expect(note).toContainText(/Last seen at .+, 22 minutes ago\. Check that the server is on and its address is on the domestic list, then check again\./)
    // both versions are behind, and the chips are there but off
    await expect(chipButton(page, 'Update to 1.3.0')).toBeDisabled()
    await expect(chipButton(page, 'Update to 26.10.21')).toBeDisabled()
    await expect(chipButton(page, 'Update to 1.3.0')).toHaveAttribute('title', 'Needs the router to reach this node')
    await expect(page.getByRole('button', { name: 'Check for updates' })).toBeDisabled()
    // no uptime for a node nobody can ask
    await expect(page.getByText(/^up /)).toHaveCount(0)

    await openMenu(page)
    for (const name of ['Update agent', 'Update Xray', 'Renew certificate', 'Rotate keys']) {
      await expect(menuItem(page, name)).toHaveAttribute('aria-disabled', 'true')
      await expect(menuItem(page, name)).toContainText('Needs the router to reach this node')
    }
    // a node that can't be reached can still be renamed and removed
    await expect(menuItem(page, 'Rename')).not.toHaveAttribute('aria-disabled', 'true')
    await expect(menuItem(page, 'Remove node…')).not.toHaveAttribute('aria-disabled', 'true')
    // a disabled item does nothing
    await menuItem(page, 'Rotate keys').click({ force: true })
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })

  test('Check again says whether the node is back, and the page follows', async ({ page, request }) => {
    await openNode(page, 2)
    await attention(page).getByRole('button', { name: 'Check again' }).click()
    await expect(toast(page, "Still can't reach Node 2")).toBeVisible()
    await expect(page.getByText('Unreachable', { exact: true })).toBeVisible()

    await setNode(request, { id: 2, down: false })
    await attention(page).getByRole('button', { name: 'Check again' }).click()
    await expect(toast(page, 'Node 2 is reachable again')).toBeVisible()
    // its Xray is old, so it is back with its tunnel off for that
    await expect(attention(page)).toContainText('The tunnel is off')
    await expect(attention(page)).toContainText('This node runs Xray 26.10.18.')
    await expect(page.getByText('Needs attention', { exact: true })).toBeVisible()
  })

  test('a node being installed says so, offers its job, and keeps its other tabs for later', async ({ page }) => {
    await openNode(page, 3)
    await expect(page.getByText('Installing', { exact: true })).toBeVisible()
    const note = attention(page)
    await expect(note).toContainText('This node is being installed')
    await expect(note).toContainText('This page fills in when the install is done.')
    await expect(page.getByRole('button', { name: 'Check for updates' })).toBeDisabled()
    // nothing of its agent to show yet
    for (const tab of ['Status', 'Settings']) {
      await page.getByRole('tab', { name: tab }).click()
      await expect(page.getByText('This fills in once the install is done.')).toBeVisible()
    }
    await openMenu(page)
    await expect(menuItem(page, 'Update agent')).toContainText('Available once the install is done')
    await expect(menuItem(page, 'Rotate keys')).toHaveAttribute('aria-disabled', 'true')
    await expect(menuItem(page, 'Rename')).not.toHaveAttribute('aria-disabled', 'true')
    await page.keyboard.press('Escape')
    // its job is one click away
    await note.getByRole('button', { name: 'Show progress' }).click()
    const job = dialog(page, /^Installing Node 3/)
    await expect(job).toContainText('Upload Xray 26.10.21')
    await expect(job).toContainText('62% · 8.4 of 13.6 MB')
  })

  test('a node whose install never finished is shown as an unfinished install, not as a node', async ({ page, request }) => {
    await setNode(request, { id: 1, registered: false })
    await page.goto('/#/nodes/1')
    await expect(page.getByRole('heading', { level: 1, name: "Node 1's install didn't finish" })).toBeVisible()
    await expect(page.getByText('Not finished', { exact: true })).toBeVisible()
    // nothing of a node: no tabs, no updates, no settings, no subscriptions
    await expect(page.getByRole('tab')).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Check for updates' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'More actions' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Add subscription' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Clean up…' })).toBeVisible()
    await page.getByRole('button', { name: 'Forget…' }).click()
    await expect(page.getByRole('alertdialog', { name: 'Forget Node 1?' })).toContainText("Only this record is deleted. The server isn't touched")
  })

  test('says why Xray is not up: crashing, stopped, or settings the agent could not apply', async ({ page, request }) => {
    await setNode(request, { id: 1, crash: true })
    await openNode(page, 1)
    await expect(page.getByText('Needs attention', { exact: true })).toBeVisible()
    await expect(attention(page)).toContainText('Xray keeps crashing')
    await expect(attention(page)).toContainText('It last stopped with "exit status 1: config: address already in use". It restarts on its own and waits longer each time.')
    // the tunnel went down with it
    await expect(attention(page)).toContainText('The tunnel is down')

    await reset(request)
    await setNode(request, { id: 1, stopped: true })
    await page.reload()
    await expect(attention(page)).toContainText('Xray is not running')
    await expect(attention(page)).toContainText("The agent reports that Xray isn't running, so subscriptions can't connect.")

    await reset(request)
    await setNode(request, { id: 1, notApplied: 'xray -test failed: port 443 already in use' })
    await page.reload()
    await expect(attention(page)).toContainText("Settings aren't applied yet")
    await expect(attention(page)).toContainText('Xray -test failed: port 443 already in use.')
  })

  test("a bridge that is not running is the router's trouble, and the page says it will come back", async ({ page, request }) => {
    await setNode(request, { id: 1, bridgeDown: true })
    await openNode(page, 1)
    await expect(page.getByText('Needs attention', { exact: true })).toBeVisible()
    await expect(attention(page)).toContainText("The router's bridge is not running")
    await expect(attention(page)).toContainText('The plugin starts it again by itself.')
    await page.getByRole('tab', { name: 'Status' }).click()
    await expect(page.getByRole('heading', { level: 2, name: "Subscriptions can't connect right now" })).toBeVisible()
    await expect(verdictChip(page, 'Bridge', 'stopped')).toBeVisible()
    await expect(verdictChip(page, 'Portal', 'listening')).toBeVisible()
  })

  test('a certificate that should have been renewed has a note that renews it', async ({ page, request }) => {
    await setNode(request, { id: 1, certDays: 9 })
    await openNode(page, 1)
    await expect(page.getByText('Needs attention', { exact: true })).toBeVisible()
    await expect(attention(page)).toContainText('The certificate expires in 9 days')
    await attention(page).getByRole('button', { name: 'Renew now' }).click()
    const confirm = confirmDialog(page, 'Renew the certificate on Node 1?')
    await expect(confirm).toContainText('valid for 12 months. Subscriptions keep their links.')
    await confirm.getByRole('button', { name: 'Renew certificate' }).click()
    const job = dialog(page, /^(Renewing the certificate|Certificate renewed)/)
    await expect(job.getByText('Renew the certificate')).toBeVisible()
    await expect(job.getByText('Save the node')).toBeVisible()
    await expect(dialog(page, /^Certificate renewed/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'Certificate renewed · Node 1')).toBeVisible()
    await finishJob(page, /^Certificate renewed/)
    await expect(attention(page)).toHaveCount(0)
    await expect(page.getByText('Working', { exact: true })).toBeVisible()
  })

  test('with entry off the node still works, and a note says where to look', async ({ page, request }) => {
    await reset(request, { entry: 'vpn-offline' })
    await openNode(page, 1)
    await expect(page.getByText('Working', { exact: true })).toBeVisible()
    await expect(attention(page)).toContainText('Entry is off')
    await attention(page).getByRole('button', { name: 'Open Settings' }).click()
    await expect(page).toHaveURL(/#\/settings$/)
  })
})

test.describe('actions that are jobs', () => {
  test('a job that fails shows where, and the node is as it was', async ({ page, request }) => {
    await setNode(request, { id: 1, xrayVersion: '26.10.18', failNext: 'update-xray' })
    await openNode(page, 1)
    await attention(page).getByRole('button', { name: 'Update Xray' }).click()
    await confirmDialog(page, 'Update Xray on Node 1 to 26.10.21?').getByRole('button', { name: 'Update Xray' }).click()
    const failed = dialog(page, /^Xray update failed/)
    await expect(failed).toBeVisible({ timeout: 20000 })
    await expect(failed).toContainText('Failed at step 2 of 3')
    await expect(failed).toContainText('the download does not match the pinned checksum')
    await expect(toast(page, 'Xray update failed · Node 1')).toBeVisible()
    await failed.getByRole('button', { name: 'Done' }).click()
    // still on the old Xray, still asking for the update
    await expect(page.getByText('Xray 26.10.18', { exact: true })).toBeVisible()
    await expect(attention(page)).toContainText('The tunnel is off')
  })

  test('a node with a job running refuses another, and the dialog says so', async ({ page, request }) => {
    // the first job has to still be running when the second is asked for
    await reset(request, { pace: 4 })
    await openNode(page, 1)
    await openMenu(page)
    await menuItem(page, 'Rotate keys').click()
    await confirmDialog(page, 'Rotate the keys on Node 1?').getByRole('button', { name: 'Rotate keys' }).click()
    const running = dialog(page, /^Rotating keys/)
    await expect(running).toBeVisible()
    // closing the job's dialog does not stop it
    await running.getByRole('button', { name: 'Close; it keeps going' }).first().click()
    await openMenu(page)
    await menuItem(page, 'Renew certificate').click()
    const confirm = confirmDialog(page, 'Renew the certificate on Node 1?')
    await confirm.getByRole('button', { name: 'Renew certificate' }).click()
    await expect(confirm.getByRole('alert')).toContainText('Another job is already running on this node.')
    await confirm.getByRole('button', { name: 'Cancel' }).click()
    await expect(toast(page, 'Keys rotated · Node 1')).toBeVisible({ timeout: 20000 })
  })

  test('the install the tray follows is on the page it belongs to', async ({ page }) => {
    await openNode(page, 1)
    await page.getByRole('button', { name: 'Installing Node 3' }).click()
    await expect(page.getByRole('region', { name: 'Jobs' })).toContainText('Installing Node 3')
  })
})

test.describe('naming and removing', () => {
  test('holds the new name to 64 characters, and shows the hub\'s sentence for a longer one', async ({ page }) => {
    await openNode(page, 1)
    await openMenu(page)
    await menuItem(page, 'Rename').click()
    const d = dialog(page, 'Rename node')
    await expect(d.getByLabel('Name')).toHaveAttribute('maxlength', '64')
    // a name that got past the field is answered by the hub in its own words
    await liftLimit(d.getByLabel('Name'))
    await d.getByLabel('Name').fill('x'.repeat(65))
    await d.getByRole('button', { name: 'Save' }).click()
    await expect(d.getByRole('alert')).toContainText('The name can have at most 64 characters.')
    await d.getByLabel('Name').fill('x'.repeat(64))
    await d.getByRole('button', { name: 'Save' }).click()
    await expect(d).toBeHidden()
  })

  test('renames a node from the menu, and refuses a name that is taken', async ({ page }) => {
    await openNode(page, 1)
    await openMenu(page)
    await menuItem(page, 'Rename').click()
    const d = dialog(page, 'Rename node')
    const save = d.getByRole('button', { name: 'Save' })
    const name = d.getByLabel('Name')
    await expect(name).toHaveValue('Node 1')
    // nothing changed, nothing to save; and nothing is not a name
    await expect(save).toBeDisabled()
    await name.fill('   ')
    await expect(save).toBeDisabled()
    await name.fill('Node 2')
    await save.click()
    await expect(d.getByRole('alert')).toContainText('That name is already used.')
    await name.fill('Tehran')
    await name.press('Enter')
    await expect(d).toBeHidden()
    await expect(toast(page, 'Renamed to Tehran')).toBeVisible()
    await expect(page.getByRole('heading', { level: 1, name: 'Tehran' })).toBeVisible()
    await page.getByRole('link', { name: 'All nodes' }).click()
    await expect(card(page, 'Tehran')).toBeVisible()
  })

  test('asks for the SSH login first and says why, then removes the node with it', async ({ page }) => {
    const secret = 'hunter2-not-a-real-password'
    const calls: unknown[] = []
    page.on('request', (r) => r.url().endsWith('/api/nodes/1/remove') && r.method() === 'POST' && calls.push(r.postDataJSON()))
    await openNode(page, 1)
    await openMenu(page)
    await menuItem(page, 'Remove node…').click()
    const d = dialog(page, 'Remove Node 1?')
    // the login form is the first thing there is: what removal does, why a login is needed, and no call yet
    await expect(d).toContainText("The agent and Xray are removed from the node, and every subscription's link to it stops working.")
    await expect(d.getByText('Removing the agent and Xray from the server needs SSH access. Log in to do it.')).toBeVisible()
    await expect(d.getByLabel('SSH port')).toHaveValue('22')
    await expect(d.getByLabel('Username')).toHaveValue('root')
    await expect(d.getByText('No login? Remove the agent by hand later.')).toBeVisible()
    await expect(d.getByRole('button', { name: 'Remove node', exact: true })).toHaveCount(0)
    expect(calls).toEqual([])

    await d.locator('input[type="password"]').fill(secret)
    await d.getByRole('button', { name: 'Log in and remove' }).click()
    const job = dialog(page, /^(Removing the node|Node removed)/)
    // taking the agent off the server is four steps, the last of which forgets the node
    for (const step of ['Check the server', 'Stop the agent', 'Remove the agent', 'Forget the node']) await expect(job.getByText(step)).toBeVisible()
    await expect(dialog(page, /^Node removed/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'Node removed · Node 1')).toBeVisible()
    await finishJob(page, /^Node removed/)
    // one call, and it carried the login: the host key is the one the node was set up with
    expect(calls).toHaveLength(1)
    expect(calls[0]).toMatchObject({ login: { sshPort: 22, sshUser: 'root', password: secret } })
    expect((calls[0] as { login: { hostKey: string } }).login.hostKey).toMatch(/^SHA256:/)
    expect(calls[0]).not.toHaveProperty('force')
    // the node page is gone, and the list has two
    await expect(page).toHaveURL(/#\/$/)
    await expect(page.getByText('2 of 3 nodes')).toBeVisible()
    await expect(card(page, 'Node 1')).toHaveCount(0)
    await expect(card(page, 'Node 2')).toBeVisible()
  })

  test('a node the router cannot reach is asked for the same login, with its host key checked', async ({ page }) => {
    const secret = 'hunter2-not-a-real-password'
    await openNode(page, 2)
    await openMenu(page)
    await menuItem(page, 'Remove node…').click()
    const d = dialog(page, 'Remove Node 2?')
    await expect(d.getByText('Removing the agent and Xray from the server needs SSH access. Log in to do it.')).toBeVisible()
    await expect(d.getByLabel('SSH port')).toHaveValue('22')
    await expect(d.getByLabel('Username')).toHaveValue('root')
    await expect(d.getByText('No login? Remove the agent by hand later.')).toBeVisible()
    // a login can't be sent without a password: the browser stops it at the field
    await d.getByRole('button', { name: 'Log in and remove' }).click()
    await expect(d.locator('input[type="password"]')).toBeFocused()
    expect(await d.locator('input[type="password"]').evaluate((el: HTMLInputElement) => el.validity.valueMissing)).toBe(true)
    // a refused login stays in the dialog
    await d.locator('input[type="password"]').fill('wrong')
    await d.getByRole('button', { name: 'Log in and remove' }).click()
    await expect(d.getByRole('alert')).toContainText('ssh: unable to authenticate: the password or key was not accepted')

    await d.locator('input[type="password"]').fill(secret)
    expect(await stateHolds(page, secret)).toBe(true)
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/nodes/2/remove') && r.method() === 'POST')
    await d.getByRole('button', { name: 'Log in and remove' }).click()
    const body = (await sent).postDataJSON()
    expect(body.login).toMatchObject({ sshPort: 22, sshUser: 'root', password: secret })
    // the key the probe showed is the key that is sent
    expect(body.login.hostKey).toMatch(/^SHA256:/)
    expect(body.force).toBeUndefined()
    await expect(dialog(page, /^(Removing the node|Node removed)/)).toBeVisible()
    // the password is not kept once the removal has started
    expect(await stateHolds(page, secret)).toBe(false)
    await expect(dialog(page, /^Node removed/)).toBeVisible({ timeout: 20000 })
    await expect(toast(page, 'Node removed · Node 2')).toBeVisible()
    await finishJob(page, /^Node removed/)
    await expect(page).toHaveURL(/#\/$/)
    await expect(card(page, 'Node 2')).toHaveCount(0)
  })

  test('without a login a node can be forgotten, which leaves the server alone', async ({ page }) => {
    await openNode(page, 2)
    await openMenu(page)
    await menuItem(page, 'Remove node…').click()
    const d = dialog(page, 'Remove Node 2?')
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/nodes/2/remove') && r.method() === 'POST')
    await d.getByRole('button', { name: 'Forget without removing' }).click()
    expect((await sent).postDataJSON()).toEqual({ force: true })
    // one step, so short that the job may be over before its dialog is up
    const job = dialog(page, /^(Removing the node|Node removed)/)
    await expect(job.getByText('Forget the node')).toBeVisible()
    // nothing is taken off the server
    await expect(job.getByText('Remove the agent')).toHaveCount(0)
    await expect(job.getByText('Check the server')).toHaveCount(0)
    await expect(dialog(page, /^Node removed/)).toBeVisible({ timeout: 20000 })
    await finishJob(page, /^Node removed/)
    await expect(card(page, 'Node 2')).toHaveCount(0)
  })

  test('a changed host key stops the removal until it is trusted on purpose', async ({ page, request }) => {
    await setNode(request, { id: 2, hostKeyChanged: true })
    await openNode(page, 2)
    await openMenu(page)
    await menuItem(page, 'Remove node…').click()
    const d = dialog(page, 'Remove Node 2?')
    await d.locator('input[type="password"]').fill('a-password')
    await d.getByRole('button', { name: 'Log in and remove' }).click()
    await expect(d).toContainText('198.51.100.7 shows a different key than last time.')
    await expect(d).toContainText('SHA256:u3Rq0Q1uV3Jc0mYw7b6o8xk3j2m9Pq5c1f0aZyXwVb8')
    const trust = d.getByRole('button', { name: 'Trust and remove' })
    await expect(trust).toBeDisabled()
    // Back keeps the login that was typed
    await d.getByRole('button', { name: 'Back' }).click()
    await expect(d.locator('input[type="password"]')).toHaveValue('a-password')
    await d.getByRole('button', { name: 'Log in and remove' }).click()
    await d.getByLabel('I know why the key changed').check()
    await expect(trust).toBeEnabled()
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/nodes/2/remove') && r.method() === 'POST')
    await trust.click()
    expect((await sent).postDataJSON().login.hostKey).toBe('SHA256:u3Rq0Q1uV3Jc0mYw7b6o8xk3j2m9Pq5c1f0aZyXwVb8')
    await expect(dialog(page, /^Node removed/)).toBeVisible({ timeout: 20000 })
  })

  test('a node being installed can not be removed yet, and the dialog says why', async ({ page }) => {
    await openNode(page, 3)
    await openMenu(page)
    await menuItem(page, 'Remove node…').click()
    const d = dialog(page, 'Remove Node 3?')
    await d.locator('input[type="password"]').fill('a-password')
    await d.getByRole('button', { name: 'Log in and remove' }).click()
    await expect(d.getByRole('alert')).toContainText('Another job is already running on this node.')
  })

  test('a hub that asks for a login all the same gets the login form back, with its reason', async ({ page }) => {
    // the page always sends a login or force, so this answer is made up: what the page does with one is what is under test
    await openNode(page, 1)
    await page.route('**/api/nodes/1/remove', (route) =>
      route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ code: 'needs_login', error: 'log in first', needLogin: true }) }),
    )
    await openMenu(page)
    await menuItem(page, 'Remove node…').click()
    const d = dialog(page, 'Remove Node 1?')
    await d.locator('input[type="password"]').fill('a-password')
    await d.getByRole('button', { name: 'Log in and remove' }).click()
    await expect(d.getByRole('alert')).toContainText('Removing the agent and Xray needs an SSH login.')
    // still the form, with what was typed
    await expect(d.locator('input[type="password"]')).toHaveValue('a-password')
    await expect(d.getByRole('button', { name: 'Log in and remove' })).toBeEnabled()
  })
})

test.describe('tabs', () => {
  test('arrow keys, Home and End move between the node tabs and the address follows', async ({ page }) => {
    await openNode(page, 1)
    await page.getByRole('tab', { name: 'Subscriptions' }).focus()
    await page.keyboard.press('ArrowRight')
    await expect(page).toHaveURL(/#\/nodes\/1\/status$/)
    await expect(page.getByRole('tab', { name: 'Status' })).toBeFocused()
    await expect(page.getByRole('tab', { name: 'Status' })).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('End')
    await expect(page).toHaveURL(/#\/nodes\/1\/settings$/)
    await page.keyboard.press('ArrowRight')
    await expect(page).toHaveURL(/#\/nodes\/1$/)
    await expect(page.getByRole('tab', { name: 'Subscriptions' })).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('ArrowLeft')
    await expect(page.getByRole('tab', { name: 'Settings' })).toBeFocused()
    await page.keyboard.press('Home')
    await expect(page).toHaveURL(/#\/nodes\/1$/)
    // the panel is the one the tab names
    await expect(page.getByRole('tabpanel')).toHaveAttribute('aria-labelledby', 'node-subscriptions')
  })

  test('a tab keeps its place when the node moves on', async ({ page }) => {
    await openNode(page, 1, 'settings')
    await expect(page.getByRole('tab', { name: 'Settings' })).toHaveAttribute('aria-selected', 'true')
    await page.reload()
    await expect(page.getByRole('tab', { name: 'Settings' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('heading', { name: 'Reality target' })).toBeVisible()
  })
})
