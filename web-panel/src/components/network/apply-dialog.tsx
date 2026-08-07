import { useEffect, useState } from "react"
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
import { Badge } from "@/components/ui/badge"
import { confirmWithFallback, isRejected, remainingSeconds, verdictSeverity } from "@/lib/api/network"
import type { NetworkApply, NetworkPlan, Verdict } from "@/lib/types/network"

type Step = "preview" | "running" | "armed" | "kept" | "reverted"

interface Props {
    open: boolean
    onOpenChange: (open: boolean) => void
    plan: NetworkPlan | null
    planning: boolean
    applied: NetworkApply | null
    /** Address the plan will move the panel to, "" when it will not move. */
    altOrigin: string
    onApply: () => void
    onDone: () => void
}

function VerdictList({ verdicts }: { verdicts: Verdict[] }) {
    const sorted = [...verdicts].sort((a, b) => verdictSeverity(b.level) - verdictSeverity(a.level))
    return (
        <ul className="space-y-2">
            {sorted.map((v) => (
                <li key={`${v.rule}-${v.message}`} className="flex gap-2 text-sm">
                    <Badge
                        variant={
                            v.level === "reject"
                                ? "destructive"
                                : v.level === "confirm"
                                  ? "default"
                                  : "secondary"
                        }
                    >
                        {v.rule}
                    </Badge>
                    <span>{v.message}</span>
                </li>
            ))}
        </ul>
    )
}

export function ApplyDialog({
    open,
    onOpenChange,
    plan,
    planning,
    applied,
    altOrigin,
    onApply,
    onDone,
}: Props) {
    const [step, setStep] = useState<Step>("preview")
    const [typed, setTyped] = useState("")
    const [left, setLeft] = useState(0)

    const rejected = plan ? isRejected(plan.verdicts ?? []) : false
    const needsConfirm = plan?.verdicts?.some((v) => v.level === "confirm") ?? false

    useEffect(() => {
        if (!open) {
            setStep("preview")
            setTyped("")
        }
    }, [open])

    useEffect(() => {
        if (applied) setStep("armed")
    }, [applied])

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

                        {planning && <p className="text-muted-foreground text-sm">Planning…</p>}

                        {plan && (plan.ops?.length ?? 0) > 0 && (
                            <ol className="list-inside list-decimal space-y-1 text-sm">
                                {plan.ops?.map((op) => (
                                    <li key={op}>{op}</li>
                                ))}
                            </ol>
                        )}

                        {plan && (plan.verdicts?.length ?? 0) > 0 && <VerdictList verdicts={plan.verdicts} />}

                        {needsConfirm && !rejected && (
                            <div className="space-y-2">
                                <p className="text-sm">Type CONFIRM to continue.</p>
                                <Input
                                    value={typed}
                                    onChange={(e) => setTyped(e.target.value)}
                                    placeholder="CONFIRM"
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
                                    onApply()
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
                                Reverting automatically in {left}s unless you confirm. If you have
                                lost access, do nothing and the change will roll back.
                            </DialogDescription>
                        </DialogHeader>
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
