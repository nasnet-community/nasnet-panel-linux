import type { MouseEvent, ReactElement, ReactNode } from "react"
import { toast as sonner } from "sonner"
import { ToastCard, type CloseMode } from "@/components/toast/toast-card"

// The panel's only door to sonner. Sonner keeps the queue, timers, stacking,
// swipe and live region; every toast renders our ToastCard through
// sonner.custom. The call signature matches the sonner subset the panel uses.

export type ToastId = string | number
export type ToastKind = "success" | "error" | "warning" | "info" | "message" | "loading"

export interface ToastAction {
    label: ReactNode
    onClick: (event: MouseEvent<HTMLButtonElement>) => void
}

export interface ToastOptions {
    id?: ToastId
    description?: ReactNode
    duration?: number
    action?: ToastAction
    cancel?: ToastAction
    icon?: ReactNode
    dismissible?: boolean
    onDismiss?: () => void
    onAutoClose?: () => void
}

export interface CustomToastOptions {
    id?: ToastId
    duration?: number
    dismissible?: boolean
    onDismiss?: () => void
    onAutoClose?: () => void
}

const BASE_MS: Record<ToastKind, number> = {
    success: 4000,
    info: 5000,
    message: 5000,
    warning: 7000,
    error: 8000,
    loading: Infinity,
}
const MAX_MS = 15000
const FREE_CHARS = 60
const MS_PER_CHAR = 60

const CLOSE_MODE: Record<ToastKind, CloseMode> = {
    success: "hover",
    info: "hover",
    message: "hover",
    warning: "always",
    error: "always",
    loading: "none",
}

function textLength(node: ReactNode): number {
    return typeof node === "string" || typeof node === "number" ? String(node).length : 0
}

/** Base time per kind plus reading time for long text, capped at 15 s. */
export function readingTime(kind: ToastKind, title: ReactNode, description?: ReactNode): number {
    const base = BASE_MS[kind]
    if (!Number.isFinite(base)) return base
    const chars = textLength(title) + textLength(description)
    return Math.min(MAX_MS, base + Math.max(0, chars - FREE_CHARS) * MS_PER_CHAR)
}

/**
 * Raw backend errors read as "context: cause: cause". A long one with a short
 * head splits into a title and a detail line; short messages stay as they are.
 */
export function splitError(message: string): { title: string; detail: string } | null {
    if (message.length <= 72) return null
    const at = message.indexOf(": ")
    if (at < 1 || at > 48) return null
    const head = message.slice(0, at).trim()
    const detail = message.slice(at + 2).trim()
    if (!head || !detail) return null
    return { title: head.charAt(0).toUpperCase() + head.slice(1), detail }
}

let seq = 0
const nextId = () => `toast-${++seq}`

// Identical toasts still on screen collapse into one card with a counter.
const byKey = new Map<string, { id: ToastId; count: number }>()
const keyOf = new Map<ToastId, string>()

function dedupeKey(kind: ToastKind, message: ReactNode, description: ReactNode): string | undefined {
    if (typeof message !== "string") return undefined
    if (description != null && typeof description !== "string") return undefined
    return `${kind}\u0000${message}\u0000${description ?? ""}`
}

function forget(id: ToastId) {
    const key = keyOf.get(id)
    if (key === undefined) return
    byKey.delete(key)
    keyOf.delete(id)
}

function show(kind: ToastKind, message: ReactNode, opts: ToastOptions = {}): ToastId {
    let title = message
    let description = opts.description
    let copyText: string | undefined
    if (kind === "error" && typeof message === "string") {
        const split = description == null ? splitError(message) : null
        if (split) {
            title = split.title
            description = split.detail
            copyText = message
        } else if (typeof description === "string" && !opts.action) {
            // An error with its own next step (View node, Retry) doesn't need a copy button.
            copyText = `${message}\n${description}`
        }
    }

    let id = opts.id
    let count = 1
    if (id === undefined) {
        // Loading toasts stand for work in flight; two of them must stay two.
        const key = kind === "loading" ? undefined : dedupeKey(kind, message, opts.description)
        const live = key === undefined ? undefined : byKey.get(key)
        if (live) {
            live.count += 1
            id = live.id
            count = live.count
        } else {
            id = nextId()
            if (key !== undefined) {
                byKey.set(key, { id, count: 1 })
                keyOf.set(id, key)
            }
        }
    } else {
        // An explicit id replaces whatever that toast showed before.
        forget(id)
    }

    // Every update restarts sonner's countdown from its remaining time, which a
    // hover pause shortens; only a new duration resets it to the full value, so
    // a repeat nudges it by a millisecond per count.
    const duration = (opts.duration ?? readingTime(kind, title, description)) + (count > 1 ? count : 0)
    const toastId = id
    const closeMode: CloseMode = opts.dismissible === false ? "none" : CLOSE_MODE[kind]

    const publish = () => {
        sonner.custom(
            (sid) => (
                <ToastCard
                    kind={kind}
                    title={title}
                    description={description}
                    icon={opts.icon}
                    count={count}
                    copyText={copyText}
                    action={opts.action}
                    cancel={opts.cancel}
                    closeMode={closeMode}
                    dismiss={() => sonner.dismiss(sid)}
                    onResize={publish}
                />
            ),
            {
                id: toastId,
                duration,
                dismissible: opts.dismissible,
                onDismiss: () => {
                    forget(toastId)
                    opts.onDismiss?.()
                },
                onAutoClose: () => {
                    forget(toastId)
                    opts.onAutoClose?.()
                },
            },
        )
    }
    publish()
    return toastId
}

type Show = (message: ReactNode, opts?: ToastOptions) => ToastId

export const toast = Object.assign(((message, opts) => show("message", message, opts)) as Show, {
    success: ((message, opts) => show("success", message, opts)) as Show,
    error: ((message, opts) => show("error", message, opts)) as Show,
    warning: ((message, opts) => show("warning", message, opts)) as Show,
    info: ((message, opts) => show("info", message, opts)) as Show,
    message: ((message, opts) => show("message", message, opts)) as Show,
    loading: ((message, opts) => show("loading", message, opts)) as Show,
    dismiss: (id?: ToastId) => {
        sonner.dismiss(id)
    },
    /** A fully custom card, rendered on the same card surface. */
    custom: (render: (id: ToastId) => ReactElement, opts: CustomToastOptions = {}): ToastId => {
        const id = opts.id ?? nextId()
        forget(id)
        sonner.custom((sid) => render(sid), {
            id,
            duration: opts.duration,
            dismissible: opts.dismissible,
            onDismiss: opts.onDismiss,
            onAutoClose: opts.onAutoClose,
        })
        return id
    },
})
