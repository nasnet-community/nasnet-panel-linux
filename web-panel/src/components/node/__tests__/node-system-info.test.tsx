import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { NodeSystemInfo } from "../node-system-info"
import type { Node, NodeOverview } from "@/lib/types"

vi.mock("@/lib/queries/use-nodes", () => ({ useNodeHostInfo: () => ({ data: undefined, isLoading: false }) }))

const node = { id: 2, name: "Frankfurt", ip: "203.0.113.2", kind: "full", connect_mode: "direct", is_online: true } as Node

function overview(agent: Partial<NodeOverview["agent"]>): NodeOverview {
    return {
        connection: { mode: "direct", rtt_ms: 20 },
        agent: { version: "v1.1.0-58-g1d8940b", bundled_version: "v1.1.0-58-g1d8940b", update_available: false, ...agent },
        config: { state: "in_sync" },
        today: { uplink: 0, downlink: 0, peak_bps: 0 },
    } as NodeOverview
}

describe("NodeSystemInfo agent row", () => {
    it("calls a rebuild of the running version a new build", () => {
        render(<NodeSystemInfo node={node} overview={overview({ update_available: true, build: "aaaa", bundled_build: "bbbb" })} />)
        expect(screen.getByText("New build available")).toBeInTheDocument()
    })

    it("names a newer version", () => {
        render(<NodeSystemInfo node={node} overview={overview({ update_available: true, bundled_version: "v1.2.0" })} />)
        expect(screen.getByText("v1.2.0 available")).toBeInTheDocument()
    })

    it("shows which build runs when there is nothing new", () => {
        render(<NodeSystemInfo node={node} overview={overview({ build: "0123456789abcdef" })} />)
        expect(screen.getByText("build 01234567")).toBeInTheDocument()
        expect(screen.queryByText(/available/)).not.toBeInTheDocument()
    })
})
