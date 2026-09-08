import { beforeEach, describe, expect, it, vi } from "vitest"
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router"
import { UplinkDetailSheet } from "@/components/router/uplink-detail-sheet"
import type { HealthSample, UplinkHealth, VPNPoolHealth } from "@/lib/types/health"
import type { NetworkInterfaceView, PortForward, PortMapStatus } from "@/lib/types/network"

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

let forwards: PortForward[] = []
let portmap: PortMapStatus | undefined
let flowEvents: { type: string; timestamp: string; payload: unknown }[] = []

vi.mock("@/lib/queries/use-network", () => ({
    usePortForwards: () => ({ data: forwards }),
    usePortMapStatus: () => ({ data: portmap }),
    useSetInterfaceLabel: () => ({ mutateAsync: vi.fn() }),
}))

vi.mock("@/lib/queries/use-flow", () => ({
    useFlowEvents: () => ({ data: flowEvents }),
}))

vi.mock("@/lib/queries/use-settings", () => ({
    useSettings: () => ({ data: { router: [] }, isLoading: false }),
    useUpdateSettings: () => ({ mutateAsync: vi.fn(), isPending: false }),
}))

function samples(n: number): HealthSample[] {
    return Array.from({ length: n }, (_, i) => ({ unix: i, ok_ratio: 1, rtt_ms: 20 }))
}

function up(over: Partial<UplinkHealth> = {}): UplinkHealth {
    return {
        slot: "domestic",
        if_name: "eth0",
        carrier: "up",
        gateway: "up",
        internet: "up",
        verdict: "up",
        via: "",
        force_state: "",
        degraded: false,
        loss_pct: 0,
        median_rtt_ms: 18,
        targets: [
            { address: "217.218.155.155:53", proto: "dns", label: "Iran DNS 1", ok: true, rtt_ms: 17 },
            { address: "178.22.122.100:53", proto: "dns", ok: false, rtt_ms: 0, error: "timeout" },
        ],
        history: samples(30),
        gateway_ip: "10.20.0.1",
        since_unix: Math.round(Date.now() / 1000) - 3600,
        rx_bytes: 1024 * 1024 * 412,
        tx_bytes: 1024 * 1024 * 61,
        routes: ["default via 10.20.0.1 dev eth0", "10.20.0.0/24 dev eth0 scope link"],
        ...over,
    }
}

function iface(over: Partial<NetworkInterfaceView> = {}): NetworkInterfaceView {
    return {
        id: 1,
        if_name: "eth0",
        phy: "",
        perm_mac: "3c:ec:ef:12:ab:cd",
        id_path: "",
        key: "3c:ec:ef:12:ab:cd",
        key_kind: "permaddr",
        source: "eth_pci",
        confidence: 100,
        driver: "igc",
        carrier: true,
        oper_state: "up",
        speed_mbit: 1000,
        mtu: 1500,
        usb_speed_mbit: 0,
        assignable: true,
        addrs: ["10.20.0.15/24"],
        role: "wan",
        slot: "domestic",
        label: "Fiber",
        present: true,
        healthy: true,
        ...over,
    }
}

function open(over: Partial<Parameters<typeof UplinkDetailSheet>[0]> = {}) {
    const self = over.up ?? up()
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return render(
        <MemoryRouter>
            <QueryClientProvider client={qc}>
                <UplinkDetailSheet
                    open
                    onOpenChange={() => {}}
                    up={self}
                    iface={iface()}
                    group={[self]}
                    labelOf={(n) => ({ eth0: "Fiber", eth1: "ADSL", wwan0: "LTE" })[n] ?? n}
                    {...over}
                />
            </QueryClientProvider>
        </MemoryRouter>,
    )
}

beforeEach(() => {
    forwards = []
    portmap = undefined
    flowEvents = []
})

