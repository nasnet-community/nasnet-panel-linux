import { useEffect, useState } from "react"
import { AlertTriangle } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useRollbackNetworkApply } from "@/lib/queries/use-network"
import { confirmWithFallback, remainingSeconds } from "@/lib/api/network"

interface Props {
    planId: number
    deadlineUnix: number
    /** Address the change may have moved the panel to, "" when it did not move. */
    altOrigin: string
    onSettled: () => void
}

/** A reload during the confirm window used to show nothing, so the operator sat
 *  on a page that was about to revert under them. This is the way back. */
export function ArmedChangeBar({ planId, deadlineUnix, altOrigin, onSettled }: Props) {
    const rollback = useRollbackNetworkApply()
    const [left, setLeft] = useState(() => remainingSeconds(deadlineUnix))
    const [keeping, setKeeping] = useState(false)
    // Whatever was left when the page loaded is the only denominator we have.
    const [total] = useState(() => Math.max(1, remainingSeconds(deadlineUnix)))

    useEffect(() => {
        const tick = () => {
            const r = remainingSeconds(deadlineUnix)
            setLeft(r)
            if (r === 0) onSettled()
        }
        tick()
        const id = setInterval(tick, 1000)
        return () => clearInterval(id)
    }, [deadlineUnix, onSettled])

    async function keep() {
        setKeeping(true)
        await confirmWithFallback(planId, deadlineUnix, altOrigin)
        setKeeping(false)
        onSettled()
    }

    const pct = Math.min(100, (left / total) * 100)

    return (
        <div
            role="alert"
            className="border-status-warning/40 bg-status-warning/[0.07] relative overflow-hidden rounded-lg border"
        >
            <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
                <div className="flex items-start gap-3">
                    <AlertTriangle className="text-status-warning mt-0.5 h-4 w-4 shrink-0" />
                    <div>
                        <p className="text-sm font-medium">
                            A network change is waiting for confirmation
                        </p>
                        <p className="text-text-secondary text-sm">
                            Reverting in{" "}
                            <span className="text-status-warning font-mono font-medium tabular-nums">
                                {left}s
                            </span>{" "}
                            unless you keep it. If the box is unreachable, do nothing.
                        </p>
                    </div>
                </div>

                <div className="flex shrink-0 gap-2 sm:pl-7">
                    <Button
                        variant="outline"
                        size="sm"
                        disabled={rollback.isPending || keeping}
                        onClick={() =>
                            rollback.mutate(undefined, { onSuccess: () => onSettled() })
                        }
                    >
                        Revert now
                    </Button>
                    <Button size="sm" disabled={keeping} onClick={() => void keep()}>
                        {keeping ? "Confirming…" : "Keep these settings"}
                    </Button>
                </div>
            </div>

            <div
                aria-hidden
                className="bg-status-warning absolute bottom-0 left-0 h-[2px] transition-[width] duration-1000 ease-linear"
                style={{ width: `${pct}%` }}
            />
        </div>
    )
}
