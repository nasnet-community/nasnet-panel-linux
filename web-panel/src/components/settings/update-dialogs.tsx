import { useEffect, useRef, useState, type ReactNode } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { CheckCircle2, Circle, CloudDownload, DatabaseBackup, FileUp, Loader2, RotateCcw, ShieldCheck, TriangleAlert, XCircle } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { streamUpdateJob, updatesApi, type UpdateJob, type UpdateOffer, type UpdateStatus, type UpdateStep, type UpdateUpload } from "@/lib/api/updates"
import { queryKeys } from "@/lib/queries/keys"
import { useApplyUpdate } from "@/lib/queries/use-updates"
import { mb, plainNotes, routeLabel, took } from "@/lib/updates-state"
import { toast } from "@/lib/toast"
import { cn } from "@/lib/utils"

const DOWNTIME = "The panel, the bot, subscription pages and this router's Xray are down for about a minute. Remote nodes keep serving users, and reverse nodes reconnect on their own."

/** datetime-local's value for a moment, in the browser's time. */
function localInput(d: Date) {
    const p = (n: number) => String(n).padStart(2, "0")
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

function nextFourAM() {
    const d = new Date()
    d.setHours(4, 0, 0, 0)
    if (d.getTime() < Date.now() + 10 * 60_000) d.setDate(d.getDate() + 1)
    return d
}

/** Now, or a time the browser picks in its own zone. */
function When({ later, setLater, at, setAt }: { later: boolean; setLater: (v: boolean) => void; at: string; setAt: (v: string) => void }) {
    return (
        <div className="space-y-2">
            <div className="flex items-center gap-3">
                <span className="text-sm text-muted-foreground">When</span>
                <span role="radiogroup" aria-label="When" className="inline-flex gap-0.5 rounded-lg border border-border/50 bg-muted/50 p-[3px]">
                    {([[false, "Now"], [true, "At a time"]] as const).map(([v, label]) => (
                        <button
                            key={label} type="button" role="radio" aria-checked={later === v} onClick={() => setLater(v)}
                            className={cn("h-7 rounded-md px-3 text-[13px] font-medium transition-colors",
                                later === v ? "border border-border/80 bg-background text-foreground shadow-sm" : "border border-transparent text-muted-foreground hover:text-foreground")}
                        >{label}</button>
                    ))}
                </span>
            </div>
            {later && (
                <div className="space-y-1">
                    <label htmlFor="update-at" className="text-sm text-muted-foreground">Date and time</label>
                    <Input id="update-at" type="datetime-local" value={at} onChange={e => setAt(e.target.value)} className="h-9" />
                    <p className="text-xs text-muted-foreground">Your browser's time. If the panel is down then, it skips the update and tells you.</p>
                </div>
            )}
        </div>
    )
}

function Line({ icon: Icon, children }: { icon: typeof CloudDownload; children: ReactNode }) {
    return (
        <div className="flex items-start gap-2 text-sm">
            <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
            <span>{children}</span>
        </div>
    )
}

function useWhen() {
    const [later, setLater] = useState(false)
    const [at, setAt] = useState(() => localInput(nextFourAM()))
    const [opened] = useState(() => Date.now())
    const atDate = new Date(at)
    // the server refuses a past time too; this only keeps the button honest
    const badTime = later && !(atDate.getTime() > opened)
    return { later, setLater, at, setAt, atISO: later ? atDate.toISOString() : undefined, badTime }
}

export interface UpdateDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    status: UpdateStatus
    offer: UpdateOffer
    onStarted: (jobId: string) => void
}

