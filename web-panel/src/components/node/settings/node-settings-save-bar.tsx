import { useLayoutEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Loader2, ArrowUpRight } from "lucide-react"
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

export function NodeSettingsSaveBar({ settingsForm, onSave, onReview }: {
    settingsForm: NodeSettingsForm; onSave: () => void; onReview: () => void
}) {
    const { isDirty, isSaving, reset, dirtyFields } = settingsForm
    const anchor = useRef<HTMLDivElement>(null)
    const [bounds, setBounds] = useState<{ left: number; width: number } | null>(null)
    useLayoutEffect(() => {
        if (!isDirty || !anchor.current) return
        const element = anchor.current
        const measure = () => {
            const rect = element.getBoundingClientRect()
            setBounds({ left: rect.left, width: rect.width })
        }
        measure()
        const observer = new ResizeObserver(measure)
        observer.observe(element)
        window.addEventListener("resize", measure)
        return () => { observer.disconnect(); window.removeEventListener("resize", measure) }
    }, [isDirty])
    if (!isDirty) return null
    return (
        <div ref={anchor} className="h-28 sm:h-20">
        <div style={bounds || undefined} className="fixed bottom-[calc(4.5rem+env(safe-area-inset-bottom))] z-40 md:bottom-[calc(1rem+env(safe-area-inset-bottom))] flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border bg-card/95 px-4 py-3 shadow-lg backdrop-blur-md" role="region" aria-label="Unsaved settings">
            <button type="button" onClick={onReview} className="flex items-center gap-2 text-sm hover:underline underline-offset-4">
                <span className="h-1.5 w-1.5 rounded-full bg-amber-500" />
                {dirtyFields.length} unsaved {dirtyFields.length === 1 ? "change" : "changes"}
                <ArrowUpRight className="size-3.5 text-muted-foreground" />
            </button>
            <div className="ml-auto flex items-center gap-2">
                <Button type="button" variant="ghost" size="sm" onClick={reset} disabled={isSaving}>Discard</Button>
                <Button type="button" size="sm" onClick={onSave} disabled={isSaving}>
                    {isSaving && <Loader2 className="size-4 animate-spin" />}{isSaving ? "Saving…" : "Save changes"}
                </Button>
            </div>
        </div>
        </div>
    )
}
