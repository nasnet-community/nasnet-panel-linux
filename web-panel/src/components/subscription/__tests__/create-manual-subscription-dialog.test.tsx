import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { CreateManualSubscriptionDialog } from "../create-manual-subscription-dialog"
import { useSubscriptionsStore } from "@/store/subscriptions-store"
import { listNodes } from "@/lib/api/nodes"
import { listAllCoreInbounds } from "@/lib/api/cores"
import { createManualSubscription } from "@/lib/api/subscriptions"
import { toast } from "@/lib/toast"
import type { Inbound, Node } from "@/lib/types"
import type { CoreInbound } from "@/lib/types/cores"

vi.mock("@/lib/api/nodes", () => ({ listNodes: vi.fn() }))
vi.mock("@/lib/api/cores", () => ({ listAllCoreInbounds: vi.fn() }))
vi.mock("@/lib/api/subscriptions", async (importOriginal) => ({
    ...(await importOriginal<typeof import("@/lib/api/subscriptions")>()),
    createManualSubscription: vi.fn(),
}))
vi.mock("@/lib/toast", () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }))

const vless = { id: 10, tag: "vless-in", protocol: "vless", port: 8443, network: "tcp", security: "reality", remark: "" } as Inbound
const nodes = [
    { id: 1, name: "Frankfurt", country_code: "DE", kind: "full", is_online: true, is_active: true, inbounds: [vless] },
    { id: 4, name: "VPS 1", country_code: "NL", kind: "lite", is_online: true, is_active: true, inbounds: [] },
    { id: 5, name: "Spare", country_code: "FI", kind: "full", is_online: true, is_active: true, inbounds: [] },
] as unknown as Node[]
const tunnel = (over: Partial<CoreInbound>) => ({ listen: "", egress_mode: "direct", settings: null, remark: "", is_disabled: false, created_at: "", updated_at: "", ...over }) as CoreInbound
const tunnels = [
    tunnel({ id: 20, node_id: 4, protocol: "mtproto", port: 443, settings: { tls_domain: "www.example.com" } }),
    tunnel({ id: 21, node_id: 1, protocol: "ssh", port: 2222, remark: "office" }),
    tunnel({ id: 22, node_id: 1, protocol: "ssh", port: 2223, is_disabled: true }),
]

function setup() {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
    useSubscriptionsStore.setState({ createManualDialog: { open: true } })
    render(<QueryClientProvider client={client}><CreateManualSubscriptionDialog /></QueryClientProvider>)
    return userEvent.setup()
}

const nextButton = () => screen.getByRole("button", { name: /next/i })

async function expandNode(user: ReturnType<typeof userEvent.setup>, name: string) {
    await user.click(await screen.findByRole("button", { name: new RegExp(name) }))
}

beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} })
    vi.mocked(listNodes).mockResolvedValue({ success: true, data: nodes })
    vi.mocked(listAllCoreInbounds).mockResolvedValue({ success: true, data: { inbounds: tunnels } })
    vi.mocked(createManualSubscription).mockResolvedValue({ success: true, data: { id: 9 } as never })
})
afterEach(() => vi.unstubAllGlobals())

describe("CreateManualSubscriptionDialog servers step", () => {
    it("lists each node's tunnels under it", async () => {
        const user = setup()
        await expandNode(user, "VPS 1")
        const mtproto = await screen.findByRole("checkbox", { name: "MTProto · port 443" })
        expect(mtproto).toBeInTheDocument()
        expect(screen.getByText("www.example.com")).toBeInTheDocument()
        expect(screen.queryByText("No inbounds configured")).toBeNull()

        await expandNode(user, "Frankfurt")
        expect(screen.getByRole("checkbox", { name: "SSH · port 2222" })).toBeInTheDocument()
        expect(screen.getByText("office")).toBeInTheDocument()
        expect(screen.queryByRole("checkbox", { name: "SSH · port 2223" })).toBeNull()
    })

    it("keeps the empty state for a node with neither", async () => {
        const user = setup()
        await expandNode(user, "Spare")
        expect(await screen.findByText("No inbounds configured")).toBeInTheDocument()
    })

    it("counts tunnels and inbounds together", async () => {
        const user = setup()
        await expandNode(user, "Frankfurt")
        await user.click(await screen.findByRole("checkbox", { name: "SSH · port 2222" }))
        expect(screen.getByText("1 selected")).toBeInTheDocument()
        await user.click(screen.getAllByRole("checkbox")[0])
        expect(screen.getByText("2 selected")).toBeInTheDocument()
    })

    it("search matches tunnels", async () => {
        const user = setup()
        await screen.findByText("Frankfurt")
        await user.type(screen.getByPlaceholderText(/search/i), "mtproto")
        expect(screen.getByText("VPS 1")).toBeInTheDocument()
        expect(screen.queryByText("Frankfurt")).toBeNull()
    })
})

describe("CreateManualSubscriptionDialog tunnel-only create", () => {
    async function pickMtprotoTunnelAndReview(user: ReturnType<typeof userEvent.setup>) {
        expect(nextButton()).toBeDisabled()
        await expandNode(user, "VPS 1")
        await user.click(await screen.findByRole("checkbox", { name: "MTProto · port 443" }))
        expect(nextButton()).toBeEnabled()
        for (let i = 0; i < 3; i++) await user.click(nextButton())
    }

    it("reviews the tunnel and sends it as core_inbound_ids", async () => {
        const user = setup()
        await pickMtprotoTunnelAndReview(user)

        const review = screen.getByRole("region", { name: "Selected tunnels" })
        expect(within(review).getByText("VPS 1")).toBeInTheDocument()
        expect(within(review).getByText("MTProto")).toBeInTheDocument()
        expect(within(review).getByText(":443")).toBeInTheDocument()
        expect(screen.queryByRole("region", { name: "Selected inbounds" })).toBeNull()

        await user.click(screen.getByRole("button", { name: /create subscription/i }))
        await waitFor(() => expect(createManualSubscription).toHaveBeenCalled())
        expect(vi.mocked(createManualSubscription).mock.calls[0][0]).toMatchObject({ inbound_ids: [], core_inbound_ids: [20] })
        await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Manual subscription created"))
        expect(toast.warning).not.toHaveBeenCalled()
    })

    it("toasts the API's warnings after a create", async () => {
        vi.mocked(createManualSubscription).mockResolvedValue({
            success: true,
            data: { id: 9 } as never,
            warnings: ["MTProto tunnel on port 443 (VPS 1) was not attached: boom"],
        })
        const user = setup()
        await pickMtprotoTunnelAndReview(user)
        await user.click(screen.getByRole("button", { name: /create subscription/i }))

        await waitFor(() => expect(toast.warning).toHaveBeenCalled())
        const [title, opts] = vi.mocked(toast.warning).mock.calls[0]
        expect(title).toMatch(/tunnel/i)
        expect(String((opts as { description?: unknown })?.description)).toContain("was not attached: boom")
        expect(toast.success).toHaveBeenCalledWith("Manual subscription created")
    })
})
