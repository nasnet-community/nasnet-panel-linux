import { beforeEach, describe, expect, it, vi } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ConnectionChecksDialog } from "@/components/router/connection-checks-dialog"
import type { Setting } from "@/lib/domain/setting"

const mutateAsync = vi.fn()
const toastSuccess = vi.fn()
const toastError = vi.fn()

vi.mock("sonner", () => ({
    toast: {
        success: (m: string) => toastSuccess(m),
        error: (m: string) => toastError(m),
    },
}))

let routerSettings: Setting[] = []

vi.mock("@/lib/queries/use-settings", () => ({
    useSettings: () => ({ data: { router: routerSettings }, isLoading: false }),
    useUpdateSettings: () => ({ mutateAsync, isPending: false }),
}))

function row(key: string, value: string): Setting {
    return { key, value, type: "string", category: "router", description: "", label: "" }
}

/** What the bulk write ended up saying about one key. */
function saved(key: string): string | undefined {
    const payload = mutateAsync.mock.calls.at(-1)?.[0] as Setting[] | undefined
    return payload?.find((s) => s.key === key)?.value
}

function open(
    over: Partial<Parameters<typeof ConnectionChecksDialog>[0]> = {},
) {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return render(
        <QueryClientProvider client={qc}>
            <ConnectionChecksDialog
                open
                onOpenChange={() => {}}
                slot="domestic"
                lineLabel="Fiber"
                siblings={[
                    { slot: "domestic2", label: "ADSL" },
                    { slot: "domestic3", label: "LTE" },
                ]}
                {...over}
            />
        </QueryClientProvider>,
    )
}

beforeEach(() => {
    mutateAsync.mockReset()
    mutateAsync.mockResolvedValue(undefined)
    toastSuccess.mockReset()
    toastError.mockReset()
    routerSettings = []
})

describe("scope", () => {
    it("starts on the shared list and names every line a save would reach", () => {
        open()
        expect(screen.getByRole("button", { name: "All domestic lines" })).toHaveAttribute(
            "aria-pressed",
            "true",
        )
        expect(
            screen.getByText(/changes affect Fiber, ADSL and LTE/),
        ).toBeInTheDocument()
    })

    // A line with its own key is already overridden, so the dialog must open
    // there rather than on the list it is not using.
    it("opens on the line's own list when one is stored", () => {
        routerSettings = [
            row("router_probe_targets_slot_domestic", '[{"address":"10.0.0.9:53","proto":"dns","label":"Head-end"}]'),
        ]
        open()
        expect(screen.getByRole("button", { name: "Just Fiber" })).toHaveAttribute(
            "aria-pressed",
            "true",
        )
        expect(screen.getByText(/apply to Fiber only/)).toBeInTheDocument()
        expect(screen.getByText("Head-end")).toBeInTheDocument()
    })

    it("says so when the line has no siblings to reassure you about", () => {
        open({ siblings: [] })
        expect(screen.getByText(/changes affect Fiber\./)).toBeInTheDocument()
    })

    it("renames the group for a foreign line and mentions its own default", () => {
        open({ slot: "secondary", lineLabel: "Starlink", siblings: [] })
        expect(screen.getByRole("button", { name: "All foreign lines" })).toBeInTheDocument()
        expect(screen.getByText("Cloudflare")).toBeInTheDocument()
        expect(screen.getAllByText("TCP Conn").length).toBeGreaterThan(0)
    })
})

