import { useEffect, useState } from "react"
import { Ban, Info, TriangleAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { cn } from "@/lib/utils"
import { confirmWithFallback, isRejected, remainingSeconds, verdictSeverity } from "@/lib/api/network"
import type { NetworkApply, NetworkPlan, Verdict, VerdictLevel } from "@/lib/types/network"

type Step = "preview" | "running" | "armed" | "kept" | "reverted"

interface Props {
    open: boolean
    onOpenChange: (open: boolean) => void
    plan: NetworkPlan | null
    planning: boolean
    /** Why the dry run failed, so the dialog is never blank. */
    planError?: string | null
    applied: NetworkApply | null
    applyError?: string | null
    /** Address the plan will move the panel to, "" when it will not move. */
    altOrigin: string
    /** Carries the typed CONFIRM through to the request V18 reads. */
    onApply: (confirmed: boolean) => void
    onDone: () => void
}

const VERDICT_STYLE: Record<VerdictLevel, { icon: typeof Info; tone: string; word: string }> = {
    reject: { icon: Ban, tone: "text-status-danger", word: "Blocks this change" },
    confirm: { icon: TriangleAlert, tone: "text-status-warning", word: "Needs confirmation" },
    warn: { icon: Info, tone: "text-status-info", word: "Worth knowing" },
}

function VerdictList({ verdicts }: { verdicts: Verdict[] }) {
    const sorted = [...verdicts].sort((a, b) => verdictSeverity(b.level) - verdictSeverity(a.level))
    return (
        <ul className="space-y-2.5">
            {sorted.map((v) => {
                const style = VERDICT_STYLE[v.level] ?? VERDICT_STYLE.warn
                const Icon = style.icon
                return (
                    <li key={`${v.rule}-${v.message}`} className="flex gap-2.5 text-sm">
                        <Icon className={cn("mt-0.5 h-4 w-4 shrink-0", style.tone)} />
                        <div className="space-y-0.5">
                            <p>{v.message}</p>
                            <p className="text-text-tertiary text-xs">
                                <span className={style.tone}>{style.word}</span>
                                <span className="font-mono"> · {v.rule}</span>
                            </p>
                        </div>
                    </li>
                )
            })}
        </ul>
    )
}

export function ApplyDialog({
    open,
    onOpenChange,
    plan,
    planning,
    planError,
    applied,
    applyError,
    altOrigin,
    onApply,
    onDone,
}: Props) {
    const [step, setStep] = useState<Step>("preview")
    const [typed, setTyped] = useState("")
    const [left, setLeft] = useState(0)
    const [total, setTotal] = useState(1)

    const rejected = plan ? isRejected(plan.verdicts ?? []) : false
    const needsConfirm = plan?.verdicts?.some((v) => v.level === "confirm") ?? false

    useEffect(() => {
        if (!open) {
            setStep("preview")
            setTyped("")
        }
    }, [open])

    useEffect(() => {
        if (applied) {
            setStep("armed")
            setTotal(Math.max(1, remainingSeconds(applied.confirm_deadline_unix)))
        }
    }, [applied])

    // A failed apply leaves the dialog stuck on "Applying…" otherwise.
    useEffect(() => {
        if (applyError) setStep("preview")
    }, [applyError])

    // Countdown drives the armed step; the server deadline is the only clock
    // that matters, so recompute from it rather than decrementing.
    useEffect(() => {
        if (step !== "armed" || !applied) return
        const tick = () => {
            const r = remainingSeconds(applied.confirm_deadline_unix)
            setLeft(r)
            if (r === 0) setStep("reverted")
        }
        tick()
        const id = setInterval(tick, 1000)
        return () => clearInterval(id)
    }, [step, applied])

    async function keep() {
        if (!applied) return
        const ok = await confirmWithFallback(
            applied.plan_id,
            applied.confirm_deadline_unix,
            altOrigin,
        )
        setStep(ok ? "kept" : "reverted")
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-2xl">
                {step === "preview" && (
                    <>
                        <DialogHeader>
                            <DialogTitle>Review the change</DialogTitle>
                            <DialogDescription>
                                Nothing has been applied yet. These are the operations that will run.
                            </DialogDescription>
                        </DialogHeader>

                        {planning && (
                            <p className="text-text-secondary text-sm">Working out the steps…</p>
                        )}

                        {planError && (
                            <div className="border-status-danger/30 bg-status-danger/[0.06] flex gap-2.5 rounded-lg border p-3 text-sm">
                                <Ban className="text-status-danger mt-0.5 h-4 w-4 shrink-0" />
                                <div>
                                    <p className="font-medium">Could not plan this change</p>
                                    <p className="text-text-secondary">{planError}</p>
                                </div>
                            </div>
                        )}

                        {applyError && (
                            <div className="border-status-danger/30 bg-status-danger/[0.06] flex gap-2.5 rounded-lg border p-3 text-sm">
                                <Ban className="text-status-danger mt-0.5 h-4 w-4 shrink-0" />
                                <div>
                                    <p className="font-medium">Apply failed — nothing changed</p>
                                    <p className="text-text-secondary">{applyError}</p>
                                </div>
                            </div>
                        )}

                        {plan && (plan.ops?.length ?? 0) > 0 && (
                            <div className="space-y-2">
                                <p className="text-text-tertiary text-[11px] font-medium uppercase tracking-[0.12em]">
                                    Steps, in order
                                </p>
                                <ol className="space-y-1.5">
                                    {plan.ops?.map((op, i) => (
                                        <li key={op} className="flex gap-3 text-sm">
                                            <span className="text-text-tertiary w-4 shrink-0 text-right font-mono text-xs tabular-nums">
                                                {i + 1}
                                            </span>
                                            <span className="font-mono text-xs leading-5">{op}</span>
                                        </li>
                                    ))}
                                </ol>
                            </div>
                        )}

                        {plan && (plan.verdicts?.length ?? 0) > 0 && (
                            <div className="border-border-subtle space-y-2 border-t pt-4">
                                <VerdictList verdicts={plan.verdicts} />
                            </div>
                        )}

                        {needsConfirm && !rejected && (
                            <div className="space-y-2">
                                <Label htmlFor="network-confirm" className="text-sm font-normal">
                                    This one can cut your access. Type{" "}
                                    <span className="font-mono font-medium">CONFIRM</span> to unlock
                                    Apply.
                                </Label>
                                <Input
                                    id="network-confirm"
                                    value={typed}
                                    onChange={(e) => setTyped(e.target.value)}
                                    placeholder="CONFIRM"
                                    autoComplete="off"
                                    className="font-mono"
                                />
                            </div>
                        )}

                        <DialogFooter>
                            <Button variant="outline" onClick={() => onOpenChange(false)}>
                                Cancel
                            </Button>
                            <Button
                                disabled={
                                    planning ||
                                    !plan ||
                                    rejected ||
                                    (needsConfirm && typed !== "CONFIRM")
                                }
                                onClick={() => {
                                    setStep("running")
                                    onApply(needsConfirm)
                                }}
                            >
                                Apply
                            </Button>
                        </DialogFooter>
                    </>
                )}

                {step === "running" && (
                    <DialogHeader>
                        <DialogTitle>Applying…</DialogTitle>
                        <DialogDescription>
                            The panel may become briefly unreachable while the network re-configures.
                        </DialogDescription>
                    </DialogHeader>
                )}

                {step === "armed" && (
                    <>
                        <DialogHeader>
                            <DialogTitle>Keep these settings?</DialogTitle>
                            <DialogDescription>
                                Reverting automatically unless you confirm. If you have lost access,
                                do nothing and the change rolls back on its own.
                            </DialogDescription>
                        </DialogHeader>

                        <div className="space-y-2">
                            <p className="text-status-warning font-mono text-3xl font-medium tabular-nums">
                                {left}s
                            </p>
                            <div className="bg-surface-3 h-[3px] overflow-hidden rounded-full">
                                <div
                                    aria-hidden
                                    className="bg-status-warning h-full transition-[width] duration-1000 ease-linear"
                                    style={{ width: `${Math.min(100, (left / total) * 100)}%` }}
                                />
                            </div>
                        </div>

                        <DialogFooter>
                            <Button size="lg" onClick={() => void keep()}>
                                Keep these settings
                            </Button>
                        </DialogFooter>
                    </>
                )}

                {step === "kept" && (
                    <>
                        <DialogHeader>
                            <DialogTitle>Settings kept</DialogTitle>
                            <DialogDescription>The change has been confirmed.</DialogDescription>
                        </DialogHeader>
                        <DialogFooter>
                            <Button
                                onClick={() => {
                                    onDone()
                                    onOpenChange(false)
                                }}
                            >
                                Close
                            </Button>
                        </DialogFooter>
                    </>
                )}

                {step === "reverted" && (
                    <>
                        <DialogHeader>
                            <DialogTitle>Reverted</DialogTitle>
                            <DialogDescription>
                                The change was rolled back automatically.
                            </DialogDescription>
                        </DialogHeader>
                        <DialogFooter>
                            <Button
                                onClick={() => {
                                    onDone()
                                    onOpenChange(false)
                                }}
                            >
                                Close
                            </Button>
                        </DialogFooter>
                    </>
                )}
            </DialogContent>
        </Dialog>
    )
}