describe("identity", () => {
    it("leads with the operator's name and what the line is for", () => {
        open()
        expect(screen.getByRole("dialog")).toHaveTextContent("Fiber")
        // One line on the box, so there is no order to describe.
        expect(screen.getByText("Internet line")).toBeInTheDocument()
    })

    it("says where the line sits in the order once there is one", () => {
        const a = up()
        const b = up({ if_name: "eth1", slot: "domestic2" })
        open({ up: a, group: [a, b] })
        expect(screen.getByText("Main internet line · used first")).toBeInTheDocument()
    })

    it("shows the verdict in plain words", () => {
        open({ up: up({ verdict: "degraded" }) })
        expect(screen.getAllByText("slow").length).toBeGreaterThan(0)
    })

    // It sits beside the name, so it reads as part of the title.
    it("says how long the line has been this way, next to its name", () => {
        open()
        expect(screen.getByText("(up for 1 hour)")).toBeInTheDocument()
    })

    it("keeps the readout free of the noise the sections already carry", () => {
        open()
        expect(screen.queryByText(/Quality good/)).toBeNull()
        expect(screen.queryByText(/slow past/)).toBeNull()
        expect(screen.queryByRole("link", { name: /Traffic flow/ })).toBeNull()
        expect(screen.queryByText(/moves to the next working line/)).toBeNull()
    })

    // A virtio NIC reports -1 for its link speed.
    it("does not print an unreported speed as a number", () => {
        open({ up: up(), iface: { ...iface(), speed_mbit: -1 } })
        expect(screen.queryByText(/-1 Mbit/)).toBeNull()
        expect(screen.getByText("Cable connected")).toBeInTheDocument()
        expect(screen.getByText("not reported")).toBeInTheDocument()
    })

    // Without a name the row printed its address twice.
    it("shows an unnamed check's address once", () => {
        open({ up: up({ targets: [{ address: "9.9.9.9:53", proto: "dns", ok: true, rtt_ms: 12 }] }) })
        expect(screen.getAllByText("9.9.9.9:53")).toHaveLength(1)
    })
})

describe("ladder", () => {
    // The rung strings are up/down/unknown, which name a layer but explain
    // nothing, so each one gets a sentence.
    it("explains every rung, and names the device the gateway dials", () => {
        open()
        expect(screen.getByText("Cable connected · 1 Gbit/s")).toBeInTheDocument()
        expect(screen.getByText("10.20.0.1 answers")).toBeInTheDocument()
        expect(screen.getByText(/Internet reachable · 1 of 2 checks answered/)).toBeInTheDocument()
    })

    it("turns the failing rung red and says what did not answer", () => {
        open({
            up: up({
                internet: "down",
                verdict: "no-internet",
                targets: [{ address: "1.1.1.1:443", proto: "tcp", ok: false, rtt_ms: 0 }],
            }),
        })
        expect(screen.getByText("Not reachable · nothing answered")).toBeInTheDocument()
    })
})

describe("failover", () => {
    it("tells you nothing needs doing while a sibling carries the line", () => {
        const self = up({ if_name: "eth1", slot: "domestic2", via: "eth0", verdict: "no-internet" })
        open({ up: self, group: [up(), self] })
        expect(screen.getByText(/using Fiber/)).toBeInTheDocument()
        expect(screen.getByText(/Nothing to do/)).toBeInTheDocument()
    })

    it("lists the backup order with a role for each line", () => {
        const a = up()
        const b = up({ if_name: "eth1", slot: "domestic2" })
        const c = up({ if_name: "wwan0", slot: "domestic3", verdict: "no-carrier", carrier: "down" })
        open({ up: a, group: [a, b, c] })

        expect(within(member("eth0")).getByText(/Carrying traffic now/)).toBeInTheDocument()
        expect(within(member("eth1")).getByText("Ready to take over")).toBeInTheDocument()
        expect(within(member("wwan0")).getByText("No cable or SIM detected")).toBeInTheDocument()
    })

    it("shows the recovery count only while the damper holds it down", () => {
        expect(screen.queryByText(/checks passed/)).toBeNull()
        open({
            up: up({
                verdict: "no-internet",
                internet: "down",
                via: "eth0",
                recovery: { passes: 4, needed: 12, dwell_seconds_left: 80 },
            }),
            group: [up({ if_name: "eth0" })],
        })
        expect(screen.getByText("4 of 12 checks passed")).toBeInTheDocument()
    })

    it("replaces the backup order with the tunnels on a foreign line", () => {
        const vpn: VPNPoolHealth = {
            present: true,
            strategy: "spread",
            loss_pct: 1,
            median_rtt_ms: 96,
            pool_history: [],
            tunnels: [
                {
                    profile_id: 1,
                    name: "Hetzner-FRA",
                    if_name: "wg0",
                    position: 0,
                    in_pool: true,
                    verdict: "up",
                    degraded: false,
                    loss_pct: 0,
                    median_rtt_ms: 90,
                    targets: [],
                    history: [],
                },
            ],
        }
        const self = up({ slot: "secondary", if_name: "enx0" })
        open({ up: self, group: [self], vpn })
        expect(screen.getByText("VPN tunnels on this line")).toBeInTheDocument()
        expect(screen.getByText("Hetzner-FRA")).toBeInTheDocument()
        expect(screen.queryByText("Backup order")).toBeNull()
    })
})