describe("rows", () => {
    it("removes the last custom target and requires a replacement before saving", async () => {
        const user = userEvent.setup()
        routerSettings = [
            row("router_probe_targets_domestic", '[{"address":"10.0.0.9:53","proto":"dns","label":"Old resolver"}]'),
        ]
        open()
        await user.click(screen.getByRole("button", { name: "Remove Old resolver" }))
        expect(screen.queryByText("Old resolver")).toBeNull()
        expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
        expect(screen.getByText("Select at least one server before saving.")).toBeInTheDocument()
        await user.click(screen.getByRole("button", { name: "Save" }))
        expect(mutateAsync).not.toHaveBeenCalled()

        await user.click(screen.getByRole("checkbox", { name: "Iran DNS 1" }))
        await user.click(screen.getByRole("button", { name: "Save" }))
        expect(JSON.parse(saved("router_probe_targets_domestic")!)).toEqual([
            { address: "217.218.155.155:53", proto: "dns", label: "Iran DNS 1" },
        ])
    })
    it("shows each server's address with its port and how it is tested", () => {
        open()
        expect(screen.getByText("217.218.155.155:53")).toBeInTheDocument()
        expect(screen.getAllByText("DNS lookup").length).toBeGreaterThan(0)
    })

    it("keeps a preset but drops an added server on request", async () => {
        const user = userEvent.setup()
        open()
        await user.type(screen.getByLabelText(/Name for the server/), "Head-end")
        await user.type(screen.getByLabelText(/Address of the server/), "10.0.0.9")
        await user.click(screen.getByLabelText("Add this server"))

        expect(screen.getByText("Head-end")).toBeInTheDocument()
        expect(screen.queryByLabelText("Remove Iran DNS 1")).toBeNull()

        await user.click(screen.getByLabelText("Remove Head-end"))
        expect(screen.queryByText("Head-end")).toBeNull()
    })

    // The prober discards these without a word, so the form has to say why.
    it("refuses an address the prober would silently discard", async () => {
        const user = userEvent.setup()
        open()
        await user.type(screen.getByLabelText(/Address of the server/), "dns.google")
        await user.click(screen.getByLabelText("Add this server"))
        expect(screen.getByText(/numeric address/)).toBeInTheDocument()
        expect(screen.queryByText("dns.google:53")).toBeNull()
    })

    it("refuses a server that is already listed", async () => {
        const user = userEvent.setup()
        open()
        await user.type(screen.getByLabelText(/Address of the server/), "217.218.155.155")
        await user.click(screen.getByLabelText("Add this server"))
        expect(screen.getByText(/already listed/)).toBeInTheDocument()
    })

    it("switching to a connection test moves the port to 443", async () => {
        const user = userEvent.setup()
        open()
        expect(screen.getByLabelText("Port")).toHaveValue("53")
        await user.click(within(screen.getByRole("group", { name: "How to test it" })).getByText("TCP Conn"))
        expect(screen.getByLabelText("Port")).toHaveValue("443")
    })
})

describe("limits", () => {
    it("stops at four servers and blocks further ticks", async () => {
        const user = userEvent.setup()
        open()
        for (const host of ["10.0.0.1", "10.0.0.2"]) {
            await user.type(screen.getByLabelText(/Address of the server/), host)
            await user.click(screen.getByLabelText("Add this server"))
        }
        expect(screen.getByLabelText("Add this server")).toBeDisabled()
    })

    // Leaving none checked would let the prober fall back to the defaults, so
    // the list on screen would stop being the truth.
    it("will not let you untick the last server", async () => {
        const user = userEvent.setup()
        routerSettings = [
            row("router_probe_targets_domestic", '[{"address":"10.0.0.1:53","proto":"dns"}]'),
        ]
        open()
        const only = screen.getByLabelText("10.0.0.1:53")
        expect(only).toBeChecked()
        await user.click(only)
        expect(screen.getByLabelText("10.0.0.1:53")).toBeChecked()
    })
})

