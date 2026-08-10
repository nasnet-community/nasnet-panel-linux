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

/** What happens to a LAN packet, in the order the kernel tries it. Numbered
 *  because it is a sequence: domestic first, foreign catch-all last. */
export function ClassificationLadder({
    geoipPrefixes,
    domainLayer,
    rangesFetchedAt,
    lanCidr,
    domesticLabel,
    foreignLabel,
}: Props) {
    const rungs: Rung[] = [
        {
            marker: "1",
            title: "Address is a known domestic range",
            detail: `${freshness(rangesFetchedAt)}. Catches addresses nothing resolved — hardcoded IPs, DNS-over-HTTPS, cached answers.`,
            egress: domesticLabel,
            state: `${geoipPrefixes.toLocaleString()} prefixes`,
            live: true,
        },
        {
            marker: "2",
            title: "Name resolved to a .ir domain",
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
            detail: "Anything unmatched goes abroad. An unrecognised address must never leave through the domestic ISP.",
            egress: foreignLabel,
            state: "Always last",
            live: true,
        },
    ]

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
