import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { FlowGraph } from "@/components/flow/flow-graph"
import { MismatchStrip } from "@/components/flow/mismatch-strip"
import type { FlowView, TraceView } from "@/lib/types/flow"

function view(over: Partial<FlowView> = {}): FlowView {
    return {
        generated_unix: 1_800_000_000,
        nodes: [
            { id: "src-lan", kind: "source", label: "LAN clients", sublabel: "3 online", status: "ok" },
            { id: "mark-foreign", kind: "classify", label: "foreign", status: "ok" },
            { id: "table-203", kind: "route", label: "table 203", status: "ok" },
            {
                id: "wg",
                kind: "tunnel",
                label: "WireGuard",
                status: "ghost",
                hint: "No active VPN — activate a profile on the VPN tab.",
            },
            { id: "dns", kind: "dns", label: "DNS", sublabel: "split resolver", status: "ok" },
        ],
        edges: [
            { id: "e-lan-for", from: "src-lan", to: "mark-foreign", kind: "data", status: "ok" },
            {
                id: "e-for-203",
                from: "mark-foreign",
                to: "table-203",
                kind: "data",
                status: "ok",
                counter_key: "nft:foreign",
            },
            { id: "e-dns-lan", from: "src-lan", to: "dns", kind: "dns", status: "ok" },
        ],
        mismatches: [],
        counters: {},
        ...over,
    }
}

function renderGraph(props: Partial<Parameters<typeof FlowGraph>[0]> = {}) {
    const onSelect = vi.fn()
    render(
        <FlowGraph
            flow={view()}
            rates={{}}
            selected={null}
            onSelect={onSelect}
            trace={null}
            showDNS={false}
            {...props}
        />,
    )
    return { onSelect }
}

describe("FlowGraph", () => {
    it("draws every node it has a position for", () => {
        renderGraph()
        expect(screen.getByRole("button", { name: "LAN clients" })).toBeInTheDocument()
        expect(screen.getByRole("button", { name: "WireGuard" })).toBeInTheDocument()
    })

    it("selects a node when it is clicked", async () => {
        const { onSelect } = renderGraph()
        await userEvent.click(screen.getByRole("button", { name: "LAN clients" }))
        expect(onSelect).toHaveBeenCalledWith("src-lan")
    })

    it("marks a ghost node so a missing piece still teaches the topology", () => {
        renderGraph()
        const wg = screen.getByRole("button", { name: "WireGuard" })
        expect(wg).toHaveAttribute("data-status", "ghost")
        // The hint travels as the SVG title, which is the hover text.
        expect(wg).toHaveTextContent("No active VPN")
    })

    it("hides the DNS overlay until it is asked for", () => {
        renderGraph()
        expect(screen.queryByRole("button", { name: "DNS" })).not.toBeInTheDocument()
        expect(document.querySelector('[data-edge="e-dns-lan"]')).toBeNull()
    })

    it("shows the DNS overlay when it is on", () => {
        renderGraph({ showDNS: true })
        expect(screen.getByRole("button", { name: "DNS" })).toBeInTheDocument()
        expect(document.querySelector('[data-edge="e-dns-lan"]')).not.toBeNull()
    })

    it("dims everything off the traced path", () => {
        const trace: TraceView = {
            dest: "1.1.1.1",
            resolved_ip: "1.1.1.1",
            source: "lan",
            steps: [],
            path_nodes: ["src-lan", "mark-foreign", "table-203"],
            path_edges: ["e-lan-for", "e-for-203"],
            final_verdict: "delivered-vpn",
        }
        renderGraph({ trace })
        expect(screen.getByRole("button", { name: "LAN clients" })).not.toHaveAttribute("data-dimmed")
        expect(screen.getByRole("button", { name: "WireGuard" })).toHaveAttribute("data-dimmed")
    })

    it("draws no traffic dots when nothing is flowing", () => {
        renderGraph({ rates: { "nft:foreign": { rxBps: 0, txBps: 0 } } })
        expect(document.querySelector("animateMotion")).toBeNull()
    })

    it("animates an edge that is actually carrying traffic", () => {
        renderGraph({ rates: { "nft:foreign": { rxBps: 4096, txBps: 1024 } } })
        expect(document.querySelector("animateMotion")).not.toBeNull()
    })
})

describe("MismatchStrip", () => {
    it("says so plainly when the kernel agrees", () => {
        render(<MismatchStrip mismatches={[]} onPick={vi.fn()} />)
        expect(screen.getByText(/kernel matches the configuration/i)).toBeInTheDocument()
    })

    it("jumps to the offending node when a chip is clicked", async () => {
        const onPick = vi.fn()
        render(
            <MismatchStrip
                mismatches={[
                    {
                        node_id: "table-203",
                        rule: "route-missing",
                        severity: "error",
                        message: "The tunnel route is missing from table 203.",
                        expected: "default dev nasnet-wg0",
                        actual: "absent",
                    },
                ]}
                onPick={onPick}
            />,
        )
        await userEvent.click(screen.getByText(/tunnel route is missing/i))
        expect(onPick).toHaveBeenCalledWith("table-203")
    })
})
