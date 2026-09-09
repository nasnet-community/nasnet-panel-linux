import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from "react"
import * as DialogPrimitive from "@radix-ui/react-dialog"
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Check, ChevronDown, ChevronUp, Copy, Download, Eraser, Keyboard, Loader2, Maximize2, Minimize2, MoreHorizontal, Power, Search, Settings2, SquareTerminal, Terminal, X } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useTheme } from "@/components/providers/theme-provider"
import { cn } from "@/lib/utils"
import { useTerminal } from "./terminal/use-terminal"
import { needsPasteReview, readTerminalPreferences, saveTerminalPreferences, terminalCommands, terminalOutput } from "./terminal/terminal-utils"
import "./terminal/terminal.css"

interface NodeTerminalProps {
    nodeId: number
    nodeName?: string
    nodeAddress?: string
    isOnline: boolean
    isActive?: boolean
}

function TerminalDialog({ open, onOpenChange, title, description, children, onRestoreFocus }: {
    open: boolean; onOpenChange: (open: boolean) => void; title: string; description: string; children: ReactNode; onRestoreFocus: () => void
}) {
    return <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
        <DialogPrimitive.Portal>
            <DialogPrimitive.Overlay className="terminal-dialog-overlay" />
            <DialogPrimitive.Content className="terminal-dialog" onCloseAutoFocus={event => { event.preventDefault(); onRestoreFocus() }}>
                <DialogPrimitive.Title className="pr-8 text-lg font-medium">{title}</DialogPrimitive.Title>
                <DialogPrimitive.Description className="mt-2 text-sm text-muted-foreground">{description}</DialogPrimitive.Description>
                {children}
                <DialogPrimitive.Close className="terminal-dialog-close" aria-label="Close dialog"><X size={18} /></DialogPrimitive.Close>
            </DialogPrimitive.Content>
        </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
}

