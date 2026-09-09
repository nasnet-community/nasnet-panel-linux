import type { ITheme, Terminal } from "@xterm/xterm"

export interface TerminalPreferences {
    fontSize: number
    screenReaderMode: boolean
    touchKeys: boolean
}

const preferencesKey = "nasnet-terminal-preferences-v1"

export function readTerminalPreferences(): TerminalPreferences {
    const defaults = { fontSize: 14, screenReaderMode: false, touchKeys: window.matchMedia?.("(pointer: coarse)").matches ?? false }
    try {
        const saved = JSON.parse(localStorage.getItem(preferencesKey) ?? "null")
        if (!saved || typeof saved !== "object") return defaults
        return {
            fontSize: Number.isFinite(saved.fontSize) ? Math.min(20, Math.max(12, Math.round(saved.fontSize))) : defaults.fontSize,
            screenReaderMode: saved.screenReaderMode === true,
            touchKeys: typeof saved.touchKeys === "boolean" ? saved.touchKeys : defaults.touchKeys,
        }
    } catch { return defaults }
}

export function saveTerminalPreferences(preferences: TerminalPreferences) {
    try { localStorage.setItem(preferencesKey, JSON.stringify(preferences)) } catch { /* Storage may be unavailable. */ }
}

export function terminalTheme(dark: boolean): ITheme {
    return dark ? {
        background: "#0d1113", foreground: "#dce5e7", cursor: "#71d9ad", cursorAccent: "#0d1113",
        selectionBackground: "#71d9ad40", selectionInactiveBackground: "#71d9ad25",
        black: "#172024", red: "#f98289", green: "#71d9ad", yellow: "#e8c67a",
        blue: "#82b5f5", magenta: "#ce9df2", cyan: "#7ad5df", white: "#dce5e7",
        brightBlack: "#8c9da4", brightRed: "#ffacb0", brightGreen: "#a2eac8", brightYellow: "#f4dda3",
        brightBlue: "#b2d2ff", brightMagenta: "#e2bcff", brightCyan: "#adebf2", brightWhite: "#ffffff",
    } : {
        background: "#fbfdfc", foreground: "#243436", cursor: "#13794f", cursorAccent: "#fbfdfc",
        selectionBackground: "#13794f30", selectionInactiveBackground: "#13794f18",
        black: "#243436", red: "#b42332", green: "#13794f", yellow: "#845b05",
        blue: "#225da8", magenta: "#8244a1", cyan: "#086d7c", white: "#516368",
        brightBlack: "#64757a", brightRed: "#bd2637", brightGreen: "#167646", brightYellow: "#7a590b",
        brightBlue: "#195ea9", brightMagenta: "#8544a6", brightCyan: "#076c78", brightWhite: "#172b30",
    }
}

/** Join soft wraps, but preserve actual newlines in the scrollback. */
export function terminalOutput(terminal: Terminal): string {
    const buffer = terminal.buffer.active
    const lines: string[] = []
    for (let i = 0; i < buffer.length; i++) {
        const line = buffer.getLine(i)
        if (!line) continue
        const nextIsWrapped = buffer.getLine(i + 1)?.isWrapped ?? false
        const text = line.translateToString(!nextIsWrapped)
        if (line.isWrapped && lines.length) lines[lines.length - 1] += text
        else lines.push(text)
    }
    return lines.join("\n").trimEnd()
}

export function needsPasteReview(text: string): boolean {
    for (const character of text) {
        const code = character.charCodeAt(0)
        if ((code < 32 && code !== 9) || code === 127) return true
    }
    return false
}

export const terminalCommands = [
    { name: "Disk usage", category: "Storage", command: "df -h", description: "Show filesystem capacity and available space." },
    { name: "Memory usage", category: "System", command: "free -h", description: "Show used and available memory and swap." },
    { name: "Uptime & load", category: "System", command: "uptime", description: "Show how long this host has been running and its load averages." },
    { name: "Listening ports", category: "Network", command: "ss -tuln", description: "List listening TCP and UDP sockets without resolving hostnames." },
    { name: "Current directory", category: "Shell", command: "pwd", description: "Print the shell's current working directory." },
] as const
