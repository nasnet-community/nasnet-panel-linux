import { useState } from "react"
import { CheckCircle2, Globe, Loader2, XCircle } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useCheckMtprotoDisguise } from "@/lib/queries"
import type { DisguiseResult, MtprotoNode } from "@/lib/api/mtproto"
import { cn, formatRelativeTime } from "@/lib/utils"
import { T } from "@/components/node/node-ui"
import { OverviewCard } from "@/components/node/overview/overview-card"
import { looksLikeDomain } from "../mtproto-labels"

function Check({ ok, label }: { ok: boolean; label: string }) {
    return (
        <span className={cn("inline-flex h-6 items-center gap-1 rounded-md border px-2 text-[12px] font-medium", ok ? "border-emerald-500/25 bg-emerald-500/[0.07]" : "border-red-500/25 bg-red-500/[0.07]")}>
            {ok ? <CheckCircle2 className="size-3 text-emerald-500" aria-hidden /> : <XCircle className="size-3 text-red-500" aria-hidden />}
            {label}
        </span>
    )
}

/** What a cover site's test found, one chip per requirement. */
export function CoverChecks({ d }: { d: DisguiseResult }) {
    return (
        <div className="flex flex-wrap gap-1.5">
            <Check ok={d.tls13} label="TLS 1.3" />
            <Check ok={d.x25519} label="X25519" />
            <Check ok={d.h2} label="HTTP/2" />
            <Check ok={d.cert_ok} label="Certificate" />
        </div>
    )
}

function Elsewhere() {
    return <p className="text-xs text-amber-500">Hosted outside your local address ranges, so the node may lose it.</p>
}

/** The site the node presents itself as: the panel's last check, a re-check, and a test for a replacement. */
export function CoverSiteCard({ r, linkUp, onChange, className }: { r: MtprotoNode; linkUp: boolean; onChange: () => void; className?: string }) {
    const check = useCheckMtprotoDisguise(r.node_id)
    const [testing, setTesting] = useState(false)
    const [domain, setDomain] = useState("")
    const last = r.disguise_check
    const agentSays = r.agent?.disguise
    const ok = last?.ok ?? agentSays?.ok
    const checkedAt = last?.checked_at ?? agentSays?.checked_at
    const tested = check.data && check.variables !== r.disguise ? check.data : undefined
    const recheck = check.data && check.variables === r.disguise ? check.data : undefined
    const shown = recheck ?? last

    return (
        <OverviewCard glow="cyan" title="Cover site" aside={checkedAt ? <span className={T.meta}>checked {formatRelativeTime(checkedAt)}</span> : undefined} className={className}>
            <div className="flex items-center gap-3">
                <div className={cn("flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-full ring-2",
                    ok === undefined ? "bg-muted/30 text-muted-foreground ring-muted-foreground/25" : ok ? "bg-emerald-500/10 text-emerald-500 ring-emerald-500/25" : "bg-red-500/10 text-red-500 ring-red-500/25")}>
                    <Globe className="h-4 w-4" aria-hidden />
                </div>
                <div className="min-w-0">
                    <p className="truncate font-mono text-sm font-semibold">{r.disguise}</p>
                    <p className={cn(T.meta, ok === false && "text-red-500")}>
                        {ok === undefined ? "Not checked yet" : ok ? `Working${shown?.latency_ms ? ` · ${shown.latency_ms} ms from the node` : ""}` : (shown?.err || agentSays?.err || "The check failed")}
                    </p>
                </div>
            </div>

            {shown && <CoverChecks d={shown} />}
            {shown?.domestic === false && <Elsewhere />}
            {shown?.addrs && shown.addrs.length > 0 && (
                <p className={cn(T.meta, "break-all font-mono")} title={shown.addrs.join(", ")}>
                    {shown.addrs.slice(0, 3).join(" · ")}{shown.addrs.length > 3 ? ` +${shown.addrs.length - 3}` : ""}
                </p>
            )}

            {testing && (
                <form
                    className="flex gap-2"
                    onSubmit={(e) => {
                        e.preventDefault()
                        if (looksLikeDomain(domain)) check.mutate(domain.trim().toLowerCase())
                    }}
                >
                    <Input aria-label="Domain to test" placeholder="www.example.com" value={domain} spellCheck={false} autoFocus onChange={(e) => setDomain(e.target.value)} className="h-9" />
                    <Button type="submit" variant="outline" className="h-9" disabled={!linkUp || !looksLikeDomain(domain) || check.isPending}>
                        {check.isPending && check.variables !== r.disguise && <Loader2 className="size-3.5 animate-spin" />}
                        Test
                    </Button>
                </form>
            )}
            {tested && (
                <div className="space-y-1.5 rounded-lg border border-border/60 bg-muted/20 p-2.5" data-testid="disguise-result">
                    <p className="text-xs font-medium">
                        <span className="font-mono">{check.variables}</span>{" "}
                        <span className={tested.ok ? "text-emerald-500" : "text-red-500"}>{tested.ok ? "would work" : "won't work"}</span>
                    </p>
                    <CoverChecks d={tested} />
                    {tested.err && <p className="text-xs text-red-500">{tested.err}</p>}
                    {tested.domestic === false && <Elsewhere />}
                </div>
            )}

            <div className="mt-auto flex flex-wrap gap-2">
                <Button type="button" size="sm" variant="outline" className="h-8" disabled={!linkUp || check.isPending} onClick={() => check.mutate(r.disguise)}>
                    {check.isPending && check.variables === r.disguise && <Loader2 className="size-3.5 animate-spin" />}
                    Check again
                </Button>
                <Button type="button" size="sm" variant="ghost" className="h-8" aria-expanded={testing} onClick={() => setTesting((v) => !v)}>
                    {testing ? "Close test" : "Test another"}
                </Button>
                <Button type="button" size="sm" variant="ghost" className="ml-auto h-8" onClick={onChange}>Change…</Button>
            </div>
        </OverviewCard>
    )
}
