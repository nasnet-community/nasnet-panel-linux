import { useState, useEffect, useRef } from "react"
import { Check, Copy, Loader2, RefreshCw, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { cn, copyToClipboard } from "@/lib/utils"
import { toast } from "sonner"

export interface EditableFieldProps {
    /** Canonical server value. Used to determine dirty state. */
    value: string
    /** Called when user clicks Apply. Must throw to indicate failure (button stays in pending). */
    onApply: (next: string) => Promise<void> | void
    /** Optional placeholder for the input. */
    placeholder?: string
    /** Monospace + xs font (for keys, uuids, tokens). */
    mono?: boolean
    /** Show a copy button. */
    copyable?: boolean
    /** Show a regenerate button that calls the provided generator and sets the input to the result. */
    regenerate?: () => string
    /** Compact / uncompact styling toggle. */
    size?: "sm" | "md"
    /** Disable all interaction. */
    disabled?: boolean
    /** Override the Apply label. */
    applyLabel?: string
    /** Input type — default "text". */
    type?: "text" | "password"
    /** aria-label for the input. */
    ariaLabel?: string
    /** Notify parent of dirty state changes (optional). */
    onDirtyChange?: (dirty: boolean) => void
}

/**
 * Editable text field with dirty-state tracking, apply/discard buttons, and
 * optional copy/regenerate helpers. Unifies the repeated pattern used across
 * the subscription sheet (label, key, uuid, custom limits).
 */
export function EditableField({
    value,
    onApply,
    placeholder,
    mono = false,
    copyable = false,
    regenerate,
    size = "sm",
    disabled = false,
    applyLabel = "Apply",
    type = "text",
    ariaLabel,
    onDirtyChange,
}: EditableFieldProps) {
    const [draft, setDraft] = useState(value)
    const [applying, setApplying] = useState(false)
    const [copied, setCopied] = useState(false)
    const syncedValueRef = useRef(value)

    const trimmed = draft.trim()
    const dirty = trimmed !== "" && trimmed !== value

    useEffect(() => {
        // Re-sync the draft from the canonical value only when the server value
        // actually changes (new prop) AND the user isn't mid-edit.
        if (syncedValueRef.current !== value) {
            syncedValueRef.current = value
            if (!dirty) setDraft(value)
        }
    }, [value, dirty])

    useEffect(() => {
        onDirtyChange?.(dirty)
    }, [dirty, onDirtyChange])

    const handleApply = async () => {
        if (!dirty || applying) return
        setApplying(true)
        try {
            await onApply(trimmed)
        } finally {
            setApplying(false)
        }
    }

    const handleDiscard = () => setDraft(value)

    const handleCopy = async () => {
        if (!draft) return
        await copyToClipboard(draft)
        setCopied(true)
        toast.success("Copied to clipboard")
        setTimeout(() => setCopied(false), 1500)
    }

    const inputClass = cn(
        "flex-1",
        size === "sm" ? "h-7" : "h-9",
        mono && "font-mono text-xs",
        dirty && "border-amber-500/70 bg-amber-500/10 dark:bg-amber-500/10",
    )
    const btnSize = size === "sm" ? "h-7" : "h-9"

    return (
        <div className="flex gap-1.5 items-center">
            <Input
                type={type}
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === "Enter") handleApply()
                    if (e.key === "Escape" && dirty) handleDiscard()
                }}
                placeholder={placeholder}
                className={inputClass}
                disabled={disabled || applying}
                aria-label={ariaLabel}
            />
            {copyable && (
                <Button
                    variant="outline"
                    size="sm"
                    className={btnSize}
                    onClick={handleCopy}
                    disabled={disabled || !draft}
                    aria-label="Copy to clipboard"
                    type="button"
                >
                    {copied ? <Check className="w-3.5 h-3.5 text-emerald-500" /> : <Copy className="w-3.5 h-3.5" />}
                </Button>
            )}
            {regenerate && (
                <Button
                    variant="outline"
                    size="sm"
                    className={btnSize}
                    onClick={() => setDraft(regenerate())}
                    disabled={disabled || applying}
                    aria-label="Generate new value"
                    type="button"
                >
                    <RefreshCw className="w-3.5 h-3.5" />
                </Button>
            )}
            {dirty && (
                <>
                    <Button
                        variant="ghost"
                        size="sm"
                        className={btnSize}
                        onClick={handleDiscard}
                        disabled={applying}
                        aria-label="Discard changes"
                        type="button"
                    >
                        <X className="w-3.5 h-3.5" />
                    </Button>
                    <Button
                        size="sm"
                        className={btnSize}
                        onClick={handleApply}
                        disabled={disabled || applying}
                        type="button"
                    >
                        {applying ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : applyLabel}
                    </Button>
                </>
            )}
        </div>
    )
}
