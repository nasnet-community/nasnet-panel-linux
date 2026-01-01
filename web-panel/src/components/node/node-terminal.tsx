import { useEffect, useRef, useState, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { HiOutlineTerminal, HiOutlineTrash, HiOutlineArrowsExpand, HiOutlineX } from "react-icons/hi"
import { cn } from "@/lib/utils"
import { getApiBaseUrl } from '@/lib/config'
import { ReconnectingWebSocket } from '@/lib/realtime'

interface NodeTerminalProps {
    nodeId: number
    isOnline: boolean
}

type ConnectionStatus = "disconnected" | "connecting" | "connected" | "error"

const statusConfig: Record<ConnectionStatus, { label: string; className: string }> = {
    disconnected: { label: "Disconnected", className: "bg-gray-500/20 text-gray-400 border-gray-500/30" },
    connecting: { label: "Connecting...", className: "bg-yellow-500/20 text-yellow-400 border-yellow-500/30" },
    connected: { label: "Connected", className: "bg-green-500/20 text-green-400 border-green-500/30" },
    error: { label: "Error", className: "bg-red-500/20 text-red-400 border-red-500/30" },
}

export function NodeTerminal({ nodeId, isOnline }: NodeTerminalProps) {
    const terminalRef = useRef<HTMLDivElement>(null)
    const terminalInstanceRef = useRef<any>(null)
    const wsRef = useRef<ReconnectingWebSocket | null>(null)
    const fitAddonRef = useRef<any>(null)
    const [status, setStatus] = useState<ConnectionStatus>("disconnected")
    const [isFullscreen, setIsFullscreen] = useState(false)

    const handleClear = useCallback(() => {
        if (terminalInstanceRef.current) {
            terminalInstanceRef.current.clear()
        }
    }, [])

    // Connect to terminal
    const connect = useCallback(async () => {
        if (!isOnline || !terminalRef.current) return

        setStatus("connecting")

        // Dynamically import xterm to avoid SSR issues
        const { Terminal } = await import("@xterm/xterm")
        const { FitAddon } = await import("@xterm/addon-fit")
        const { WebLinksAddon } = await import("@xterm/addon-web-links")

        // Create terminal instance
        const term = new Terminal({
            cursorBlink: true,
            cursorStyle: "block",
            fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', Consolas, monospace",
            fontSize: 13,
            lineHeight: 1.2,
            theme: {
                background: "#0d0d0d",
                foreground: "#22c55e",
                cursor: "#22c55e",
                cursorAccent: "#0d0d0d",
                selectionBackground: "#22c55e40",
                black: "#000000",
                red: "#ef4444",
                green: "#22c55e",
                yellow: "#eab308",
                blue: "#3b82f6",
                magenta: "#a855f7",
                cyan: "#06b6d4",
                white: "#f5f5f5",
                brightBlack: "#404040",
                brightRed: "#f87171",
                brightGreen: "#4ade80",
                brightYellow: "#facc15",
                brightBlue: "#60a5fa",
                brightMagenta: "#c084fc",
                brightCyan: "#22d3ee",
                brightWhite: "#ffffff",
            },
            allowProposedApi: true,
        })

        // Load addons
        const fitAddon = new FitAddon()
        term.loadAddon(fitAddon)
        term.loadAddon(new WebLinksAddon())

        // Open terminal
        term.open(terminalRef.current)
        fitAddon.fit()

        terminalInstanceRef.current = term
        fitAddonRef.current = fitAddon

        // Build WebSocket URL
        const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
        const apiUrl = getApiBaseUrl()
        // apiUrl may be a full URL (server-side) or a relative base path like "/your-panel-path" (client-side)
        let host: string
        try {
            host = apiUrl && apiUrl.startsWith("http") ? new URL(apiUrl).host : window.location.host
        } catch {
            host = window.location.host
        }
        const basePath = getApiBaseUrl()
        const wsUrl = `${protocol}//${host}${basePath}/api/v1/nodes/${nodeId}/terminal/ws`

        // Wire xterm input handlers once. They capture `wsRef` (not a raw ws
        // instance) so each keystroke sends through whatever socket is
        // currently live, tolerating reconnects without rewiring handlers.
        const encoder = new TextEncoder()
        term.onData((data) => {
            wsRef.current?.send(encoder.encode(data).buffer)
        })
        term.onBinary((data) => {
            const buffer = new Uint8Array(data.length)
            for (let i = 0; i < data.length; i++) {
                buffer[i] = data.charCodeAt(i) & 255
            }
            wsRef.current?.send(buffer.buffer)
        })

        const ws = new ReconnectingWebSocket({
            url: wsUrl,
            binaryType: "arraybuffer",
            // Buffer outbound bytes while reconnecting — short typing bursts
            // should not be lost when the tunnel flaps.
            bufferWhenDisconnected: true,
            maxBufferedMessages: 256,
            // Keepalive: JSON ping every 20s; close the socket if nothing
            // echoes back within 30s.
            pingIntervalMs: 20_000,
            pongTimeoutMs: 30_000,
            pingPayload: () => JSON.stringify({ ping: true }),
            onOpen: () => {
                term.writeln("\x1b[2m--- Terminal connected ---\x1b[0m")
                term.focus()
                // (Re)announce terminal size on every (re)connect.
                wsRef.current?.send(JSON.stringify({ resize: { cols: term.cols, rows: term.rows } }))
            },
            onStatus: (s) => {
                switch (s) {
                    case "connecting":
                        setStatus("connecting")
                        break
                    case "reconnecting":
                        setStatus("connecting")
                        term.writeln("\x1b[33m--- Reconnecting... ---\x1b[0m")
                        break
                    case "connected":
                        setStatus("connected")
                        break
                    case "closed":
                        setStatus("disconnected")
                        term.writeln("\x1b[2m--- Terminal disconnected ---\x1b[0m")
                        break
                }
            },
            onMessage: (ev) => {
                if (typeof ev.data === "string") {
                    // Text frame = JSON control message (exit_code, error, pong)
                    try {
                        const msg = JSON.parse(ev.data)
                        if (msg.pong) return
                        if (msg.exit_code !== undefined) {
                            term.writeln(`\r\n\x1b[2m--- Shell exited (code: ${msg.exit_code}) ---\x1b[0m`)
                        }
                        if (msg.error) {
                            term.writeln(`\r\n\x1b[31m--- Error: ${msg.error} ---\x1b[0m`)
                        }
                    } catch {
                        term.write(ev.data)
                    }
                } else {
                    // Binary frame = raw PTY output
                    term.write(new Uint8Array(ev.data as ArrayBuffer))
                }
            },
        })
        wsRef.current = ws
        ws.start()

        // Handle window resize
        const handleResize = () => {
            if (fitAddonRef.current) {
                fitAddonRef.current.fit()
            }
            if (term) {
                wsRef.current?.send(JSON.stringify({ resize: { cols: term.cols, rows: term.rows } }))
            }
        }

        window.addEventListener("resize", handleResize)

        // Also fit when container might have changed
        const resizeObserver = new ResizeObserver(() => {
            if (fitAddonRef.current) {
                requestAnimationFrame(() => {
                    fitAddonRef.current?.fit()
                })
            }
        })
        if (terminalRef.current) {
            resizeObserver.observe(terminalRef.current)
        }

        return () => {
            window.removeEventListener("resize", handleResize)
            resizeObserver.disconnect()
            if (wsRef.current) {
                wsRef.current.close()
                wsRef.current = null
            }
            if (terminalInstanceRef.current) {
                terminalInstanceRef.current.dispose()
                terminalInstanceRef.current = null
            }
        }
    }, [nodeId, isOnline])

    useEffect(() => {
        if (!isOnline) {
            setStatus("disconnected")
            return
        }

        let cleanup: (() => void) | undefined

        connect().then((cleanupFn) => {
            cleanup = cleanupFn
        })

        return () => {
            cleanup?.()
            if (wsRef.current) {
                wsRef.current.close()
                wsRef.current = null
            }
            if (terminalInstanceRef.current) {
                terminalInstanceRef.current.dispose()
                terminalInstanceRef.current = null
            }
        }
    }, [connect, isOnline])

    // Handle fullscreen resize
    useEffect(() => {
        if (!fitAddonRef.current) return

        // Small delay to allow transition to complete
        const timer = setTimeout(() => {
            fitAddonRef.current.fit()
            if (terminalInstanceRef.current) {
                const { cols, rows } = terminalInstanceRef.current
                wsRef.current?.send(JSON.stringify({ resize: { cols, rows } }))
            }
            // Focus terminal to ensure captures input
            terminalInstanceRef.current?.focus()
        }, 300)

        return () => clearTimeout(timer)
    }, [isFullscreen])

    const toggleFullscreen = () => setIsFullscreen(!isFullscreen)

    return (
        <Card className={cn(
            "flex flex-col bg-card/50 backdrop-blur-sm border-border/50 transition-all duration-200",
            isFullscreen ? "fixed inset-0 z-50 h-screen rounded-none bg-background/95 backdrop-blur-xl" : "h-[600px]"
        )}>
            <CardHeader className="border-b border-border/50 bg-muted/10 py-3 shrink-0">
                <div className="flex items-center justify-between">
                    <CardTitle className="flex items-center gap-2 text-base font-medium">
                        <HiOutlineTerminal className="w-5 h-5 text-primary" />
                        Interactive Terminal
                    </CardTitle>
                    <div className="flex items-center gap-2">
                        <Button
                            variant="ghost"
                            size="sm"
                            onClick={handleClear}
                            className="h-8 px-3"
                            disabled={!isOnline}
                        >
                            <HiOutlineTrash className="w-4 h-4 mr-1" />
                            Clear
                        </Button>
                        <Button
                            variant="ghost"
                            size="sm"
                            onClick={toggleFullscreen}
                            className="h-8 w-8 p-0"
                            title={isFullscreen ? "Exit Fullscreen" : "Fullscreen"}
                            aria-label={isFullscreen ? "Exit Fullscreen" : "Fullscreen"}
                        >
                            {isFullscreen ? <HiOutlineX className="w-4 h-4" /> : <HiOutlineArrowsExpand className="w-4 h-4" />}
                        </Button>
                        <Badge
                            variant="outline"
                            className={cn("text-xs", statusConfig[status].className)}
                        >
                            <span className={cn(
                                "w-1.5 h-1.5 rounded-full mr-1.5",
                                status === "connected" && "bg-green-400",
                                status === "connecting" && "bg-yellow-400 animate-pulse",
                                status === "error" && "bg-red-400",
                                status === "disconnected" && "bg-gray-400"
                            )} />
                            {statusConfig[status].label}
                        </Badge>
                    </div>
                </div>
            </CardHeader>
            <CardContent className="p-0 flex-1 overflow-hidden">
                {!isOnline ? (
                    <div className="h-full flex items-center justify-center text-muted-foreground">
                        <div className="text-center">
                            <HiOutlineTerminal className="w-12 h-12 mx-auto mb-3 opacity-30" />
                            <p className="text-sm">Node is offline</p>
                            <p className="text-xs opacity-70 mt-1">Terminal requires an online node</p>
                        </div>
                    </div>
                ) : (
                    <div
                        ref={terminalRef}
                        className="h-full w-full bg-[#0d0d0d] rounded-b-lg"
                        style={{ padding: "8px" }}
                    />
                )}
            </CardContent>
        </Card >
    )
}
