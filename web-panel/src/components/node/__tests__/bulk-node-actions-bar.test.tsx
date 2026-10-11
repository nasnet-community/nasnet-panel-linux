import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { BulkNodeActionsBar } from "../bulk-node-actions-bar"
import { useNodesStore } from "@/store/nodes-store"
import type { Node } from "@/lib/types"

const restart = vi.fn()
const push = vi.fn()

vi.mock("@/lib/queries", () => ({
    useBulkRestartNodes: () => ({ mutate: restart, isPending: false }),
    useBulkPushNodeConfig: () => ({ mutate: push, isPending: false }),
    useBulkCheckNodeHealth: () => ({ mutate: vi.fn(), isPending: false }),
    useNodes: () => ({ data: nodes }),
}))
vi.mock("@/components/ui/confirm-dialog", () => ({ useConfirm: () => async () => true }))
vi.mock("@/components/node/bulk-agent-update-dialog", () => ({
    BulkAgentUpdateDialog: ({ selectedIds }: { selectedIds: number[] }) => <p>agents for {selectedIds.join(",")}</p>,
}))
let agentDue: Record<string, boolean> | undefined = { "1": true }
vi.mock("@/lib/queries/use-bulk-agent-update", () => ({ useAgentUpdates: () => ({ data: agentDue }) }))
vi.mock("@/components/node/bulk-xray-version-dialog", () => ({
    BulkXrayVersionDialog: ({ open, selectedIds }: { open: boolean; selectedIds: number[] }) => (open ? <p>xray for {selectedIds.join(",")}</p> : null),
}))

const nodes = [
    { id: 1, name: "full", ip: "203.0.113.1", kind: "full", connect_mode: "direct", is_online: true },
    { id: 2, name: "current", ip: "203.0.113.2", kind: "full", connect_mode: "direct", is_online: true },
    { id: 4, name: "mtproto", ip: "203.0.113.9", kind: "lite", connect_mode: "link", is_online: true },
] as Node[]

beforeEach(() => {
    vi.clearAllMocks()
    useNodesStore.getState().clearSelection()
    agentDue = { "1": true, "2": false }
})

describe("BulkNodeActionsBar", () => {
    it("updates Xray on the full nodes only", async () => {
        const user = userEvent.setup()
        useNodesStore.getState().setSelected([1, 4])
        render(<BulkNodeActionsBar />)
        await user.click(screen.getAllByRole("button", { name: /Update Xray/ })[0])
        expect(screen.getByText("xray for 1")).toBeInTheDocument()
    })

    it("leaves MTProto nodes out of the Xray actions", async () => {
        const user = userEvent.setup()
        useNodesStore.getState().setSelected([1, 4])
        render(<BulkNodeActionsBar />)
        await user.click(screen.getAllByRole("button", { name: /Restart Xray/ })[0])
        expect(restart).toHaveBeenCalledWith([1], expect.anything())
        await user.click(screen.getAllByRole("button", { name: /Push Config/i })[0])
        expect(push).toHaveBeenCalledWith([1], expect.anything())
    })

    it("disables the Xray actions when only MTProto nodes are selected", () => {
        useNodesStore.getState().setSelected([4])
        render(<BulkNodeActionsBar />)
        for (const b of screen.getAllByRole("button", { name: /Restart Xray|Push Config|Update Xray/i })) {
            expect(b).toBeDisabled()
        }
    })

    it("updates agents only where the panel ships a new one", async () => {
        const user = userEvent.setup()
        useNodesStore.getState().setSelected([1, 2, 4])
        render(<BulkNodeActionsBar />)
        await user.click(screen.getAllByRole("button", { name: /Update Agent/i })[0])
        expect(screen.getByText("agents for 1")).toBeInTheDocument()
    })

    it("turns the agent update off when the selected nodes run the panel's agent", () => {
        agentDue = { "1": false, "2": false }
        useNodesStore.getState().setSelected([1, 2])
        render(<BulkNodeActionsBar />)
        for (const b of screen.getAllByRole("button", { name: /Update Agent/i })) {
            expect(b).toBeDisabled()
            expect(b).toHaveAttribute("title", "Already up to date")
        }
    })
})
