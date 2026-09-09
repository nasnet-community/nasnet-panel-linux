import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ComponentProps, ReactNode } from "react"
import { OutboundSettingsDialog } from "../outbound-settings-dialog"
import type { Outbound } from "@/lib/types"
import type { ConnectionDialogShell } from "@/components/connection-dialog/connection-dialog-shell"

vi.mock("@/components/ui/confirm-dialog", () => ({ useConfirm: () => vi.fn().mockResolvedValue(true) }))
vi.mock("@/components/connection-dialog/connection-dialog-shell", () => ({
    ConnectionDialogShell: ({ children, onPrimary, secondaryAction, loading }: ComponentProps<typeof ConnectionDialogShell>) => <div>
        {children}
        <button onClick={onPrimary} disabled={loading}>Save</button>
        {secondaryAction && <button onClick={secondaryAction.onClick} disabled={loading}>{secondaryAction.label}</button>}
    </div>,
}))
vi.mock("../tabs/general-tab", () => ({ GeneralTab: () => <div>General fields</div> }))
vi.mock("../tabs/protocol-tab", () => ({ ProtocolTab: () => null }))
vi.mock("../tabs/transport-tab", () => ({ TransportTab: () => null }))
vi.mock("../tabs/advanced-tab", () => ({ AdvancedTab: () => null }))
vi.mock("@/components/ui/form", () => ({ Form: ({ children }: { children: ReactNode }) => <>{children}</> }))

afterEach(cleanup)
const outbound = { id: 7, node_id: 1, tag: "direct-out", protocol: "freedom", network: "tcp", security: "none", freedom_settings: { domainStrategy: "AsIs" } } as Outbound

describe("outbound Save & test", () => {
    it("keeps the editor open and never tests after the save rejects", async () => {
        const onSave = vi.fn().mockRejectedValue(new Error("A routing rule still references this tag"))
        const onTest = vi.fn()
        const onOpenChange = vi.fn()
        render(<OutboundSettingsDialog open mode="edit" nodeId={1} outbound={outbound} onSave={onSave} onTest={onTest} onOpenChange={onOpenChange} />)
        fireEvent.click(screen.getByRole("button", { name: "Save & test" }))
        await waitFor(() => expect(onSave).toHaveBeenCalledOnce())
        await waitFor(() => expect(screen.getByRole("button", { name: "Save & test" })).toBeEnabled())
        expect(onTest).not.toHaveBeenCalled()
        expect(onOpenChange).not.toHaveBeenCalled()
    })

    it("waits for a successful save before invoking the probe", async () => {
        let finishSave!: () => void
        const onSave = vi.fn(() => new Promise<void>(resolve => { finishSave = resolve }))
        const onTest = vi.fn()
        const onOpenChange = vi.fn()
        render(<OutboundSettingsDialog open mode="edit" nodeId={1} outbound={outbound} onSave={onSave} onTest={onTest} onOpenChange={onOpenChange} />)
        fireEvent.click(screen.getByRole("button", { name: "Save & test" }))
        await waitFor(() => expect(onSave).toHaveBeenCalledOnce())
        expect(onTest).not.toHaveBeenCalled()
        finishSave()
        await waitFor(() => expect(onTest).toHaveBeenCalledWith(expect.objectContaining({ id: 7, tag: "direct-out" })))
        expect(onOpenChange).toHaveBeenCalledWith(false)
    })

    it.each([
        { ...outbound, managed: true, id: 0 },
        { ...outbound, protocol: "http", address: "127.0.0.1", port: 8080 },
    ])("offers no probe for generated rows or unsupported HTTP outbounds", row => {
        render(<OutboundSettingsDialog open mode="edit" nodeId={1} outbound={row} onSave={vi.fn()} onTest={vi.fn()} onOpenChange={vi.fn()} />)
        expect(screen.queryByRole("button", { name: "Save & test" })).not.toBeInTheDocument()
    })
})
