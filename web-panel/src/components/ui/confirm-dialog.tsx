import { useState, useCallback, createContext, useContext, useRef, useEffect } from "react"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"
import { AlertTriangle, Loader2 } from "lucide-react"

interface ConfirmOptions {
    title: React.ReactNode
    description: React.ReactNode
    confirmLabel?: string
    cancelLabel?: string
    variant?: "default" | "destructive" | "warning"
    /** When set, user must type this exact string to enable confirm */
    typeToConfirm?: string
    /** When set, promise-based confirm runs this before resolving; button shows spinner */
    onConfirm?: () => Promise<void>
    /** Show a warning icon in the header */
    icon?: React.ReactNode
}

type ConfirmFn = (options: ConfirmOptions) => Promise<boolean>

const ConfirmContext = createContext<ConfirmFn | null>(null)

export function useConfirm(): ConfirmFn {
    const fn = useContext(ConfirmContext)
    if (!fn) throw new Error("useConfirm must be used within ConfirmDialogProvider")
    return fn
}

export function ConfirmDialogProvider({ children }: { children: React.ReactNode }) {
    const [open, setOpen] = useState(false)
    const [options, setOptions] = useState<ConfirmOptions>({
        title: "",
        description: "",
    })
    const [typed, setTyped] = useState("")
    const [pending, setPending] = useState(false)
    const resolveRef = useRef<((value: boolean) => void) | null>(null)

    const confirm: ConfirmFn = useCallback((opts) => {
        setOptions(opts)
        setTyped("")
        setPending(false)
        setOpen(true)
        return new Promise<boolean>((resolve) => {
            resolveRef.current = resolve
        })
    }, [])

    const close = (value: boolean) => {
        setOpen(false)
        resolveRef.current?.(value)
        resolveRef.current = null
    }

    const handleConfirm = async () => {
        if (options.onConfirm) {
            setPending(true)
            try {
                await options.onConfirm()
                close(true)
            } catch {
                setPending(false)
                // keep dialog open on error so user sees toast and can retry
            }
        } else {
            close(true)
        }
    }

    const handleCancel = () => close(false)

    useEffect(() => {
        if (!open) setPending(false)
    }, [open])

    const typeMatch = !options.typeToConfirm || typed === options.typeToConfirm
    const isDestructive = options.variant === "destructive"
    const isWarning = options.variant === "warning"
    const showIcon = options.icon ?? ((isDestructive || isWarning) ? <AlertTriangle className="h-5 w-5" /> : null)

    return (
        <ConfirmContext.Provider value={confirm}>
            {children}
            <Dialog open={open} onOpenChange={(v) => { if (!v && !pending) handleCancel() }}>
                <DialogContent className="sm:max-w-[425px]">
                    <DialogHeader>
                        <DialogTitle className={cn(
                            "flex items-center gap-2",
                            isDestructive && "text-red-600 dark:text-red-500",
                            isWarning && "text-amber-600 dark:text-amber-500",
                        )}>
                            {showIcon}
                            {options.title}
                        </DialogTitle>
                        <DialogDescription asChild>
                            <div className="text-sm text-muted-foreground">{options.description}</div>
                        </DialogDescription>
                    </DialogHeader>

                    {options.typeToConfirm && (
                        <div className="space-y-1.5">
                            <label className="text-xs text-muted-foreground">
                                Type <span className="font-mono font-semibold text-foreground">{options.typeToConfirm}</span> to confirm
                            </label>
                            <Input
                                autoFocus
                                value={typed}
                                onChange={(e) => setTyped(e.target.value)}
                                placeholder={options.typeToConfirm}
                                className="font-mono text-sm"
                                disabled={pending}
                            />
                        </div>
                    )}

                    <DialogFooter className="gap-2 sm:gap-0">
                        <Button variant="outline" onClick={handleCancel} disabled={pending}>
                            {options.cancelLabel || "Cancel"}
                        </Button>
                        <Button
                            variant={isDestructive ? "destructive" : "default"}
                            onClick={handleConfirm}
                            disabled={pending || !typeMatch}
                            className={cn(
                                isDestructive && "bg-red-600 hover:bg-red-700",
                                isWarning && "bg-amber-600 hover:bg-amber-700 text-white",
                            )}
                        >
                            {pending && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
                            {options.confirmLabel || "Confirm"}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </ConfirmContext.Provider>
    )
}
