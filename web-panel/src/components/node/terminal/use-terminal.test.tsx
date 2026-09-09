import { act, cleanup, render } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { TerminalConnectionState } from "./terminal-connection"
import { useTerminal } from "./use-terminal"
import { terminalOutput } from "./terminal-utils"

const mocks = vi.hoisted(() => {
    type Events = {
        data: (data: string) => void
        binary: (data: string) => void
        resize: (size: { cols: number; rows: number }) => void
        scroll: () => void
        selection: () => void
    }
    let resolveImport!: () => void
    const importReady = new Promise<void>(resolve => { resolveImport = resolve })
    class Terminal {
        static instances: Terminal[] = []
        options: Record<string, unknown>
        cols = 80
        rows = 24
        textarea = document.createElement("textarea")
        buffer = { active: { type: "normal" as "normal" | "alternate", viewportY: 0, baseY: 0, length: 0, getLine: (_index: number): { isWrapped: boolean; translateToString: () => string } | undefined => undefined } }
        modes = { applicationCursorKeysMode: false }
        handlers: Partial<Events> = {}
        eventDisposers: ReturnType<typeof vi.fn>[] = []
        output: Array<string | Uint8Array> = []
        deferWrites = false
        writes: Array<{ data: string | Uint8Array; callback?: () => void }> = []
        parseWrite: ((data: string | Uint8Array) => void) | undefined
        open = vi.fn()
        loadAddon = vi.fn()
        dispose = vi.fn()
        focus = vi.fn()
        clear = vi.fn()
        clearSelection = vi.fn()
        scrollToBottom = vi.fn()
        hasSelection = vi.fn(() => false)
        attachCustomKeyEventHandler = vi.fn()
        write = vi.fn((data: string | Uint8Array, callback?: () => void) => {
            this.writes.push({ data, callback })
            if (!this.deferWrites) this.flushWrites()
        })
        writeln = vi.fn((data: string) => { this.output.push(data) })
        paste = vi.fn((data: string) => { this.handlers.data?.(data) })
        constructor(options: Record<string, unknown>) {
            this.options = options
            Terminal.instances.push(this)
        }
        flushWrites(limit = Number.POSITIVE_INFINITY) {
            while (this.writes.length && limit-- > 0) {
                const { data, callback } = this.writes.shift()!
                if (data.length) this.output.push(data)
                this.parseWrite?.(data)
                callback?.()
            }
        }
        private listen<T extends keyof Events>(event: T, handler: Events[T]) {
            this.handlers[event] = handler
            const dispose = vi.fn(() => { delete this.handlers[event] })
            this.eventDisposers.push(dispose)
            return { dispose }
        }
        onData(handler: Events["data"]) { return this.listen("data", handler) }
        onBinary(handler: Events["binary"]) { return this.listen("binary", handler) }
        onResize(handler: Events["resize"]) { return this.listen("resize", handler) }
        onScroll(handler: Events["scroll"]) { return this.listen("scroll", handler) }
        onSelectionChange(handler: Events["selection"]) { return this.listen("selection", handler) }
    }
    class FitAddon {
        static instances: FitAddon[] = []
        fit = vi.fn()
        constructor() { FitAddon.instances.push(this) }
    }
    class SearchAddon {
        static instances: SearchAddon[] = []
        clearDecorations = vi.fn()
        findNext = vi.fn()
        findPrevious = vi.fn()
        disposeResults = vi.fn()
        onDidChangeResults = vi.fn(() => ({ dispose: this.disposeResults }))
        constructor() { SearchAddon.instances.push(this) }
    }
    class Connection {
        static instances: Connection[] = []
        options: { url: string; onState: (state: TerminalConnectionState) => void; onData: (data: string | Uint8Array) => void }
        disposed = false
        ready = false
        start = vi.fn(() => this.options.onState({ status: "connecting" }))
        dispose = vi.fn(() => { this.disposed = true })
        disconnect = vi.fn(() => { this.ready = false; this.options.onState({ status: "disconnected", message: "Session ended" }) })
        resize = vi.fn()
        sendInput = vi.fn((_data: string | Uint8Array) => this.ready && !this.disposed)
        constructor(options: Connection["options"]) {
            this.options = options
            Connection.instances.push(this)
        }
        emitState(state: TerminalConnectionState) {
            this.ready = state.status === "ready"
            this.options.onState(state)
        }
    }
    class Observer {
        static instances: Observer[] = []
        static failNext = false
        observe = vi.fn()
        disconnect = vi.fn()
        callback: () => void
        constructor(callback: () => void) {
            if (Observer.failNext) { Observer.failNext = false; throw new Error("Cannot observe terminal") }
            this.callback = callback
            Observer.instances.push(this)
        }
    }
    return { importReady, resolveImport, Terminal, FitAddon, SearchAddon, Connection, Observer }
})

