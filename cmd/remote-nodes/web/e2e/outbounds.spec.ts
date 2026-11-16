import { dialog, expect, leaks, reset, stateHolds, test, toast } from './fixtures'
import { ID, LINK, PBK, byName, isTable, listed, obRow, openAdd, openOutbounds, setOutbound } from './outbounds'

const FALLBACK = "Use the router's VPN while this is down"

test.describe('the list', () => {
  test('shows each outbound: its kind, server, state, traffic, who uses it, and its actions', async ({ page }) => {
    await openOutbounds(page)
    await expect(page.getByRole('tab', { name: 'Outbounds' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByText('5 of 16 outbounds')).toBeVisible()
    await expect(page.getByRole('table')).toHaveCount(isTable(page) ? 1 : 0)

    const frankfurt = obRow(page, 'Frankfurt')
    await expect(frankfurt).toContainText('VLESS · TCP · REALITY')
    await expect(frankfurt).toContainText('de.example.com:443')
    await expect(frankfurt.locator('[data-state="up"]')).toContainText(/^Up · \d+ ms$/)
    await expect(frankfurt).toContainText('7.6 GB')
    await expect(frankfurt).toContainText('Node 1')
    for (const name of ['Test Frankfurt', 'Edit Frankfurt', 'Remove Frankfurt']) await expect(frankfurt.getByRole('button', { name, exact: true })).toBeVisible()

    // down, with its fallback on: its traffic is on the router's VPN for now
    const amsterdam = obRow(page, 'Amsterdam')
    await expect(amsterdam).toContainText('Trojan · WebSocket · TLS')
    await expect(amsterdam.locator('[data-state="fallback"]')).toContainText("Down · on the router's VPN")
    await expect(amsterdam).toContainText('dial tcp 198.51.100.80:443: i/o timeout')
    await expect(amsterdam).toContainText('Node 2')
    if (!isTable(page)) await expect(amsterdam).toContainText("It doesn't answer, so its traffic leaves through the router's VPN for now.")

    // down with no fallback, with the core's error
    const paris = obRow(page, 'Paris')
    await expect(paris.locator('[data-state="down"]')).toContainText(/^Down/)
    await expect(paris).toContainText('connect: connection refused')
    await expect(paris).toContainText('This router')
    await expect(paris).toContainText('fr.example.org:8443')

    // nothing leaves through these
    for (const [name, kind] of [
      ['Helsinki', 'Shadowsocks'],
      ['Office', 'SOCKS'],
    ]) {
      const row = obRow(page, name)
      await expect(row.locator('[data-state="idle"]')).toHaveText('Not in use')
      await expect(row).toContainText(kind)
      await expect(row).toContainText('None')
      await expect(row).toContainText('–')
    }
    await expect(page.getByText("Traffic counts from the last start of the router's Xray.")).toBeVisible()
  })

  test('says an outbound in use is not checked yet while the bridge is not running', async ({ page, request }) => {
    await reset(request, { entry: 'off' })
    await openOutbounds(page)
    for (const name of ['Frankfurt', 'Amsterdam', 'Paris']) await expect(obRow(page, name).locator('[data-state="unknown"]')).toHaveText('Not checked yet')
    await expect(obRow(page, 'Helsinki').locator('[data-state="idle"]')).toBeVisible()
  })

  test('sorts by name', async ({ page }) => {
    await openOutbounds(page)
    const names = isTable(page) ? page.getByRole('rowheader') : page.getByRole('list', { name: 'Outbounds' }).getByRole('listitem')
    await expect(names).toHaveText([/^Amsterdam/, /^Frankfurt/, /^Helsinki/, /^Office/, /^Paris/])
  })

  test('Test on a row shows the answer and its delay, or why it did not answer', async ({ page }) => {
    await openOutbounds(page)
    await obRow(page, 'Frankfurt').getByRole('button', { name: 'Test Frankfurt' }).click()
    await expect(obRow(page, 'Frankfurt').getByRole('status')).toHaveText(/^Answered in \d+ ms$/)
    await obRow(page, 'Paris').getByRole('button', { name: 'Test Paris' }).click()
    await expect(obRow(page, 'Paris').getByRole('status')).toContainText("Didn't answer")
    await expect(obRow(page, 'Paris').getByRole('status')).toContainText('connection refused')
    // an edit takes the answer away: it was for what the outbound had before
    await obRow(page, 'Paris').getByRole('button', { name: 'Edit Paris' }).click()
    await dialog(page, 'Edit Paris').getByRole('switch', { name: FALLBACK }).click()
    await dialog(page, 'Edit Paris').getByRole('button', { name: 'Save' }).click()
    await expect(toast(page, 'Paris saved')).toBeVisible()
    await expect(obRow(page, 'Paris').getByRole('status')).toHaveText('')
    await expect(obRow(page, 'Frankfurt').getByRole('status')).toHaveText(/^Answered in \d+ ms$/)
  })

  test('a test the router cannot run says so in the page\'s words', async ({ page }) => {
    await openOutbounds(page)
    await page.route('**/api/outbounds/1/test', (r) => r.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ code: 'test_unavailable', error: 'no bridge program' }) }))
    await obRow(page, 'Frankfurt').getByRole('button', { name: 'Test Frankfurt' }).click()
    await expect(obRow(page, 'Frankfurt').getByRole('status')).toHaveText("Testing isn't available here: the router's Xray program can't be run.")
  })

  test('an empty list says what an outbound is for, and offers to add one', async ({ page, request }) => {
    await reset(request, { outbounds: false })
    await openOutbounds(page, 'No outbounds yet')
    await expect(page.getByText("An outbound is another server that traffic can leave through, instead of the router's VPN.", { exact: false })).toBeVisible()
    await page.getByRole('button', { name: 'Add your first outbound' }).click()
    await expect(dialog(page, 'Add an outbound')).toBeVisible()
  })

  test('at the limit the Add button says why instead of opening the dialog', async ({ page, request }) => {
    await reset(request, { outboundLimit: 5 })
    await openOutbounds(page)
    await expect(page.getByText('5 of 5 outbounds')).toBeVisible()
    await expect(page.getByText('Limit reached')).toBeVisible()
    const add = page.getByRole('button', { name: 'Add outbound' })
    // aria-disabled, not disabled: it takes focus, so keyboards and touch reach the reason too
    await expect(add).toHaveAttribute('aria-disabled', 'true')
    await add.click({ force: true })
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await add.focus()
    await expect(page.getByRole('tooltip')).toContainText('This router keeps at most 5 outbounds.')
  })
})

