import { generateUUID } from "@/lib/utils"

interface ErrorEntry {
    id: string
    timestamp: number
    type: "api" | "react" | "network" | "validation" | "unknown"
    message: string
    stack?: string
    context?: Record<string, unknown>
}

interface ErrorLoggerOptions {
    maxEntries?: number
    storageKey?: string
}

class ErrorLogger {
    private entries: ErrorEntry[] = []
    private maxEntries: number
    private storageKey: string
    private listeners: Set<() => void> = new Set()

    constructor(options: ErrorLoggerOptions = {}) {
        this.maxEntries = options.maxEntries ?? 100
        this.storageKey = options.storageKey ?? "app-error-log"
        this.loadFromStorage()
    }

    private loadFromStorage() {
        if (typeof window === "undefined") return
        try {
            const stored = localStorage.getItem(this.storageKey)
            if (stored) {
                this.entries = JSON.parse(stored)
            }
        } catch {
            // Ignore corrupt storage
        }
    }

    private saveToStorage() {
        if (typeof window === "undefined") return
        try {
            localStorage.setItem(this.storageKey, JSON.stringify(this.entries))
        } catch {
            // Storage full, trim older entries
            this.entries = this.entries.slice(-Math.floor(this.maxEntries / 2))
            try {
                localStorage.setItem(this.storageKey, JSON.stringify(this.entries))
            } catch {
                // Give up on persistence
            }
        }
    }

    private notify() {
        this.listeners.forEach((fn) => fn())
    }

    log(entry: Omit<ErrorEntry, "id" | "timestamp">) {
        const full: ErrorEntry = {
            ...entry,
            id: generateUUID(),
            timestamp: Date.now(),
        }

        this.entries.push(full)

        // Trim if over limit
        if (this.entries.length > this.maxEntries) {
            this.entries = this.entries.slice(-this.maxEntries)
        }

        this.saveToStorage()
        this.notify()

        // Also log to console in development
        if (process.env.NODE_ENV === "development") {
            console.error(`[ErrorLogger][${entry.type}]`, entry.message, entry.context)
        }
    }

    getEntries(): ReadonlyArray<ErrorEntry> {
        return this.entries
    }

    getRecentEntries(count: number = 20): ReadonlyArray<ErrorEntry> {
        return this.entries.slice(-count)
    }

    clear() {
        this.entries = []
        this.saveToStorage()
        this.notify()
    }

    subscribe(listener: () => void): () => void {
        this.listeners.add(listener)
        return () => this.listeners.delete(listener)
    }

    get count(): number {
        return this.entries.length
    }
}

export const errorLogger = new ErrorLogger()

// Global error handler
if (typeof window !== "undefined") {
    window.addEventListener("unhandledrejection", (event) => {
        errorLogger.log({
            type: "unknown",
            message: event.reason?.message || String(event.reason),
            stack: event.reason?.stack,
            context: { source: "unhandledrejection" },
        })
    })
}