vi.mock("@xterm/xterm", async () => {
    await mocks.importReady
    return { Terminal: mocks.Terminal }
})
vi.mock("@xterm/addon-fit", () => ({ FitAddon: mocks.FitAddon }))
vi.mock("@xterm/addon-search", () => ({ SearchAddon: mocks.SearchAddon }))
vi.mock("@xterm/addon-web-links", () => ({ WebLinksAddon: class {} }))
vi.mock("./terminal-connection", () => ({
    TerminalConnection: mocks.Connection,
    buildTerminalWebSocketUrl: (_base: string, nodeId: number) => `wss://panel.example/api/v1/nodes/${nodeId}/terminal/ws`,
}))
vi.mock("@/lib/config", () => ({ getApiBaseUrl: () => "/panel" }))

type Options = Parameters<typeof useTerminal>[0]
let current: ReturnType<typeof useTerminal>
let frames: Map<number, FrameRequestCallback>

function Harness(options: Options) {
    current = useTerminal(options)
    return <div ref={current.containerRef} data-testid="terminal-container" />
}

function options(overrides: Partial<Options> = {}): Options {
    return {
        nodeId: 6,
        isOnline: true,
        isActive: true,
        dark: true,
        preferences: { fontSize: 14, screenReaderMode: false, touchKeys: false },
        onSearch: vi.fn(),
        onToolbar: vi.fn(),
        ...overrides,
    }
}

async function settleImports() {
    await act(async () => { await vi.dynamicImportSettled() })
}

function flushFrames() {
    act(() => {
        const pending = [...frames.values()]
        frames.clear()
        pending.forEach(callback => callback(0))
    })
}

beforeEach(() => {
    mocks.Terminal.instances = []
    mocks.FitAddon.instances = []
    mocks.SearchAddon.instances = []
    mocks.Connection.instances = []
    mocks.Observer.instances = []
    mocks.Observer.failNext = false
    frames = new Map()
    let frameId = 0
    vi.stubGlobal("ResizeObserver", mocks.Observer)
    vi.stubGlobal("requestAnimationFrame", vi.fn((callback: FrameRequestCallback) => { frames.set(++frameId, callback); return frameId }))
    vi.stubGlobal("cancelAnimationFrame", vi.fn((id: number) => frames.delete(id)))
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(1000)
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(600)
})

afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
})

