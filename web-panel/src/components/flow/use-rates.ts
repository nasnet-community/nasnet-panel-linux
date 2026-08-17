import { useEffect, useRef, useState } from "react"
import type { FlowCounter, FlowView } from "@/lib/types/flow"

export interface Rate {
    rxBps: number
    txBps: number
}

interface Sample {
    at: number
    counters: Record<string, FlowCounter>
}

/**
 * Rates from consecutive cumulative samples. The interval comes from the
 * backend's own timestamps, not a wall clock here, so a slow render or a
 * delayed response cannot invent traffic. A counter reset — a reboot, an nft
 * reapply — shows up as a negative delta and clamps to zero rather than
 * drawing a spike that never happened.
 */
export function useRates(flow: FlowView | undefined): Record<string, Rate> {
    const previous = useRef<Sample | null>(null)
    const [rates, setRates] = useState<Record<string, Rate>>({})

    useEffect(() => {
        if (!flow) return
        const sample: Sample = { at: flow.generated_unix, counters: flow.counters }
        const prev = previous.current
        previous.current = sample
        if (!prev) return

        const dt = sample.at - prev.at
        if (dt <= 0) return

        const next: Record<string, Rate> = {}
        for (const [key, c] of Object.entries(sample.counters)) {
            const p = prev.counters[key]
            if (!p) continue
            next[key] = {
                rxBps: Math.max(0, (c.rx_bytes - p.rx_bytes) / dt),
                txBps: Math.max(0, (c.tx_bytes - p.tx_bytes) / dt),
            }
        }
        setRates(next)
    }, [flow])

    return rates
}
