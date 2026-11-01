// Naming the Reality target when a node is added, in a real browser against a real hub and a real server: the
// field under Advanced, a target that works ending up in the node's config and its Settings tab, a target the
// node can't use failing the install at its step with the server cleaned up and fixed in the same dialog, and an
// Add node form with no take-over option. Run it with RN_REALITY=1 npm run e2e:real -- reality-target (see
// playwright.real.config.ts). It changes the server.
//
// What it needs, all from the environment:
//   RN_BASE_URL       where the hub is, with its base path when it has one (default http://127.0.0.1:8080/)
//   RN_NODE_HOST      the server's address as the hub reaches it
//   RN_NODE_PASSWORD  its root password
//   RN_SERVER_LAB     the Lima instance that is the server, for looking at it (default rnnode)
//   RN_WORKING_TARGET a site the server can reach with TLS 1.3 and X25519 (default www.cloudflare.com)
//   RN_SHOTS          a folder for screenshots of each stage; none are taken when it is not set
//
// The hub has no node when the tests start and none when they end; the server is left clean.
import { execFileSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { test as base, expect, type Page } from '@playwright/test'
import { PAGE, dialog, fillAddNode, leaks, openAddNode, stateHolds } from '../e2e/fixtures'

const HOST = process.env.RN_NODE_HOST ?? ''
const PASSWORD = process.env.RN_NODE_PASSWORD ?? ''
const LAB = process.env.RN_SERVER_LAB ?? 'rnnode'
const WORKING = process.env.RN_WORKING_TARGET ?? 'www.cloudflare.com'
const SHOTS = process.env.RN_SHOTS ?? ''
const RUN = process.env.RN_REALITY === '1'
// no name that resolves: the node's own check says why it can't use it
const FAILING = 'no-such-site.invalid'

const test = base.extend<{ seen: string[] }>({
  seen: [
    async ({ page }, use) => {
      // for screenshots, a window tall enough that a dialog which scrolls shows whole; set once, before anything is open
      const size = page.viewportSize()
      if (SHOTS && size) await page.setViewportSize({ width: size.width, height: Math.max(size.height, 1500) })
      const errors: string[] = []
      page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`))
      page.on('console', (m) => m.type() === 'error' && !/Failed to load resource/.test(m.text()) && errors.push(`console.error: ${m.text()}`))
      await use(errors)
      expect(errors, 'the page logged errors').toEqual([])
    },
    { auto: true },
  ],
})
test.describe.configure({ mode: 'serial' })
// a trace keeps a copy of every typed value, the login included
test.use({ trace: 'off' })
test.setTimeout(420_000)
test.skip(!RUN || !HOST || !PASSWORD, 'set RN_REALITY=1, RN_NODE_HOST and RN_NODE_PASSWORD: this changes the server')

const server = (script: string) => execFileSync('limactl', ['shell', LAB, '--', 'sudo', 'bash', '-c', script], { encoding: 'utf8', timeout: 240_000 })

/** Nothing of the agent or Xray on the server. */
function clean() {
  server(`
    systemctl disable --now nasnet-agent xray 2>/dev/null || true
    pkill -x xray 2>/dev/null || true
    rm -rf /usr/local/bin/nasnet-agent /usr/local/bin/xray /usr/local/bin/xray.bak /usr/local/bin/.xray-push-* /etc/nasnet-agent /var/lib/nasnet-agent /var/log/nasnet-agent \\
      /usr/local/etc/xray /etc/systemd/system/nasnet-agent.service /etc/systemd/system/xray.service /etc/systemd/system/xray@.service /etc/systemd/system/xray.service.d \\
      /etc/systemd/system/xray@.service.d /etc/sysctl.d/60-nasnet-agent.conf /usr/local/share/xray /var/log/xray
    echo 15 > /proc/sys/net/ipv4/tcp_retries2
    systemctl daemon-reload`)
}

/** What is left of an install on the server: any of the agent's or Xray's files, the service, or a process. */
const leftOver = () =>
  server(`ls -d /usr/local/bin/nasnet-agent /usr/local/bin/xray /etc/nasnet-agent /usr/local/etc/xray /etc/systemd/system/nasnet-agent.service 2>/dev/null; pgrep -x xray || true`).trim()

/** The dest the node's Xray is set up to borrow from, read from its config. */
const dest = () => server(`grep -o '"dest": *"[^"]*"' /usr/local/etc/xray/config.json | head -1 | sed 's/.*"\\([^"]*\\)"$/\\1/'`).trim()

async function noNodes(page: Page) {
  const nodes: { id: number }[] = await (await page.request.get('api/nodes', { headers: PAGE })).json()
  for (const n of nodes) await page.request.post(`api/nodes/${n.id}/remove`, { headers: PAGE, data: { force: true } })
  await expect.poll(async () => (await (await page.request.get('api/nodes', { headers: PAGE })).json()).length).toBe(0)
}

/** Takes a node off the server through the hub with a login, as a person would. */
async function remove(page: Page, name: string) {
  const nodes: { id: number; name: string }[] = await (await page.request.get('api/nodes', { headers: PAGE })).json()
  const n = nodes.find((x) => x.name === name)
  expect(n, `${name} is listed`).toBeTruthy()
  const probe = await (await page.request.post('api/probe', { headers: PAGE, data: { host: HOST, sshPort: 22, sshUser: 'root' } })).json()
  const res = await page.request.post(`api/nodes/${n!.id}/remove`, {
    headers: PAGE,
    data: { login: { sshPort: 22, sshUser: 'root', password: PASSWORD, hostKey: probe.fingerprint } },
  })
  const { jobId } = await res.json()
  await expect
    .poll(async () => (await (await page.request.get(`api/jobs/${jobId}`, { headers: PAGE })).json()).state, { timeout: 120_000, intervals: [1000] })
    .toBe('ok')
  expect(leftOver()).toBe('')
}

/** A screenshot of the page as it is (see the tall window the tests get when RN_SHOTS is set). */
async function shot(page: Page, name: string) {
  if (!SHOTS) return
  mkdirSync(SHOTS, { recursive: true })
  const phone = (page.viewportSize()?.width ?? 1280) < 600
  await page.screenshot({ path: `${SHOTS}/${phone ? 'm-' : ''}${name}.png` })
}

const isInstall = (r: import('@playwright/test').Request) => r.url().endsWith('/api/nodes') && r.method() === 'POST'

test.beforeAll(clean)
test.afterAll(clean)

test('the form has the Reality target under Advanced and no take-over option anywhere', async ({ page }) => {
  await noNodes(page)
  await openAddNode(page)
  const form = dialog(page, 'Add a node')
  await shot(page, '1-add-node-closed')
  await expect(form.getByLabel('Reality target')).toHaveCount(0)
  await form.getByRole('button', { name: 'Advanced' }).click()
  await expect(form.getByLabel('Entry port')).toBeVisible()
  await expect(form.getByLabel('Reality target')).toHaveAttribute('placeholder', 'www.example.com:443')
  await expect(form).toContainText('Optional. Left empty, the node picks one that works.')
  await expect(form.getByRole('checkbox')).toHaveCount(0)
  await expect(form.getByText(/take over|taking over/i)).toHaveCount(0)
  await shot(page, '2-add-node-advanced')
})

test('a target that works is used as it is, and the node\'s Settings tab shows it', async ({ page }) => {
  await noNodes(page)
  clean()
  await openAddNode(page)
  const form = await fillAddNode(page, HOST, PASSWORD)
  await form.getByLabel('Name', { exact: true }).fill('target-ok')
  await form.getByRole('button', { name: 'Advanced' }).click()
  await form.getByLabel('Reality target').fill(` https://${WORKING.toUpperCase()}/ `)
  await shot(page, '3-target-typed')
  const sent = page.waitForRequest(isInstall)
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const body = (await sent).postDataJSON()
  expect(body.handshakeTarget).toBe(`${WORKING}:443`)
  expect(body.takeOverXray).toBeUndefined()
  const done = dialog(page, 'Node added')
  await expect(done).toBeVisible({ timeout: 240_000 })
  // the node's own check of it is the step's detail
  await expect(done).toContainText(`${WORKING}:443`)
  await shot(page, '4-node-added')
  // what the server runs borrows from it
  expect(dest()).toBe(`${WORKING}:443`)
  // the node's Settings tab says it, and the hub's API too
  const nodes: { id: number; name: string; handshakeTarget: string }[] = await (await page.request.get('api/nodes', { headers: PAGE })).json()
  const n = nodes.find((x) => x.name === 'target-ok')!
  expect(n.handshakeTarget).toBe(`${WORKING}:443`)
  await done.getByRole('button', { name: 'Open node' }).click()
  await page.getByRole('tab', { name: 'Settings' }).click()
  await expect(page.getByRole('heading', { name: 'Reality target' })).toBeVisible()
  await expect(page.locator('b').filter({ hasText: new RegExp(`^${WORKING.replace(/\./g, '\\.')}:443$`) })).toBeVisible()
  await shot(page, '5-node-settings')
  expect(await stateHolds(page, PASSWORD)).toBe(false)
  expect(await leaks(page, PASSWORD)).toEqual([])
  await remove(page, 'target-ok')
})

test('a target the node can\'t use fails the install, cleans the server up, and "Use another" goes on in the dialog', async ({ page }) => {
  await noNodes(page)
  clean()
  await openAddNode(page)
  const form = await fillAddNode(page, HOST, PASSWORD)
  await form.getByLabel('Name', { exact: true }).fill('target-fix')
  await form.getByRole('button', { name: 'Advanced' }).click()
  await form.getByLabel('Reality target').fill(FAILING)
  const first = page.waitForRequest(isInstall)
  await form.getByRole('button', { name: 'Connect and install' }).click()
  expect((await first).postDataJSON().handshakeTarget).toBe(`${FAILING}:443`)
  const d = dialog(page, 'Installation failed')
  await expect(d).toBeVisible({ timeout: 240_000 })
  // it failed where the node checks it, and says which target, and why in the node's own words
  await expect(d.getByText(`The Reality target ${FAILING}:443 doesn't work from this server.`)).toBeVisible()
  await expect(d.getByText('Choose a Reality target')).toBeVisible()
  await expect(d.getByText('Clean up the server')).toBeVisible()
  await expect(d.getByText('The server was cleaned up, and nothing was kept.')).toBeVisible()
  await shot(page, '6-target-failed')
  // the agent and Xray were on the server by then, and nothing of them is left
  expect(leftOver()).toBe('')
  // the two fixes
  const fixes = d.getByRole('region', { name: 'What you can do' })
  await expect(fixes.getByText('Use another Reality target')).toBeVisible()
  await expect(fixes.getByRole('button', { name: 'Let the node choose one' })).toBeVisible()
  expect(await stateHolds(page, PASSWORD)).toBe(true)
  expect(await leaks(page, PASSWORD)).toEqual([])

  await fixes.getByLabel('Reality target').fill(WORKING)
  const second = page.waitForRequest(isInstall)
  await fixes.getByRole('button', { name: 'Continue', exact: true }).click()
  const body = (await second).postDataJSON()
  expect(body).toMatchObject({ host: HOST, name: 'target-fix', handshakeTarget: `${WORKING}:443` })
  expect(body.takeOverXray).toBeUndefined()
  await expect(page.getByRole('dialog')).toHaveCount(1)
  const done = dialog(page, 'Node added')
  await expect(done).toBeVisible({ timeout: 240_000 })
  await shot(page, '7-fixed-and-added')
  expect(dest()).toBe(`${WORKING}:443`)
  expect(await stateHolds(page, PASSWORD)).toBe(false)
  expect(await leaks(page, PASSWORD)).toEqual([])
  for (const path of ['api/jobs', 'api/nodes']) expect(await (await page.request.get(path, { headers: PAGE })).text()).not.toContain(PASSWORD)
  await done.getByRole('button', { name: 'Close', exact: true }).first().click()
  await remove(page, 'target-fix')
})

test('"Let the node choose one" sends no target, and the node picks one that works', async ({ page }) => {
  await noNodes(page)
  clean()
  await openAddNode(page)
  const form = await fillAddNode(page, HOST, PASSWORD)
  await form.getByLabel('Name', { exact: true }).fill('target-choose')
  await form.getByRole('button', { name: 'Advanced' }).click()
  await form.getByLabel('Reality target').fill(FAILING)
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const d = dialog(page, 'Installation failed')
  await expect(d).toBeVisible({ timeout: 240_000 })
  expect(leftOver()).toBe('')
  const again = page.waitForRequest(isInstall)
  await d.getByRole('button', { name: 'Let the node choose one' }).click()
  const body = (await again).postDataJSON()
  expect('handshakeTarget' in body).toBe(false)
  const done = dialog(page, 'Node added')
  await expect(done).toBeVisible({ timeout: 240_000 })
  const chosen = dest()
  expect(chosen).toMatch(/^[a-z0-9.-]+:443$/)
  expect(chosen).not.toContain(FAILING)
  expect(await stateHolds(page, PASSWORD)).toBe(false)
  await done.getByRole('button', { name: 'Close', exact: true }).first().click()
  await remove(page, 'target-choose')
})
