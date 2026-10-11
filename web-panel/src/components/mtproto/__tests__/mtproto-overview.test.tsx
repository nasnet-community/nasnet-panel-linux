import { cleanup, render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, Route, Routes, useLocation } from "react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MtprotoOverview } from "../mtproto-overview"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import type { MtprotoNode } from "@/lib/api/mtproto"
import type { Node } from "@/lib/types"

const state = vi.hoisted(() => ({
    node: undefined as unknown,
    allow: undefined as unknown,
    vpn: undefined as unknown,
    events: undefined as unknown,
    restart: vi.fn(),
    setStopped: vi.fn(),
    refresh: vi.fn(),
    check: vi.fn(),
    checkData: undefined as unknown,
    checkVars: undefined as unknown,
}))

vi.mock("@/lib/queries", () => ({
    useMtprotoNode: () => ({ isLoading: false, isError: false, data: state.node, refetch: vi.fn(), dataUpdatedAt: 1 }),
    useMtprotoAllowlist: () => ({ data: state.allow }),
    useVPNStatus: () => ({ data: state.vpn }),
    useRefreshMtprotoAllowlist: () => ({ mutate: state.refresh, isPending: false }),
    useRestartMtprotoTelemt: () => ({ mutate: state.restart, isPending: false }),
    useSetMtprotoProxyStopped: () => ({ mutate: state.setStopped, isPending: false }),
    useCheckMtprotoDisguise: () => ({ mutate: state.check, isPending: false, data: state.checkData, variables: state.checkVars }),
    useMtprotoEvents: () => ({ isLoading: false, isError: false, data: state.events }),
    useRefreshNodes: () => vi.fn(),
    useNodeOverview: () => ({ data: { today: { uplink: 1024, downlink: 4096, peak_bps: 0 } } }),
    useNodeStatsSeries: () => ({ data: undefined, isLoading: false }),
}))
vi.mock("@/lib/api/mtproto", () => ({
    updateMtprotoNode: vi.fn(), rotateMtprotoKeys: vi.fn(), repairMtprotoNode: vi.fn(), removeMtprotoNode: vi.fn(), streamMtprotoJob: vi.fn(),
}))
vi.mock("@/lib/toast", () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }))
vi.mock("@/hooks/use-is-mobile", () => ({ useIsMobile: () => false }))
vi.mock("@/components/node/uptime-timeline", () => ({ UptimeTimeline: () => <div>uptime timeline</div> }))
vi.mock("@/components/node/disconnection-history", () => ({ DisconnectionHistory: () => <div>disconnections</div> }))
vi.mock("@/components/node/traffic-summary-chart", () => ({ TrafficSummaryChart: () => null }))

const liteNode = { id: 4, name: "Tehran 1", ip: "203.0.113.9", kind: "lite", connect_mode: "link", is_online: true, is_active: true, created_at: "2026-09-30T00:00:00Z" } as Node

function fixture(over: Partial<MtprotoNode> = {}): MtprotoNode {
    return {
        node_id: 4, agent_id: "a", hub_port: 8443, capture_port: 9443, ssh_port: 22, ssh_user: "root", host_key: "SHA256:x",
        disguise: "www.example.ir", max_streams: 5000, arch: "amd64", agent_version: "0.1.0", telemt_version: "3.4.25",
        running: true, healthy: true, users: 12, active_users: 3,
        throughput: { up_bps: 2048, down_bps: 10240 },
        latest: { agent: "0.1.0", telemt: "3.4.25" },
        installed_at: "2026-09-30T00:00:00Z",
        link: { up: 2, conns: [
            { name: "domestic-0", up: true, rtt_ms: 31, streams: 12, since: "2026-10-01T00:00:00Z" },
            { name: "domestic-1", up: true, rtt_ms: 35, streams: 0, since: "2026-10-01T00:00:00Z" },
        ], streams: 12, relayed: 340, dial_failures: 3, violations: 0, bytes_to_dc: 1024, bytes_from_dc: 2048 },
        agent: {
            agent_version: "0.1.0", telemt_version: "3.4.25", state_rev: "r1", uptime_sec: 3600, rss_bytes: 30 << 20, warm: true,
            telemt: { running: true, restarts: 1, uptime_sec: 600 },
            capture: { rules: true, active: 12, rejected: 0 },
            disguise: { domain: "www.example.ir", ok: true, checked_at: "2026-10-01T00:00:00Z" },
        },
        desired_rev: "r1", applied_rev: "r1",
        ...over,
    }
}

