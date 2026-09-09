import { createEvent, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { NodeTerminal } from "../node-terminal"
import { useTerminal } from "../terminal/use-terminal"
import type { TerminalConnectionState } from "../terminal/terminal-connection"

vi.mock("../terminal/use-terminal", () => ({ useTerminal: vi.fn() }))
vi.mock("@/components/providers/theme-provider", () => ({ useTheme: () => ({ resolvedTheme: "dark" }) }))

function createTerminal() {
    return {
        containerRef: { current: null },
        terminalRef: { current: { focus: vi.fn(), getSelection: vi.fn(() => "selected output"), textarea: undefined } },
        state: { status: "ready" } as TerminalConnectionState,
        dimensions: { cols: 120, rows: 36 },
        atBottom: true,
        hasSelection: false,
        hasOutput: true,
        searchResults: { resultIndex: 0, resultCount: 3 },
        fit: vi.fn(),
        find: vi.fn(),
        startSession: vi.fn(),
        endSession: vi.fn(),
        paste: vi.fn(),
        sendKey: vi.fn(),
        clear: vi.fn(),
        jumpToLatest: vi.fn(),
    }
}

let terminal: ReturnType<typeof createTerminal>
const defaultProps = { nodeId: 6, nodeName: "Rasp", nodeAddress: "192.0.2.6", isOnline: true, isActive: true }

function pasteToEmulator(text: string) {
    const event = createEvent.paste(screen.getByTestId("terminal-emulator"), {
        clipboardData: { getData: (format: string) => format === "text/plain" ? text : "" },
    })
    fireEvent(screen.getByTestId("terminal-emulator"), event)
    return event
}

beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    terminal = createTerminal()
    vi.mocked(useTerminal).mockImplementation(() => terminal as unknown as ReturnType<typeof useTerminal>)
    vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} unobserve() {} })
    vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false })))
})

afterEach(() => vi.unstubAllGlobals())

