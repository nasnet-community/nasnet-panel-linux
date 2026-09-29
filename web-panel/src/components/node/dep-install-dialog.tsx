import { useEffect, useRef, useState } from "react"
import { Loader2, CheckCircle2, XCircle, MinusCircle, Circle, AlertTriangle } from "lucide-react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import { cn } from "@/lib/utils"
import {
    startDepInstall,
    streamDepInstall,
    type InstallProgress,
    type InstallStep,
} from "@/lib/api/core-deps"

export interface DepInstallDialogProps {
    nodeId: number
    cores: string[]
    /** When true, remove the cores' dependencies instead of installing them. */
    uninstall?: boolean
    open: boolean
    onOpenChange: (open: boolean) => void
    onComplete?: () => void
}

function StepIcon({ state }: { state: InstallStep["state"] }) {
    switch (state) {
        case "running":
            return <Loader2 className="size-4 animate-spin text-blue-500" />
        case "ok":
            return <CheckCircle2 className="size-4 text-emerald-500" />
        case "skipped":
            return <MinusCircle className="size-4 text-muted-foreground" />
        case "failed":
            return <XCircle className="size-4 text-red-500" />
        default:
            return <Circle className="size-4 text-muted-foreground/40" />
    }
}

const STATUS_LABEL: Record<string, string> = {
    pending: "Preparing…",
    running: "Installing dependencies…",
    succeeded: "Completed",
    partial: "Completed with warnings",
    failed: "Failed",
}

