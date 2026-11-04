import type { Page } from '@playwright/test'
import { PAGE, dialog, expect, liftLimit, reset, test, toast } from './fixtures'
import { addViaApi, fillToLimit, flipper, isTable, listed, openNodeSubscriptions, openSubscriptions, pick, row } from './subs'

const tile = (page: Page, label: string) => page.locator('main .grid > div').filter({ hasText: label })
const add = (page: Page) => page.getByRole('button', { name: 'Add subscription' })

// what the design board shows: who is on which node (Node 1, Node 2), how each is doing, what it moved
const SAMPLE: [name: string, on: [boolean, boolean], status: string, sub: string, today: string, month: string][] = [
  ['Sara', [true, true], 'Online', 'on Node 1', '1.2 GB', '38.0 GB in 30 days'],
  ['Ali', [true, false], 'Online', 'on Node 1', '640 MB', '21.0 GB in 30 days'],
  ['Reza', [true, true], 'Online', 'on Node 1', '1.9 GB', '52.0 GB in 30 days'],
  ['Mina', [true, false], 'Active', 'last seen 3 hours ago', '310 MB', '9.4 GB in 30 days'],
  ['Parsa', [true, false], 'Active', 'last seen yesterday', '0 B', '1.1 GB in 30 days'],
  ['Dana', [true, true], 'Active', 'last seen 2 days ago', '0 B', '600 MB in 30 days'],
  ['Kian', [false, true], 'Active', 'last seen 25 minutes ago', '210 MB', '5.2 GB in 30 days'],
]

test('the tab shows the sample data: the tiles, the toolbar, and every subscription with its switches, status and traffic', async ({ page, isMobile }) => {
  await openSubscriptions(page)

  await expect(tile(page, 'Subscriptions')).toHaveText('Subscriptions 7 of 10 3 places left', { useInnerText: true })
  await expect(tile(page, 'Online now')).toHaveText('Online now 3 on Node 1', { useInnerText: true })
  await expect(tile(page, 'Today')).toHaveText('Today 4.3 GB ↑ 1.1 GB · ↓ 3.2 GB', { useInnerText: true })
  await expect(tile(page, 'Last 30 days')).toHaveText('Last 30 days 127 GB ↑ 31.8 GB · ↓ 95.5 GB', { useInnerText: true })

  await expect(page.getByText(/^Updated \d+ s ago$/)).toBeVisible()
  await expect(page.getByText('7 of 10 subscriptions')).toBeVisible()
  await expect(add(page)).toBeVisible()
  await expect(add(page)).not.toHaveAttribute('aria-disabled', 'true')

  for (const [name, on, status, sub, today, month] of SAMPLE) {
    const r = row(page, name)
    await expect(r, name).toBeVisible()
    await expect(r).toContainText(status)
    await expect(r).toContainText(sub)
    await expect(r).toContainText(`${today} today`)
    await expect(r).toContainText(month)
    await expect(flipper(page, name, 'Node 1'), `${name} on Node 1`).toHaveAttribute('aria-checked', String(on[0]))
    await expect(flipper(page, name, 'Node 2'), `${name} on Node 2`).toHaveAttribute('aria-checked', String(on[1]))
    await expect(r.getByRole('button', { name: 'Share', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: `Actions for ${name}`, exact: true })).toBeVisible()
  }

  // Node 2 can't be reached, and says so where its switches are
  if (isMobile) await expect(row(page, 'Sara')).toContainText(/Node 2\s*unreachable/)
  else {
    const heads = page.getByRole('columnheader')
    await expect(heads.nth(0)).toHaveText('Subscription')
    await expect(heads.nth(1)).toHaveText('Node 1')
    await expect(heads.nth(2)).toContainText('Node 2')
    await expect(heads.nth(2)).toContainText('unreachable')
    await expect(heads.nth(3)).toHaveText('Status')
    await expect(heads.nth(4)).toHaveText('Traffic')
    // a dot for how each node is doing
    await expect(heads.nth(1).locator('[aria-hidden]')).toHaveClass(/bg-ok/)
    await expect(heads.nth(2).locator('[aria-hidden]')).toHaveClass(/bg-bad/)
  }

  // a node still installing has no switches yet, and the page says it will
  await expect(page.getByText("Node 3 is installing. It shows up here when it's ready.")).toBeVisible()
  await expect(page.getByRole('switch', { name: /on Node 3/ })).toHaveCount(0)
})

