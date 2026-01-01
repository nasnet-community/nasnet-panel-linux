import { useState, useCallback, useRef, useEffect } from "react"
import { useUpdateUserNotes } from "@/lib/queries"
import { cn } from "@/lib/utils"

interface AdminNotesPanelProps {
    userId: number
    initialNotes: string
}

export function AdminNotesPanel({ userId, initialNotes }: AdminNotesPanelProps) {
    const [notes, setNotes] = useState(initialNotes || "")
    const [saved, setSaved] = useState(false)
    const updateNotes = useUpdateUserNotes()
    const timerRef = useRef<NodeJS.Timeout | null>(null)
    const lastSavedRef = useRef(initialNotes || "")

    useEffect(() => {
        setNotes(initialNotes || "")
        lastSavedRef.current = initialNotes || ""
    }, [initialNotes])

    const save = useCallback((value: string) => {
        if (value === lastSavedRef.current) return
        lastSavedRef.current = value
        updateNotes.mutate(
            { userId, notes: value },
            {
                onSuccess: () => {
                    setSaved(true)
                    setTimeout(() => setSaved(false), 2000)
                },
            }
        )
    }, [userId, updateNotes])

    const handleBlur = () => {
        if (timerRef.current) clearTimeout(timerRef.current)
        save(notes)
    }

    const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
        const value = e.target.value
        setNotes(value)
        if (timerRef.current) clearTimeout(timerRef.current)
        timerRef.current = setTimeout(() => save(value), 2000)
    }

    return (
        <div className="space-y-1.5">
            <div className="flex items-center justify-between">
                <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70 font-semibold">Admin Notes</span>
                {saved && (
                    <span className="text-[10px] text-emerald-500 font-medium animate-in fade-in duration-200">Saved</span>
                )}
                {updateNotes.isPending && (
                    <span className="text-[10px] text-muted-foreground font-medium">Saving...</span>
                )}
            </div>
            <textarea
                value={notes}
                onChange={handleChange}
                onBlur={handleBlur}
                placeholder="Add notes about this user..."
                rows={3}
                className={cn(
                    "w-full rounded-md border bg-transparent px-3 py-2 text-sm",
                    "placeholder:text-muted-foreground/50 resize-none",
                    "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
                    "transition-colors"
                )}
            />
        </div>
    )
}