test.describe('adding', () => {
  test('by link: shows what was read, takes the name from the remark, and saves with the fallback off', async ({ page, request }) => {
    await openOutbounds(page)
    const d = await openAdd(page, LINK)
    const read = d.locator('[data-read]')
    await expect(read).toContainText('VLESS · WebSocket · TLS')
    await expect(read).toContainText('nl2.example.com:443')
    await expect(read).toContainText('WebSocket · /ws?ed=2048')
    await expect(read).toContainText('TLS · nl2.example.com')
    // the secret is never shown back
    await expect(read).not.toContainText(ID)
    await expect(d.getByLabel('Name', { exact: true })).toHaveValue('🇳🇱 Rotterdam')
    await expect(d.getByRole('switch', { name: FALLBACK })).toHaveAttribute('aria-checked', 'false')

    const sent = page.waitForRequest((r) => r.url().endsWith('/api/outbounds') && r.method() === 'POST')
    await d.getByRole('button', { name: 'Add outbound' }).click()
    const body = (await sent).postDataJSON()
    expect(body).toEqual({
      name: '🇳🇱 Rotterdam',
      fallback: false,
      settings: {
        protocol: 'vless',
        host: 'nl2.example.com',
        port: 443,
        id: ID,
        password: '',
        username: '',
        method: '',
        flow: '',
        network: 'ws',
        path: '/ws?ed=2048',
        hostHeader: '',
        mode: '',
        security: 'tls',
        serverName: 'nl2.example.com',
        fingerprint: 'chrome',
        alpn: [],
        publicKey: '',
        shortId: '',
        spiderX: '',
      },
    })
    await expect(toast(page, '🇳🇱 Rotterdam added')).toBeVisible()
    await expect(d).toBeHidden()
    await expect(obRow(page, '🇳🇱 Rotterdam').locator('[data-state="idle"]')).toBeVisible()
    await expect(page.getByText('6 of 16 outbounds')).toBeVisible()
  })

  test('a name typed in stays when the paste changes, and the fallback switch is sent when on', async ({ page, request }) => {
    await openOutbounds(page)
    const d = await openAdd(page, LINK)
    await d.getByLabel('Name', { exact: true }).fill('Mine')
    await d.getByLabel('Link or JSON').fill(`trojan://pw@tr.example.com:443?sni=tr.example.com#Other`)
    await expect(d.getByLabel('Name', { exact: true })).toHaveValue('Mine')
    await d.getByRole('switch', { name: FALLBACK }).click()
    await d.getByRole('button', { name: 'Add outbound' }).click()
    await expect(toast(page, 'Mine added')).toBeVisible()
    expect(await byName(request, 'Mine')).toMatchObject({ protocol: 'trojan', fallback: true, secretHint: '' })
  })

  test('by JSON: one outbound from an Xray config', async ({ page, request }) => {
    await openOutbounds(page)
    const json = JSON.stringify({
      outbounds: [
        { protocol: 'freedom', tag: 'direct' },
        { tag: 'Zurich', protocol: 'vless', settings: { vnext: [{ address: 'ch.example.com', port: 443, users: [{ id: ID, flow: 'xtls-rprx-vision', encryption: 'none' }] }] }, streamSettings: { network: 'tcp', security: 'reality', realitySettings: { serverName: 'www.example.com', publicKey: PBK, shortId: 'ab12' } } },
      ],
    }, null, 2)
    const d = await openAdd(page, json)
    await expect(d.locator('[data-read]')).toContainText('VLESS · TCP · REALITY')
    await expect(d.locator('[data-read]')).toContainText('REALITY · www.example.com')
    await expect(d.getByLabel('Name', { exact: true })).toHaveValue('Zurich')
    await d.getByRole('button', { name: 'Add outbound' }).click()
    await expect(toast(page, 'Zurich added')).toBeVisible()
    expect(await byName(request, 'Zurich')).toMatchObject({ protocol: 'vless', flow: 'xtls-rprx-vision', security: 'reality', publicKey: PBK, shortId: 'ab12', secretHint: 'b831…0811' })
  })

  test('a link that is not carried is refused in one sentence, and nothing can be saved', async ({ page }) => {
    await openOutbounds(page)
    const d = await openAdd(page, `vless://${ID}@a.example.com:443?type=grpc&serviceName=x&security=tls#g`)
    await expect(d.getByText("The gRPC transport isn't carried here. Use TCP, WebSocket, HTTPUpgrade or XHTTP.")).toBeVisible()
    await expect(d.locator('[data-read]')).toHaveCount(0)
    await expect(d.getByRole('button', { name: 'Add outbound' })).toBeDisabled()
    await d.getByLabel('Link or JSON').fill('wireguard://key@a.example.com:51820?publickey=x')
    await expect(d.getByText("WireGuard isn't carried here. Use VLESS, VMess, Trojan, Shadowsocks, SOCKS or HTTP.")).toBeVisible()
    await d.getByLabel('Link or JSON').fill(`vless://${ID}@a.example.com:443?type=ws&security=reality&sni=w.example.com&pbk=${PBK}`)
    await expect(d.getByText("REALITY over WebSocket isn't carried here. It runs over TCP or XHTTP.")).toBeVisible()
  })

  test('says when certificate checks were off in the link, and reads it anyway', async ({ page }) => {
    await openOutbounds(page)
    const d = await openAdd(page, `${LINK.replace('#', '&allowInsecure=1#')}`)
    await expect(d.locator('[data-read]')).toContainText('It turns certificate checks off. They stay on here.')
    await expect(d.getByRole('button', { name: 'Add outbound' })).toBeEnabled()
  })

  test('an http:// address is a proxy only once the admin says so', async ({ page, request }) => {
    await openOutbounds(page)
    const d = await openAdd(page, 'http://user:secret@px.example.com:3128#Proxy')
    await expect(d.getByText('That is a web address. If it is an HTTP proxy, say so below.')).toBeVisible()
    await d.getByLabel('This is an HTTP proxy').check()
    await expect(d.locator('[data-read]')).toContainText('HTTP')
    await expect(d.locator('[data-read]')).toContainText('px.example.com:3128')
    await d.getByRole('button', { name: 'Add outbound' }).click()
    await expect(toast(page, 'Proxy added')).toBeVisible()
    expect(await byName(request, 'Proxy')).toMatchObject({ protocol: 'http', username: 'user', security: 'none' })
  })

  test('Test tries the unsaved settings: an answer, no answer, or none possible here', async ({ page }) => {
    await openOutbounds(page)
    const d = await openAdd(page, LINK)
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/outbounds/test'))
    await d.getByRole('button', { name: 'Test', exact: true }).click()
    expect((await sent).postDataJSON().settings).toMatchObject({ host: 'nl2.example.com', id: ID })
    await expect(d.getByRole('status')).toHaveText(/^Answered in \d+ ms$/)

    // the answer is for those settings only
    await d.getByLabel('Link or JSON').fill(`trojan://pw@fail.example.com:443#F`)
    await expect(d.getByRole('status')).toHaveText('')
    await d.getByRole('button', { name: 'Test', exact: true }).click()
    await expect(d.getByRole('status')).toContainText("Didn't answer")
    await expect(d.getByRole('status')).toContainText('dial tcp fail.example.com:443: i/o timeout')

    await d.getByLabel('Link or JSON').fill(`trojan://pw@notest.example.com:443#N`)
    await d.getByRole('button', { name: 'Test', exact: true }).click()
    await expect(d.getByRole('status')).toHaveText("Testing isn't available here: the router's Xray program can't be run.")
  })

  test('a name that is taken is said under the name; settings Xray refuses are said in a box', async ({ page }) => {
    await openOutbounds(page)
    const d = await openAdd(page, LINK)
    await d.getByLabel('Name', { exact: true }).fill('frankfurt')
    await d.getByRole('button', { name: 'Add outbound' }).click()
    await expect(d.getByText('That name is already used.')).toBeVisible()
    await d.getByLabel('Name', { exact: true }).fill('New one')
    await d.getByLabel('Link or JSON').fill(`trojan://pw@refuse.example.com:443`)
    await d.getByRole('button', { name: 'Add outbound' }).click()
    await expect(d.getByRole('alert')).toContainText('Xray refused these settings')
    await expect(d).toBeVisible()
  })

  test('keeps the pasted link nowhere in the browser once the dialog is closed or saved', async ({ page }) => {
    await openOutbounds(page)
    let d = await openAdd(page, LINK)
    await page.keyboard.press('Escape')
    await expect(d).toBeHidden()
    expect(await leaks(page, ID)).toEqual([])
    expect(await stateHolds(page, ID)).toBe(false)

    d = await openAdd(page, LINK)
    await d.getByRole('button', { name: 'Add outbound' }).click()
    await expect(d).toBeHidden()
    expect(await leaks(page, ID)).toEqual([])
    expect(await stateHolds(page, ID)).toBe(false)
  })
})

