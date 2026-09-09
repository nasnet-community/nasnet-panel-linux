import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import type { Terminal, IDisposable } from "@xterm/xterm"
import type { FitAddon } from "@xterm/addon-fit"
import type { SearchAddon, ISearchOptions } from "@xterm/addon-search"
import { getApiBaseUrl } from "@/lib/config"
import { buildTerminalWebSocketUrl, TerminalConnection, type TerminalConnectionState } from "./terminal-connection"
import { terminalOutput, terminalTheme, type TerminalPreferences } from "./terminal-utils"

interface Options {
    nodeId: number
    isOnline: boolean
    isActive: boolean
    dark: boolean
    preferences: TerminalPreferences
    onSearch: () => void
    onToolbar: () => void
}

export function useTerminal(options: Options) {
    const optionsRef = useRef(options)
    useLayoutEffect(() => { optionsRef.current = options }, [options])
    const containerRef = useRef<HTMLDivElement>(null)
    const terminalRef = useRef<Terminal | null>(null)
    const fitRef = useRef<FitAddon | null>(null)
    const searchRef = useRef<SearchAddon | null>(null)
    const connectionRef = useRef<TerminalConnection | null>(null)
    const startedRef = useRef(false)
    const sessionGenerationRef = useRef(0)
    const frameRef = useRef<number | null>(null)
    const [rendererVersion, setRendererVersion] = useState(0)
    const [initializationAttempt, setInitializationAttempt] = useState(0)
    const [state, setState] = useState<TerminalConnectionState>({ status: "connecting" })
    const stateRef = useRef(state)
    const [dimensions, setDimensions] = useState({ cols: 0, rows: 0 })
    const [atBottom, setAtBottom] = useState(true)
    const [hasSelection, setHasSelection] = useState(false)
    const [hasOutput, setHasOutput] = useState(false)
    const [searchResults, setSearchResults] = useState({ resultIndex: -1, resultCount: 0 })

    const updateState = useCallback((next: TerminalConnectionState) => {
        stateRef.current = next
        setState(next)
        if (terminalRef.current) terminalRef.current.options.disableStdin = next.status !== "ready"
    }, [])

    const fit = useCallback(() => {
        if (frameRef.current !== null) cancelAnimationFrame(frameRef.current)
        frameRef.current = requestAnimationFrame(() => {
            frameRef.current = null
            const container = containerRef.current
            if (!container || !optionsRef.current.isActive || container.clientWidth <= 0 || container.clientHeight <= 0) return
            fitRef.current?.fit()
        })
    }, [])

    const startSession = useCallback(() => {
        const term = terminalRef.current
        if (!optionsRef.current.isOnline) return
        if (!term) { setInitializationAttempt(attempt => attempt + 1); return }
        const generation = ++sessionGenerationRef.current
        const isCurrent = () => sessionGenerationRef.current === generation && terminalRef.current === term && optionsRef.current.isOnline
        connectionRef.current?.dispose()
        connectionRef.current = null
        updateState({ status: "connecting" })
        const connect = () => {
            if (!isCurrent()) return
            startedRef.current = true
            const connection = new TerminalConnection({
                url: buildTerminalWebSocketUrl(getApiBaseUrl(), optionsRef.current.nodeId),
                onState: next => {
                    if (connectionRef.current !== connection) return
                    updateState(next)
                    if (next.status === "ready") {
                        connection.resize(term.cols, term.rows)
                        const activeElement = document.activeElement
                        const usingTools = activeElement instanceof HTMLElement && activeElement.closest('.node-terminal-tools, .node-terminal-search, .node-terminal-commands, [role="dialog"]')
                        if (optionsRef.current.isActive && !usingTools) term.focus()
                    }
                },
                onData: data => {
                    if (connectionRef.current !== connection) return
                    term.write(data)
                    setHasOutput(true)
                },
            })
            connectionRef.current = connection
            connection.resize(term.cols, term.rows)
            connection.start()
        }
        if (!startedRef.current) { connect(); return }

        // Drain already accepted output before inspecting its final buffer. A
        // disconnected vim/tmux may leave both alternate-screen and input modes
        // active. Reset them in-band so pending escape sequences cannot restore
        // stale modes after the replacement shell has started.
        term.write("", () => {
            if (!isCurrent()) return
            const alternateSnapshot = term.buffer.active.type === "alternate" ? terminalOutput(term) : ""
            const resetModes = "\x18\x1b[?1049l\x1b[?1000;1002;1003;1006;1016l\x1b[!p\x1b[0 q"
            const snapshot = alternateSnapshot
                ? `\r\n──────── Previous full-screen output ────────\r\n${alternateSnapshot.replace(/\n/g, "\r\n")}\r\n`
                : ""
            term.write(`${resetModes}${snapshot}\r\n──────── New shell · previous output above ────────\r\n\r\n`, () => {
                if (!isCurrent()) return
                term.clearSelection()
                term.scrollToBottom()
                setAtBottom(true)
                connect()
            })
        })
    }, [updateState])

    useEffect(() => {
        let cancelled = false
        let terminal: Terminal | undefined
        let observer: ResizeObserver | undefined
        const disposables: IDisposable[] = []
        const releaseRenderer = () => {
            ++sessionGenerationRef.current
            observer?.disconnect()
            observer = undefined
            if (frameRef.current !== null) cancelAnimationFrame(frameRef.current)
            frameRef.current = null
            connectionRef.current?.dispose()
            connectionRef.current = null
            disposables.splice(0).forEach(disposable => disposable.dispose())
            terminal?.dispose()
            if (terminalRef.current === terminal) {
                terminalRef.current = null
                fitRef.current = null
                searchRef.current = null
            }
            terminal = undefined
        }
        setHasOutput(false)
        setHasSelection(false)
        startedRef.current = false
        updateState({ status: optionsRef.current.isOnline ? "connecting" : "disconnected", message: optionsRef.current.isOnline ? undefined : "Node is offline" })

        async function initialize() {
            try {
                const [{ Terminal }, { FitAddon }, { SearchAddon }, { WebLinksAddon }] = await Promise.all([
                    import("@xterm/xterm"), import("@xterm/addon-fit"), import("@xterm/addon-search"), import("@xterm/addon-web-links"),
                ])
                if (cancelled || !containerRef.current) return
                const current = optionsRef.current
                terminal = new Terminal({
                    fontFamily: "'Geist Mono Variable', ui-monospace, 'SFMono-Regular', Consolas, monospace",
                    fontSize: current.preferences.fontSize, lineHeight: 1.35, cursorBlink: true,
                    theme: terminalTheme(current.dark), minimumContrastRatio: 4.5,
                    screenReaderMode: current.preferences.screenReaderMode,
                    scrollback: 10_000, disableStdin: true, allowProposedApi: true,
                })
                const term = terminal
                const fitter = new FitAddon()
                const search = new SearchAddon()
                term.loadAddon(fitter)
                term.loadAddon(search)
                term.loadAddon(new WebLinksAddon())
                term.open(containerRef.current)
                terminalRef.current = term
                fitRef.current = fitter
                searchRef.current = search
                if (term.textarea) term.textarea.setAttribute("aria-label", `Terminal input for ${current.nodeId}. Control Shift M moves focus to the toolbar.`)
                term.attachCustomKeyEventHandler(event => {
                    if ((event.ctrlKey || event.metaKey) && event.shiftKey && ["f", "m"].includes(event.key.toLowerCase())) {
                        if (event.type === "keydown") {
                            event.preventDefault()
                            event.stopPropagation()
                            if (event.key.toLowerCase() === "f") optionsRef.current.onSearch()
                            else optionsRef.current.onToolbar()
                        }
                        return false
                    }
                    return true
                })
                disposables.push(
                    term.onData(data => { connectionRef.current?.sendInput(data) }),
                    term.onBinary(data => { connectionRef.current?.sendInput(Uint8Array.from(data, char => char.charCodeAt(0) & 255)) }),
                    term.onResize(size => { setDimensions(size); connectionRef.current?.resize(size.cols, size.rows) }),
                    term.onScroll(() => setAtBottom(term.buffer.active.viewportY >= term.buffer.active.baseY)),
                    term.onSelectionChange(() => setHasSelection(term.hasSelection())),
                    search.onDidChangeResults(setSearchResults),
                )
                observer = new ResizeObserver(fit)
                observer.observe(containerRef.current)
                setDimensions({ cols: term.cols, rows: term.rows })
                setRendererVersion(version => version + 1)
                fit()
                void document.fonts?.ready.then(() => { if (!cancelled) fit() })
                if (current.isOnline) startSession()
            } catch (error) {
                if (!cancelled) {
                    releaseRenderer()
                    updateState({ status: "error", message: error instanceof Error ? `Unable to load terminal: ${error.message}` : "Unable to load terminal. Try again." })
                }
            }
        }
        void initialize()
        return () => {
            cancelled = true
            releaseRenderer()
        }
    }, [options.nodeId, initializationAttempt, fit, startSession, updateState])

    useEffect(() => {
        if (!options.isOnline) {
            ++sessionGenerationRef.current
            connectionRef.current?.dispose()
            connectionRef.current = null
            updateState({ status: "disconnected", message: "Node is offline. Previous output is preserved." })
        }
    }, [options.isOnline, updateState])

    useEffect(() => {
        const term = terminalRef.current
        if (!term) return
        term.options.fontSize = options.preferences.fontSize
        term.options.screenReaderMode = options.preferences.screenReaderMode
        term.options.theme = terminalTheme(options.dark)
        fit()
    }, [options.preferences.fontSize, options.preferences.screenReaderMode, options.dark, rendererVersion, fit])

    useEffect(() => {
        if (!options.isActive) return
        fit()
        const frame = requestAnimationFrame(() => {
            if (optionsRef.current.isActive && stateRef.current.status === "ready") terminalRef.current?.focus()
        })
        return () => cancelAnimationFrame(frame)
    }, [options.isActive, fit])

    const find = useCallback((query: string, previous = false, incremental = false) => {
        if (!query) { searchRef.current?.clearDecorations(); terminalRef.current?.clearSelection(); setSearchResults({ resultIndex: -1, resultCount: 0 }); return }
        const dark = optionsRef.current.dark
        const settings: ISearchOptions = { incremental, decorations: {
            matchBackground: dark ? "#443d1f" : "#fff0b8", matchOverviewRuler: "#c8a34b",
            activeMatchBackground: dark ? "#5d4b17" : "#f9da77", activeMatchColorOverviewRuler: "#e8ba54",
        } }
        if (previous) searchRef.current?.findPrevious(query, settings)
        else searchRef.current?.findNext(query, settings)
    }, [])

    return {
        containerRef, terminalRef, state, dimensions, atBottom, hasSelection, hasOutput, searchResults, fit, find, startSession,
        endSession: () => {
            ++sessionGenerationRef.current
            if (connectionRef.current) connectionRef.current.disconnect()
            else updateState({ status: "disconnected", message: "Session ended" })
        },
        sendKey: (data: string) => {
            const term = terminalRef.current
            const key = term?.modes.applicationCursorKeysMode && data.startsWith("\x1b[") && data.length === 3 && "ABCD".includes(data[2]) ? `\x1bO${data[2]}` : data
            const sent = connectionRef.current?.sendInput(key)
            term?.focus()
            return sent
        },
        paste: (text: string) => { if (stateRef.current.status === "ready") { terminalRef.current?.paste(text); terminalRef.current?.focus() } },
        clear: () => { terminalRef.current?.clear(); setAtBottom(true); setSearchResults({ resultIndex: -1, resultCount: 0 }); terminalRef.current?.focus() },
        jumpToLatest: () => { terminalRef.current?.scrollToBottom(); setAtBottom(true); terminalRef.current?.focus() },
    }
}