export function NodeTerminal({ nodeId, nodeName, nodeAddress, isOnline, isActive = true }: NodeTerminalProps) {
    const name = nodeName || `Node ${nodeId}`
    const { resolvedTheme } = useTheme()
    const [preferences, setPreferences] = useState(readTerminalPreferences)
    const [focused, setFocused] = useState(false)
    const [searchOpen, setSearchOpen] = useState(false)
    const [query, setQuery] = useState("")
    const [commandsOpen, setCommandsOpen] = useState(false)
    const [selectedCommand, setSelectedCommand] = useState(0)
    const [modal, setModal] = useState<"settings" | "paste" | "end" | null>(null)
    const [pasteText, setPasteText] = useState("")
    const [copied, setCopied] = useState(false)
    const rootRef = useRef<HTMLElement>(null)
    const searchButtonRef = useRef<HTMLButtonElement>(null)
    const searchInputRef = useRef<HTMLInputElement>(null)
    const commandsButtonRef = useRef<HTMLButtonElement>(null)
    const firstCommandRef = useRef<HTMLButtonElement>(null)
    const copyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
    const searchId = useId()
    const commandsId = useId()
    const settingsId = useId()
    const openSearch = useCallback(() => { setSearchOpen(true); requestAnimationFrame(() => searchInputRef.current?.focus()) }, [])
    const focusToolbar = useCallback(() => searchButtonRef.current?.focus(), [])
    const terminal = useTerminal({ nodeId, isOnline, isActive, dark: resolvedTheme === "dark", preferences, onSearch: openSearch, onToolbar: focusToolbar })
    const { state, find, fit } = terminal
    const ready = state.status === "ready"
    const busy = state.status === "connecting" || state.status === "starting"
    const canStart = isOnline && !ready && !busy
    const command = terminalCommands[selectedCommand]
    const statusLabel = !isOnline ? "Node offline" : ({ connecting: "Connecting", starting: "Starting shell", ready: "Connected", disconnected: "Disconnected", exited: "Shell exited", error: "Connection error" }[state.status])
    const stateTitle = !isOnline ? "Node is offline" : state.status === "exited" ? `Shell exited${state.exitCode === undefined ? "" : ` · code ${state.exitCode}`}` : state.status === "error" ? "Unable to use this shell" : state.status === "disconnected" ? (state.message === "Session ended" ? "Session ended" : "Connection lost") : statusLabel
    const stateDescription = !isOnline ? "Reconnect the node to start a shell. Previous output stays available." : busy ? `Opening an interactive shell on ${name}.` : state.message === "Session ended" ? "Output is preserved. Start a new shell when you're ready." : state.message?.replace(/^Connection lost\.\s*/, "") || "Previous output is preserved. Start a new shell to continue."

    useEffect(() => { saveTerminalPreferences(preferences) }, [preferences])
    useEffect(() => {
        if (!isActive) { setFocused(false); setModal(null); setCommandsOpen(false); setSearchOpen(false); setQuery(""); find("") }
    }, [isActive, find])
    useEffect(() => { if (!ready && modal === "paste") { setModal(null); setPasteText("") } }, [ready, modal])
    useEffect(() => { fit() }, [focused, commandsOpen, searchOpen, preferences.touchKeys, fit])
    useEffect(() => { if (commandsOpen) firstCommandRef.current?.focus() }, [commandsOpen])
    useEffect(() => () => { if (copyTimerRef.current) clearTimeout(copyTimerRef.current) }, [])

    // Preserve the terminal DOM/PTY in focus mode. Escape still belongs to the shell.
    useEffect(() => {
        if (!focused || !isActive || !rootRef.current) return
        const previousOverflow = document.body.style.overflow
        document.body.style.overflow = "hidden"
        const viewport = window.visualViewport
        const measureViewport = () => {
            rootRef.current?.style.setProperty("--terminal-visual-height", `${viewport?.height ?? window.innerHeight}px`)
            rootRef.current?.style.setProperty("--terminal-visual-top", `${viewport?.offsetTop ?? 0}px`)
        }
        measureViewport()
        viewport?.addEventListener("resize", measureViewport)
        viewport?.addEventListener("scroll", measureViewport)
        window.addEventListener("resize", measureViewport)
        const changed: { element: HTMLElement; inert: boolean }[] = []
        let branch: HTMLElement = rootRef.current
        while (branch.parentElement) {
            for (const sibling of Array.from(branch.parentElement.children)) {
                if (sibling !== branch && sibling instanceof HTMLElement && !["SCRIPT", "STYLE"].includes(sibling.tagName)) {
                    changed.push({ element: sibling, inert: sibling.inert })
                    sibling.inert = true
                }
            }
            if (branch.parentElement === document.body) break
            branch = branch.parentElement
        }
        terminal.terminalRef.current?.focus()
        return () => {
            document.body.style.overflow = previousOverflow
            changed.forEach(({ element, inert }) => { element.inert = inert })
            viewport?.removeEventListener("resize", measureViewport)
            viewport?.removeEventListener("scroll", measureViewport)
            window.removeEventListener("resize", measureViewport)
        }
    }, [focused, isActive, terminal.terminalRef])

    function closeSearch() { setSearchOpen(false); setQuery(""); find(""); searchButtonRef.current?.focus() }
    function closeCommands() { setCommandsOpen(false); commandsButtonRef.current?.focus() }
    function restoreFocus() { if (isActive) terminal.terminalRef.current?.focus() }
    function updateQuery(value: string) { setQuery(value); terminal.find(value, false, true) }

    async function copySelection() {
        const selection = terminal.terminalRef.current?.getSelection()
        if (!selection) return
        try {
            await navigator.clipboard.writeText(selection)
            setCopied(true)
            if (copyTimerRef.current) clearTimeout(copyTimerRef.current)
            copyTimerRef.current = setTimeout(() => setCopied(false), 1800)
        } catch { toast.error("Couldn't copy. Use your browser's copy shortcut on the selected text.") }
    }

    function downloadOutput() {
        const term = terminal.terminalRef.current
        if (!term) return
        const url = URL.createObjectURL(new Blob([terminalOutput(term)], { type: "text/plain;charset=utf-8" }))
        const anchor = document.createElement("a")
        anchor.href = url
        anchor.download = `${name.replace(/[^a-zA-Z0-9_-]/g, "_")}-terminal-${new Date().toISOString().replace(/[:.]/g, "-")}.txt`
        document.body.appendChild(anchor)
        anchor.click()
        anchor.remove()
        setTimeout(() => URL.revokeObjectURL(url), 1000)
    }

    const connectionNotice = <div className="node-terminal-notice" data-error={state.status === "error"}>
        <div><strong>{stateTitle}</strong><p>{stateDescription}</p></div>
        {canStart && <Button size="sm" variant="outline" onClick={() => { setPasteText(""); terminal.startSession() }}>Start new shell</Button>}
    </div>

    return <section ref={rootRef} aria-label={`Terminal for ${name}`} className={cn("node-terminal", focused && "node-terminal-focused")}
        onKeyDown={event => {
            if (!focused || event.key !== "Tab" || event.target === terminal.terminalRef.current?.textarea) return
            const controls = Array.from(rootRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), textarea:not([disabled]), [href]') ?? []).filter(element => element.getClientRects().length > 0)
            const target = event.target as HTMLElement
            if (event.shiftKey && target === controls[0]) { event.preventDefault(); controls.at(-1)?.focus() }
            else if (!event.shiftKey && target === controls.at(-1)) { event.preventDefault(); controls[0]?.focus() }
        }}>
        <header className="node-terminal-header">
            <div className="node-terminal-identity"><SquareTerminal size={18} aria-hidden="true" /><div className="min-w-0"><h2 className="truncate text-sm font-medium">{name}</h2><span className="node-terminal-address">{nodeAddress || `Node ${nodeId}`}<span aria-hidden="true"> · </span>Local shell</span></div></div>
            <span className="node-terminal-status" data-ready={ready && isOnline} data-error={state.status === "error"} role="status" aria-live="polite">{busy && isOnline ? <Loader2 size={12} className="animate-spin motion-reduce:animate-none" aria-hidden="true" /> : <span className="node-terminal-dot" aria-hidden="true" />}{statusLabel}</span>
            <div className="node-terminal-tools" role="group" aria-label="Terminal tools">
                <Button ref={searchButtonRef} size="sm" variant="ghost" onClick={() => searchOpen ? closeSearch() : openSearch()} aria-expanded={searchOpen} aria-controls={searchId} title="Find in output (Ctrl/⌘ Shift F)"><Search size={15} />Find</Button>
                <Button ref={commandsButtonRef} size="sm" variant="ghost" onClick={() => commandsOpen ? closeCommands() : setCommandsOpen(true)} aria-expanded={commandsOpen} aria-controls={commandsId}><Terminal size={15} />Commands</Button>
                <Button size="sm" variant="ghost" onClick={() => { setFocused(value => !value); requestAnimationFrame(restoreFocus) }} aria-pressed={focused}>{focused ? <Minimize2 size={15} /> : <Maximize2 size={15} />}<span>{focused ? "Exit focus" : "Focus"}</span></Button>
                <DropdownMenu>
                    <DropdownMenuTrigger asChild><Button size="icon" variant="ghost" className="h-9 w-9" aria-label="More terminal options"><MoreHorizontal size={18} /></Button></DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="z-[90] w-52" onCloseAutoFocus={event => { if (modal) event.preventDefault() }}>
                        <DropdownMenuItem disabled={!terminal.hasSelection} onSelect={() => void copySelection()}><Copy className="mr-2 h-4 w-4" />Copy selection</DropdownMenuItem>
                        <DropdownMenuItem disabled={!terminal.hasOutput} onSelect={downloadOutput}><Download className="mr-2 h-4 w-4" />Download output</DropdownMenuItem>
                        <DropdownMenuItem disabled={!terminal.hasOutput} onSelect={() => { terminal.clear(); if (query) terminal.find(query, false, true) }}><Eraser className="mr-2 h-4 w-4" />Clear output</DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onSelect={() => setModal("settings")}><Settings2 className="mr-2 h-4 w-4" />Terminal settings</DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem disabled={!ready && !busy} className="text-destructive focus:text-destructive" onSelect={() => setModal("end")}><Power className="mr-2 h-4 w-4" />End session…</DropdownMenuItem>
                    </DropdownMenuContent>
                </DropdownMenu>
            </div>
        </header>
        {searchOpen && <div className="node-terminal-search" id={searchId} role="search" aria-label="Find in terminal output" onKeyDown={event => {
            if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); closeSearch() }
        }}>
            <Search size={15} className="shrink-0 text-muted-foreground" aria-hidden="true" />
            <input ref={searchInputRef} aria-label="Find in output" placeholder="Find in output…" value={query} onChange={event => updateQuery(event.target.value)} onKeyDown={event => {
                if (event.key === "Enter") { event.preventDefault(); terminal.find(query, event.shiftKey) }
                if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); closeSearch() }
            }} />
            <span className="node-terminal-match-count" role="status">{query ? terminal.searchResults.resultCount ? `${terminal.searchResults.resultIndex < 0 ? "–" : terminal.searchResults.resultIndex + 1} / ${terminal.searchResults.resultCount}` : "No matches" : ""}</span>
            <Button size="icon" variant="ghost" aria-label="Previous match" disabled={!query || !terminal.searchResults.resultCount} onClick={() => terminal.find(query, true)}><ChevronUp size={17} /></Button>
            <Button size="icon" variant="ghost" aria-label="Next match" disabled={!query || !terminal.searchResults.resultCount} onClick={() => terminal.find(query)}><ChevronDown size={17} /></Button>
            <Button size="icon" variant="ghost" aria-label="Close search" onClick={closeSearch}><X size={16} /></Button>
        </div>}
        {!ready && terminal.hasOutput && connectionNotice}
        <div className="node-terminal-content">
            <div className="node-terminal-screen">
                <div ref={terminal.containerRef} className="node-terminal-emulator" data-testid="terminal-emulator" onPasteCapture={event => {
                    const text = event.clipboardData.getData("text/plain")
                    if (!ready) { event.preventDefault(); event.stopPropagation(); return }
                    if (needsPasteReview(text)) { event.preventDefault(); event.stopPropagation(); setPasteText(text); setModal("paste") }
                }} />
                {!ready && !terminal.hasOutput && <div className="node-terminal-empty">{busy && isOnline ? <Loader2 size={26} className="animate-spin motion-reduce:animate-none text-muted-foreground" aria-hidden="true" /> : <SquareTerminal size={30} className="text-muted-foreground" aria-hidden="true" />}{connectionNotice}</div>}
                {!terminal.atBottom && <Button className="node-terminal-latest" variant="secondary" size="sm" onClick={terminal.jumpToLatest}><ArrowDown size={14} />Jump to latest</Button>}
                {terminal.hasSelection && <Button className="node-terminal-copy" variant="secondary" size="sm" onClick={() => void copySelection()}>{copied ? <Check size={14} /> : <Copy size={14} />}{copied ? "Copied" : "Copy selection"}</Button>}
            </div>
            {commandsOpen && <aside id={commandsId} className="node-terminal-commands" aria-label="Command library" onKeyDown={event => { if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); closeCommands() } }}>
                <div className="flex items-center justify-between"><h3 className="text-sm font-medium">Commands</h3><Button size="icon" variant="ghost" aria-label="Close command library" onClick={closeCommands}><X size={16} /></Button></div>
                <p className="mb-4 text-xs text-muted-foreground">Useful diagnostics, ready to review.</p>
                <div className="space-y-1" role="group" aria-label="Diagnostic commands">{terminalCommands.map((item, index) => <button ref={index === 0 ? firstCommandRef : undefined} key={item.name} type="button" className="node-terminal-command" aria-pressed={selectedCommand === index} onClick={() => setSelectedCommand(index)}><span>{item.name}</span><span>{item.category}</span></button>)}</div>
                <div className="node-terminal-command-preview"><span className="text-xs text-muted-foreground">Command preview</span><code>{command.command}</code><p>{command.description}</p><Button variant="outline" className="w-full" disabled={!ready} onClick={() => { terminal.paste(command.command); setCommandsOpen(false) }}>Insert at cursor</Button><p className="text-xs">Inserts without pressing Enter. Check any existing input before running.</p></div>
            </aside>}
        </div>
        {preferences.touchKeys && <div className="node-terminal-touch" role="group" aria-label="Terminal keys">
            {[{ label: "Esc", value: "\x1b" }, { label: "Tab", value: "\t" }, { label: "Ctrl+C", value: "\x03" }].map(key => <button key={key.label} disabled={!ready} type="button" onClick={() => terminal.sendKey(key.value)}>{key.label}</button>)}
            {[{ label: "Arrow up", value: "\x1b[A", icon: ArrowUp }, { label: "Arrow down", value: "\x1b[B", icon: ArrowDown }, { label: "Arrow left", value: "\x1b[D", icon: ArrowLeft }, { label: "Arrow right", value: "\x1b[C", icon: ArrowRight }].map(key => <button key={key.label} disabled={!ready} type="button" aria-label={key.label} onClick={() => terminal.sendKey(key.value)}><key.icon size={16} /></button>)}
        </div>}
        <footer className="node-terminal-footer"><div><span>{terminal.dimensions.cols > 0 ? `${terminal.dimensions.cols} × ${terminal.dimensions.rows}` : "Terminal"}</span><button type="button" onClick={() => setModal("settings")} aria-label="Change terminal text size">{preferences.fontSize}px</button><span className="node-terminal-shortcut">Ctrl/⌘ Shift M → toolbar</span></div><button type="button" onClick={() => setModal("settings")} aria-label="Keyboard and accessibility settings"><Keyboard size={14} /><span>Keyboard & access</span></button></footer>
        <TerminalDialog open={modal === "settings"} onOpenChange={open => { if (!open) setModal(null) }} title="Terminal settings" description="Your preferences apply to terminals on this browser." onRestoreFocus={restoreFocus}>
            <div className="my-5 space-y-5">
                <div><label htmlFor={`${settingsId}-font`} className="flex justify-between text-sm">Text size <span>{preferences.fontSize}px</span></label><input id={`${settingsId}-font`} className="mt-3 w-full accent-emerald-600" type="range" min={12} max={20} step={1} value={preferences.fontSize} onChange={event => setPreferences(value => ({ ...value, fontSize: Number(event.target.value) }))} /></div>
                <label className="flex cursor-pointer items-start gap-3"><input className="mt-1 h-4 w-4 accent-emerald-600" type="checkbox" checked={preferences.screenReaderMode} onChange={event => setPreferences(value => ({ ...value, screenReaderMode: event.target.checked }))} /><span className="text-sm">Screen reader support<span className="mt-1 block text-xs text-muted-foreground">Expose terminal output to VoiceOver and NVDA.</span></span></label>
                <label className="flex cursor-pointer items-start gap-3"><input className="mt-1 h-4 w-4 accent-emerald-600" type="checkbox" checked={preferences.touchKeys} onChange={event => setPreferences(value => ({ ...value, touchKeys: event.target.checked }))} /><span className="text-sm">Show terminal keys<span className="mt-1 block text-xs text-muted-foreground">Esc, Tab, interrupt, and arrows for touch keyboards.</span></span></label>
                <dl className="node-terminal-keyboard-help"><div><dt>Find output</dt><dd>Ctrl/⌘ Shift F</dd></div><div><dt>Focus toolbar</dt><dd>Ctrl/⌘ Shift M</dd></div><div><dt>Interrupt process</dt><dd>Ctrl C</dd></div><div><dt>Shell history</dt><dd>Ctrl R</dd></div><div><dt>Complete command</dt><dd>Tab</dd></div></dl>
                <p className="text-xs text-muted-foreground">Escape stays available to terminal programs. Clear output removes the buffer and keeps your shell running.</p>
            </div><Button className="w-full" onClick={() => setModal(null)}>Done</Button>
        </TerminalDialog>
        <TerminalDialog open={modal === "paste"} onOpenChange={open => { if (!open) { setModal(null); setPasteText("") } }} title="Review pasted text" description="This text contains line breaks or control characters. Pasting it can run commands immediately. Review it before sending it to this shell." onRestoreFocus={restoreFocus}>
            <label className="sr-only" htmlFor={`${settingsId}-paste`}>Text to paste</label><textarea id={`${settingsId}-paste`} className="node-terminal-paste" value={pasteText} onChange={event => setPasteText(event.target.value)} spellCheck={false} />
            <div className="flex justify-end gap-2"><Button variant="outline" onClick={() => { setModal(null); setPasteText("") }}>Cancel</Button><Button disabled={!ready || !pasteText} onClick={() => { terminal.paste(pasteText); setModal(null); setPasteText("") }}>Paste to {name}</Button></div>
        </TerminalDialog>
        <TerminalDialog open={modal === "end"} onOpenChange={open => { if (!open) setModal(null) }} title={`End shell on ${name}?`} description="This closes the shell and may stop commands running inside it. The output will remain available to copy or download." onRestoreFocus={restoreFocus}>
            <div className="mt-5 flex justify-end gap-2"><Button variant="outline" onClick={() => setModal(null)}>Keep open</Button><Button variant="destructive" onClick={() => { terminal.endSession(); setModal(null) }}>End session</Button></div>
        </TerminalDialog>
    </section>
}
