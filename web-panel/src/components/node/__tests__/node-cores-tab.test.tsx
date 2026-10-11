import { render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { NodeCoresTab } from "../node-cores-tab"
import { useNodeDeps } from "@/lib/queries"
import type { Node } from "@/lib/types"

vi.mock("@/lib/queries", () => ({
    useCoreInbounds: () => ({ data: [], isLoading: false }),
    useNodeProtocols: () => ({ data: [] }),
    useNodeDeps: vi.fn(() => ({ data: [] })),
}))
vi.mock("@/components/cores/core-list", () => ({ CoreList: () => null }))
vi.mock("@/components/cores/core-detail-modal", () => ({ CoreDetailModal: () => null }))
vi.mock("@/components/cores/create-core-dialog", () => ({ CreateCoreDialog: () => null }))
vi.mock("@/components/cores/node-protocols-dialog", () => ({ NodeProtocolsDialog: () => null }))
vi.mock("@/components/node/node-protocols-panel", () => ({ countDisabledProtocols: () => 0 }))
vi.mock("@/components/node/node-setup-tab", () => ({ TunnelDependencies: () => <section>Dependencies</section> }))

const full = { id: 1, name: "n1", ip: "203.0.113.1", kind: "full", connect_mode: "direct", is_online: true } as Node
const lite = { id: 4, name: "VPS 1", ip: "203.0.113.9", kind: "lite", connect_mode: "link", is_online: true } as Node

beforeEach(() => vi.clearAllMocks())

describe("NodeCoresTab", () => {
    it("shows a full node's dependencies and the Xray wording", () => {
        render(<NodeCoresTab nodeId={1} node={full} />)
        expect(screen.getByText("Dependencies")).toBeInTheDocument()
        expect(screen.getByText(/alongside Xray/)).toBeInTheDocument()
    })

    it("on an MTProto node there is nothing to install and no Xray", () => {
        const { container } = render(<NodeCoresTab nodeId={4} node={lite} />)
        expect(screen.queryByText("Dependencies")).toBeNull()
        expect(container.textContent).not.toMatch(/xray/i)
        for (const [, enabled] of vi.mocked(useNodeDeps).mock.calls) expect(enabled).toBe(false)
    })
})