describe("address", () => {
    // The two labelled rows this replaced never said which address was which.
    it("draws the path out, naming both ends", () => {
        open()
        // "internet" is also a rung label, so scope to the drawing.
        expect(screen.getAllByText("internet").length).toBeGreaterThan(0)
        expect(screen.getByText("upstream router")).toBeInTheDocument()
        expect(screen.getByText("this box")).toBeInTheDocument()
        expect(screen.getByText("10.20.0.1")).toBeInTheDocument()
        expect(screen.getByText("10.20.0.15/24")).toBeInTheDocument()
    })

    it("marks the broken hop when the internet is unreachable", () => {
        open({ up: up({ internet: "down", verdict: "no-internet" }) })
        expect(screen.getByRole("img", { name: "blocked here" })).toBeInTheDocument()
    })
})

describe("facts", () => {
    it("reports the data used in units a person reads", () => {
        open()
        expect(screen.getByText("412 MB")).toBeInTheDocument()
        expect(screen.getByText("61 MB")).toBeInTheDocument()
    })

    it("lists the checks with names, addresses and timings", () => {
        open()
        expect(screen.getByText("Iran DNS 1")).toBeInTheDocument()
        expect(screen.getByText("17 ms")).toBeInTheDocument()
        expect(screen.getByText("no answer")).toBeInTheDocument()
    })

    it("says port mapping is off rather than implying a failure", () => {
        portmap = { enabled: false, wans: [] }
        open()
        expect(screen.getByText(/Port mapping is turned off/)).toBeInTheDocument()
    })

    it("reports this line's forwards and ignores another line's", () => {
        forwards = [
            { id: 1, uplink_key: "3c:ec:ef:12:ab:cd", proto: "tcp", dport: 443, to_addr: "10.10.0.5", to_port: 443, comment: "nginx", enabled: true },
            { id: 2, uplink_key: "other", proto: "tcp", dport: 22, to_addr: "10.10.0.9", to_port: 22, comment: "ssh", enabled: true },
        ]
        open()
        expect(screen.getByText("Port 443")).toBeInTheDocument()
        expect(screen.queryByText("Port 22")).toBeNull()
    })

    it("shows only this line's events, as outcomes", () => {
        flowEvents = [
            { type: "wan.up", timestamp: new Date().toISOString(), payload: { if_name: "eth0" } },
            { type: "wan.down", timestamp: new Date().toISOString(), payload: { if_name: "eth9" } },
        ]
        open()
        expect(screen.getByText("Back to normal")).toBeInTheDocument()
        expect(screen.queryByText("Lost internet")).toBeNull()
    })
})

describe("technical details", () => {
    it("keeps the jargon shut until asked", async () => {
        const user = userEvent.setup()
        open()
        expect(screen.queryByText("igc")).toBeNull()

        await user.click(screen.getByRole("button", { name: /Technical details/ }))
        expect(screen.getByText("igc")).toBeInTheDocument()
        expect(screen.getByText("3c:ec:ef:12:ab:cd")).toBeInTheDocument()
        expect(screen.getByText(/default via 10.20.0.1 dev eth0/)).toBeInTheDocument()
    })
})

function member(ifName: string): HTMLElement {
    return document.querySelector(`[data-member="${ifName}"]`) as HTMLElement
}
