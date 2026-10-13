import { useEffect, useRef, useState } from "react"
import { CheckCircle2, Circle, Loader2, XCircle } from "lucide-react"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { toast } from "@/lib/toast"
import { streamMtprotoJob, type MtprotoJob, type MtprotoStep } from "@/lib/api/mtproto"
import { stepLabel } from "./mtproto-labels"

export interface MtprotoJobDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    title: string
    description?: string
    /** The job id, from a job the user's action already started (see startMtprotoJob). */
    job: Promise<string> | null
    onFinished?: (job: MtprotoJob) => void
}

function StepIcon({ state }: { state: MtprotoStep["state"] }) {
    switch (state) {
        case "running":
            return <Loader2 className="size-4 animate-spin text-blue-500" aria-label="Running" />
        case "ok":
            return <CheckCircle2 className="size-4 text-emerald-500" aria-label="Done" />
        case "failed":
            return <XCircle className="size-4 text-red-500" aria-label="Failed" />
        default:
            return <Circle className="size-4 text-muted-foreground/40" aria-label="Waiting" />
    }
}

/** Follows one background job (install, repair, update, rotate, remove) to its end. */
export function MtprotoJobDialog({ open, onOpenChange, title, description, job: started, onFinished }: MtprotoJobDialogProps) {
    const [job, setJob] = useState<MtprotoJob | null>(null)
    const [error, setError] = useState<string | null>(null)
    const [running, setRunning] = useState(false)
    const finishedRef = useRef(onFinished)
    // the latest callback, without re-following the job when the parent re-renders
    useEffect(() => {
        finishedRef.current = onFinished
    })

    useEffect(() => {
        if (!open || !started) return
        let cancelled = false
        const ac = new AbortController()
        ;(async () => {
            setJob(null)
            setError(null)
            setRunning(true)
            let terminal = false
            try {
                const id = await started
                if (cancelled) return
                await streamMtprotoJob(
                    id,
                    {
                        onProgress: (j) => {
                            if (!cancelled) setJob(j)
                        },
                        onDone: (j) => {
                            terminal = true
                            if (cancelled) return
                            setJob(j)
                            setRunning(false)
                            if (j.state === "ok") toast.success(`${title} finished`)
                            else toast.error(j.error || `${title} failed`)
                            finishedRef.current?.(j)
                        },
                        onError: (e) => {
                            terminal = true
                            if (cancelled) return
                            setError(e.message)
                            setRunning(false)
                            toast.error(e.message)
                        },
                    },
                    ac.signal,
                )
                if (!cancelled && !terminal) {
                    setError("The progress connection ended. The job may still be running: check the node before retrying.")
                    setRunning(false)
                }
            } catch (e) {
                if (!cancelled) {
                    const msg = e instanceof Error ? e.message : String(e)
                    setError(msg)
                    setRunning(false)
                    toast.error(msg)
                }
            }
        })()
        return () => {
            cancelled = true
            ac.abort()
        }
    }, [open, started, title])

    const failed = error != null || job?.state === "failed"
    const done = job?.state === "ok"
    const message = error ?? (job?.state === "failed" ? job.error : null)

    return (
        <Dialog open={open} onOpenChange={(next) => (running ? undefined : onOpenChange(next))}>
            <DialogContent
                className="sm:max-w-md"
                onEscapeKeyDown={(e) => running && e.preventDefault()}
                onInteractOutside={(e) => running && e.preventDefault()}
            >
                <DialogHeader>
                    <DialogTitle>{title}</DialogTitle>
                    {description && <DialogDescription>{description}</DialogDescription>}
                </DialogHeader>

                <ol className="space-y-2" aria-label="Progress">
                    {(job?.steps ?? []).map((s) => (
                        <li key={s.name} className="flex items-start gap-2 text-sm">
                            <span className="mt-0.5"><StepIcon state={s.state} /></span>
                            <span className="min-w-0">
                                <span className={s.state === "failed" ? "text-red-500" : s.state === "pending" ? "text-muted-foreground" : undefined}>{stepLabel(s.name, job?.kind)}</span>
                                {s.state === "failed" && s.detail && (
                                    <span className="mt-0.5 block break-words text-xs text-muted-foreground">{s.detail}</span>
                                )}
                            </span>
                        </li>
                    ))}
                    {!job && !error && (
                        <li className="flex items-center gap-2 text-sm text-muted-foreground">
                            <Loader2 className="size-4 animate-spin" /> Starting…
                        </li>
                    )}
                </ol>

                {failed && message && (
                    <p role="alert" className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400">
                        {message}
                    </p>
                )}

                <DialogFooter>
                    <Button type="button" variant={done ? "default" : "outline"} disabled={running} onClick={() => onOpenChange(false)}>
                        {running ? "Working…" : done ? "Done" : "Close"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
