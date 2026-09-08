/** Integrated Router UI checks. All /api responses are local fixtures; no router is changed.
 * NODE_PATH=/path/to/playwright/node_modules WAN_SCREENSHOT_DIR=/output node scripts/test-wan-ui.cjs
 * Start Vite on 127.0.0.1:3001 first, or set WAN_TEST_URL.
 */
const { chromium } = require("playwright")
const assert = require("node:assert/strict")
const fs = require("node:fs")
const path = require("node:path")
const output = process.env.WAN_SCREENSHOT_DIR || "/tmp/nasnet-wan-ui"
fs.mkdirSync(output, { recursive: true })
const defaults = (slot) => ({
    method: "dhcp4",
    static_address: "",
    static_gateway: "",
    gateway_on_link: false,
    dns_mode: slot.startsWith("secondary") ? "vpn" : "default",
    dns_servers: [],
})
function fixture() {
    const interfaces = ["domestic", "secondary", ""].map((slot, i) => ({
        id: i + 1,
        if_name: `eth${i}`,
        key: `nic${i}`,
        key_kind: "permaddr",
        perm_mac: `02:00:00:00:00:0${i}`,
        source: "eth_onboard",
        phy: "",
        label: ["Home fiber", "Satellite", ""][i],
        assignable: true,
        present: true,
        healthy: !!slot,
        carrier: true,
        oper_state: "up",
        speed_mbit: 1000,
        mtu: 1500,
        addrs: slot ? [i ? "100.64.0.2/24" : "10.0.2.15/24"] : [],
        role: slot ? "wan" : "unassigned",
        slot,
        wan: defaults(slot),
    }))
    const state = {
        router_mode: true,
        takeover_done: true,
        warnings: [],
        pending_plan_id: 0,
        confirm_deadline_unix: 0,
        last_apply_id: 0,
        last_apply_phase: "",
        vpn: { active: true, connected: true },
        uplinks: interfaces
            .filter((i) => i.slot)
            .map((i) => ({
                if_name: i.if_name,
                slot: i.slot,
                label: i.label,
                table: i.id === 1 ? 201 : 202,
                addrs: i.addrs,
                gateway: i.id === 1 ? "10.0.2.1" : "100.64.0.1",
                healthy: true,
                verdict: "up",
                force_state: "",
            })),
    }
    return {
        interfaces,
        state,
        plans: [],
        applies: [],
        unknown: new Set(),
        loseApply: false,
        offline: false,
        short: false,
        before: null,
    }
}
async function mock(page, f) {
    await page.route("**/api/v1/**", async (route) => {
        const req = route.request(),
            url = new URL(req.url()),
            p = url.pathname
        const json = (data) =>
            route.fulfill({
                status: 200,
                contentType: "application/json",
                body: JSON.stringify({ success: true, data }),
            })
        if (p.endsWith("/auth/me"))
            return json({ id: 1, username: "demo", first_name: "Operator", is_admin: true })
        if (p.endsWith("/network/interfaces")) return json(f.interfaces)
        if (p.endsWith("/network/state")) {
            if (f.offline) return route.abort()
            return json(f.state)
        }
        if (p.endsWith("/network/health"))
            return json({
                generated_unix: Date.now() / 1000,
                failover_active: false,
                vpn: null,
                uplinks: f.state.uplinks.map((u) => ({
                    ...u,
                    carrier: "up",
                    gateway: "up",
                    gateway_ip: u.gateway,
                    internet: "up",
                    via: "",
                    targets: [],
                    history: [],
                    loss_pct: 0,
                    median_rtt_ms: 12,
                    rx_bytes: 1e6,
                    tx_bytes: 1e5,
                    routes: [],
                    degraded: false,
                })),
            })
        if (p.endsWith("/network/plan")) {
            const r = req.postDataJSON()
            f.plans.push(r)
            return json({ ops: ["Save WAN settings", "Activate this connection"], verdicts: [] })
        }
        if (p.endsWith("/network/apply")) {
            const r = req.postDataJSON()
            f.applies.push(r)
            f.before = structuredClone(f.interfaces)
            const row = f.interfaces.find((i) => i.id === r.interface_id)
            Object.assign(row, { wan: r.wan, role: r.role, slot: r.slot })
            Object.assign(f.state, {
                pending_plan_id: 42,
                confirm_deadline_unix: Math.floor(Date.now() / 1000) + (f.short ? 2 : 90),
                pending_request_id: r.request_id,
                last_apply_id: 42,
                last_apply_request_id: r.request_id,
                last_apply_phase: "applied",
            })
            if (f.loseApply) return route.abort()
            return json({
                plan_id: 42,
                confirm_deadline_unix: f.state.confirm_deadline_unix,
                ops: [],
            })
        }
        if (p.endsWith("/network/confirm")) {
            assert.equal(req.postDataJSON().plan_id, 42)
            f.state.pending_plan_id = 0
            f.state.pending_request_id = ""
            f.state.last_apply_phase = "confirmed"
            return json(null)
        }
        if (p.endsWith("/network/rollback")) {
            assert.equal(req.postDataJSON().plan_id, 42)
            f.interfaces = f.before
            f.state.pending_plan_id = 0
            f.state.pending_request_id = ""
            f.state.last_apply_phase = "rolled_back"
            return json(null)
        }
        if (p.endsWith("/network/lan"))
            return json({
                bridge_name: "lan0",
                cidr: "10.77.0.1/24",
                enabled: true,
                dhcp_range_low: "10.77.0.100",
                dhcp_range_high: "10.77.0.200",
                lease_hours: 12,
            })
        if (p.endsWith("/network/vpn/status"))
            return json({ tunnels: [], uplinks: [], kill_switch: true, strategy: "spread" })
        if (p.endsWith("/network/portmap/status")) return json({ enabled: false, wans: [] })
        if (/port-forwards|wifi\/radios|vpn\/profiles|flow\/events|notifications/.test(p))
            return json([])
        if (p.endsWith("/nodes")) return json([])
        if (p.includes("settings")) return json({})
        if (p.includes("maintenance")) return json({ active: false, enabled: false })
        if (p.includes("events"))
            return route.fulfill({
                status: 200,
                contentType: "text/event-stream",
                body: ": fixture\n\n",
            })
        f.unknown.add(p)
        return json({})
    })
}
async function openEditor(page, label = "Home fiber") {
    try {
        await page
            .getByRole("button", { name: `Configure WAN ${label}`, exact: true })
            .filter({ visible: true })
            .click()
    } catch (e) {
        await screenshot(page, "panel-wan-debug.png")
        console.error(page.url(), await page.locator("body").innerText())
        throw e
    }
    await page.getByRole("heading", { name: "Configure WAN", exact: true }).waitFor()
    return page.locator("[data-wan-editor]")
}
async function staticForm(sheet) {
    await sheet.getByText("Static IPv4", { exact: true }).click()
    await sheet.getByLabel("IPv4 address", { exact: true }).fill("10.0.2.20")
    await sheet.getByLabel("Prefix length", { exact: true }).fill("24")
    await sheet.getByLabel("Gateway", { exact: true }).fill("10.0.2.1")
    await sheet.getByLabel("Domestic name lookups", { exact: true }).selectOption("custom")
    await sheet.getByLabel("DNS server 1", { exact: true }).fill("178.22.122.100")
    await sheet.getByLabel("DNS server 2 (optional)", { exact: true }).fill("185.51.200.2")
}
async function screenshot(page, filename) {
    await page.screenshot({ path: path.join(output, filename), fullPage: false })
}
;(async () => {
    const browser = await chromium.launch({ channel: "chrome", headless: true })
    const results = []
    try {
        for (const theme of ["light", "dark"])
            for (const width of [1440, 390]) {
                const context = await browser.newContext({
                    viewport: { width, height: 1000 },
                    colorScheme: theme,
                })
                const page = await context.newPage()
                const f = fixture()
                const errors = []
                page.on("pageerror", (e) => {
                    errors.push(e.message)
                    console.error("Page error:", e.message)
                })
                await mock(page, f)
                await page.addInitScript((theme) => localStorage.setItem("theme", theme), theme)
                await page.goto(process.env.WAN_TEST_URL || "http://127.0.0.1:3001/router")
                const sheet = await openEditor(page)
                await staticForm(sheet)
                await sheet.getByLabel("IPv4 address", { exact: true }).fill("999.2.3.4")
                await sheet.getByRole("button", { name: "Review changes", exact: true }).click()
                assert.equal(f.plans.length, 0)
                await sheet.getByText("Enter a unicast IPv4 address.", { exact: true }).waitFor()
                await sheet.getByLabel("IPv4 address", { exact: true }).fill("10.0.2.20")
                await screenshot(page, `panel-wan-edit-${theme}-${width}.png`)
                const overflow = await sheet.evaluate((el) => ({
                    scroll: el.scrollWidth,
                    client: el.clientWidth,
                }))
                assert(overflow.scroll <= overflow.client + 1, JSON.stringify(overflow))
                await sheet.getByRole("button", { name: "Review changes", exact: true }).click()
                await sheet
                    .getByRole("heading", { name: "Review WAN changes", exact: true })
                    .waitFor()
                assert.deepEqual(f.plans.at(-1).wan.dns_servers, ["178.22.122.100", "185.51.200.2"])
                assert.equal(f.applies.length, 0)
                await screenshot(page, `panel-wan-review-${theme}-${width}.png`)
                await sheet.getByRole("button", { name: "Apply changes", exact: true }).click()
                await sheet.getByRole("button", { name: "Keep settings", exact: true }).waitFor()
                await screenshot(page, `panel-wan-confirm-${theme}-${width}.png`)
                await sheet.getByRole("button", { name: "Keep settings", exact: true }).click()
                await sheet
                    .getByRole("heading", { name: "WAN settings kept", exact: true })
                    .waitFor()
                await sheet.getByRole("button", { name: "Done", exact: true }).click()
                const secondary = await openEditor(page, "Satellite")
                await secondary.getByText("Managed through the VPN", { exact: true }).waitFor()
                assert.equal(await secondary.getByLabel("DNS server 1", { exact: true }).count(), 0)
                await secondary.getByRole("button", { name: "Cancel", exact: true }).click()
                // A new static-only WAN opens the same form before any write.
                const writesBeforeAssignment = f.applies.length
                await page.getByRole("combobox", { name: "Role for eth2", exact: true }).filter({ visible: true }).click()
                await page.getByRole("option", { name: /^Domestic 2/ }).click()
                const newWAN = page.locator('[data-wan-editor="eth2"]')
                await newWAN.getByRole("heading", { name: "Configure WAN", exact: true }).waitFor()
                await staticForm(newWAN)
                await newWAN.getByLabel("IPv4 address", { exact: true }).fill("10.0.3.20")
                await newWAN.getByLabel("Gateway", { exact: true }).fill("10.0.3.1")
                await newWAN.getByRole("button", { name: "Review changes", exact: true }).click()
                await newWAN.getByText("WAN assignment", { exact: true }).waitFor()
                assert.equal(f.plans.at(-1).slot, "domestic2")
                assert.equal(f.plans.at(-1).wan.method, "static")
                assert.equal(f.applies.length, writesBeforeAssignment)
                await newWAN.getByRole("button", { name: "Close", exact: true }).click()
                assert.deepEqual(errors, [])
                results.push({
                    theme,
                    width,
                    flow: "validation → review → apply → keep; secondary DNS; new static WAN assignment",
                    overflow,
                    unknown: [...f.unknown],
                })
                await context.close()
            }
        // A lost apply response must be recovered by its request ID; reload must
        // preserve access to the pending operation's controls.
        const context = await browser.newContext({ viewport: { width: 1280, height: 960 } })
        const page = await context.newPage()
        const f = fixture()
        await mock(page, f)
        await page.goto(process.env.WAN_TEST_URL || "http://127.0.0.1:3001/router")
        let sheet = await openEditor(page)
        await staticForm(sheet)
        await sheet.getByRole("button", { name: "Review changes", exact: true }).click()
        f.loseApply = true
        await sheet.getByRole("button", { name: "Apply changes", exact: true }).click()
        await sheet
            .getByRole("button", { name: "Keep settings", exact: true })
            .waitFor({ timeout: 15000 })
        assert.equal(f.applies.length, 1)
        await page.reload()
        await page
            .getByText("A network change is waiting for confirmation", { exact: true })
            .waitFor()
        await page.getByRole("button", { name: "Revert now", exact: true }).click()
        assert.equal(f.state.last_apply_phase, "rolled_back")
        results.push({ flow: "lost apply response → recovered request → reload → manual revert" })
        await context.close()
        const timeoutContext = await browser.newContext({ viewport: { width: 390, height: 844 } })
        const timeoutPage = await timeoutContext.newPage()
        const tf = fixture()
        tf.short = true
        await mock(timeoutPage, tf)
        await timeoutPage.goto(process.env.WAN_TEST_URL || "http://127.0.0.1:3001/router")
        sheet = await openEditor(timeoutPage)
        await staticForm(sheet)
        await sheet.getByRole("button", { name: "Review changes", exact: true }).click()
        await sheet.getByRole("button", { name: "Apply changes", exact: true }).click()
        await sheet
            .getByRole("heading", { name: "Checking connection", exact: true })
            .waitFor({ timeout: 10000 })
        assert.equal(
            await sheet
                .getByRole("heading", { name: "Previous settings restored", exact: true })
                .count(),
            0,
        )
        await screenshot(timeoutPage, "panel-wan-recovery-mobile.png")
        tf.state.pending_plan_id = 0
        tf.state.pending_request_id = ""
        tf.state.last_apply_phase = "rolled_back"
        await sheet
            .getByRole("heading", { name: "Previous settings restored", exact: true })
            .waitFor({ timeout: 10000 })
        results.push({ flow: "expired countdown waits for server-confirmed rollback" })
        await timeoutContext.close()
        fs.writeFileSync(
            path.join(output, "panel-wan-results.json"),
            JSON.stringify(results, null, 2),
        )
        console.log(JSON.stringify(results, null, 2))
    } finally {
        await browser.close()
    }
})().catch((e) => {
    console.error(e)
    process.exitCode = 1
})
