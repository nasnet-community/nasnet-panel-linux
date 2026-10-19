import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it, vi } from "vitest"
import { RouterDangerZone } from "../router-danger-zone"
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog"
import { useNodes } from "@/lib/queries"
import type { Node } from "@/lib/types"

vi.mock("@/lib/queries", () => ({ useNodes: vi.fn() }))

const nodes = [
    { id: 2, name: "fra-1", connect_mode: "direct", is_online: true },
    { id: 1, name: "Home router", connect_mode: "local", is_online: true },
] as Node[]

function setup() {
    render(<MemoryRouter><ConfirmDialogProvider><RouterDangerZone /></ConfirmDialogProvider></MemoryRouter>)
}

describe("the router's danger zone in panel settings", () => {
    it("offers Wipe and Nuke for this router and says the panel goes too", () => {
        vi.mocked(useNodes).mockReturnValue({ data: nodes, isError: false } as unknown as ReturnType<typeof useNodes>)
        setup()
        expect(screen.getAllByRole("heading", { name: "This router" })).toHaveLength(1)
        expect(screen.getByText(/The panel is deleted too/)).toBeInTheDocument()
        expect(screen.getByText(/then the panel itself/)).toBeInTheDocument()
        expect(screen.getByRole("button", { name: "Wipe…" })).toBeEnabled()
        expect(screen.getByRole("button", { name: "Nuke…" })).toBeEnabled()
        // the built-in node's record can't be removed
        expect(screen.queryByRole("button", { name: /Delete/ })).not.toBeInTheDocument()
    })

    it("waits for the node list", () => {
        vi.mocked(useNodes).mockReturnValue({ data: undefined, isError: false } as unknown as ReturnType<typeof useNodes>)
        setup()
        expect(screen.getByText("Loading…")).toBeInTheDocument()
        expect(screen.queryByRole("button", { name: "Wipe…" })).not.toBeInTheDocument()
    })
})