describe("terminal interaction safeguards", () => {
    it("previews diagnostic commands without sending and inserts the exact selection without Enter", async () => {
        const user = userEvent.setup()
        render(<NodeTerminal {...defaultProps} />)
        await user.click(screen.getByRole("button", { name: "Commands" }))
        await user.click(screen.getByRole("button", { name: /^Listening ports/ }))

        const library = screen.getByRole("complementary", { name: "Command library" })
        expect(within(library).getByText("ss -tuln")).toBeVisible()
        expect(terminal.paste).not.toHaveBeenCalled()
        expect(terminal.sendKey).not.toHaveBeenCalled()

        await user.click(within(library).getByRole("button", { name: "Insert at cursor" }))
        expect(terminal.paste).toHaveBeenCalledExactlyOnceWith("ss -tuln")
        expect(terminal.sendKey).not.toHaveBeenCalled()
        expect(screen.queryByRole("complementary", { name: "Command library" })).not.toBeInTheDocument()
    })

    it("intercepts multiline paste, lets the user edit it, and sends only after review", async () => {
        const user = userEvent.setup()
        render(<NodeTerminal {...defaultProps} />)
        const event = pasteToEmulator("pwd\nls -l\n")
        expect(event.defaultPrevented).toBe(true)
        const dialog = screen.getByRole("dialog", { name: "Review pasted text" })
        expect(within(dialog).getByRole("textbox", { name: "Text to paste" })).toHaveValue("pwd\nls -l\n")
        expect(terminal.paste).not.toHaveBeenCalled()

        fireEvent.change(within(dialog).getByRole("textbox", { name: "Text to paste" }), { target: { value: "pwd\nls -la" } })
        await user.click(within(dialog).getByRole("button", { name: "Paste to Rasp" }))
        expect(terminal.paste).toHaveBeenCalledExactlyOnceWith("pwd\nls -la")
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    })

    it("discards an unconfirmed paste when the session drops and never reuses it in a new shell", async () => {
        const { rerender } = render(<NodeTerminal {...defaultProps} />)
        pasteToEmulator("cd /var/log\nrm old.log")
        expect(screen.getByRole("dialog", { name: "Review pasted text" })).toBeVisible()

        terminal.state = { status: "disconnected", message: "Connection lost" }
        rerender(<NodeTerminal {...defaultProps} />)
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
        expect(terminal.paste).not.toHaveBeenCalled()
        expect(pasteToEmulator("uptime").defaultPrevented).toBe(true)

        terminal.state = { status: "ready" }
        rerender(<NodeTerminal {...defaultProps} />)
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
        expect(terminal.paste).not.toHaveBeenCalled()
        pasteToEmulator("pwd\nuptime")
        expect(screen.getByRole("textbox", { name: "Text to paste" })).toHaveValue("pwd\nuptime")
    })

    it("requires an explicit new shell after disconnection and blocks insertion while unavailable", async () => {
        const user = userEvent.setup()
        terminal.state = { status: "disconnected", message: "Connection lost" }
        const { rerender } = render(<NodeTerminal {...defaultProps} />)
        expect(terminal.startSession).not.toHaveBeenCalled()
        await user.click(screen.getByRole("button", { name: "Commands" }))
        expect(screen.getByRole("button", { name: "Insert at cursor" })).toBeDisabled()
        expect(terminal.paste).not.toHaveBeenCalled()
        await user.click(screen.getByRole("button", { name: "Start new shell" }))
        expect(terminal.startSession).toHaveBeenCalledTimes(1)

        rerender(<NodeTerminal {...defaultProps} isOnline={false} />)
        expect(screen.queryByRole("button", { name: "Start new shell" })).not.toBeInTheDocument()
        expect(screen.getByRole("button", { name: "Insert at cursor" })).toBeDisabled()
        expect(screen.getByRole("status")).toHaveTextContent("Node offline")
    })

    it("keeps Escape available in focus mode while search Escape closes only search", async () => {
        const user = userEvent.setup()
        render(<NodeTerminal {...defaultProps} />)
        await user.click(screen.getByRole("button", { name: "Focus" }))
        const shellEscape = createEvent.keyDown(screen.getByTestId("terminal-emulator"), { key: "Escape", code: "Escape" })
        fireEvent(screen.getByTestId("terminal-emulator"), shellEscape)
        expect(shellEscape.defaultPrevented).toBe(false)
        expect(screen.getByRole("button", { name: "Exit focus" })).toHaveAttribute("aria-pressed", "true")

        await user.click(screen.getByRole("button", { name: "Find" }))
        const search = screen.getByRole("textbox", { name: "Find in output" })
        await user.type(search, "failed")
        expect(terminal.find).toHaveBeenLastCalledWith("failed", false, true)
        fireEvent.keyDown(search, { key: "Escape" })
        expect(screen.queryByRole("search")).not.toBeInTheDocument()
        expect(screen.getByRole("button", { name: "Find" })).toHaveFocus()
        expect(screen.getByRole("button", { name: "Exit focus" })).toHaveAttribute("aria-pressed", "true")
        expect(terminal.sendKey).not.toHaveBeenCalled()
    })

    it("keeps the shell open until the user confirms End session", async () => {
        const user = userEvent.setup()
        render(<NodeTerminal {...defaultProps} />)
        await user.click(screen.getByRole("button", { name: "More terminal options" }))
        await user.click(screen.getByRole("menuitem", { name: "End session…" }))
        let dialog = screen.getByRole("dialog", { name: "End shell on Rasp?" })
        expect(terminal.endSession).not.toHaveBeenCalled()
        await user.click(within(dialog).getByRole("button", { name: "Keep open" }))
        expect(terminal.endSession).not.toHaveBeenCalled()

        await user.click(screen.getByRole("button", { name: "More terminal options" }))
        await user.click(screen.getByRole("menuitem", { name: "End session…" }))
        dialog = screen.getByRole("dialog", { name: "End shell on Rasp?" })
        await user.click(within(dialog).getByRole("button", { name: "End session" }))
        expect(terminal.endSession).toHaveBeenCalledTimes(1)
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    })

    it("persists text, accessibility, and touch keyboard preferences across terminal visits", async () => {
        const user = userEvent.setup()
        const first = render(<NodeTerminal {...defaultProps} />)
        await user.click(screen.getByRole("button", { name: "Keyboard and accessibility settings" }))
        let dialog = screen.getByRole("dialog", { name: "Terminal settings" })
        fireEvent.change(within(dialog).getByRole("slider", { name: /Text size/ }), { target: { value: "18" } })
        await user.click(within(dialog).getByRole("checkbox", { name: /^Screen reader support/ }))
        await user.click(within(dialog).getByRole("checkbox", { name: /^Show terminal keys/ }))
        await user.click(within(dialog).getByRole("button", { name: "Done" }))
        expect(screen.getByRole("group", { name: "Terminal keys" })).toBeVisible()
        expect(vi.mocked(useTerminal).mock.lastCall?.[0].preferences).toEqual({ fontSize: 18, screenReaderMode: true, touchKeys: true })

        first.unmount()
        render(<NodeTerminal {...defaultProps} nodeId={7} nodeName="Other node" />)
        await user.click(screen.getByRole("button", { name: "Change terminal text size" }))
        dialog = screen.getByRole("dialog", { name: "Terminal settings" })
        expect(within(dialog).getByRole("slider", { name: /Text size/ })).toHaveValue("18")
        expect(within(dialog).getByRole("checkbox", { name: /^Screen reader support/ })).toBeChecked()
        expect(within(dialog).getByRole("checkbox", { name: /^Show terminal keys/ })).toBeChecked()
    })

    it("closes floating terminal tools when the retained terminal becomes inactive", async () => {
        const user = userEvent.setup()
        const { rerender } = render(<NodeTerminal {...defaultProps} />)
        await user.click(screen.getByRole("button", { name: "Focus" }))
        await user.click(screen.getByRole("button", { name: "Commands" }))
        pasteToEmulator("pwd\nuptime")
        rerender(<NodeTerminal {...defaultProps} isActive={false} />)
        await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
        expect(screen.queryByRole("complementary", { name: "Command library" })).not.toBeInTheDocument()
        expect(screen.getByRole("button", { name: "Focus" })).toHaveAttribute("aria-pressed", "false")
        expect(terminal.paste).not.toHaveBeenCalled()
        expect(terminal.endSession).not.toHaveBeenCalled()
        expect(document.body.style.overflow).not.toBe("hidden")
    })
})