/** Update… on an offered release: now or at a time. */
export function UpdateDialog({ open, onOpenChange, status, offer, onStarted }: UpdateDialogProps) {
    const when = useWhen()
    const apply = useApplyUpdate()
    const from = status.current
    const route = routeLabel(status.check?.route, status.mirror_url)
    const run = () => apply.mutate({ version: offer.version, at: when.atISO }, {
        onSuccess: res => {
            onOpenChange(false)
            if (res.job_id) onStarted(res.job_id)
        },
    })
    return (
        <Dialog open={open} onOpenChange={o => { if (!apply.isPending) onOpenChange(o) }}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>Update to {offer.version}</DialogTitle>
                    <DialogDescription>From {from}, on the {status.channel} channel.</DialogDescription>
                </DialogHeader>
                {offer.prerelease && (
                    <p className="rounded-md bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400">A prerelease from the beta channel. It's for testing and may change before it ships.</p>
                )}
                <div className="space-y-2">
                    <Line icon={CloudDownload}>{when.later ? `Downloads ${mb(offer.size)} at that time` : `Downloads ${mb(offer.size)} from ${route}`}</Line>
                    <Line icon={ShieldCheck}>Checks the signature and every file before anything installs</Line>
                    <Line icon={DatabaseBackup}>Backs up the database first</Line>
                    <Line icon={RotateCcw}>Puts {from} back if {offer.version} isn't healthy within 2 minutes</Line>
                </div>
                <p className="text-xs text-muted-foreground">{DOWNTIME}</p>
                <When {...when} />
                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)} disabled={apply.isPending}>Cancel</Button>
                    <Button onClick={run} disabled={apply.isPending || when.badTime}>
                        {apply.isPending && <Loader2 className="size-4 animate-spin" />}{when.later ? "Schedule" : "Update now"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}

/** Why the server refused a file, as a title and what to do. */
function refusal(message: string, arch: string): { title: string; text: string } {
    const m = message.replace(/^update: /, "")
    if (/signature|signed|key this panel trusts/i.test(m)) return { title: "This isn't a signed NasNet release", text: "Its signature doesn't match any key this panel trusts." }
    const other = /bundle is for (\w+)/i.exec(m)
    if (other) return { title: `This bundle is for ${other[1]}`, text: `This server is ${arch}. Upload nasnet-panel-offline-linux-${arch}.tar.gz.` }
    const older = /^(v\S+) isn't newer than (v\S+)/i.exec(m)
    if (older) return { title: `${older[1]} is older than this panel`, text: `You run ${older[2]}, and updates never go backwards.` }
    if (/over 300 MB/i.test(m)) return { title: "The file is too big", text: "An update file is at most 300 MB. Pick the offline bundle for this server." }
    return { title: "This file can't be installed", text: m }
}

export interface UploadDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    status: UpdateStatus
    arch: string
    onStarted: (jobId: string) => void
}

