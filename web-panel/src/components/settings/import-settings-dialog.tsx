import { useRef, useState } from "react"
import { ArrowRight, FileUp, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useImportSettings } from "@/lib/queries/use-settings"
import { CARDS, EVENTS, RETENTION, CHANNEL_LABEL, type Channel, type Values } from "./catalog"

interface Entry { key: string; value: string }

/** "Backups · Every" for a key the page shows, else the key itself. */
function labelFor(key: string): string {
    for (const c of CARDS) {
        const f = c.fields?.find(f => f.key === key)
        if (f) return `${c.title} · ${f.label}`
        const item = c.fields?.flatMap(f => f.items ?? []).find(i => i[0] === key)
        if (item) return `${c.title} · ${item[1]}`
    }
    const ev = /^notification_(telegram|discord|webhook)_(.+)$/.exec(key)
    if (ev) {
        const label = EVENTS.flatMap(g => g.items).find(i => i[0] === ev[2])?.[1]
        if (label) return `Alert events · ${CHANNEL_LABEL[ev[1] as Channel]} · ${label}`
    }
    const ds = RETENTION.flatMap(g => g.rows).find(r => r[0] === key)
    if (ds) return `Retention · ${ds[1]}`
    return key
}

const masked = (v: string) => v.startsWith("***")
const show = (v: string, secret: boolean) => (secret ? "••••" : v === "" ? "empty" : v)

export function ImportSettingsDialog({ open, onOpenChange, values }: { open: boolean; onOpenChange: (o: boolean) => void; values: Values }) {
    const fileRef = useRef<HTMLInputElement>(null)
    const importSettings = useImportSettings()
    const [file, setFile] = useState<{ name: string; entries: Entry[] } | null>(null)
    const [error, setError] = useState<string | null>(null)

    const reset = () => { setFile(null); setError(null); if (fileRef.current) fileRef.current.value = "" }
    const close = (o: boolean) => { onOpenChange(o); if (!o) reset() }

    const read = async (f: File) => {
        setError(null)
        try {
            const data = JSON.parse(await f.text())
            if (!Array.isArray(data)) throw new Error()
            const entries = data.filter((d): d is Entry => d && typeof d.key === "string" && typeof d.value === "string")
            if (!entries.length) throw new Error()
            setFile({ name: f.name, entries })
        } catch {
            setFile(null)
            setError("That isn't a settings export. Pick a file from Export settings.")
        }
    }

    const changes: Entry[] = []
    let unknown = 0, secrets = 0, same = 0
    for (const e of file?.entries ?? []) {
        if (!Object.hasOwn(values, e.key)) unknown++
        // a masked secret, or one the other panel never set: neither may overwrite ours
        else if (masked(e.value) || (e.value === "" && masked(values[e.key]))) secrets++
        else if (values[e.key] === e.value) same++
        else changes.push(e)
    }
    const skipped = [
        same && `${same} unchanged`,
        secrets && `${secrets} masked ${secrets === 1 ? "secret" : "secrets"}`,
        unknown && `${unknown} ${unknown === 1 ? "key" : "keys"} this panel doesn't have or can't change`,
    ].filter(Boolean).join(", ")

    const apply = async () => {
        try {
            await importSettings.mutateAsync(changes)
            close(false)
        } catch { /* the mutation toasts */ }
    }

    return (
        <Dialog open={open} onOpenChange={close}>
            <DialogContent className="sm:max-w-xl">
                <DialogHeader>
                    <DialogTitle>Import settings</DialogTitle>
                    <DialogDescription>
                        {file ? <><span className="font-mono text-foreground">{file.name}</span> · {changes.length === 1 ? "1 change" : `${changes.length} changes`}</> : "Pick a file made with Export settings. You see every change before it's applied."}
                    </DialogDescription>
                </DialogHeader>
                <input ref={fileRef} type="file" accept=".json,application/json" className="hidden" onChange={e => { const f = e.target.files?.[0]; if (f) void read(f) }} />
                {!file && (
                    <Button type="button" variant="outline" className="h-20 w-full flex-col gap-1.5 border-dashed" onClick={() => fileRef.current?.click()}>
                        <FileUp className="size-5 text-muted-foreground" />Choose a file
                    </Button>
                )}
                {error && <p className="text-sm text-red-500">{error}</p>}
                {file && changes.length > 0 && (
                    <div className="max-h-[50vh] overflow-auto rounded-lg border border-border">
                        <div className="sticky top-0 grid grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_14px_minmax(0,1fr)] gap-2 border-b border-border bg-muted/60 px-3 py-1.5 text-xs text-muted-foreground">
                            <span>Setting</span><span>Now</span><span /><span>In the file</span>
                        </div>
                        {changes.map(e => {
                            const secret = masked(values[e.key] ?? "")
                            return (
                                <div key={e.key} className="grid grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_14px_minmax(0,1fr)] items-center gap-2 border-b border-border/40 px-3 py-1.5 text-[13px] last:border-0">
                                    <span className="[overflow-wrap:anywhere]">{labelFor(e.key)}</span>
                                    <span className="truncate text-muted-foreground line-through">{show(values[e.key] ?? "", secret)}</span>
                                    <ArrowRight className="size-3 text-muted-foreground" aria-hidden />
                                    <span className="truncate font-medium">{show(e.value, secret)}</span>
                                </div>
                            )
                        })}
                    </div>
                )}
                {file && changes.length === 0 && <p className="text-sm text-muted-foreground">Nothing in this file differs from what's set.</p>}
                {file && skipped && <p className="text-xs text-muted-foreground">Left out: {skipped}.</p>}
                <DialogFooter>
                    <Button variant="ghost" onClick={() => close(false)}>Cancel</Button>
                    {file && <Button variant="outline" onClick={reset}>Another file</Button>}
                    <Button onClick={apply} disabled={!changes.length || importSettings.isPending}>
                        {importSettings.isPending && <Loader2 className="mr-1.5 size-4 animate-spin" />}
                        {changes.length ? `Apply ${changes.length === 1 ? "1 change" : `${changes.length} changes`}` : "Apply"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
