import { useEffect } from "react"
import { Ban, CheckCircle2, Download, X } from "lucide-react"
import { Button } from "@/components/ui/button"

export function SelectionBar({ count, canBan, canUnban, busy, onBan, onUnban, onExport, onClear }: {
    count: number
    canBan: boolean
    canUnban: boolean
    busy: boolean
    onBan: () => void
    onUnban: () => void
    onExport: () => void
    onClear: () => void
}) {
    // Escape leaves selection mode unless a dialog or menu is handling it.
    useEffect(() => {
        if (count === 0) return
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape" || e.defaultPrevented || document.querySelector("[role=dialog], [role=menu]")) return
            onClear()
        }
        window.addEventListener("keydown", onKey)
        return () => window.removeEventListener("keydown", onKey)
    }, [count, onClear])

    if (count === 0) return null
    return (
        // A full-width strip that centres the bar: a left-50% bar can only
        // shrink into half the screen, which folds it into a column on phones.
        <div className="pointer-events-none fixed inset-x-0 bottom-[calc(env(safe-area-inset-bottom)+5rem)] z-50 flex justify-center px-4 md:bottom-6">
            <div
                role="toolbar"
                aria-label="Selected users"
                className="pointer-events-auto flex max-w-full flex-wrap items-center justify-center gap-2 rounded-xl border bg-card px-3 py-2.5 shadow-lg animate-in fade-in slide-in-from-bottom-4 duration-200"
            >
                <span className="flex h-7 items-center gap-1.5 rounded-md bg-foreground px-2.5 text-[13px] font-semibold text-background"><span className="font-mono">{count}</span> selected</span>
                <span aria-hidden className="mx-1 hidden h-5 w-px bg-border sm:block" />
                <Button size="sm" variant="outline" disabled={busy || !canBan} onClick={onBan} className="h-8 gap-1.5 border-status-danger/40 text-status-danger hover:bg-status-danger-soft hover:text-status-danger">
                    <Ban className="size-3.5" />Ban
                </Button>
                <Button size="sm" variant="outline" disabled={busy || !canUnban} onClick={onUnban} className="h-8 gap-1.5"><CheckCircle2 className="size-3.5" />Unban</Button>
                <Button size="sm" variant="outline" disabled={busy} onClick={onExport} className="h-8 gap-1.5"><Download className="size-3.5" />Export CSV</Button>
                <span aria-hidden className="mx-1 hidden h-5 w-px bg-border sm:block" />
                <Button size="sm" variant="ghost" onClick={onClear} className="h-8 gap-1 text-muted-foreground"><X className="size-3.5" />Clear</Button>
            </div>
        </div>
    )
}