test('a table on wide screens, cards on phones', async ({ page, isMobile }) => {
  await openSubscriptions(page)
  if (isMobile) {
    await expect(page.getByRole('table')).toHaveCount(0)
    // one card each, with a row of switch per node
    const card = row(page, 'Sara')
    await expect(card.getByRole('switch')).toHaveCount(2)
    await expect(card).toContainText('1.2 GB today · 38.0 GB in 30 days')
  } else {
    await expect(page.getByRole('table')).toHaveCount(1)
    await expect(page.getByRole('row')).toHaveCount(8)
  }
})

test('the table gives way to cards below 768 px, and neither makes the page scroll sideways', async ({ page, isMobile }) => {
  test.skip(isMobile, 'sets its own widths')
  for (const open of [() => openSubscriptions(page), () => openNodeSubscriptions(page, 1)]) {
    for (const [width, table] of [[1280, true], [768, true], [767, false], [360, false]] as const) {
      await page.setViewportSize({ width, height: 900 })
      await open()
      await expect(page.getByRole('table'), `${width} px`).toHaveCount(table ? 1 : 0)
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth), `${width} px`).toBeLessThanOrEqual(0)
    }
  }
})

test('the subscriptions are in order of name, whatever the order they were added in', async ({ page }) => {
  await openSubscriptions(page)
  expect(await listed(page, 'Node 1')).toEqual(['Ali', 'Dana', 'Kian', 'Mina', 'Parsa', 'Reza', 'Sara'])
  // a new one finds its place, and "Extra 2" comes before "Extra 10"
  await addViaApi(page.request, 'extra 10')
  await addViaApi(page.request, 'Extra 2')
  await page.reload()
  await expect(flipper(page, 'Extra 2', 'Node 1')).toBeVisible()
  expect(await listed(page, 'Node 1')).toEqual(['Ali', 'Dana', 'Extra 2', 'extra 10', 'Kian', 'Mina', 'Parsa', 'Reza', 'Sara'])
})

test('adds a subscription on the nodes ticked, then offers its link', async ({ page }) => {
  await openSubscriptions(page)
  await add(page).click()
  const d = dialog(page, 'Add a subscription')
  await expect(d).toContainText("3 places left. Its link is ready as soon as it's added.")
  await expect(d.getByLabel('Name')).toBeFocused()
  await expect(d).toContainText('The app shows this name too.')
  // the node that works is ticked; the one that can't be reached can be, but isn't; an installing one can't
  await expect(d.getByRole('checkbox', { name: 'Node 1', exact: true })).toBeChecked()
  await expect(d.getByRole('checkbox', { name: 'Node 2 unreachable' })).not.toBeChecked()
  await expect(d.getByRole('checkbox', { name: 'Node 2 unreachable' })).toBeEnabled()
  await expect(d.getByRole('checkbox', { name: 'Node 3 installing' })).toBeDisabled()
  await expect(d).toContainText('Each node gives it its own link.')

  await d.getByLabel('Name').fill('  Nika ')
  const sent = page.waitForRequest((r) => r.url().endsWith('/api/subscriptions') && r.method() === 'POST')
  await d.getByRole('button', { name: 'Add subscription' }).click()
  expect((await sent).postDataJSON()).toEqual({ name: 'Nika', nodeIds: [1] })
  await expect(toast(page, 'Nika added')).toBeVisible()

  // its link is ready: the share dialog opens on it
  const share = dialog(page, "Share Nika's link")
  await expect(share).toBeVisible()
  await expect(share.locator('[data-link]')).toHaveText(/^vless:\/\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}@203\.0\.113\.24:443\?.*#Nika$/)
  await share.getByRole('button', { name: 'Close', exact: true }).click()

  // the list and the count follow, with Nika on Node 1 only
  await expect(page.getByText('8 of 10 subscriptions')).toBeVisible()
  await expect(tile(page, 'Subscriptions')).toContainText('8 of 10')
  await expect(tile(page, 'Subscriptions')).toContainText('2 places left')
  await expect(flipper(page, 'Nika', 'Node 1')).toHaveAttribute('aria-checked', 'true')
  await expect(flipper(page, 'Nika', 'Node 2')).toHaveAttribute('aria-checked', 'false')
  await expect(row(page, 'Nika')).toContainText('never connected')
  await expect(row(page, 'Nika')).toContainText('0 B today')
})

