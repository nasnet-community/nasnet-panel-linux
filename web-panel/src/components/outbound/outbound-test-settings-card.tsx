import { useState, useEffect, useRef } from "react"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Skeleton } from "@/components/ui/skeleton"
import { HiOutlineSave, HiOutlineStatusOnline } from "react-icons/hi"
import { Loader2 } from "lucide-react"
import { useOutboundTestSettings, useUpdateOutboundTestSettings } from "@/lib/queries/use-nodes"
import type { OutboundTestSettings } from "@/lib/types"

interface OutboundTestSettingsCardProps {
    nodeId: number
}

// Shown as placeholders so an empty field visibly means "use the default".
const PLACEHOLDERS = {
    concurrency: "4",
    max_delay_ms: "5000",
    retries: "1",
    test_url: "https://cloudflare.com/cdn-cgi/trace",
    speedtest_kb: "10000",
}

/** Numeric fields come back from inputs as strings; empty means "default". */
type FormState = {
    concurrency: string
    max_delay_ms: string
    retries: string
    test_url: string
    speedtest_kb: string
    insecure_tls: boolean
}

const EMPTY_FORM: FormState = {
    concurrency: "",
    max_delay_ms: "",
    retries: "",
    test_url: "",
    speedtest_kb: "",
    insecure_tls: true,
}

function toForm(settings: OutboundTestSettings): FormState {
    return {
        concurrency: settings.concurrency ? String(settings.concurrency) : "",
        max_delay_ms: settings.max_delay_ms ? String(settings.max_delay_ms) : "",
        retries: settings.retries ? String(settings.retries) : "",
        test_url: settings.test_url ?? "",
        speedtest_kb: settings.speedtest_kb ? String(settings.speedtest_kb) : "",
        insecure_tls: settings.insecure_tls ?? true,
    }
}

/** Only non-empty fields are sent; the backend fills the rest with defaults. */
function toPayload(form: FormState): OutboundTestSettings {
    const num = (v: string) => {
        const parsed = Number.parseInt(v, 10)
        return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined
    }
    return {
        concurrency: num(form.concurrency),
        max_delay_ms: num(form.max_delay_ms),
        retries: num(form.retries),
        test_url: form.test_url.trim() || undefined,
        speedtest_kb: num(form.speedtest_kb),
        insecure_tls: form.insecure_tls,
    }
}

export function OutboundTestSettingsCard({ nodeId }: OutboundTestSettingsCardProps) {
    const { data: settings, isLoading } = useOutboundTestSettings(nodeId)
    const saveSettings = useUpdateOutboundTestSettings(nodeId)

    const [form, setForm] = useState<FormState>(EMPTY_FORM)
    const [loadedForm, setLoadedForm] = useState<FormState>(EMPTY_FORM)
    const seeded = useRef(false)

    // The node page reuses this component across nodes instead of remounting it,
    // so a node switch has to re-arm the seed or the next save would write the
    // previous node's values onto this one.
    useEffect(() => {
        seeded.current = false
        setForm(EMPTY_FORM)
        setLoadedForm(EMPTY_FORM)
    }, [nodeId])

    // Seed once per node. A background refetch (window focus, for one) must not
    // wipe edits the operator has in progress.
    useEffect(() => {
        if (!settings || seeded.current) return
        seeded.current = true
        const next = toForm(settings)
        setForm(next)
        setLoadedForm(next)
    }, [settings])

    const isDirty = JSON.stringify(form) !== JSON.stringify(loadedForm)

    const handleSave = async () => {
        try {
            await saveSettings.mutateAsync(toPayload(form))
            setLoadedForm(form)
        } catch {
            // mutation surfaces the toast
        }
    }

    const setField = <K extends keyof FormState>(key: K, value: FormState[K]) =>
        setForm(prev => ({ ...prev, [key]: value }))

    return (
        <Card>
            <CardHeader>
                <CardTitle className="flex items-center gap-2">
                    <HiOutlineStatusOnline className="w-5 h-5 text-emerald-500" />
                    Outbound Testing
                </CardTitle>
                <CardDescription>
                    How this node probes its outbounds. Leave a field empty to use the default.
                </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
                {isLoading ? (
                    <div className="space-y-3">
                        <Skeleton className="h-9 w-full" />
                        <Skeleton className="h-9 w-full" />
                        <Skeleton className="h-9 w-full" />
                    </div>
                ) : (
                    <>
                        <div className="space-y-1.5">
                            <Label className="text-xs text-muted-foreground">Test URL</Label>
                            <Input
                                value={form.test_url}
                                placeholder={PLACEHOLDERS.test_url}
                                onChange={(e) => setField("test_url", e.target.value)}
                            />
                            <p className="text-xs text-muted-foreground/70">
                                Fetched through the outbound. A Cloudflare trace URL also reveals the exit IP and country.
                            </p>
                        </div>

                        <div className="grid grid-cols-2 gap-3">
                            <div className="space-y-1.5">
                                <Label className="text-xs text-muted-foreground">Max delay (ms)</Label>
                                <Input
                                    type="number"
                                    min={1}
                                    max={15000}
                                    value={form.max_delay_ms}
                                    placeholder={PLACEHOLDERS.max_delay_ms}
                                    onChange={(e) => setField("max_delay_ms", e.target.value)}
                                />
                            </div>
                            <div className="space-y-1.5">
                                <Label className="text-xs text-muted-foreground">Retries</Label>
                                <Input
                                    type="number"
                                    min={1}
                                    max={3}
                                    value={form.retries}
                                    placeholder={PLACEHOLDERS.retries}
                                    onChange={(e) => setField("retries", e.target.value)}
                                />
                            </div>
                            <div className="space-y-1.5">
                                <Label className="text-xs text-muted-foreground">Test All concurrency</Label>
                                <Input
                                    type="number"
                                    min={1}
                                    max={32}
                                    value={form.concurrency}
                                    placeholder={PLACEHOLDERS.concurrency}
                                    onChange={(e) => setField("concurrency", e.target.value)}
                                />
                            </div>
                            <div className="space-y-1.5">
                                <Label className="text-xs text-muted-foreground">Speedtest size (KB)</Label>
                                <Input
                                    type="number"
                                    min={100}
                                    value={form.speedtest_kb}
                                    placeholder={PLACEHOLDERS.speedtest_kb}
                                    onChange={(e) => setField("speedtest_kb", e.target.value)}
                                />
                            </div>
                        </div>

                        <div className="flex items-center justify-between rounded-lg border p-3">
                            <div className="space-y-0.5">
                                <Label className="text-sm">Skip TLS verification</Label>
                                <p className="text-xs text-muted-foreground">
                                    Tests self-signed upstreams instead of failing on the certificate.
                                </p>
                            </div>
                            <Switch
                                checked={form.insecure_tls}
                                onCheckedChange={(checked) => setField("insecure_tls", checked)}
                            />
                        </div>

                        <div className="flex justify-end">
                            <Button
                                size="sm"
                                onClick={handleSave}
                                disabled={!isDirty || saveSettings.isPending}
                            >
                                {saveSettings.isPending ? (
                                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                                ) : (
                                    <HiOutlineSave className="w-4 h-4 mr-2" />
                                )}
                                Save
                            </Button>
                        </div>
                    </>
                )}
            </CardContent>
        </Card>
    )
}
