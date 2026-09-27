import { useEffect, useLayoutEffect, useRef, useState, type MouseEvent, type ReactNode } from "react"
import { Check, Copy, Loader2, TriangleAlert, X } from "lucide-react"
import { cn, copyToClipboard } from "@/lib/utils"
import type { ToastAction, ToastKind } from "@/lib/toast"

export type CloseMode = "always" | "hover" | "none"

export interface ToastCardProps {
    kind: ToastKind
    title: ReactNode
    description?: ReactNode
    icon?: ReactNode
    count?: number
    /** Full text behind "Copy details"; the button only shows when set. */
    copyText?: string
    action?: ToastAction
    cancel?: ToastAction
    /** Custom content under the text. */
    footer?: ReactNode
    closeMode: CloseMode
    dismiss: () => void
    /** Called after the card changes height on its own, so sonner re-measures. */
    onResize?: () => void
}

const CHIP: Record<ToastKind, string> = {
    success: "bg-status-success-soft text-(--toast-glyph-success)",
    error: "bg-status-danger-soft text-(--toast-glyph-danger)",
    warning: "bg-status-warning-soft text-(--toast-glyph-warning)",
    info: "bg-status-info-soft text-(--toast-glyph-info)",
    message: "bg-muted text-foreground",
    loading: "bg-muted text-foreground",
}

const TEXT_BUTTON = "inline-flex items-center gap-1.5 rounded-sm text-[13px] leading-[18px] font-medium text-foreground underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"

export const TOAST_BUTTON = "inline-flex h-7 items-center justify-center gap-1.5 rounded-lg border border-(--toast-border) px-2.5 text-[13px] font-medium text-foreground shadow-xs transition-colors hover:bg-accent disabled:pointer-events-none disabled:opacity-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 pointer-coarse:h-10 pointer-coarse:px-3.5 [&_svg]:size-3.5"

function Glyph({ kind }: { kind: ToastKind }) {
    switch (kind) {
        case "success":
            return <Check className="size-4" strokeWidth={2.5} aria-hidden />
        case "error":
            return (
                <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth={2.5} strokeLinecap="round" aria-hidden>
                    <path d="M12 6v7" />
                    <path d="M12 17.5h.01" />
                </svg>
            )
        case "warning":
            return <TriangleAlert className="size-4" strokeWidth={2.25} aria-hidden />
        case "loading":
            return <Loader2 className="size-4 animate-spin" strokeWidth={2.25} aria-hidden />
        default:
            return (
                <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth={2.5} strokeLinecap="round" aria-hidden>
                    <path d="M12 11v6" />
                    <path d="M12 7h.01" />
                </svg>
            )
    }
}

function CopyDetails({ text }: { text: string }) {
    const [copied, setCopied] = useState(false)
    useEffect(() => {
        if (!copied) return
        const t = setTimeout(() => setCopied(false), 1500)
        return () => clearTimeout(t)
    }, [copied])
    return (
        <button type="button" className={TEXT_BUTTON} onClick={async () => setCopied(await copyToClipboard(text))}>
            {copied ? <Check className="size-3.5" aria-hidden /> : <Copy className="size-3.5" aria-hidden />}
            {copied ? "Copied" : "Copy details"}
        </button>
    )
}

export function ToastCard({ kind, title, description, icon, count = 1, copyText, action, cancel, footer, closeMode, dismiss, onResize }: ToastCardProps) {
    const detailRef = useRef<HTMLDivElement>(null)
    const [expanded, setExpanded] = useState(false)
    const [overflows, setOverflows] = useState(false)
    const toggled = useRef(false)
    const clamp = typeof description === "string"

    useLayoutEffect(() => {
        const el = detailRef.current
        if (!el || !clamp || expanded) return
        setOverflows(el.scrollHeight > el.clientHeight + 1)
    }, [clamp, description, expanded])

    useEffect(() => {
        // onResize is a fresh closure on every publish; only the toggle matters here.
        if (toggled.current) onResize?.()
    }, [expanded])

    const hasDescription = description !== undefined && description !== null && description !== ""
    const runAndDismiss = (a: ToastAction) => (event: MouseEvent<HTMLButtonElement>) => {
        a.onClick(event)
        if (!event.defaultPrevented) dismiss()
    }

    return (
        <div
            className={cn(
                "group/toast relative flex w-full items-start gap-3 py-3 pl-3 text-left",
                closeMode === "always" && "pr-10",
                closeMode === "hover" && "pr-10 pointer-coarse:pr-3.5",
                closeMode === "none" && "pr-3.5",
            )}
        >
            <div className={cn("flex size-7 shrink-0 items-center justify-center rounded-full [&_svg]:size-4", CHIP[kind])}>
                {icon ?? <Glyph kind={kind} />}
            </div>
            <div className="min-w-0 flex-1 pt-1">
                <div className="flex items-baseline gap-1.5">
                    <div className="min-w-0 text-sm leading-5 font-medium [overflow-wrap:anywhere]">{title}</div>
                    {count > 1 && (
                        <span className="shrink-0 rounded-full bg-muted px-1.5 text-[12px] leading-[18px] font-medium tabular-nums text-muted-foreground">
                            ×{count}
                        </span>
                    )}
                </div>
                {hasDescription && (
                    <div
                        ref={detailRef}
                        className={cn("mt-0.5 text-[13px] leading-[18px] text-muted-foreground [overflow-wrap:anywhere]", clamp && !expanded && "line-clamp-3")}
                    >
                        {description}
                    </div>
                )}
                {(overflows || copyText) && (
                    <div className="mt-1.5 flex flex-wrap items-center gap-x-3.5 gap-y-1">
                        {overflows && (
                            <button
                                type="button"
                                className={TEXT_BUTTON}
                                aria-expanded={expanded}
                                onClick={() => {
                                    toggled.current = true
                                    setExpanded((v) => !v)
                                }}
                            >
                                {expanded ? "Show less" : "Show more"}
                            </button>
                        )}
                        {copyText && <CopyDetails text={copyText} />}
                    </div>
                )}
                {(action || cancel) && (
                    <div className="mt-2 flex flex-wrap gap-2">
                        {action && (
                            <button type="button" className={TOAST_BUTTON} onClick={runAndDismiss(action)}>
                                {action.label}
                            </button>
                        )}
                        {cancel && (
                            <button type="button" className={cn(TOAST_BUTTON, "border-transparent shadow-none text-muted-foreground")} onClick={runAndDismiss(cancel)}>
                                {cancel.label}
                            </button>
                        )}
                    </div>
                )}
                {footer}
            </div>
            {closeMode !== "none" && (
                <button
                    type="button"
                    aria-label="Dismiss"
                    data-toast-close=""
                    onClick={dismiss}
                    className={cn(
                        "absolute top-2 right-2 flex size-6 items-center justify-center rounded-md text-muted-foreground transition-opacity hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 pointer-coarse:top-1 pointer-coarse:right-1 pointer-coarse:size-9",
                        closeMode === "hover" && "opacity-0 group-hover/toast:opacity-100 group-focus-within/toast:opacity-100 focus-visible:opacity-100 pointer-coarse:hidden",
                    )}
                >
                    <X className="size-3.5" aria-hidden />
                </button>
            )}
        </div>
    )
}
