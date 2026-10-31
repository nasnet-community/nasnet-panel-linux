// The failed-install dialog and its fixes in a real browser, against a real hub and a real server: what the
// page offers, what it sends, what the server ends up with, and where the login is afterwards. Run it with
// RN_TAKEOVER=1 npm run e2e:real -- takeover (see playwright.real.config.ts). It changes the server.
//
// What it needs, all from the environment:
//   RN_BASE_URL       where the hub is, with its base path when it has one (default http://127.0.0.1:8080/)
//   RN_NODE_HOST      the server's address as the hub reaches it
//   RN_NODE_PASSWORD  its root password
//   RN_SERVER_LAB     the Lima instance that is the server, for arranging it and looking at it (default rnnode)
//   RN_HUB_LAB        the Lima instance the hub runs in and its unit, to read its log: "rnlinux:remote-nodes-f1"
//
// The server is made to look like one set up with the official Xray script (a service for the user xray, a config
// folder only root can open, a drop-in, assets and logs of its own) before each test, and left clean after. Tests
// run in order and share the hub, which has no node when they start and none when they end.
import { execFileSync } from 'node:child_process'
import { test as base, expect, type Page } from '@playwright/test'
import { PAGE, dialog, fillAddNode, heapHolds, leaks, openAddNode, stateHolds } from '../e2e/fixtures'

const HOST = process.env.RN_NODE_HOST ?? ''
const PASSWORD = process.env.RN_NODE_PASSWORD ?? ''
const LAB = process.env.RN_SERVER_LAB ?? 'rnnode'
const [HUB_LAB, HUB_UNIT] = (process.env.RN_HUB_LAB ?? 'rnlinux:remote-nodes-f1').split(':')
const RUN = process.env.RN_TAKEOVER === '1'