describe("saving", () => {
    it.each([
        { slot: "domestic2" as const, group: "domestic", other: "foreign" },
        { slot: "secondary2" as const, group: "foreign", other: "domestic" },
    ])("keeps a $group loss threshold change within its group", async ({ slot, group, other }) => {
        const user = userEvent.setup()
        routerSettings = [row("router_degraded_loss_pct", "40")]
        open({ slot, siblings: [] })
        await user.click(screen.getByRole("button", { name: /Advanced/ }))
        const input = screen.getByLabelText("Failing share that counts as slow")
        expect(input).toHaveValue("40")
        await user.clear(input)
        await user.type(input, "60")
        await user.click(screen.getByRole("button", { name: "Save" }))
        expect(saved(`router_degraded_loss_pct_${group}`)).toBe("60")
        expect(saved(`router_degraded_loss_pct_${other}`)).toBeUndefined()
        expect(saved("router_degraded_loss_pct")).toBeUndefined()
    })
    it("writes the group keys and clears any stale override", async () => {
        const user = userEvent.setup()
        open()
        await user.click(screen.getByRole("button", { name: "Save" }))

        await waitFor(() => expect(mutateAsync).toHaveBeenCalled())
        expect(saved("router_probe_targets_domestic")).toContain("217.218.155.155:53")
        expect(saved("router_degraded_rtt_ms_domestic")).toBe("300")
        expect(saved("router_degraded_loss_pct_domestic")).toBe("25")
        expect(saved("router_degraded_loss_pct")).toBeUndefined()
        expect(saved("router_probe_targets_slot_domestic")).toBe("")
        expect(saved("router_degraded_rtt_ms_slot_domestic")).toBe("")
        expect(toastSuccess).toHaveBeenCalledWith("Checks updated for all domestic lines")
    })

    it("writes only this line's keys when scoped to it", async () => {
        const user = userEvent.setup()
        open()
        await user.click(screen.getByRole("button", { name: "Just Fiber" }))
        await user.click(screen.getByRole("button", { name: "Save" }))

        await waitFor(() => expect(mutateAsync).toHaveBeenCalled())
        expect(saved("router_probe_targets_slot_domestic")).toContain("217.218.155.155:53")
        expect(saved("router_probe_targets_domestic")).toBeUndefined()
        expect(toastSuccess).toHaveBeenCalledWith("Checks updated for Fiber")
    })

    it("keeps the name the backend ignores", async () => {
        const user = userEvent.setup()
        open()
        await user.click(screen.getByRole("button", { name: "Just Fiber" }))
        await user.type(screen.getByLabelText(/Name for the server/), "Head-end")
        await user.type(screen.getByLabelText(/Address of the server/), "10.0.0.9")
        await user.click(screen.getByLabelText("Add this server"))
        await user.click(screen.getByRole("button", { name: "Save" }))

        await waitFor(() => expect(mutateAsync).toHaveBeenCalled())
        expect(saved("router_probe_targets_slot_domestic")).toContain('"label":"Head-end"')
    })

    it("hands a line back to the shared list by emptying its keys", async () => {
        const user = userEvent.setup()
        routerSettings = [
            row("router_probe_targets_slot_domestic", '[{"address":"10.0.0.9:53","proto":"dns"}]'),
        ]
        open()
        await user.click(screen.getByRole("button", { name: "Use the shared list" }))

        await waitFor(() => expect(mutateAsync).toHaveBeenCalled())
        expect(saved("router_probe_targets_slot_domestic")).toBe("")
        expect(saved("router_degraded_loss_pct_slot_domestic")).toBe("")
        expect(toastSuccess).toHaveBeenCalledWith("Fiber is back on the shared list")
    })

    it("reports a failed save instead of closing quietly", async () => {
        const user = userEvent.setup()
        mutateAsync.mockRejectedValue(new Error("nope"))
        open()
        await user.click(screen.getByRole("button", { name: "Save" }))
        await waitFor(() => expect(toastError).toHaveBeenCalledWith("nope"))
    })
})

describe("advanced", () => {
    it.each([
        { own: "", expected: "60" },
        { own: "80", expected: "80" },
    ])("reads group and line loss overrides in priority order ($expected)", async ({ own, expected }) => {
        const user = userEvent.setup()
        routerSettings = [
            row("router_degraded_loss_pct", "40"),
            row("router_degraded_loss_pct_domestic", "60"),
            row("router_degraded_loss_pct_foreign", "70"),
            row("router_degraded_loss_pct_slot_domestic", own),
        ]
        open()
        await user.click(screen.getByRole("button", { name: /Advanced/ }))
        expect(screen.getByLabelText("Failing share that counts as slow")).toHaveValue(expected)
    })
    it("stays shut until asked, then offers both thresholds", async () => {
        const user = userEvent.setup()
        open()
        expect(screen.queryByLabelText(/Answer time that counts as slow/)).toBeNull()
        await user.click(screen.getByRole("button", { name: /Advanced/ }))
        expect(screen.getByLabelText(/Answer time that counts as slow/)).toHaveValue("300")
        expect(screen.getByLabelText(/Failing share that counts as slow/)).toHaveValue("25")
    })

    it("offers the foreign ceiling for a foreign line", async () => {
        const user = userEvent.setup()
        open({ slot: "secondary", lineLabel: "Starlink", siblings: [] })
        await user.click(screen.getByRole("button", { name: /Advanced/ }))
        expect(screen.getByLabelText(/Answer time that counts as slow/)).toHaveValue("800")
    })
})
