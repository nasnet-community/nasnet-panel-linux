import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MtprotoLogs } from "../mtproto-logs"
import { formatTimestamp } from "@/components/node/log-format"
import { getMtprotoLogs, getMtprotoNode, type MtprotoNode } from "@/lib/api/mtproto"

vi.mock("@/lib/api/mtproto", async (importOriginal) => ({
    ...(await importOriginal<typeof import("@/lib/api/mtproto")>()),
    getMtprotoLogs: vi.fn(),
    getMtprotoNode: vi.fn(),
}))

const LINES = [
    "MAESTRO: Telemt MTProxy v3.4.25",
    "2026-10-02T10:15:57.148698Z  INFO telemt::maestro: Log level: normal",
    "2026-10-02T10:16:07.660741Z  WARN telemt::maestro::listeners: Skipping IPv6 listener",
    "2026-10-02T10:16:08.000000Z ERROR telemt::proxy: upstream reset",
    "the capture rules are back",
]

const mtprotoNode = (up: number) => ({ node_id: 4, running: true, link: { up, conns: [] } }) as unknown as MtprotoNode

function setup(props: { nodeName?: string } = {}) {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><MtprotoLogs nodeId={4} {...props} /></QueryClientProvider>)
    return userEvent.setup()
}

const pane = () => screen.getByTestId("mtproto-log")
const status = () => screen.getByTestId("mtproto-log-status")

beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getMtprotoLogs).mockResolvedValue({ success: true, data: { lines: LINES } })
    vi.mocked(getMtprotoNode).mockResolvedValue({ success: true, data: mtprotoNode(2) })
})
afterEach(() => vi.useRealTimers())