export function DepInstallDialog({
    nodeId,
    cores,
    uninstall = false,
    open,
    onOpenChange,
    onComplete,
}: DepInstallDialogProps) {
    const verb = uninstall ? "Uninstall" : "Install"
    const [progress, setProgress] = useState<InstallProgress | null>(null)
    const [log, setLog] = useState<string[]>([])
    const [error, setError] = useState<string | null>(null)
    const [running, setRunning] = useState(false)
    const abortRef = useRef<AbortController | null>(null)

    useEffect(() => {
        if (!open) return
        let cancelled = false
        const ac = new AbortController()
        abortRef.current = ac
        setProgress(null)
        setLog([])
        setError(null)
        setRunning(true)

        ;(async () => {
            let terminal = false
            try {
                const res = await startDepInstall(nodeId, { cores, uninstall })
                if (!res.success || !res.data?.job_id) {
                    throw new Error(res.error || `Failed to start ${uninstall ? "uninstall" : "install"}`)
                }
                await streamDepInstall(
                    nodeId,
                    res.data.job_id,
                    {
                        onProgress: (p) => {
                            if (cancelled) return
                            setProgress(p)
                            if (p.log_lines?.length) setLog((prev) => [...prev, ...p.log_lines])
                        },
                        onDone: () => {
                            terminal = true
                            if (cancelled) return
                            setRunning(false)
                            onComplete?.()
                        },
                        onError: (e) => {
                            terminal = true
                            if (cancelled) return
                            setError(e.message)
                            setRunning(false)
                        },
                    },
                    ac.signal,
                )
                if (!cancelled && !terminal) {
                    setError("Progress connection ended. The node may still be working; refresh Setup status before retrying.")
                    setRunning(false)
                }
            } catch (e) {
                if (!cancelled) {
                    setError(e instanceof Error ? e.message : String(e))
                    setRunning(false)
                }
            }
        })()

        return () => {
            cancelled = true
            ac.abort()
        }
    }, [open])

    const steps = progress?.steps ?? []
    const doneCount = steps.filter((s) => s.state === "ok" || s.state === "skipped" || s.state === "failed").length
    const pct = steps.length ? Math.round((doneCount / steps.length) * 100) : 0
    const status = error ? "failed" : progress?.status ?? "pending"

    return (
        <Dialog
            open={open}
            onOpenChange={(o) => {
                if (running) return // block close while installing
                onOpenChange(o)
            }}
        >
            <DialogContent className="max-w-lg max-h-[90dvh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        {running && <Loader2 className="size-4 animate-spin" />}
                        {uninstall ? "Uninstalling dependencies" : "Installing dependencies"}
                    </DialogTitle>
                    <DialogDescription>
                        {cores.join(", ")}
                        {!uninstall && " · pinned release, checksum verified"}
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4">
                    <div className="space-y-1.5">
                        <div className="flex items-center justify-between text-sm">
                            <span
                                className={cn(
                                    "font-medium",
                                    status === "failed" && "text-red-500",
                                    status === "partial" && "text-amber-500",
                                    status === "succeeded" && "text-emerald-500",
                                )}
                            >
                                {uninstall && (status === "running" || status === "pending")
                                    ? "Removing…"
                                    : STATUS_LABEL[status] ?? status}
                            </span>
                            <span className="text-xs text-muted-foreground">
                                {steps.length ? `${doneCount} of ${steps.length} steps` : "Preparing steps"}
                            </span>
                        </div>
                        <Progress value={pct} aria-label="Installation progress" indicatorClassName={status === "failed" ? "bg-red-500" : status === "succeeded" ? "bg-emerald-500" : undefined} />
                    </div>

                    {error && (
                        <div role="alert" className="flex items-start gap-2 rounded-md border border-red-500/30 bg-red-500/5 p-2.5 text-sm text-red-500">
                            <AlertTriangle className="mt-0.5 size-4 shrink-0" />
                            <span className="break-words">{error}</span>
                        </div>
                    )}

                    <div className="max-h-56 space-y-1 overflow-y-auto rounded-md border p-1">
                        {steps.length === 0 && (
                            <div className="p-3 text-center text-sm text-muted-foreground">
                                {running ? "Starting…" : "No steps."}
                            </div>
                        )}
                        {steps.map((s) => (
                            <div key={`${s.core_type}:${s.name}`} className="flex items-start gap-2 rounded px-2 py-1.5 text-sm">
                                <span className="mt-0.5">
                                    <StepIcon state={s.state} />
                                </span>
                                <div className="min-w-0 flex-1">
                                    <div className="flex items-center gap-2">
                                        <span className="font-medium">{s.name}</span>
                                        <span className="font-mono text-[12px] text-muted-foreground">{s.core_type}</span>
                                    </div>
                                    {s.detail && s.state !== "ok" && (
                                        <p
                                            className={cn(
                                                "text-xs",
                                                s.state === "failed"
                                                    ? "break-words text-red-500"
                                                    : "truncate text-muted-foreground",
                                            )}
                                            title={s.detail}
                                        >
                                            {s.detail}
                                        </p>
                                    )}
                                </div>
                            </div>
                        ))}
                    </div>

                    {log.length > 0 && (
                        <Accordion type="single" collapsible>
                            <AccordionItem value="log" className="border-b-0">
                                <AccordionTrigger className="py-2 text-xs text-muted-foreground">
                                    Full log ({log.length} lines)
                                </AccordionTrigger>
                                <AccordionContent>
                                    <div className="max-h-48 overflow-y-auto rounded-md border bg-muted/30">
                                        <pre className="whitespace-pre-wrap p-2 font-mono text-[12px] leading-relaxed">
                                            {log.join("\n")}
                                        </pre>
                                    </div>
                                </AccordionContent>
                            </AccordionItem>
                        </Accordion>
                    )}

                    <div className="flex items-center justify-between gap-3 border-t pt-4">
                        <span className="text-xs text-muted-foreground">{running ? "Changes are being applied on the node." : status === "succeeded" ? "Dependency status will refresh automatically." : "Check the step details before retrying."}</span>
                        <Button variant={running ? "outline" : "default"} disabled={running} onClick={() => onOpenChange(false)}>
                            {running ? `${verb}ing…` : "Close"}
                        </Button>
                    </div>
                </div>
            </DialogContent>
        </Dialog>
    )
}