test('ticking the unreachable node too gives one link for each, and the picker says which node is down', async ({ page }) => {
  await openSubscriptions(page)
  await add(page).click()
  const d = dialog(page, 'Add a subscription')
  await d.getByLabel('Name').fill('Nika')
  await d.getByRole('checkbox', { name: 'Node 2 unreachable' }).check()
  const sent = page.waitForRequest((r) => r.url().endsWith('/api/subscriptions') && r.method() === 'POST')
  await d.getByRole('button', { name: 'Add subscription' }).click()
  expect((await sent).postDataJSON()).toEqual({ name: 'Nika', nodeIds: [1, 2] })
  const share = dialog(page, "Share Nika's link")
  const picker = share.getByRole('group', { name: 'Node' })
  await expect(picker.getByRole('button')).toHaveText(['Node 1', 'Node 2 unreachable'])
  await expect(picker.getByRole('button', { name: 'Node 2 unreachable' })).toBeVisible()
  await expect(picker.getByRole('button', { name: 'Node 1' })).toHaveAttribute('aria-pressed', 'true')
})

test('a subscription on no node is allowed: it has no link to share yet', async ({ page }) => {
  await openSubscriptions(page)
  await add(page).click()
  const d = dialog(page, 'Add a subscription')
  await d.getByLabel('Name').fill('Nika')
  await d.getByRole('checkbox', { name: 'Node 1', exact: true }).uncheck()
  const sent = page.waitForRequest((r) => r.url().endsWith('/api/subscriptions') && r.method() === 'POST')
  await d.getByRole('button', { name: 'Add subscription' }).click()
  expect((await sent).postDataJSON()).toEqual({ name: 'Nika', nodeIds: [] })
  await expect(toast(page, 'Nika added')).toBeVisible()
  // nothing to share, so no dialog, no Share button, and the menu says why
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const r = row(page, 'Nika')
  await expect(r).toContainText('Off')
  await expect(r).toContainText('off on every node')
  await expect(r.getByRole('button', { name: 'Share', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Actions for Nika' }).click()
  const item = page.getByRole('menuitem', { name: /Share link/ })
  await expect(item).toHaveAttribute('aria-disabled', 'true')
  await expect(item).toContainText('Switch it on for a node first')
})

test('a name is needed, and one already used is refused under the field, keeping what was typed', async ({ page }) => {
  await openSubscriptions(page)
  await add(page).click()
  const d = dialog(page, 'Add a subscription')
  let posts = 0
  page.on('request', (r) => r.method() === 'POST' && r.url().endsWith('/api/subscriptions') && (posts += 1))
  await d.getByRole('button', { name: 'Add subscription' }).click()
  await expect(d).toContainText('Give the subscription a name.')
  await expect(d.getByLabel('Name')).toHaveAttribute('aria-invalid', 'true')
  expect(posts).toBe(0)

  // case doesn't make a name new
  await d.getByLabel('Name').fill('sara')
  await d.getByRole('button', { name: 'Add subscription' }).click()
  await expect(d).toContainText('That name is already used.')
  await expect(d.getByLabel('Name')).toHaveValue('sara')
  await expect(d.getByLabel('Name')).toHaveAttribute('aria-invalid', 'true')
  expect(posts).toBe(1)
  await expect(dialog(page, "Share sara's link")).toHaveCount(0)

  // another name goes through, and the message is gone with the dialog
  await d.getByLabel('Name').fill('Sam')
  await d.getByRole('button', { name: 'Add subscription' }).click()
  await expect(toast(page, 'Sam added')).toBeVisible()
  await expect(dialog(page, 'Add a subscription')).toHaveCount(0)
})

test('Enter in the name field adds it', async ({ page }) => {
  await openSubscriptions(page)
  await add(page).click()
  await dialog(page, 'Add a subscription').getByLabel('Name').fill('Nika')
  await page.keyboard.press('Enter')
  await expect(toast(page, 'Nika added')).toBeVisible()
})

test.describe('at the limit', () => {
  test('the count, a badge and a tooltip on the disabled button say why', async ({ page, request, isMobile }) => {
    await fillToLimit(request)
    await openSubscriptions(page)
    await expect(page.getByText('10 of 10 subscriptions')).toBeVisible()
    await expect(page.getByText('Limit reached')).toBeVisible()
    await expect(tile(page, 'Subscriptions')).toContainText('0 places left')
    // aria-disabled, not disabled: it can take focus, so keyboards and touch can reach the reason
    await expect(add(page)).toHaveAttribute('aria-disabled', 'true')
    await expect(add(page)).toHaveAttribute('aria-describedby', 'sub-cap-tip')
    await expect(page.getByRole('tooltip')).toBeHidden()

    if (!isMobile) {
      await add(page).hover()
      await expect(page.getByRole('tooltip')).toBeVisible()
      await expect(page.getByRole('tooltip')).toContainText(
        'This router holds at most 10 subscriptions. Each subscription is a client on every node it is on. Remove one to add another.',
      )
      await page.mouse.move(2, 2)
      await expect(page.getByRole('tooltip')).toBeHidden()
    }

    // by keyboard: Tab from the last tab lands on the button
    await page.getByRole('tab', { name: 'Settings' }).focus()
    await page.keyboard.press('Tab')
    await expect(add(page)).toBeFocused()
    await expect(page.getByRole('tooltip')).toBeVisible()
    await expect(add(page)).toHaveAccessibleDescription(/This router holds at most 10 subscriptions/)

    // pressing it opens nothing, and keeps the reason in view
    await add(page).click({ force: true })
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(page.getByRole('tooltip')).toBeVisible()
    // the tooltip stays on the screen, whatever the width
    const box = (await page.getByRole('tooltip').boundingBox())!
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width)
  })

  test('with room left the button is live and the badge is gone', async ({ page, request }) => {
    await fillToLimit(request, 9)
    await openSubscriptions(page)
    await expect(page.getByText('9 of 10 subscriptions')).toBeVisible()
    await expect(page.getByText('Limit reached')).toHaveCount(0)
    await expect(add(page)).not.toHaveAttribute('aria-disabled', 'true')
    await add(page).click()
    // one place left, in the singular
    await expect(dialog(page, 'Add a subscription')).toContainText("1 place left. Its link is ready as soon as it's added.")
  })

  test('a subscription added elsewhere while the dialog is open meets the limit, and the dialog says so', async ({ page, request }) => {
    await fillToLimit(request, 9)
    await openSubscriptions(page)
    await add(page).click()
    const d = dialog(page, 'Add a subscription')
    await d.getByLabel('Name').fill('Nika')
    // somebody else takes the last place
    expect((await addViaApi(request, 'Taken')).ok()).toBe(true)
    await d.getByRole('button', { name: 'Add subscription' }).click()
    await expect(d.getByRole('alert')).toHaveText('This router has reached its limit of subscriptions. Remove one to add another.')
    // the name is kept, and nothing was added
    await expect(d.getByLabel('Name')).toHaveValue('Nika')
    await expect(toast(page, 'Nika added')).toHaveCount(0)
  })

  test('the hub refuses the 11th with its own code', async ({ request }) => {
    await fillToLimit(request)
    const res = await addViaApi(request, 'Eleven')
    expect(res.status()).toBe(409)
    expect((await res.json()).code).toBe('subscription_limit')
    const again = await request.post('/api/subscriptions', { headers: PAGE, data: { name: 'Sara', nodeIds: [] } })
    // at the limit the limit is what is reported, not the name
    expect((await again.json()).code).toBe('subscription_limit')
  })
})

test('edits the name of a subscription: the list and the link carry the new name', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Sara', 'Edit')
  const d = dialog(page, 'Edit Sara')
  await expect(d.getByLabel('Name')).toHaveValue('Sara')
  await d.getByLabel('Name').fill('Sarah')
  const sent = page.waitForRequest((r) => r.method() === 'PATCH' && r.url().endsWith('/api/subscriptions/1'))
  await d.getByRole('button', { name: 'Save' }).click()
  expect((await sent).postDataJSON()).toEqual({ name: 'Sarah' })
  await expect(toast(page, 'Renamed to Sarah')).toBeVisible()
  await expect(row(page, 'Sarah')).toBeVisible()
  await expect(row(page, 'Sara')).toHaveCount(0)
  // sorted again, and its switches are the same ones
  await expect(flipper(page, 'Sarah', 'Node 1')).toHaveAttribute('aria-checked', 'true')

  await row(page, 'Sarah').getByRole('button', { name: 'Share', exact: true }).click()
  await expect(page.locator('[data-link]')).toHaveText(/^vless:\/\/3f0c9a52-7d1e-4b8a-9c44-0e5b2a1d6f73@.*#Sarah$/)
})

test('a new name keeps to the rules: a name is needed, one in use is refused, an unchanged one asks nothing, Enter saves', async ({ page }) => {
  await openSubscriptions(page)
  let patches = 0
  page.on('request', (r) => r.method() === 'PATCH' && (patches += 1))

  await pick(page, 'Sara', 'Edit')
  const d = dialog(page, 'Edit Sara')
  await d.getByLabel('Name').fill('  ')
  await d.getByRole('button', { name: 'Save' }).click()
  await expect(d).toContainText('Give the subscription a name.')
  await d.getByLabel('Name').fill('dana')
  await d.getByRole('button', { name: 'Save' }).click()
  await expect(d).toContainText('That name is already used.')
  expect(patches).toBe(1)

  // back to what it was: nothing to send
  await d.getByLabel('Name').fill('Sara')
  await d.getByRole('button', { name: 'Save' }).click()
  await expect(d).toHaveCount(0)
  expect(patches).toBe(1)

  // a change of case alone is a rename, and Enter saves it
  await pick(page, 'Sara', 'Edit')
  await dialog(page, 'Edit Sara').getByLabel('Name').fill('SARA')
  await page.keyboard.press('Enter')
  await expect(toast(page, 'Renamed to SARA')).toBeVisible()
  expect(patches).toBe(2)
})

test('holds a name to 64 characters, in the add dialog and in an edit, and shows the hub\'s sentence for a longer one', async ({ page }) => {
  await openSubscriptions(page)
  // adding
  await page.getByRole('button', { name: 'Add subscription' }).first().click()
  const add = dialog(page, 'Add a subscription')
  await expect(add.getByLabel('Name')).toHaveAttribute('maxlength', '64')
  await add.getByLabel('Name').pressSequentially('x'.repeat(70))
  expect(await add.getByLabel('Name').inputValue()).toHaveLength(64)
  // one that got past the field is refused by the hub in its own words
  await liftLimit(add.getByLabel('Name'))
  await add.getByLabel('Name').fill('y'.repeat(65))
  await add.getByRole('button', { name: 'Add subscription' }).click()
  await expect(add.getByRole('alert')).toContainText('The name can have at most 64 characters.')
  await add.getByRole('button', { name: 'Cancel' }).click()

  // editing
  await pick(page, 'Sara', 'Edit')
  const rename = dialog(page, 'Edit Sara')
  await expect(rename.getByLabel('Name')).toHaveAttribute('maxlength', '64')
  await liftLimit(rename.getByLabel('Name'))
  await rename.getByLabel('Name').fill('y'.repeat(65))
  await rename.getByRole('button', { name: 'Save' }).click()
  await expect(rename.getByRole('alert')).toContainText('The name can have at most 64 characters.')
})

test('deletes a subscription after asking, and the count drops', async ({ page }) => {
  await openSubscriptions(page)
  await pick(page, 'Sara', 'Delete subscription')
  const d = page.getByRole('alertdialog', { name: 'Delete Sara?' })
  await expect(d).toContainText('The subscription is removed from every node, and its links stop working.')
  // the button that does it is the danger one
  await expect(d.getByRole('button', { name: 'Delete subscription' })).toHaveClass(/bg-bad/)

  // Cancel keeps it
  await d.getByRole('button', { name: 'Cancel' }).click()
  await expect(d).toHaveCount(0)
  await expect(row(page, 'Sara')).toBeVisible()

  await pick(page, 'Sara', 'Delete subscription')
  const sent = page.waitForRequest((r) => r.method() === 'DELETE' && r.url().endsWith('/api/subscriptions/1'))
  await page.getByRole('alertdialog', { name: 'Delete Sara?' }).getByRole('button', { name: 'Delete subscription' }).click()
  await sent
  await expect(toast(page, 'Sara deleted')).toBeVisible()
  await expect(row(page, 'Sara')).toHaveCount(0)
  await expect(page.getByText('6 of 10 subscriptions')).toBeVisible()
  await expect(tile(page, 'Subscriptions')).toContainText('4 places left')
  await expect(add(page)).not.toHaveAttribute('aria-disabled', 'true')
})

test('deleting what is already gone says so inside the dialog', async ({ page, request }) => {
  await openSubscriptions(page)
  await pick(page, 'Sara', 'Delete subscription')
  expect((await request.delete('/api/subscriptions/1', { headers: PAGE })).ok()).toBe(true)
  const d = page.getByRole('alertdialog', { name: 'Delete Sara?' })
  await d.getByRole('button', { name: 'Delete subscription' }).click()
  await expect(d.getByRole('alert')).toHaveText('It no longer exists. Reload the page.')
  await d.getByRole('button', { name: 'Cancel' }).click()
  await expect(d).toHaveCount(0)
})

test('a switch turns a subscription off for a node, and Undo turns it back on with the same link', async ({ page }) => {
  await openSubscriptions(page)
  const sw = flipper(page, 'Sara', 'Node 1')
  await expect(sw).toHaveAttribute('aria-checked', 'true')
  const sent = page.waitForRequest((r) => r.method() === 'PUT' && r.url().endsWith('/api/subscriptions/1/nodes/1'))
  await sw.click()
  expect((await sent).postDataJSON()).toEqual({ enabled: false })
  const note = toast(page, 'Sara switched off on Node 1')
  await expect(note).toBeVisible()
  await expect(sw).toHaveAttribute('aria-checked', 'false')
  // Sara is still on Node 2, so she stays online-able; Node 1 shows nothing for her
  await expect(flipper(page, 'Sara', 'Node 2')).toHaveAttribute('aria-checked', 'true')

  await note.getByRole('button', { name: 'Undo' }).click()
  await expect(toast(page, 'Sara switched on for Node 1')).toBeVisible()
  await expect(sw).toHaveAttribute('aria-checked', 'true')

  // the link is the one she had
  await row(page, 'Sara').getByRole('button', { name: 'Share', exact: true }).click()
  await expect(page.locator('[data-link]')).toHaveText(
    'vless://3f0c9a52-7d1e-4b8a-9c44-0e5b2a1d6f73@203.0.113.24:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=x7QnLw4Rk2VtB9yD0sEoUj3HaFcN6mXz8PqIg1KvTbA&sid=a1b2c3d4&type=tcp#Sara',
  )
})

test('switching one on has no Undo, and the switch waits for the answer', async ({ page }) => {
  await openSubscriptions(page)
  const sw = flipper(page, 'Kian', 'Node 1')
  await expect(sw).toHaveAttribute('aria-checked', 'false')
  await sw.click()
  const note = toast(page, 'Kian switched on for Node 1')
  await expect(note).toBeVisible()
  await expect(note.getByRole('button')).toHaveCount(0)
  await expect(sw).toHaveAttribute('aria-checked', 'true')
  await expect(sw).toBeEnabled()
})

test('a switch for the node that is down is accepted, and the toast says when it takes effect', async ({ page }) => {
  await openSubscriptions(page)
  await flipper(page, 'Ali', 'Node 2').click()
  await expect(toast(page, 'Ali switched on for Node 2. It takes effect when the node is back.')).toBeVisible()
  await expect(flipper(page, 'Ali', 'Node 2')).toHaveAttribute('aria-checked', 'true')
  await flipper(page, 'Ali', 'Node 2').click()
  const off = toast(page, 'Ali switched off on Node 2. It takes effect when the node is back.')
  await expect(off).toBeVisible()
  await expect(off.getByRole('button', { name: 'Undo' })).toBeVisible()
})

test('a switch that fails says why in a toast, and the switch stays where it was', async ({ page, request }) => {
  await openSubscriptions(page)
  // the subscription goes away behind the page's back
  expect((await request.delete('/api/subscriptions/1', { headers: PAGE })).ok()).toBe(true)
  await flipper(page, 'Sara', 'Node 1').click()
  const note = toast(page, 'It no longer exists. Reload the page.')
  await expect(note).toBeVisible()
  await expect(note).toHaveAttribute('role', 'alert')
  // an error stays until it is dismissed
  await page.waitForTimeout(500)
  await expect(note).toBeVisible()
  await expect(flipper(page, 'Sara', 'Node 1')).toHaveAttribute('aria-checked', 'true')
  await note.getByRole('button', { name: 'Dismiss' }).click()
  await expect(note).toHaveCount(0)
})

test('nobody can be told apart from unknown: when no node answers, the status is Unknown and the tile has no number', async ({ page }) => {
  // every node down: the hub reports nothing about who is online
  await page.route('**/api/subscriptions', async (route) => {
    if (route.request().method() !== 'GET') return route.continue()
    const list = await (await route.fetch()).json()
    const down = list.map((s: any) => ({ ...s, online: null, nodes: s.nodes.map((n: any) => ({ ...n, online: null, connections: null })) }))
    await route.fulfill({ json: down })
  })
  await openSubscriptions(page)
  await expect(tile(page, 'Online now')).toContainText('–')
  await expect(tile(page, 'Online now')).toContainText('unknown while unreachable')
  await expect(row(page, 'Sara')).toContainText('Unknown')
  await expect(row(page, 'Sara')).toContainText('node unreachable')
  await expect(row(page, 'Sara')).not.toContainText('Online')
})

test.describe('before there is anything', () => {
  test('with no nodes yet it says to add one first, and the dialog says the subscription can wait for a node', async ({ page, request }) => {
    await reset(request, { fresh: true })
    await page.goto('/#/subscriptions')
    await expect(page.getByRole('heading', { name: 'No subscriptions yet' })).toBeVisible()
    await expect(page.getByText('A subscription is a name with one link for each node it is on. Add one, then share its link.')).toBeVisible()
    await expect(page.getByText('Add a node first: subscriptions connect through nodes.')).toBeVisible()
    await expect(tile(page, 'Subscriptions')).toContainText('0 of 10')
    await expect(tile(page, 'Online now')).toContainText('no connections now')
    await page.getByRole('button', { name: 'Add your first subscription' }).click()
    const d = dialog(page, 'Add a subscription')
    await expect(d).toContainText('There are no nodes yet. You can add the subscription now and switch it on for a node later.')
    await expect(d.getByRole('checkbox')).toHaveCount(0)
    await d.getByLabel('Name').fill('Nika')
    await d.getByRole('button', { name: 'Add subscription' }).click()
    await expect(toast(page, 'Nika added')).toBeVisible()
    await expect(row(page, 'Nika')).toContainText('off on every node')
  })

  test('with nodes but no subscriptions, the empty state offers the first one', async ({ page, request }) => {
    for (let id = 1; id <= 7; id += 1) expect((await request.delete(`/api/subscriptions/${id}`, { headers: PAGE })).ok()).toBe(true)
    await page.goto('/#/subscriptions')
    await expect(page.getByRole('heading', { name: 'No subscriptions yet' })).toBeVisible()
    await expect(page.getByText('Add a node first')).toHaveCount(0)
    await expect(page.getByText('0 of 10 subscriptions')).toBeVisible()
    await page.getByRole('button', { name: 'Add your first subscription' }).click()
    await dialog(page, 'Add a subscription').getByLabel('Name').fill('Nika')
    await page.keyboard.press('Enter')
    await expect(toast(page, 'Nika added')).toBeVisible()
    await expect(dialog(page, "Share Nika's link")).toBeVisible()
  })
})

test('a node that finishes installing gets its switches', async ({ page, request }) => {
  // two nodes of three: room for one more, which installs while the page is open
  await reset(request, { nodes: 2, pace: 1 })
  const started = await request.post('/api/nodes', {
    headers: PAGE,
    data: { host: '192.0.2.60', name: 'Node 3', sshPort: 22, sshUser: 'root', password: 'a-password', hostKey: 'SHA256:p2OiwWbYoEAsmmYWEMy1L12ya7lUDzuHYODmtJ18R/w', entryPort: 443 },
  })
  expect(started.status()).toBe(202)
  await openSubscriptions(page)
  await expect(page.getByText("Node 3 is installing. It shows up here when it's ready.")).toBeVisible()
  await expect(page.getByRole('switch', { name: /on Node 3/ })).toHaveCount(0)
  // the note is shown while Node 3 can't take a subscription: the add dialog lists it, disabled
  await add(page).click()
  await expect(dialog(page, 'Add a subscription').getByRole('checkbox', { name: 'Node 3 installing' })).toBeDisabled()
  await page.keyboard.press('Escape')

  await expect
    .poll(async () => ((await (await request.get('/api/nodes', { headers: PAGE })).json()) as { name: string; installing?: unknown }[]).find((n) => n.name === 'Node 3')?.installing, {
      timeout: 20000,
    })
    .toBeUndefined()
  await page.reload()
  await expect(flipper(page, 'Sara', 'Node 3')).toBeVisible()
  await expect(flipper(page, 'Sara', 'Node 3')).toHaveAttribute('aria-checked', 'false')
  await expect(page.getByText(/is installing/)).toHaveCount(0)
  if (isTable(page)) await expect(page.getByRole('columnheader', { name: 'Node 3' })).toBeVisible()
  await flipper(page, 'Sara', 'Node 3').click()
  await expect(toast(page, 'Sara switched on for Node 3')).toBeVisible()
})
