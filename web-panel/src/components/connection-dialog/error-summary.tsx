import { AlertCircle } from "lucide-react"
import type { FieldErrors } from "react-hook-form"
import { flattenErrors } from "@/lib/form-errors"

export interface SummaryError {
    label: string
    tab: string
}

interface ErrorSummaryProps {
    errors: SummaryError[]
    onJump: (tab: string) => void
}

// Footer line shown after a failed submit: how many fields need attention and
// which, each a link to the tab that holds it. Replaces the "Please fix
// validation errors" toast, which said neither.
export function ErrorSummary({ errors, onJump }: ErrorSummaryProps) {
    if (errors.length === 0) return null
    const seen = new Set<string>()
    const unique = errors.filter((e) => {
        if (seen.has(e.label)) return false
        seen.add(e.label)
        return true
    })
    const shown = unique.slice(0, 3)
    const more = unique.length - shown.length
    return (
        <div className="flex items-center gap-2 text-status-danger">
            <AlertCircle className="h-4 w-4 shrink-0" />
            <span className="truncate">
                {errors.length} {errors.length === 1 ? "field needs" : "fields need"} attention:{" "}
                {shown.map((e, i) => (
                    <span key={e.label}>
                        {i > 0 && ", "}
                        <button
                            type="button"
                            onClick={() => onJump(e.tab)}
                            className="underline underline-offset-[3px] hover:text-foreground"
                        >
                            {e.label}
                        </button>
                    </span>
                ))}
                {more > 0 && ` and ${more} more`}
            </span>
        </div>
    )
}

// Messages for a nested settings object (tls_settings, reality_settings…)
// whose form is a plain controlled component without FormField wiring.
export function NestedErrors({ errors }: { errors: unknown }) {
    if (!errors || typeof errors !== "object") return null
    const messages = flattenErrors(errors as FieldErrors).map((e) => e.message)
    if (messages.length === 0) return null
    return (
        <ul className="space-y-1">
            {messages.map((m) => (
                <li key={m} className="flex items-center gap-1.5 text-xs text-status-danger">
                    <AlertCircle className="h-3.5 w-3.5 shrink-0" />{m}
                </li>
            ))}
        </ul>
    )
}

export function UnsavedIndicator() {
    return (
        <div className="flex items-center gap-2 text-text-tertiary">
            <span className="h-1.5 w-1.5 rounded-full bg-status-warning" />
            Unsaved changes
        </div>
    )
}