const test = base.extend<{ seen: string[] }>({
  seen: [
    async ({ page }, use) => {
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
// Playwright's trace keeps a copy of every typed value on the element it was typed into (in a property called
// __playwright_snapshot_cache_), which a check of the page's heap for the login would find. So no trace here.
test.use({ trace: 'off' })
test.setTimeout(420_000)
test.skip(!RUN || !HOST || !PASSWORD, 'set RN_TAKEOVER=1, RN_NODE_HOST and RN_NODE_PASSWORD: this changes the server')

/** Runs a script on the server as root, through Lima's own ssh, and gives what it printed. */
const server = (script: string) => execFileSync('limactl', ['shell', LAB, '--', 'sudo', 'bash', '-c', script], { encoding: 'utf8', timeout: 240_000 })
const hub = (script: string) => execFileSync('limactl', ['shell', HUB_LAB, '--', 'sudo', 'bash', '-c', script], { encoding: 'utf8', timeout: 60_000 })

/** Nothing of the agent or Xray on the server, and nothing in the way of a port. */
function clean() {
  server(`
    systemctl disable --now nasnet-agent xray 2>/dev/null || true
    pkill -x xray 2>/dev/null || true
    pkill -f '[h]ttp.server 443' 2>/dev/null || true   # the bracket keeps it from matching this very script
    rm -rf /usr/local/bin/nasnet-agent /usr/local/bin/xray /usr/local/bin/xray.bak /usr/local/bin/.xray-push-* /etc/nasnet-agent /var/lib/nasnet-agent /var/log/nasnet-agent \\
      /usr/local/etc/xray /etc/systemd/system/nasnet-agent.service /etc/systemd/system/xray.service /etc/systemd/system/xray@.service /etc/systemd/system/xray.service.d \\
      /etc/systemd/system/xray@.service.d /etc/sysctl.d/60-nasnet-agent.conf /usr/local/share/xray /var/log/xray
    echo 15 > /proc/sys/net/ipv4/tcp_retries2
    systemctl daemon-reload`)
}

/** A server set up by the official script at this version, with a config of its own that holds port 443. */
function officialXray(version: string) {
  server(`
    [ -s /tmp/install-release.sh ] || curl -fsSL -m 60 https://github.com/XTLS/Xray-install/raw/main/install-release.sh -o /tmp/install-release.sh
    bash /tmp/install-release.sh install --version ${version} >/dev/null 2>&1
    cat > /usr/local/etc/xray/config.json <<'EOF'
{"log":{"loglevel":"warning"},"inbounds":[{"tag":"theirs-socks","listen":"0.0.0.0","port":443,"protocol":"socks","settings":{"udp":false}}],"outbounds":[{"protocol":"freedom"}]}
EOF
    chown root:xray /usr/local/etc/xray/config.json; chmod 640 /usr/local/etc/xray/config.json
    systemctl restart xray; sleep 1; systemctl is-active xray`)
}

const sha = (path: string) => server(`sha256sum ${path} | cut -d' ' -f1`).trim()
const listing = (dir: string) => server(`ls -A ${dir} 2>/dev/null || true`).trim().split('\n').filter(Boolean)

/** Gives the hub no node, as it started. */
async function noNodes(page: Page) {
  const nodes: { id: number }[] = await (await page.request.get('api/nodes', { headers: PAGE })).json()
  for (const n of nodes) await page.request.post(`api/nodes/${n.id}/remove`, { headers: PAGE, data: { force: true } })
  await expect.poll(async () => (await (await page.request.get('api/nodes', { headers: PAGE })).json()).length).toBe(0)
}

/** s as one word for a POSIX shell. */
const shq = (s: string) => `'${s.replace(/'/g, `'\\''`)}'`

/** Whether the hub's own log has `text` in it, which it must never. */
const hubLogHolds = (text: string) => hub(`journalctl -u ${HUB_UNIT} --no-pager -o cat | grep -cF -- ${shq(text)} || true`).trim() !== '0'

/** Takes a node off the server through the hub with a login, as a person would, and checks that the server is clean of it. */
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
}

test.beforeAll(clean)
test.afterAll(clean)

test('a server with the official Xray is taken over from the refusing dialog, and its binary stays', async ({ page }) => {
  await noNodes(page)
  const pinned = (await (await page.request.get('api/config', { headers: PAGE })).json()).xray as string
  officialXray(pinned)
  const binary = sha('/usr/local/bin/xray')

  await openAddNode(page)
  const form = await fillAddNode(page, HOST, PASSWORD)
  await form.getByLabel('Name', { exact: true }).fill('takeover-a')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const d = dialog(page, "This server can't be used")
  await expect(d).toBeVisible({ timeout: 60_000 })
  // what the hub found, said by the page from the hub's facts
  await expect(d).toContainText('Found on the server:')
  await expect(d).toContainText('its service, xray.service')
  await expect(d).toContainText('a running process named xray')
  await expect(d).toContainText(`It is Xray ${pinned}, the same build as this plugin's, so its program stays as it is.`)
  await expect(d).toContainText('keeps no backup')
  await expect(d.getByRole('button', { name: /^\d+ steps not started$/ })).toBeVisible()
  await expect(d.getByText('Discard the node')).toHaveCount(0)
  // nothing has changed on the server
  expect(listing('/usr/local/etc/xray')).toEqual(['config.json'])
  expect(server('grep -c theirs-socks /usr/local/etc/xray/config.json || true').trim()).toBe('1')
  expect(listing('/etc/systemd/system/xray.service.d')).toEqual(['10-donot_touch_single_conf.conf'])
  // the login is held in the dialog, and in no storage, address or history
  expect(await stateHolds(page, PASSWORD)).toBe(true)
  expect(await leaks(page, PASSWORD)).toEqual([])

  await d.getByRole('button', { name: 'Take over and continue' }).click()
  const done = dialog(page, 'Node added')
  await expect(done).toBeVisible({ timeout: 240_000 })
  await expect(page.getByRole('dialog')).toHaveCount(1)
  await expect(done).toContainText('It is the pinned build, so its binary stays.')
  await expect(done.getByText('Not needed')).toHaveCount(2)

  // the server: their binary kept as it was, their config and drop-in gone, ours in, their assets and logs left
  expect(sha('/usr/local/bin/xray')).toBe(binary)
  expect(server('grep -c theirs-socks /usr/local/etc/xray/config.json || true').trim()).toBe('0')
  expect(listing('/etc/systemd/system/xray.service.d')).toEqual(['asset-location.conf'])
  expect(server('systemctl cat xray.service | grep -E "^User="').trim()).toBe('User=root')
  expect(listing('/usr/local/share/xray').sort()).toEqual(['geoip.dat', 'geosite.dat'])
  expect(server('stat -c %a /var/log/xray').trim()).toBe('700')

  // the login is gone from the page as soon as the install worked, and was never anywhere else
  expect(await stateHolds(page, PASSWORD)).toBe(false)
  await expect.poll(() => heapHolds(page, PASSWORD), { timeout: 8000, intervals: [200, 500, 1000] }).toBe(false)
  expect(await leaks(page, PASSWORD)).toEqual([])
  for (const path of ['api/jobs', 'api/nodes']) expect(await (await page.request.get(path, { headers: PAGE })).text()).not.toContain(PASSWORD)
  expect(hubLogHolds(PASSWORD)).toBe(false)

  await done.getByRole('button', { name: 'Close', exact: true }).first().click()
  await expect.poll(() => heapHolds(page, PASSWORD), { timeout: 8000, intervals: [200, 500, 1000] }).toBe(false)
  await remove(page, 'takeover-a')
  // removal takes the taken-over Xray off, and leaves what was never the install's
  expect(server('ls /usr/local/bin/xray /usr/local/etc/xray /etc/systemd/system/xray.service 2>&1 | grep -c "No such file" || true').trim()).toBe('3')
  expect(listing('/usr/local/share/xray').sort()).toEqual(['geoip.dat', 'geosite.dat'])
})

test('a port that something else holds is gone round with another entry port, in the dialog', async ({ page }) => {
  await noNodes(page)
  clean()
  server(`nohup setsid python3 -m http.server 443 --bind 0.0.0.0 >/dev/null 2>&1 & sleep 1; ss -ltnp | grep -c ':443 ' `)

  await openAddNode(page)
  const form = await fillAddNode(page, HOST, PASSWORD)
  await form.getByLabel('Name', { exact: true }).fill('portway')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const d = dialog(page, 'Installation failed')
  await expect(d).toBeVisible({ timeout: 60_000 })
  // who holds it, from what the hub sent
  await expect(d).toContainText('Port 443 on the server is used by python3.')
  const port = d.getByLabel('Entry port')
  await expect(port).toHaveValue('8443')
  expect(await stateHolds(page, PASSWORD)).toBe(true)
  expect(await leaks(page, PASSWORD)).toEqual([])
  // nothing was changed on the server
  expect(listing('/usr/local/etc/xray')).toEqual([])
  expect(server('ls /usr/local/bin/nasnet-agent 2>&1 | grep -c "No such file" || true').trim()).toBe('1')

  await d.getByRole('button', { name: 'Continue', exact: true }).click()
  const done = dialog(page, 'Node added')
  await expect(done).toBeVisible({ timeout: 240_000 })
  expect(server("ss -ltnp | grep -E ':8443 ' | grep -c xray || true").trim()).toBe('1')
  expect(server("ss -ltnp | grep -E ':443 ' | grep -c python3 || true").trim()).toBe('1')
  expect(await stateHolds(page, PASSWORD)).toBe(false)
  await expect.poll(() => heapHolds(page, PASSWORD), { timeout: 8000, intervals: [200, 500, 1000] }).toBe(false)
  expect(await leaks(page, PASSWORD)).toEqual([])
  expect(hubLogHolds(PASSWORD)).toBe(false)
  await done.getByRole('button', { name: 'Close', exact: true }).first().click()
  await remove(page, 'portway')
})

test('a login the server refuses is typed again in the dialog, and the wrong one is never kept', async ({ page }) => {
  await noNodes(page)
  clean()
  const wrong = 'not-the-password-0001'
  await openAddNode(page)
  const form = await fillAddNode(page, HOST, wrong)
  await form.getByLabel('Name', { exact: true }).fill('loginway')
  await form.getByRole('button', { name: 'Connect and install' }).click()
  const d = dialog(page, 'Installation failed')
  await expect(d).toBeVisible({ timeout: 60_000 })
  await expect(d).toContainText("The server didn't accept the login.")
  expect(await stateHolds(page, wrong)).toBe(true)
  await d.locator('input[type="password"]').fill(PASSWORD)
  await d.getByRole('button', { name: 'Continue', exact: true }).click()
  await expect(dialog(page, 'Node added')).toBeVisible({ timeout: 240_000 })
  for (const secret of [wrong, PASSWORD]) {
    expect(await stateHolds(page, secret)).toBe(false)
    await expect.poll(() => heapHolds(page, secret), { timeout: 8000, intervals: [200, 500, 1000] }).toBe(false)
    expect(await leaks(page, secret)).toEqual([])
    expect(hubLogHolds(secret)).toBe(false)
  }
  await dialog(page, 'Node added').getByRole('button', { name: 'Close', exact: true }).first().click()
  await remove(page, 'loginway')
})
