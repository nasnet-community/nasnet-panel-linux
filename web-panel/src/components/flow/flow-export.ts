import type { FlowConnsView, FlowEvent, FlowView, TraceView } from "@/lib/types/flow"

interface Bundle {
    flow: FlowView | null
    conns: FlowConnsView | null
    events: FlowEvent[] | null
    trace: TraceView | null
}

/** Everything the page knows, in one file — what to attach to a bug report. */
export function exportDebugBundle(bundle: Bundle) {
    const body = JSON.stringify({ exported_at: new Date().toISOString(), ...bundle }, null, 2)
    const url = URL.createObjectURL(new Blob([body], { type: "application/json" }))
    const a = document.createElement("a")
    a.href = url
    a.download = `nasnet-flow-${new Date().toISOString().replace(/[:.]/g, "-")}.json`
    // Firefox ignores a click on a detached anchor, and revoking straight after
    // it kills the blob before the download starts.
    document.body.appendChild(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 10_000)
}
