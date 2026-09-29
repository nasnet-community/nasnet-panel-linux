import { useState, type FormEvent, type ReactNode } from "react"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { defaultPool, emptySource, type OutboundPool, type PoolSettings } from "@/lib/api/outbound-pools"
import type { BalancingRule, Outbound } from "@/lib/types"

const selectClass = "h-10 w-full rounded-md border border-input bg-background px-3 text-sm"
function Field({ label, children }: { label: string, children: ReactNode }) {
    return <label className="grid gap-1.5 text-sm"><span className="font-medium">{label}</span>{children}</label>
}
export function PoolForm({ pool, balancers, outbounds, pending, onClose, onSave, maxThreads = 2000 }: {
    pool?: OutboundPool
    /** Node-wide scan budget: 32 on the built-in node, 2000 on remote nodes. */
    maxThreads?: number
    balancers: BalancingRule[]
    outbounds: Outbound[]
    pending: boolean
    onClose: () => void
    onSave: (settings: PoolSettings) => Promise<void>
}) {
    const [step, setStep] = useState(0)
    const [bulk, setBulk] = useState("")
    const [bulkError, setBulkError] = useState("")
    const steps = ["Sources", "Scanning", "Routing"]
    const [form, setForm] = useState<PoolSettings>(() => pool
        ? { ...pool, sources: pool.sources.map(s => ({ ...emptySource(), ...s })), fallback_tag: pool.balancer.fallback_tag }
        : defaultPool())
    const update = <K extends keyof PoolSettings>(key: K, value: PoolSettings[K]) => setForm(f => ({ ...f, [key]: value }))
    const submit = (event: FormEvent) => {
        event.preventDefault()
        if (step < 2) { setStep(step + 1); return }
        void onSave({ ...form, test_urls: form.test_urls.map(url => url.trim()).filter(Boolean) })
    }
    const addBulk = () => {
        const urls = [...new Set(bulk.split(/\s+/).map(url => url.trim()).filter(Boolean))]
        const existing = form.sources.filter(source => source.url || source.links)
        if (!urls.length || existing.length + urls.length > 20) { setBulkError("Add between 1 and 20 sources in total."); return }
        try { for (const raw of urls) { const url = new URL(raw); if (!["https:", "http:"].includes(url.protocol)) throw new Error() } }
        catch { setBulkError("Each source must be a complete HTTP or HTTPS URL."); return }
        update("sources", [...existing, ...urls.map(url => ({ ...emptySource(), url, name: new URL(url).hostname }))])
        setBulk(""); setBulkError("")
    }
    const number = (key: keyof PoolSettings, label: string, min: number, max: number, scale = 1) => <Field label={label}>
        <Input type="number" required min={min} max={max} value={Number(form[key]) / scale} onChange={e => update(key, Number(e.target.value) * scale)} />
    </Field>
    return <Dialog open onOpenChange={open => { if (!open && !pending) onClose() }}>
        <DialogContent className="max-w-3xl max-h-[90dvh] overflow-hidden p-0 gap-0 flex flex-col">
            <DialogHeader className="px-6 pt-6 pb-4"><DialogTitle>{pool ? "Edit subscription pool" : "Create subscription pool"}</DialogTitle>
                <DialogDescription>Fetch and scan from this node. Healthy members are published automatically.</DialogDescription></DialogHeader>
            <form onSubmit={submit} className="flex flex-col min-h-0">
                <nav aria-label="Subscription pool setup steps" className="flex border-y px-6 py-3 gap-2 bg-muted/20">
                    {steps.map((label, index) => <button key={label} type="button" disabled={!pool && index > step} onClick={() => setStep(index)} aria-current={step === index ? "step" : undefined} className={cn("flex-1 flex items-center justify-center gap-2 rounded-md px-2 py-2 text-xs transition-colors", step === index ? "bg-background font-semibold shadow-sm border" : "text-muted-foreground disabled:opacity-40")}><span className={cn("inline-flex size-5 items-center justify-center rounded-full text-[11px]", step === index ? "bg-primary text-primary-foreground" : "bg-muted")}>{index + 1}</span>{label}</button>)}
                </nav>
                <div className="overflow-y-auto min-h-0 p-6 space-y-5">
                {step === 0 && <>

                <Field label="Subscription pool name"><Input required maxLength={100} value={form.name} onChange={e => update("name", e.target.value)} placeholder="Upstream proxies" /></Field>
                <div className="space-y-3">
                    <div className="flex items-center justify-between"><h3 className="font-medium">Sources</h3><Button type="button" size="sm" variant="outline" disabled={form.sources.length >= 20} onClick={() => update("sources", [...form.sources, emptySource()])}>Add source</Button></div>
                    {form.sources.map((source, index) => {
                        const change = (patch: Partial<typeof source>) => update("sources", form.sources.map((s, i) => i === index ? { ...s, ...patch } : s))
                        return <fieldset key={source.id ?? `new-${index}`} className="rounded-lg border p-3 space-y-3">
                            <legend className="px-1 text-xs text-muted-foreground">Source {index + 1}</legend>
                            <div className="grid sm:grid-cols-2 gap-3">
                                <Field label="Source label"><Input value={source.name} maxLength={100} onChange={e => change({ name: e.target.value })} placeholder={`Source ${index + 1}`} /></Field>
                                <Field label="Input type"><select className={selectClass} value={source.kind} onChange={e => change({ kind: e.target.value as "url" | "links" })}><option value="url">Subscription URL</option><option value="links">Pasted config links</option></select></Field>
                            </div>
                            {source.kind === "url" ? <>
                                <Field label="Subscription URL"><Input type="url" required value={source.url} onChange={e => change({ url: e.target.value })} placeholder="https://provider.example/subscription" /></Field>
                                <details><summary className="text-xs text-muted-foreground cursor-pointer">Source options</summary><div className="space-y-3 mt-3">
                                    <Field label="User-Agent (optional)"><Input value={source.user_agent} onChange={e => change({ user_agent: e.target.value })} maxLength={512} /></Field>
                                    <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={source.allow_private} onChange={e => change({ allow_private: e.target.checked })} />Allow fetching this source from private network addresses</label>
                                </div></details>
                            </> : <Field label="Config links, one per line"><Textarea required rows={5} value={source.links} onChange={e => change({ links: e.target.value })} className="font-mono text-xs" placeholder="vless://…" /></Field>}
                            {form.sources.length > 1 && <Button type="button" variant="ghost" size="sm" onClick={() => update("sources", form.sources.filter((_, i) => i !== index))}>Remove source</Button>}
                        </fieldset>
                    })}
                </div>

                <details className="rounded-lg border border-dashed p-3"><summary className="text-xs font-medium cursor-pointer">Paste multiple subscription URLs</summary><div className="space-y-2 mt-3"><Textarea aria-label="Subscription URLs in bulk" value={bulk} onChange={e => setBulk(e.target.value)} rows={3} placeholder="One subscription URL per line" />{bulkError && <p role="alert" className="text-xs text-destructive">{bulkError}</p>}<Button type="button" variant="outline" size="sm" onClick={addBulk}>Add URLs</Button></div></details>
                </>}
                {step === 2 && <>
                <div className="rounded-lg bg-muted/30 p-4 text-sm"><p className="font-medium">{form.name}</p><p className="text-muted-foreground text-xs mt-1">{form.sources.length} source(s) · {form.concurrency.toLocaleString()} scan threads · Up to {form.max_active} active members</p></div>
                <fieldset className="space-y-3"><legend className="font-medium mb-3">Balancer and fallback</legend>
                    <Field label="Target balancer"><select disabled={!!pool} className={selectClass} value={form.balancer_id} onChange={e => {
                        const id = Number(e.target.value); const balancer = balancers.find(b => b.id === id)
                        setForm(f => ({ ...f, balancer_id: id, fallback_tag: balancer?.fallback_tag || "" }))
                    }}><option value={0}>Create a new balancer</option>{balancers.filter(b => b.enabled).map(b => <option key={b.id} value={b.id}>{b.tag}</option>)}</select></Field>
                    {!form.balancer_id && <div className="grid sm:grid-cols-2 gap-3">
                        <Field label="Balancer tag"><Input required maxLength={100} value={form.balancer_tag} onChange={e => update("balancer_tag", e.target.value)} placeholder="upstream-pool" /></Field>
                        <Field label="Strategy"><select className={selectClass} value={form.strategy} onChange={e => update("strategy", e.target.value)}><option value="leastping">Lowest latency</option><option value="random">Random</option><option value="roundrobin">Round robin</option><option value="leastload">Least load</option></select></Field>
                    </div>}
                    <Field label="Fallback outbound"><select required disabled={!!pool} className={selectClass} value={form.fallback_tag} onChange={e => update("fallback_tag", e.target.value)}><option value="">Choose an explicit fallback</option>{outbounds.filter(o => !o.managed_pool_id && !o.is_disabled).map(o => <option key={o.id} value={o.tag}>{o.tag} · {o.protocol}</option>)}</select></Field>
                    <p className="text-xs text-muted-foreground">The fallback is used when no pool member is reachable. Choose routing rules for this balancer in Routing. {pool && "Edit its fallback and strategy there."}</p>
                </fieldset>
                </>}
                {step === 1 && <>
                <fieldset className="space-y-3"><legend className="font-medium mb-3">Scanning and schedule</legend>
                    <Field label="Scan mode"><select className={selectClass} value={form.stop_when_full ? "fill" : "all"} onChange={e => update("stop_when_full", e.target.value === "fill")}><option value="fill">Stop when enough healthy configs are found</option><option value="all">Test every candidate</option></select></Field>
                    <div className="grid grid-cols-2 gap-3">
                        {number("max_active", "Active-member limit", 1, 1000)}
                        {number("concurrency", "Scan threads", 1, maxThreads)}
                        {number("refresh_seconds", "Refresh sources (minutes)", 1, 10080, 60)}
                        {number("rescan_seconds", "Rescan configs (minutes)", 1, 10080, 60)}
                    </div>
                    <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.enabled} onChange={e => update("enabled", e.target.checked)} />Enable scheduled refresh and scanning</label>
                    <p className="text-xs text-muted-foreground">Choose up to 2,000 parallel scan threads. The default is 4.</p>
                    <p className="text-xs text-muted-foreground">Pausing preserves applied members. Changed membership restarts Xray once; unchanged results do not.</p>
                </fieldset>
                <details className="rounded-lg border p-3"><summary className="font-medium text-sm cursor-pointer">Probe policy and source limits</summary><div className="mt-4 space-y-3">
                    <Field label="Required HTTPS probe URLs (one to three, one per line)"><Textarea required rows={3} value={form.test_urls.join("\n")} onChange={e => update("test_urls", e.target.value.split("\n"))} /></Field>
                    <div className="grid grid-cols-2 gap-3">
                        {number("max_delay_ms", "Maximum latency (ms)", 100, 30000)}
                        {number("retries", "Retries", 0, 3)}
                        {number("failure_threshold", "Failed scans before removal", 1, 10)}
                        {number("max_source_links", "Links per source", 1, 100000)}
                        {number("max_source_bytes", "Source size limit (MiB)", 1, 64, 1048576)}
                        {number("max_candidates", "Unique candidate limit", form.max_active, 100000)}
                    </div>
                </div></details>
                </>}
                </div>
                <div className="flex items-center justify-between gap-3 border-t bg-background px-6 py-4">
                    <span className="text-xs text-muted-foreground">Step {step + 1} of 3</span>
                    <div className="flex gap-2"><Button type="button" variant="outline" disabled={pending} onClick={() => step > 0 ? setStep(step - 1) : onClose()}>{step > 0 ? "Back" : "Cancel"}</Button><Button type="submit" disabled={pending}>{pending ? "Saving…" : step < 2 ? "Continue" : "Save subscription pool"}</Button></div>
                </div>
            </form>
        </DialogContent>
    </Dialog>
}