describe("MtprotoLogs", () => {
    it("splits each line into time, level and message", async () => {
        setup()
        expect(await within(pane()).findByText("telemt::maestro: Log level: normal")).toBeInTheDocument()
        expect(within(pane()).getByText(formatTimestamp(Date.parse("2026-10-02T10:15:57.148Z")))).toBeInTheDocument()
        expect(within(pane()).getByText("Info")).toBeInTheDocument()
        expect(within(pane()).getByText("Warn")).toBeInTheDocument()
        expect(within(pane()).getByText("Error")).toBeInTheDocument()
        // lines that don't parse show raw, with no badge
        const raw = within(pane()).getByText("MAESTRO: Telemt MTProxy v3.4.25").parentElement!
        expect(raw.children).toHaveLength(1)
        expect(status()).toHaveTextContent("Following")
        expect(screen.getByText("Kept in memory on the node, addresses hidden.")).toBeInTheDocument()
        expect(getMtprotoLogs).toHaveBeenCalledWith(4, 200)
    })

    it("filters by level and hides lines without one", async () => {
        const user = setup()
        await within(pane()).findByText("telemt::maestro: Log level: normal")
        await user.click(screen.getByRole("combobox", { name: "Level" }))
        await user.click(await screen.findByRole("option", { name: "Warn" }))
        expect(within(pane()).getByText("telemt::maestro::listeners: Skipping IPv6 listener")).toBeInTheDocument()
        expect(within(pane()).queryByText("telemt::maestro: Log level: normal")).not.toBeInTheDocument()
        expect(within(pane()).queryByText("the capture rules are back")).not.toBeInTheDocument()
    })

    it("highlights search matches and drops the rest", async () => {
        const user = setup()
        await within(pane()).findByText("telemt::maestro: Log level: normal")
        await user.type(screen.getByRole("textbox", { name: "Search the log" }), "capture")
        expect(pane().querySelector("mark")).toHaveTextContent("capture")
        expect(within(pane()).queryByText("telemt::maestro: Log level: normal")).not.toBeInTheDocument()
        await user.clear(screen.getByRole("textbox", { name: "Search the log" }))
        await user.type(screen.getByRole("textbox", { name: "Search the log" }), "nothing like this")
        expect(within(pane()).getByText("No lines match the filters.")).toBeInTheDocument()
    })

    it("keeps only recent lines with Since, unstamped ones going with the line above", async () => {
        const now = Date.now()
        const at = (ago: number) => new Date(now - ago).toISOString()
        vi.mocked(getMtprotoLogs).mockResolvedValue({ success: true, data: { lines: [
            `${at(10 * 60_000)}  INFO old line`,
            "old continuation",
            `${at(20_000)}  INFO fresh line`,
            "fresh continuation",
        ] } })
        const user = setup()
        await within(pane()).findByText("old line")
        await user.click(screen.getByRole("combobox", { name: "Since" }))
        await user.click(await screen.findByRole("option", { name: "Last 1m" }))
        expect(within(pane()).getByText("fresh line")).toBeInTheDocument()
        expect(within(pane()).getByText("fresh continuation")).toBeInTheDocument()
        expect(within(pane()).queryByText("old line")).not.toBeInTheDocument()
        expect(within(pane()).queryByText("old continuation")).not.toBeInTheDocument()
    })

    it("hides the times with the Timestamps switch", async () => {
        const user = setup()
        const time = formatTimestamp(Date.parse("2026-10-02T10:15:57.148Z"))
        expect(await within(pane()).findByText(time)).toBeInTheDocument()
        await user.click(screen.getByRole("switch", { name: "Timestamps" }))
        expect(within(pane()).queryByText(time)).not.toBeInTheDocument()
    })

    it("asks for the chosen number of lines", async () => {
        const user = setup()
        await within(pane()).findByText("telemt::maestro: Log level: normal")
        await user.click(screen.getByRole("combobox", { name: "Lines" }))
        await user.click(await screen.findByRole("option", { name: "1,000 lines" }))
        await waitFor(() => expect(getMtprotoLogs).toHaveBeenCalledWith(4, 1000))
    })

    it("polls every 5 seconds until paused", async () => {
        vi.useFakeTimers({ shouldAdvanceTime: true })
        const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
        const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
        render(<QueryClientProvider client={client}><MtprotoLogs nodeId={4} /></QueryClientProvider>)
        await within(pane()).findByText("telemt::maestro: Log level: normal")
        expect(getMtprotoLogs).toHaveBeenCalledTimes(1)
        await vi.advanceTimersByTimeAsync(5_000)
        await waitFor(() => expect(getMtprotoLogs).toHaveBeenCalledTimes(2))

        await user.click(screen.getByRole("button", { name: "Pause" }))
        expect(status()).toHaveTextContent("Paused")
        await vi.advanceTimersByTimeAsync(15_000)
        expect(getMtprotoLogs).toHaveBeenCalledTimes(2)

        await user.click(screen.getByRole("button", { name: "Resume" }))
        expect(status()).toHaveTextContent("Following")
        await vi.advanceTimersByTimeAsync(5_000)
        await waitFor(() => expect(getMtprotoLogs).toHaveBeenCalledTimes(3))
    })

    it("says the link is down and retries on request", async () => {
        vi.mocked(getMtprotoNode).mockResolvedValue({ success: true, data: mtprotoNode(0) })
        vi.mocked(getMtprotoLogs).mockResolvedValue({ success: false, error: "this MTProto node is not running" })
        const user = setup()
        expect(await within(pane()).findByText("this MTProto node is not running")).toBeInTheDocument()
        await waitFor(() => expect(status()).toHaveTextContent("Link down"))
        vi.mocked(getMtprotoLogs).mockResolvedValue({ success: true, data: { lines: LINES } })
        await user.click(within(pane()).getByRole("button", { name: "Retry" }))
        expect(await within(pane()).findByText("telemt::maestro: Log level: normal")).toBeInTheDocument()
        expect(status()).toHaveTextContent("Following")
    })

    it("says when nothing is logged", async () => {
        vi.mocked(getMtprotoLogs).mockResolvedValue({ success: true, data: { lines: null } })
        setup()
        expect(await within(pane()).findByText("Nothing logged yet.")).toBeInTheDocument()
        expect(screen.getByRole("button", { name: /Download/ })).toBeDisabled()
    })

    it("downloads the raw lines under the node's name", async () => {
        let saved: { name: string; blob: Blob } | undefined
        const blobs = new Map<string, Blob>()
        // jsdom has no object URLs
        URL.createObjectURL = (b: Blob) => { blobs.set("blob:1", b); return "blob:1" }
        URL.revokeObjectURL = () => {}
        const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
            saved = { name: this.download, blob: blobs.get(this.href)! }
        })
        const user = setup({ nodeName: "VPS 1" })
        await within(pane()).findByText("telemt::maestro: Log level: normal")
        await user.click(screen.getByRole("button", { name: /Download/ }))
        const d = new Date()
        const day = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`
        expect(saved?.name).toBe(`VPS-1-mtproto-agent-${day}.log`)
        expect(await saved!.blob.text()).toBe(LINES.join("\n") + "\n")
        click.mockRestore()
    })

    it("stops following when scrolled up and jumps back on request", async () => {
        setup()
        await within(pane()).findByText("telemt::maestro: Log level: normal")
        const el = pane()
        Object.defineProperty(el, "scrollHeight", { configurable: true, value: 1000 })
        Object.defineProperty(el, "clientHeight", { configurable: true, value: 200 })
        el.scrollTop = 100
        fireEvent.scroll(el)
        const jump = await screen.findByRole("button", { name: "Jump to latest" })
        fireEvent.click(jump)
        expect(el.scrollTop).toBe(1000)
        expect(screen.queryByRole("button", { name: "Jump to latest" })).not.toBeInTheDocument()
    })

    it("goes full screen and back", async () => {
        const user = setup()
        await user.click(screen.getByRole("button", { name: "Full screen" }))
        expect(screen.getByRole("button", { name: "Exit full screen" })).toBeInTheDocument()
        await user.keyboard("{Escape}")
        expect(screen.getByRole("button", { name: "Full screen" })).toBeInTheDocument()
    })
})