describe("terminal renderer and shell lifecycle", () => {
    it("does not create a renderer or shell when lazy imports finish after unmount", async () => {
        const view = render(<Harness {...options()} />)
        view.unmount()
        mocks.resolveImport()
        await settleImports()
        expect(mocks.Terminal.instances).toHaveLength(0)
        expect(mocks.Connection.instances).toHaveLength(0)
        expect(mocks.Observer.instances).toHaveLength(0)
        expect(frames.size).toBe(0)
    })

    it("updates appearance and visibility without recreating the shell or scrollback", async () => {
        const initial = options()
        const view = render(<Harness {...initial} />)
        await settleImports()
        const term = mocks.Terminal.instances[0]
        const connection = mocks.Connection.instances[0]
        act(() => {
            connection.emitState({ status: "ready" })
            connection.options.onData("existing shell output")
        })
        const changed = { ...initial, dark: false, isActive: false, preferences: { ...initial.preferences, fontSize: 18, screenReaderMode: true } }
        view.rerender(<Harness {...changed} />)
        view.rerender(<Harness {...changed} isActive />)
        flushFrames()
        expect(mocks.Terminal.instances).toHaveLength(1)
        expect(mocks.Connection.instances).toHaveLength(1)
        expect(term.dispose).not.toHaveBeenCalled()
        expect(connection.dispose).not.toHaveBeenCalled()
        expect(term.options).toMatchObject({ fontSize: 18, screenReaderMode: true, theme: { background: "#fbfdfc" } })
        expect(term.output).toEqual(["existing shell output"])
        expect(current.hasOutput).toBe(true)
        expect(current.state.status).toBe("ready")
    })

    it("does not steal focus or fit a hidden terminal when shell readiness arrives", async () => {
        const initial = options()
        const view = render(<Harness {...initial} />)
        await settleImports()
        const term = mocks.Terminal.instances[0]
        const fitter = mocks.FitAddon.instances[0]
        view.rerender(<Harness {...initial} isActive={false} />)
        act(() => mocks.Connection.instances[0].emitState({ status: "ready" }))
        flushFrames()
        expect(term.focus).not.toHaveBeenCalled()
        expect(fitter.fit).not.toHaveBeenCalled()
        expect(term.options.disableStdin).toBe(false)
        view.rerender(<Harness {...initial} />)
        flushFrames()
        expect(fitter.fit).toHaveBeenCalledOnce()
        expect(term.focus).toHaveBeenCalledOnce()
    })

    it("closes an offline shell, preserves output, and requires an explicit new session", async () => {
        const initial = options()
        const view = render(<Harness {...initial} />)
        await settleImports()
        const term = mocks.Terminal.instances[0]
        const oldConnection = mocks.Connection.instances[0]
        act(() => {
            oldConnection.emitState({ status: "ready" })
            oldConnection.options.onData("retained output")
        })
        view.rerender(<Harness {...initial} isOnline={false} />)
        expect(oldConnection.dispose).toHaveBeenCalledOnce()
        expect(current.state).toMatchObject({ status: "disconnected", message: expect.stringContaining("offline") })
        expect(term.options.disableStdin).toBe(true)
        act(() => {
            term.handlers.data?.("offline input\r")
            term.handlers.binary?.("\x03")
            current.paste("offline paste")
            current.startSession()
            oldConnection.options.onData("late disposed output")
            oldConnection.emitState({ status: "ready" })
        })
        expect(oldConnection.sendInput).not.toHaveBeenCalled()
        expect(term.paste).not.toHaveBeenCalled()
        expect(term.output).toEqual(["retained output"])
        expect(current.state.status).toBe("disconnected")
        view.rerender(<Harness {...initial} />)
        expect(mocks.Connection.instances).toHaveLength(1)
        act(() => current.startSession())
        expect(mocks.Connection.instances).toHaveLength(2)
        expect(mocks.Terminal.instances).toHaveLength(1)
        expect(mocks.Connection.instances[1].sendInput).not.toHaveBeenCalled()
    })

    it("starts a replacement shell in the same renderer and ignores callbacks from the previous shell", async () => {
        render(<Harness {...options()} />)
        await settleImports()
        const term = mocks.Terminal.instances[0]
        const oldConnection = mocks.Connection.instances[0]
        act(() => {
            oldConnection.emitState({ status: "ready" })
            oldConnection.options.onData("old shell output")
            current.startSession()
        })
        const replacement = mocks.Connection.instances[1]
        expect(oldConnection.dispose).toHaveBeenCalledOnce()
        expect(mocks.Terminal.instances).toHaveLength(1)
        expect(term.clear).not.toHaveBeenCalled()
        expect(term.output[0]).toBe("old shell output")
        expect(term.write).toHaveBeenCalledWith(expect.stringContaining("New shell"), expect.any(Function))
        expect(term.options.disableStdin).toBe(true)
        act(() => {
            oldConnection.options.onData("stale output")
            oldConnection.emitState({ status: "exited", exitCode: 1 })
            replacement.emitState({ status: "ready" })
            replacement.options.onData("new shell output")
            term.handlers.data?.("new input\r")
        })
        expect(current.state.status).toBe("ready")
        expect(term.output).not.toContain("stale output")
        expect(term.output.at(-1)).toBe("new shell output")
        expect(replacement.sendInput).toHaveBeenCalledExactlyOnceWith("new input\r")
        expect(oldConnection.sendInput).not.toHaveBeenCalled()
    })

    it("announces initial and actual resized dimensions to the current remote shell", async () => {
        render(<Harness {...options()} />)
        await settleImports()
        const term = mocks.Terminal.instances[0]
        const connection = mocks.Connection.instances[0]
        expect(connection.resize).toHaveBeenCalledWith(80, 24)
        act(() => term.handlers.resize?.({ cols: 127, rows: 43 }))
        expect(current.dimensions).toEqual({ cols: 127, rows: 43 })
        expect(connection.resize).toHaveBeenLastCalledWith(127, 43)
        mocks.Observer.instances[0].callback()
        flushFrames()
        expect(mocks.FitAddon.instances[0].fit).toHaveBeenCalledOnce()
    })

    it("disposes the socket, renderer, subscriptions, observer and pending frame on unmount", async () => {
        const view = render(<Harness {...options()} />)
        await settleImports()
        const term = mocks.Terminal.instances[0]
        const connection = mocks.Connection.instances[0]
        expect(frames.size).toBeGreaterThan(0)
        view.unmount()
        expect(connection.dispose).toHaveBeenCalledOnce()
        expect(term.dispose).toHaveBeenCalledOnce()
        term.eventDisposers.forEach(dispose => expect(dispose).toHaveBeenCalledOnce())
        expect(mocks.SearchAddon.instances[0].disposeResults).toHaveBeenCalledOnce()
        expect(mocks.Observer.instances[0].disconnect).toHaveBeenCalledOnce()
        expect(frames.size).toBe(0)
        const writes = term.write.mock.calls.length
        act(() => {
            connection.options.onData("arrived after unmount")
            connection.emitState({ status: "ready" })
        })
        expect(term.write).toHaveBeenCalledTimes(writes)
        expect(term.focus).not.toHaveBeenCalled()
    })

    it("initializes an offline node without opening a shell and starts on explicit request", async () => {
        const initial = options({ isOnline: false })
        const view = render(<Harness {...initial} />)
        await settleImports()
        expect(mocks.Terminal.instances).toHaveLength(1)
        expect(mocks.Connection.instances).toHaveLength(0)
        expect(current.state.status).toBe("disconnected")
        view.rerender(<Harness {...initial} isOnline />)
        expect(mocks.Connection.instances).toHaveLength(0)
        act(() => current.startSession())
        expect(mocks.Connection.instances).toHaveLength(1)
        expect(mocks.Connection.instances[0].start).toHaveBeenCalledOnce()
    })

    it("drains old output, preserves a final alternate-screen snapshot, and resets actual xterm modes before replacement", async () => {
        render(<Harness {...options()} />)
        await settleImports()
        const term = mocks.Terminal.instances[0]
        const oldConnection = mocks.Connection.instances[0]
        act(() => {
            oldConnection.emitState({ status: "ready" })
            oldConnection.options.onData("normal shell history")
        })
        term.deferWrites = true
        term.parseWrite = data => {
            if (data === "queued final application screen") {
                term.buffer.active.type = "alternate"
                term.buffer.active.length = 2
                term.buffer.active.getLine = index => index < 2
                    ? { isWrapped: false, translateToString: () => ["final application status", "last unsaved text"][index] }
                    : undefined
            }
        }
        act(() => {
            oldConnection.options.onData("queued final application screen")
            current.startSession()
        })
        expect(oldConnection.dispose).toHaveBeenCalledOnce()
        expect(term.options.disableStdin).toBe(true)
        expect(current.state.status).toBe("connecting")
        expect(mocks.Connection.instances).toHaveLength(1)
        act(() => term.flushWrites(1))
        expect(mocks.Connection.instances).toHaveLength(1)
        act(() => term.flushWrites(1))
        expect(mocks.Connection.instances).toHaveLength(1)
        const preparedOutput = term.writes[0].data as string
        expect(preparedOutput).toContain("Previous full-screen output")
        expect(preparedOutput).toContain("final application status\r\nlast unsaved text")

        // Exercise the produced reset against the real parser rather than
        // asserting a duplicate list of escape-sequence implementation details.
        vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null)
        const { Terminal } = await vi.importActual<typeof import("@xterm/xterm")>("@xterm/xterm")
        const real = new Terminal({ cols: 80, rows: 24, allowProposedApi: true })
        try {
            await new Promise<void>(resolve => real.write("normal shell history\r\n\x1b[?1049h\x1b[?1;2004;1003;1006h\x1b[?25l\x1b[4;8rapplication screen\x1b]0;unfinished title", resolve))
            expect(real.buffer.active.type).toBe("alternate")
            expect(real.modes.applicationCursorKeysMode).toBe(true)
            expect(real.modes.bracketedPasteMode).toBe(true)
            expect(real.modes.mouseTrackingMode).not.toBe("none")
            await new Promise<void>(resolve => real.write(preparedOutput, resolve))
            expect(real.buffer.active.type).toBe("normal")
            expect(real.modes.applicationCursorKeysMode).toBe(false)
            expect(real.modes.bracketedPasteMode).toBe(false)
            expect(real.modes.mouseTrackingMode).toBe("none")
            expect(terminalOutput(real)).toContain("normal shell history")
            expect(terminalOutput(real)).toContain("final application status\nlast unsaved text")
            expect(terminalOutput(real)).toContain("New shell")
        } finally { real.dispose() }

        act(() => term.flushWrites(1))
        expect(mocks.Connection.instances).toHaveLength(2)
        expect(mocks.Connection.instances[1].start).toHaveBeenCalledOnce()
        expect(mocks.Terminal.instances).toHaveLength(1)
        expect(term.output[0]).toBe("normal shell history")
    })

    it.each(["unmount", "offline", "end"])("does not create a replacement if %s occurs while old writes drain", async action => {
        const initial = options()
        const view = render(<Harness {...initial} />)
        await settleImports()
        const term = mocks.Terminal.instances[0]
        term.deferWrites = true
        act(() => current.startSession())
        expect(mocks.Connection.instances).toHaveLength(1)
        if (action === "unmount") view.unmount()
        else if (action === "offline") view.rerender(<Harness {...initial} isOnline={false} />)
        else act(() => current.endSession())
        act(() => term.flushWrites())
        expect(mocks.Connection.instances).toHaveLength(1)
        expect(term.output).toEqual([])
    })

    it("discards an obsolete replacement request while a newer request drains", async () => {
        render(<Harness {...options()} />)
        await settleImports()
        const term = mocks.Terminal.instances[0]
        term.deferWrites = true
        act(() => {
            current.startSession()
            current.startSession()
            term.flushWrites()
        })
        expect(mocks.Connection.instances).toHaveLength(2)
        expect(term.output.filter(data => typeof data === "string" && data.includes("New shell"))).toHaveLength(1)
    })

    it("uses application cursor sequences for touch arrows when the running application requests them", async () => {
        render(<Harness {...options()} />)
        await settleImports()
        const term = mocks.Terminal.instances[0]
        const connection = mocks.Connection.instances[0]
        act(() => {
            connection.emitState({ status: "ready" })
            current.sendKey("\x1b[A")
            term.modes.applicationCursorKeysMode = true
            for (const direction of "ABCD") current.sendKey(`\x1b[${direction}`)
            current.sendKey("\x03")
        })
        expect(connection.sendInput.mock.calls.map(([data]) => data)).toEqual(["\x1b[A", "\x1bOA", "\x1bOB", "\x1bOC", "\x1bOD", "\x03"])
    })

    it("cleans a partially initialized renderer and completely retries after an error following open", async () => {
        mocks.Observer.failNext = true
        const view = render(<Harness {...options()} />)
        await settleImports()
        const failedTerminal = mocks.Terminal.instances[0]
        expect(failedTerminal.open).toHaveBeenCalledOnce()
        expect(failedTerminal.dispose).toHaveBeenCalledOnce()
        failedTerminal.eventDisposers.forEach(dispose => expect(dispose).toHaveBeenCalledOnce())
        expect(mocks.SearchAddon.instances[0].disposeResults).toHaveBeenCalledOnce()
        expect(current.terminalRef.current).toBeNull()
        expect(current.state).toMatchObject({ status: "error", message: expect.stringContaining("Cannot observe terminal") })
        expect(mocks.Connection.instances).toHaveLength(0)
        act(() => current.startSession())
        await settleImports()
        expect(mocks.Terminal.instances).toHaveLength(2)
        expect(mocks.Connection.instances).toHaveLength(1)
        expect(mocks.Observer.instances).toHaveLength(1)
        expect(mocks.Terminal.instances[1].open).toHaveBeenCalledOnce()
        view.unmount()
        expect(failedTerminal.dispose).toHaveBeenCalledOnce()
        expect(mocks.Terminal.instances[1].dispose).toHaveBeenCalledOnce()
        expect(mocks.Observer.instances[0].disconnect).toHaveBeenCalledOnce()
    })
})