/** Upload… : the offline bundle for a server that can't reach GitHub. */
export function UploadDialog({ open, onOpenChange, status, arch, onStarted }: UploadDialogProps) {
    const [file, setFile] = useState<File | null>(null)
    const [sent, setSent] = useState(0)
    const [busy, setBusy] = useState(false)
    const [up, setUp] = useState<UpdateUpload | null>(null)
    const [error, setError] = useState<{ title: string; text: string } | null>(null)
    const picker = useRef<HTMLInputElement>(null)
    const ac = useRef<AbortController | null>(null)
    const when = useWhen()
    const apply = useApplyUpdate()

    const close = () => {
        ac.current?.abort()
        setFile(null); setSent(0); setBusy(false); setUp(null); setError(null)
        onOpenChange(false)
    }

    const send = async (f: File) => {
        setFile(f); setUp(null); setError(null); setSent(0); setBusy(true)
        ac.current = new AbortController()
        try {
            setUp(await updatesApi.upload(f, n => setSent(n), ac.current.signal))
        } catch (e) {
            if (e instanceof DOMException && e.name === "AbortError") return
            setError(refusal(e instanceof Error ? e.message : String(e), arch))
        } finally {
            setBusy(false)
        }
    }

    const run = () => up && apply.mutate({ upload_id: up.upload_id, at: when.atISO }, {
        onSuccess: res => {
            close()
            if (res.job_id) onStarted(res.job_id)
        },
    })
    const choose = () => picker.current?.click()

    return (
        <Dialog open={open} onOpenChange={o => { if (!o && !apply.isPending) close() }}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>Upload an update file</DialogTitle>
                    <DialogDescription>The offline bundle for this server's architecture: {arch}.</DialogDescription>
                </DialogHeader>
                <input
                    ref={picker} type="file" accept=".tar.gz,.tgz,application/gzip" className="hidden"
                    onChange={e => { const f = e.target.files?.[0]; e.target.value = ""; if (f) void send(f) }}
                />
                {!file && (
                    <button
                        type="button" onClick={choose}
                        className="flex w-full flex-col items-center gap-2 rounded-lg border border-dashed border-border px-4 py-6 text-center text-sm transition-colors hover:bg-muted/40"
                    >
                        <FileUp className="size-5 text-muted-foreground" aria-hidden />
                        <span className="font-medium">Choose nasnet-panel-offline-linux-{arch}.tar.gz</span>
                        <span className="text-xs text-muted-foreground">Up to 300 MB. Download it from the release page on a computer that can reach GitHub. Nothing installs until it passes the signature check.</span>
                    </button>
                )}
                {file && (
                    <div className="space-y-2 rounded-lg border border-border/60 px-3 py-2.5">
                        <div className="flex items-center justify-between gap-3 text-sm">
                            <span className="min-w-0 truncate font-mono text-[13px]">{file.name}</span>
                            <span className="shrink-0 text-xs text-muted-foreground">{mb(file.size)}</span>
                        </div>
                        {busy && (
                            <>
                                <Progress value={file.size ? (sent / file.size) * 100 : 0} className="h-1.5" />
                                <p className="text-xs text-muted-foreground">{sent < file.size ? `Uploading · ${mb(sent)} of ${mb(file.size)}` : "Checking the signature…"}</p>
                            </>
                        )}
                        {up && (
                            <div className="space-y-1 text-sm">
                                <p className="flex items-center gap-1.5 text-emerald-600 dark:text-emerald-500"><CheckCircle2 className="size-4" aria-hidden />Signed release, for {up.arch}</p>
                                <p>Update {status.current} → <span className="font-mono">{up.version}</span>{up.prerelease ? " · beta" : ""}</p>
                                {up.notes && <p className="line-clamp-3 text-xs text-muted-foreground">{plainNotes(up.notes).join(" ")}</p>}
                            </div>
                        )}
                    </div>
                )}
                {error && (
                    <div role="alert" className="space-y-1 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm">
                        <p className="font-medium text-red-600 dark:text-red-400">{error.title}</p>
                        <p className="text-red-600/90 dark:text-red-400/90">{error.text}</p>
                        <p className="text-xs text-muted-foreground">Nothing was installed, and the file was deleted from the server.</p>
                    </div>
                )}
                {up && (
                    <>
                        <p className="text-xs text-muted-foreground">{DOWNTIME}</p>
                        <When {...when} />
                    </>
                )}
                <DialogFooter>
                    <Button variant="outline" onClick={close} disabled={apply.isPending}>{error ? "Close" : "Cancel"}</Button>
                    {error && <Button onClick={choose}>Choose another file</Button>}
                    {up && (
                        <Button onClick={run} disabled={apply.isPending || when.badTime}>
                            {apply.isPending && <Loader2 className="size-4 animate-spin" />}{when.later ? "Schedule" : "Update now"}
                        </Button>
                    )}
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}

function StepIcon({ status }: { status: UpdateStep["status"] }) {
    switch (status) {
        case "running": return <Loader2 className="size-4 animate-spin text-blue-500" aria-label="Running" />
        case "done": return <CheckCircle2 className="size-4 text-emerald-500" aria-label="Done" />
        case "failed": return <XCircle className="size-4 text-red-500" aria-label="Failed" />
        default: return <Circle className="size-4 text-muted-foreground/40" aria-label="Waiting" />
    }
}

/** The toast a finished job leaves behind. */
function finishToast(j: UpdateJob) {
    const secs = j.finished_at ? took({ started_at: j.started_at, finished_at: j.finished_at }) : ""
    if (j.kind === "rollback") {
        if (j.result === "ok") toast.success(`Rolled back to ${j.to}`, { description: "The database wasn't touched." })
        else toast.error("The rollback didn't finish", { description: j.error })
        return
    }
    if (j.result === "ok") toast.success(`Updated to ${j.to}`, { description: `Took ${secs}. Roll back stays open for 7 days.` })
    else if (j.result === "rolled_back") toast.error(`Update failed, back on ${j.from}`, { description: j.error })
    else toast.error("The update didn't run", { description: j.error })
}

export interface UpdateProgressDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    jobId: string | null
}

/**
 * Follows a job: the stream while the panel runs it, then, once the restart
 * cuts the stream, the job file through whichever version answers.
 */