const navigate = vi.fn()

function setup() {
    return render(<MemoryRouter><ConfirmDialogProvider><MtprotoOverview node={liteNode} onNavigateTab={navigate} /></ConfirmDialogProvider></MemoryRouter>)
}

const card = (title: string) => screen.getByRole("heading", { name: title }).closest("[data-slot=card]")
const attention = () => screen.queryByRole("region", { name: "Needs attention" })

beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} })
    state.node = fixture()
    state.allow = { prefixes: 14, placeholder: false }
    state.vpn = undefined
    state.events = { since: "2026-10-02T09:00:00Z", events: [] }
    state.checkData = undefined
    state.checkVars = undefined
})
afterEach(() => vi.unstubAllGlobals())

describe("MtprotoOverview", () => {
    it("leads with who is using the node and how hard", () => {
        setup()
        expect(card("Users online")).toHaveTextContent("3of 12")
        expect(card("Open streams")).toHaveTextContent("12of 5,000")
        expect(card("Throughput")).toHaveTextContent("10 KB/s")
        expect(card("MTProto proxy")).toHaveTextContent("Running")
    })

    it("has nothing to flag on a healthy node", () => {
        setup()
        expect(attention()).toBeNull()
    })

    it("lists each link connection with its round trip", () => {
        setup()
        const conns = screen.getByRole("list", { name: "Link connections" })
        expect(within(conns).getByText("domestic-0")).toBeInTheDocument()
        expect(conns).toHaveTextContent("31 ms")
        expect(card("Link")).toHaveTextContent("33ms")
    })

    it("explains a proxy stopped for want of users, and points at the tunnel", async () => {
        const user = userEvent.setup()
        state.node = fixture({ users: 0, active_users: 0, agent: { ...fixture().agent!, telemt: { running: false, restarts: 0, uptime_sec: 0 } } })
        setup()
        expect(screen.getAllByText("Waiting for users").length).toBeGreaterThan(0)
        const box = attention()!
        expect(box).toHaveTextContent("The proxy is waiting for users")
        await user.click(within(box).getByRole("button", { name: "Open tunnel" }))
        expect(navigate).toHaveBeenCalledWith("tunnels")
    })

    it("says why the agent holds the proxy", () => {
        state.node = fixture({ agent: { ...fixture().agent!, telemt: { running: false, restarts: 0, uptime_sec: 0, held: "waiting for the panel's first state" } } })
        setup()
        expect(attention()).toHaveTextContent("Waiting for the panel's first state")
    })

    it("puts a crash loop first and offers the log", async () => {
        const user = userEvent.setup()
        state.node = fixture({
            applied_rev: "r0",
            agent: { ...fixture().agent!, telemt: { running: false, restarts: 9, uptime_sec: 0, last_exit: "exit status 1", crash_loop: true } },
        })
        setup()
        const items = within(attention()!).getAllByRole("listitem")
        expect(items[0]).toHaveTextContent("The proxy keeps crashing")
        expect(items[0]).toHaveTextContent("exit status 1")
        await user.click(within(items[0]).getByRole("button", { name: "View log" }))
        expect(navigate).toHaveBeenCalledWith("logs")
    })

    it("warns when the agent is behind the panel's settings", () => {
        state.node = fixture({ applied_rev: "r0", last_push_error: "bad user" })
        setup()
        expect(attention()).toHaveTextContent("The agent hasn't applied the latest settings")
        expect(attention()).toHaveTextContent("bad user")
    })

    it("says so plainly when the address list is still the placeholder", () => {
        state.allow = { prefixes: 2, placeholder: true }
        setup()
        expect(attention()).toHaveTextContent("The Telegram address list is a placeholder")
    })

    it("blames a disconnected VPN, not DNS, and opens the VPN page", async () => {
        state.allow = { prefixes: 14, placeholder: false, last_error: "lookup core.telegram.org on 127.0.0.53:53: i/o timeout" }
        state.vpn = { tunnels: [{ connected: false }, { connected: false }], uplinks: [], kill_switch: true }
        function Where() {
            const l = useLocation()
            return <p>at {l.pathname + l.search}</p>
        }
        render(
            <MemoryRouter initialEntries={["/nodes/4"]}>
                <ConfirmDialogProvider>
                    <Routes>
                        <Route path="/nodes/4" element={<MtprotoOverview node={liteNode} onNavigateTab={navigate} />} />
                        <Route path="/router" element={<Where />} />
                    </Routes>
                </ConfirmDialogProvider>
            </MemoryRouter>,
        )
        expect(attention()).toHaveTextContent("The VPN isn't connected")
        expect(attention()).not.toHaveTextContent("i/o timeout")
        expect(card("Telegram egress")).toHaveTextContent("The VPN isn't connected, so nothing reaches Telegram.")
        await userEvent.click(within(attention()!).getByRole("button", { name: "Open VPN" }))
        expect(screen.getByText("at /router?tab=vpn")).toBeInTheDocument()
    })

    it("keeps the list's own error while a VPN tunnel is connected", () => {
        state.allow = { prefixes: 14, placeholder: false, last_error: "cidr.txt: 503 Service Unavailable" }
        state.vpn = { tunnels: [{ connected: false }, { connected: true }], uplinks: [], kill_switch: true }
        setup()
        expect(attention()).toHaveTextContent("503 Service Unavailable")
        expect(attention()).not.toHaveTextContent("VPN")
    })

    it("offers a repair when the link is down, and keeps link-only buttons off", async () => {
        state.node = fixture({ link: { ...fixture().link!, up: 0, conns: [{ name: "domestic-0", up: false, rtt_ms: 0, streams: 0, error: "dial: i/o timeout" }] } })
        setup()
        const box = attention()!
        expect(box).toHaveTextContent("The panel can't reach the MTProto agent")
        expect(box).toHaveTextContent("dial: i/o timeout")
        expect(within(box).getByRole("button", { name: "Repair…" })).toBeEnabled()
        expect(screen.getByRole("button", { name: "Restart" })).toBeDisabled()
        expect(screen.getByRole("button", { name: "Check again" })).toBeDisabled()
    })

    it("tells the operator to fix NTP past a minute of clock difference, not before", () => {
        const link = fixture().link!
        state.node = fixture({ link: { ...link, conns: [{ ...link.conns![0], skew_ms: 1200 }, { ...link.conns![1], skew_ms: -75400 }] } })
        const { unmount } = setup()
        expect(attention()).toHaveTextContent("The node's clock is off by 75 s")
        unmount()
        state.node = fixture({ link: { ...link, conns: [{ ...link.conns![0], skew_ms: 59000 }] } })
        setup()
        expect(attention()).toBeNull()
    })

    it("offers an update when the panel installs a newer agent", async () => {
        const user = userEvent.setup()
        state.node = fixture({ latest: { agent: "0.2.0", telemt: "3.4.25" }, updates: { agent: true, telemt: false } })
        setup()
        expect(attention()).toHaveTextContent("MTProto agent 0.2.0 is available")
        // the proxy runs what the panel installs, so its card has nothing to update
        expect(screen.queryByRole("button", { name: /Update to/ })).not.toBeInTheDocument()
        // an update runs on click: users only reconnect
        await user.click(within(attention()!).getByRole("button", { name: "Update agent" }))
        expect(await screen.findByRole("dialog")).toHaveTextContent("Updating the agent")
    })

    it("restarts the proxy on click, without asking", async () => {
        const user = userEvent.setup()
        setup()
        await user.click(screen.getByRole("button", { name: "Restart" }))
        expect(state.restart).toHaveBeenCalled()
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    })

    it("stops the proxy from its card, after asking", async () => {
        const user = userEvent.setup()
        setup()
        await user.click(screen.getByRole("button", { name: "Stop" }))
        const dialog = await screen.findByRole("dialog")
        expect(dialog).toHaveTextContent("stays stopped across restarts")
        await user.click(within(dialog).getByRole("button", { name: "Stop proxy" }))
        expect(state.setStopped).toHaveBeenCalledWith(true)
    })

    it("offers Start in place of Restart and Stop once stopped", async () => {
        const user = userEvent.setup()
        const f = fixture({ proxy_stopped: true })
        state.node = { ...f, agent: { ...f.agent!, telemt: { ...f.agent!.telemt, running: false, held: "stopped from the panel" } } }
        setup()
        expect(screen.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument()
        expect(screen.queryByRole("button", { name: "Restart" })).not.toBeInTheDocument()
        expect(attention()).toHaveTextContent("The proxy is stopped")
        expect(attention()).not.toHaveTextContent("holding")
        // the note and the card both offer it
        const starts = screen.getAllByRole("button", { name: "Start proxy" })
        expect(starts).toHaveLength(2)
        await user.click(starts[1])
        expect(state.setStopped).toHaveBeenCalledWith(false)
    })

    it("puts a proxy update at the card's top right only when there is one", () => {
        setup()
        expect(screen.queryByRole("button", { name: /Update to/ })).not.toBeInTheDocument()
        cleanup()
        state.node = fixture({ latest: { agent: "0.1.0", telemt: "3.4.26" }, updates: { agent: false, telemt: true } })
        setup()
        expect(screen.getByRole("button", { name: "Update to 3.4.26" })).toBeInTheDocument()
    })

    it("refreshes the address list from the egress card", async () => {
        const user = userEvent.setup()
        setup()
        await user.click(screen.getByRole("button", { name: "Refresh the Telegram address list" }))
        expect(state.refresh).toHaveBeenCalled()
    })

    it("re-checks the cover site and tests another domain", async () => {
        const user = userEvent.setup()
        state.checkData = { ok: false, tls13: true, x25519: true, h2: false, cert_ok: true, latency_ms: 40, err: "the site does not offer h2", domestic: false }
        state.checkVars = "other.example.ir"
        setup()
        await user.click(screen.getByRole("button", { name: "Check again" }))
        expect(state.check).toHaveBeenCalledWith("www.example.ir")
        await user.click(screen.getByRole("button", { name: "Test another" }))
        const test = screen.getByRole("button", { name: "Test" })
        expect(test).toBeDisabled()
        await user.type(screen.getByLabelText("Domain to test"), "other.example.ir")
        await user.click(test)
        expect(state.check).toHaveBeenCalledWith("other.example.ir")
        const result = screen.getByTestId("disguise-result")
        expect(result).toHaveTextContent("won't work")
        expect(result).toHaveTextContent("the site does not offer h2")
        expect(result).toHaveTextContent("outside your local address ranges")
    })

    it("sends a cover site change to the tunnel, where it is set", async () => {
        const user = userEvent.setup()
        setup()
        await user.click(screen.getByRole("button", { name: "Change…" }))
        expect(navigate).toHaveBeenCalledWith("tunnels")
    })

    it("shows the node's activity with bursts counted", () => {
        state.events = { since: "2026-10-02T09:00:00Z", events: [
            { at: "2026-10-02T10:00:00Z", kind: "refused", level: "warn", count: 7 },
            { at: "2026-10-02T09:30:00Z", kind: "job_ok", level: "info", subject: "update", detail: "agent" },
        ] }
        setup()
        const feed = screen.getByRole("list", { name: "Recent activity" })
        expect(feed).toHaveTextContent("Refused destinations outside Telegram×7")
        expect(feed).toHaveTextContent("Update finished")
    })

    it("shows a cover site the node could not take yet", () => {
        state.node = fixture({ cover_pending: "bad.example.ir can't be used as a cover site from this node: no h2" })
        setup()
        expect(attention()).toHaveTextContent(/bad\.example\.ir can't be used/)
    })

    it("never renders link credentials or the old name", () => {
        const { container } = setup()
        const text = container.textContent!.toLowerCase()
        for (const word of ["token", "reality", "private", "relay"]) expect(text).not.toContain(word)
    })
})
