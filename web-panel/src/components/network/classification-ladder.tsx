import { useState } from "react"
import { ChevronDown } from "lucide-react"
import { cn } from "@/lib/utils"

interface Props {
    /** How many prefixes the floor layer holds. */
    geoipPrefixes: number
    /** Whether this dnsmasq build can populate the resolved-name sets. */
    domainLayer: boolean
    /** When the prefix list last came from upstream; null means the build's own. */
    rangesFetchedAt: string | null
    lanCidr: string
    domesticLabel: string
    foreignLabel: string
}

interface Rung {
    marker: string
    title: string
    /** Just enough for the one-line strip. */
    short: string
    detail: string
    egress: string
    state: string
    live: boolean
}

/** Plain-language age, so staleness is legible without arithmetic. */
function freshness(iso: string | null): string {
    if (!iso) return "Built in"
    const days = Math.floor((Date.now() - new Date(iso).getTime()) / 86_400_000)
    if (days < 1) return "Updated today"
    if (days === 1) return "Updated yesterday"
    return `Updated ${days} days ago`
}

function buildRungs({
    geoipPrefixes,
    domainLayer,
    rangesFetchedAt,
    domesticLabel,
    foreignLabel,
}: Props): Rung[] {
    return [
        {
            marker: "1",
            title: "Address is a known domestic range",
            short: "known domestic range",
            detail: `${freshness(rangesFetchedAt)}. Catches addresses nothing resolved — hardcoded IPs, DNS-over-HTTPS, cached answers.`,
            egress: domesticLabel,
            state: `${geoipPrefixes.toLocaleString()} prefixes`,
            live: true,
        },
        {
            marker: "2",
            title: "Name resolved to a .ir domain",
            short: ".ir names",
            detail: domainLayer
                ? "This box's dnsmasq writes every address it resolves under .ir into the set as it answers."
                : "This box's dnsmasq was built without --nftset, so names are not matched. Layer 1 still covers the traffic.",
            egress: domesticLabel,
            state: domainLayer ? "Active" : "Unavailable",
            live: domainLayer,
        },
        {
            marker: "→",
            title: "Everything else",
            short: "everything else",
            detail: "Anything unmatched goes abroad. An unrecognised address must never leave through the domestic ISP.",
            egress: foreignLabel,
            state: "Always last",
            live: true,
        },
    ]
}

/** What happens to a LAN packet, in the order the kernel tries it. Numbered
 *  because it is a sequence: domestic first, foreign catch-all last. */
export function ClassificationLadder(props: Props) {
    const { lanCidr } = props
    const rungs = buildRungs(props)

    return (
        <div>
            <p className="text-text-tertiary font-mono text-xs">{lanCidr || "LAN"}</p>

            <ol className="mt-3 space-y-0">
                {rungs.map((r, i) => (
                    <li key={r.marker} className="relative flex gap-3 pb-5 last:pb-0">
                        {/* Connector, drawn only between rungs. */}
                        {i < rungs.length - 1 && (
                            <span
                                aria-hidden
                                className="bg-border-subtle absolute top-6 bottom-0 left-[11px] w-px"
                            />
                        )}
                        <span
                            aria-hidden
                            className={cn(
                                "relative z-10 flex h-6 w-6 shrink-0 items-center justify-center rounded-full border font-mono text-[11px]",
                                r.live
                                    ? "border-border-strong bg-surface-1 text-text-secondary"
                                    : "border-border-subtle bg-surface-2 text-text-disabled",
                            )}
                        >
                            {r.marker}
                        </span>

                        <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                                <p
                                    className={cn(
                                        "text-sm font-medium",
                                        !r.live && "text-text-disabled",
                                    )}
                                >
                                    {r.title}
                                </p>
                                <span
                                    className={cn(
                                        "font-mono text-xs tabular-nums",
                                        r.live ? "text-text-tertiary" : "text-status-warning",
                                    )}
                                >
                                    {r.state}
                                </span>
                            </div>
                            <p
                                className={cn(
                                    "mt-0.5 text-xs",
                                    r.live ? "text-text-secondary" : "text-text-tertiary",
                                )}
                            >
                                {r.detail}
                            </p>
                            <p className="text-text-tertiary mt-1 text-xs">
                                Leaves via{" "}
                                <span className="text-text-secondary font-medium">{r.egress}</span>
                            </p>
                        </div>
                    </li>
                ))}
            </ol>

            <p className="text-text-tertiary border-border-subtle mt-4 border-t pt-3 text-xs">
                LAN devices are sorted by destination address. Domain and geosite routing rules
                apply to VPN clients only.
            </p>
        </div>
    )
}

/** The ladder folded into one quiet band: enough to know where traffic goes,
 *  expandable when the why matters. */
export function ClassificationStrip(props: Props) {
    const [open, setOpen] = useState(false)
    const rungs = buildRungs(props)

    return (
        <div className="border-border bg-surface-2 rounded-lg border px-4 py-3">
            <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
                <p className="text-text-tertiary text-xs font-medium whitespace-nowrap">
                    How traffic is sorted
                </p>
                {rungs.map((r) => (
                    <span
                        key={r.marker}
                        className="flex items-center gap-1.5 text-xs whitespace-nowrap"
                    >
                        <span
                            aria-hidden
                            className={cn(
                                "flex h-4 w-4 items-center justify-center rounded-full border font-mono text-[10px]",
                                r.live
                                    ? "border-border-strong text-text-tertiary"
                                    : "border-border-subtle text-text-disabled",
                            )}
                        >
                            {r.marker}
                        </span>
                        <span className={r.live ? "text-text-secondary" : "text-text-disabled"}>
                            {r.short}
                        </span>
                        {r.live ? (
                            <span className="text-text-tertiary">
                                → <span className="text-text-secondary">{r.egress}</span>
                            </span>
                        ) : (
                            <span className="text-status-warning">off</span>
                        )}
                    </span>
                ))}
                <button
                    type="button"
                    onClick={() => setOpen((v) => !v)}
                    aria-expanded={open}
                    className="text-text-tertiary hover:text-text-secondary ml-auto flex items-center gap-1 text-xs transition-colors"
                >
                    Details
                    <ChevronDown
                        aria-hidden
                        className={cn("h-3.5 w-3.5 transition-transform", open && "rotate-180")}
                    />
                </button>
            </div>
            {open && (
                <div className="border-border-subtle mt-3 border-t pt-4">
                    <ClassificationLadder {...props} />
                </div>
            )}
        </div>
    )
}