export function UpdateProgressDialog({ open, onOpenChange, jobId }: UpdateProgressDialogProps) {
    const qc = useQueryClient()
    const [job, setJob] = useState<UpdateJob | null>(null)
    const [away, setAway] = useState(0) // seconds the panel hasn't answered for
    const [error, setError] = useState<string | null>(null)

    useEffect(() => {
        if (!open || !jobId) return
        let cancelled = false
        const ac = new AbortController()
        const finish = (j: UpdateJob) => {
            setJob(j)
            finishToast(j)
            void qc.invalidateQueries({ queryKey: queryKeys.settingsStatus() })
            void qc.invalidateQueries({ queryKey: queryKeys.updates() })
        }
        ;(async () => {
            setJob(null); setAway(0); setError(null)
            let done = false
            try {
                await streamUpdateJob(jobId, {
                    onProgress: j => { if (!cancelled) setJob(j) },
                    onDone: j => { done = true; if (!cancelled) finish(j) },
                }, ac.signal)
            } catch { /* the restart cuts it */ }
            if (done || cancelled) return
            // the restart: ask until the job file says how it went
            const since = Date.now()
            while (!cancelled) {
                await new Promise(r => setTimeout(r, 2000))
                if (cancelled) return
                try {
                    const st = await updatesApi.status()
                    setAway(0)
                    if (st.job?.id !== jobId) { setError("The job is gone. Check Settings → Updates."); return }
                    setJob(st.job)
                    if (st.job.phase === "done") { finish(st.job); return }
                } catch {
                    setAway(Math.round((Date.now() - since) / 1000))
                }
            }
        })()
        return () => { cancelled = true; ac.abort() }
    }, [open, jobId, qc])

    const running = !!job && job.phase !== "done" && !error
    const title = job?.kind === "rollback" ? `Roll back to ${job.to}` : job ? `Update to ${job.to}` : "Update"
    const steps = (job?.steps ?? []).filter(s => s.status !== "skipped")
    const restarting = job?.steps.find(s => s.id === "restart")?.status === "running" || (running && away > 0)
    const updated = job?.phase === "done" && job.result === "ok" && job.kind === "update"
    const message = error ?? (job?.phase === "done" && job.result !== "ok" ? job.error : null)

    return (
        <Dialog open={open} onOpenChange={o => (running ? undefined : onOpenChange(o))}>
            <DialogContent
                className="sm:max-w-md"
                onEscapeKeyDown={e => running && e.preventDefault()}
                onInteractOutside={e => running && e.preventDefault()}
            >
                <DialogHeader>
                    <DialogTitle>{title}</DialogTitle>
                    <DialogDescription>It keeps going if you close this tab.</DialogDescription>
                </DialogHeader>
                <ol className="space-y-2" aria-label="Progress">
                    {steps.map(s => (
                        <li key={s.id} className="flex items-start gap-2 text-sm">
                            <span className="mt-0.5"><StepIcon status={s.status} /></span>
                            <span className="min-w-0">
                                <span className={s.status === "failed" ? "text-red-500" : s.status === "waiting" ? "text-muted-foreground" : undefined}>{s.label}</span>
                                {s.detail && s.status !== "waiting" && (
                                    <span className="mt-0.5 block break-words text-xs text-muted-foreground">{s.detail}</span>
                                )}
                                {s.id === "restart" && restarting && away > 0 && (
                                    <span className="mt-0.5 block text-xs text-muted-foreground">{away < 180 ? "Waiting for the panel to come back…" : `Still waiting · ${Math.round(away / 60)} min`}</span>
                                )}
                            </span>
                        </li>
                    ))}
                    {!job && !error && (
                        <li className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />Starting…</li>
                    )}
                </ol>
                {away >= 180 && running && (
                    <p role="alert" className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-400">
                        <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />The panel hasn't come back. Check the server. This page keeps trying.
                    </p>
                )}
                {message && (
                    <p role="alert" className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400">{message}</p>
                )}
                <DialogFooter>
                    {updated ? (
                        // the page still runs the old version's code
                        <Button onClick={() => window.location.reload()}>Reload the page</Button>
                    ) : (
                        <Button variant="outline" disabled={running && away < 180} onClick={() => onOpenChange(false)}>
                            {running ? "Working…" : "Close"}
                        </Button>
                    )}
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