test.describe('editing', () => {
  test('renames without a word about restarting: the name is not in the bridge', async ({ page, request }) => {
    await openOutbounds(page)
    await obRow(page, 'Frankfurt').getByRole('button', { name: 'Edit Frankfurt' }).click()
    const d = dialog(page, 'Edit Frankfurt')
    // what it has now, the id only as a hint
    await expect(d.locator('[data-read]')).toContainText('VLESS · TCP · REALITY')
    await expect(d.locator('[data-read]')).toContainText('b831…0811')
    await expect(d.getByRole('button', { name: 'Save' })).toBeDisabled()
    await d.getByLabel('Name', { exact: true }).fill('Frankfurt 2')
    await expect(d.getByText(/restarts the router's Xray/)).toHaveCount(0)
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/outbounds/1') && r.method() === 'PATCH')
    await d.getByRole('button', { name: 'Save' }).click()
    expect((await sent).postDataJSON()).toEqual({ name: 'Frankfurt 2' })
    await expect(toast(page, 'Frankfurt 2 saved')).toBeVisible()
    await expect(obRow(page, 'Frankfurt 2')).toBeVisible()
    expect((await byName(request, 'Frankfurt 2'))?.id).toBe(1)
  })

  test('the fallback switch on one in use says the bridge restarts and connections drop', async ({ page, request }) => {
    await openOutbounds(page)
    await obRow(page, 'Paris').getByRole('button', { name: 'Edit Paris' }).click()
    const d = dialog(page, 'Edit Paris')
    await d.getByRole('switch', { name: FALLBACK }).click()
    await expect(d.getByText("Paris is in use, so saving this restarts the router's Xray. Open connections drop for a few seconds.")).toBeVisible()
    await d.getByRole('switch', { name: FALLBACK }).click()
    await expect(d.getByText(/restarts the router's Xray/)).toHaveCount(0)
    await d.getByRole('switch', { name: FALLBACK }).click()
    await d.getByRole('button', { name: 'Save' }).click()
    await expect(toast(page, 'Paris saved')).toBeVisible()
    expect(await byName(request, 'Paris')).toMatchObject({ fallback: true })
  })

  test('a new link replaces all the settings; on one not in use nothing restarts', async ({ page, request }) => {
    await openOutbounds(page)
    await obRow(page, 'Helsinki').getByRole('button', { name: 'Edit Helsinki' }).click()
    const d = dialog(page, 'Edit Helsinki')
    // a password shows as dots
    await expect(d.locator('[data-read]')).toContainText('••••••••')
    await d.getByLabel('New link or JSON').fill('ss://2022-blake3-aes-256-gcm:' + encodeURIComponent('AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=') + '@fi2.example.org:8389#x')
    await expect(d.locator('[data-read]')).toContainText('fi2.example.org:8389')
    await expect(d.getByLabel('Name', { exact: true })).toHaveValue('Helsinki')
    await expect(d.getByText(/restarts the router's Xray/)).toHaveCount(0)
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/outbounds/4') && r.method() === 'PATCH')
    await d.getByRole('button', { name: 'Save' }).click()
    const body = (await sent).postDataJSON()
    expect(Object.keys(body)).toEqual(['settings'])
    expect(body.settings).toMatchObject({ protocol: 'shadowsocks', host: 'fi2.example.org', port: 8389, method: '2022-blake3-aes-256-gcm', password: 'AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=' })
    await expect(toast(page, 'Helsinki saved')).toBeVisible()
    expect(await byName(request, 'Helsinki')).toMatchObject({ host: 'fi2.example.org', method: '2022-blake3-aes-256-gcm' })
  })

  test('a new link on one in use says so before saving; a refused one cannot be saved', async ({ page }) => {
    await openOutbounds(page)
    await obRow(page, 'Frankfurt').getByRole('button', { name: 'Edit Frankfurt' }).click()
    const d = dialog(page, 'Edit Frankfurt')
    await d.getByLabel('New link or JSON').fill(LINK)
    await expect(d.getByText("Frankfurt is in use, so saving this restarts the router's Xray. Open connections drop for a few seconds.")).toBeVisible()
    await d.getByLabel('New link or JSON').fill('hy2://auth@a.example.com:443')
    await expect(d.getByText("Hysteria isn't carried here. Use VLESS, VMess, Trojan, Shadowsocks, SOCKS or HTTP.")).toBeVisible()
    await expect(d.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  test('Test in the dialog tries the saved settings, or the new ones once pasted', async ({ page }) => {
    await openOutbounds(page)
    await obRow(page, 'Paris').getByRole('button', { name: 'Edit Paris' }).click()
    const d = dialog(page, 'Edit Paris')
    const saved = page.waitForRequest((r) => r.url().endsWith('/api/outbounds/3/test'))
    await d.getByRole('button', { name: 'Test', exact: true }).click()
    await saved
    await expect(d.getByRole('status')).toContainText("Didn't answer")
    await d.getByLabel('New link or JSON').fill(LINK)
    const fresh = page.waitForRequest((r) => r.url().endsWith('/api/outbounds/test'))
    await d.getByRole('button', { name: 'Test', exact: true }).click()
    await fresh
    await expect(d.getByRole('status')).toHaveText(/^Answered in \d+ ms$/)
  })
})

test.describe('removing', () => {
  test('asks first, then removes one nothing uses', async ({ page, request }) => {
    await openOutbounds(page)
    await obRow(page, 'Office').getByRole('button', { name: 'Remove Office' }).click()
    const ask = page.getByRole('alertdialog', { name: 'Remove Office?' })
    await expect(ask).toContainText('Its settings are deleted. Nothing else changes.')
    await ask.getByRole('button', { name: 'Cancel' }).click()
    await expect(obRow(page, 'Office')).toBeVisible()
    await obRow(page, 'Office').getByRole('button', { name: 'Remove Office' }).click()
    await page.getByRole('alertdialog', { name: 'Remove Office?' }).getByRole('button', { name: 'Remove outbound' }).click()
    await expect(toast(page, 'Office removed')).toBeVisible()
    await expect(obRow(page, 'Office')).toHaveCount(0)
    expect(await byName(request, 'Office')).toBeUndefined()
  })

  test('one in use says who uses it instead of offering to remove it', async ({ page }) => {
    await openOutbounds(page)
    await obRow(page, 'Paris').getByRole('button', { name: 'Remove Paris' }).click()
    const said = page.getByRole('alertdialog', { name: 'Paris is in use' })
    await expect(said).toContainText('This router leaves through it. Choose another way out for it under Leaves through first.')
    await expect(said.getByRole('button', { name: 'Remove outbound' })).toHaveCount(0)
    await said.getByRole('button', { name: 'Close' }).last().click()
    await expect(said).toBeHidden()
  })

  test("when the hub says it is in use after all, the dialog says so in the hub's words", async ({ page }) => {
    await openOutbounds(page)
    await page.route('**/api/outbounds/4', (r) =>
      r.request().method() === 'DELETE' ? r.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ code: 'outbound_in_use', error: 'Node 1 leaves through Helsinki' }) }) : r.fallback(),
    )
    await obRow(page, 'Helsinki').getByRole('button', { name: 'Remove Helsinki' }).click()
    await page.getByRole('alertdialog', { name: 'Remove Helsinki?' }).getByRole('button', { name: 'Remove outbound' }).click()
    const said = page.getByRole('alertdialog', { name: 'Helsinki is in use' })
    await expect(said).toContainText('Node 1 leaves through Helsinki')
    await expect(obRow(page, 'Helsinki')).toBeVisible()
  })
})

test.describe('the health check', () => {
  test('shows the default when empty, saves another address, and goes back to the default', async ({ page, request }) => {
    await openOutbounds(page)
    const field = page.getByLabel('Address', { exact: true })
    await expect(field).toHaveValue('')
    await expect(field).toHaveAttribute('placeholder', 'https://www.google.com/generate_204')
    await expect(page.getByText('Left empty, it is https://www.google.com/generate_204.')).toBeVisible()
    await expect(page.getByText('Checked every 30 seconds.')).toBeVisible()
    const save = page.getByRole('button', { name: 'Save', exact: true })
    await expect(save).toBeDisabled()

    await field.fill('cp.example.com/check')
    await expect(page.getByText('Give an http or https address.')).toBeVisible()
    await expect(save).toBeDisabled()
    await field.fill('http://cp.example.com/generate_204')
    await save.click()
    await expect(toast(page, 'The health check fetches http://cp.example.com/generate_204')).toBeVisible()
    expect((await listed(request)).check.url).toBe('http://cp.example.com/generate_204')

    await page.getByRole('button', { name: 'Use the default' }).click()
    await expect(toast(page, 'The health check fetches the default address')).toBeVisible()
    await expect(field).toHaveValue('')
    await expect(page.getByRole('button', { name: 'Use the default' })).toHaveCount(0)
    expect((await listed(request)).check.url).toBe('')
  })
})

test('the state of a row follows the hub', async ({ page, request }) => {
  await openOutbounds(page)
  await setOutbound(request, { id: 3, health: 'up', delayMs: 95 })
  await expect(obRow(page, 'Paris').locator('[data-state="up"]')).toHaveText(/^Up · \d+ ms$/, { timeout: 15000 })
})
