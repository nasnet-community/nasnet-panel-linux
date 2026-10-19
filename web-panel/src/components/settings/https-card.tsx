import { useMemo, useState } from "react"
import { Loader2, RefreshCw, ShieldCheck } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { settingsApi, type TLSTestResult } from "@/lib/api/settings"
import { useRegenerateCertificate } from "@/lib/queries/use-settings"
import { SettingRow, SettingsGroup } from "@/components/settings-page/settings-group"
import { cn } from "@/lib/utils"
import { day, on, type CardDef, type FieldDef, type Values } from "./catalog"
import { CopyValue, FieldRow, Hl, StatusRow, toneOf, useCardEdit, usePage } from "./settings-card"

type Mode = "off" | "acme" | "files" | "self-signed"
const MODES: [Mode, string][] = [["off", "Off"], ["acme", "Let's Encrypt"], ["files", "Certificate files"], ["self-signed", "Self-signed"]]

const FIELDS: FieldDef[] = [
    { key: "acme_email", label: "Account email", type: "text", placeholder: "you@example.com", show: v => v.https_mode === "acme" },
    { key: "acme_staging", label: "Staging", type: "bool", onText: "On · test certificates only", show: v => v.https_mode === "acme" },
    { key: "tls_cert_file", label: "Certificate", type: "text", mono: true, placeholder: "/etc/letsencrypt/live/example.com/fullchain.pem", show: v => v.https_mode === "files" },
    { key: "tls_key_file", label: "Private key", type: "text", mono: true, placeholder: "/etc/letsencrypt/live/example.com/privkey.pem", show: v => v.https_mode === "files" },
]

/** The mode the panel uses at boot: certificate files, then Let's Encrypt, then self-signed. */
function modeOf(v: Values): Mode {
    if (v.tls_cert_file && v.tls_key_file) return "files"
    if (on(v.acme_enabled)) return "acme"
    return on(v.tls_self_signed) ? "self-signed" : "off"
}

export function HttpsCard({ card }: { card: CardDef }) {
    const { ctx } = usePage()
    const extra = useMemo<Values>(() => ({ https_mode: modeOf(ctx.values) }), [ctx.values])
    const [test, setTest] = useState<TLSTestResult | "running" | null>(null)
    const confirm = useConfirm()
    const regenerate = useRegenerateCertificate()
    const edit = useCardEdit(card, {
        extra,
        validate: d => {
            const e: Record<string, string> = {}
            if (d.https_mode === "acme" && !/^\S+@\S+$/.test(d.acme_email ?? "")) e.acme_email = "Let's Encrypt needs an email"
            if (d.https_mode === "files") {
                if (!d.tls_cert_file?.startsWith("/")) e.tls_cert_file = "A full path on the panel's server"
                if (!d.tls_key_file?.startsWith("/")) e.tls_key_file = "A full path on the panel's server"
            }
            return e
        },
        toPayload: d => {
            const selfSigned = d.https_mode === "self-signed" ? "true" : "false"
            const want: Values = d.https_mode === "files"
                ? { acme_enabled: "false", tls_cert_file: d.tls_cert_file, tls_key_file: d.tls_key_file, tls_self_signed: selfSigned }
                : d.https_mode === "acme"
                    ? { acme_enabled: "true", acme_email: d.acme_email, acme_staging: d.acme_staging, tls_cert_file: "", tls_key_file: "", tls_self_signed: selfSigned }
                    : { acme_enabled: "false", tls_cert_file: "", tls_key_file: "", tls_self_signed: selfSigned }
            return Object.fromEntries(Object.entries(want).filter(([k, v]) => (ctx.values[k] ?? "") !== (v ?? "")))
        },
    })
    const status = card.status?.(ctx)
    const mode = (edit.editing ? edit.draft.https_mode : extra.https_mode) as Mode
    const source = edit.editing ? edit.draft : { ...ctx.values, ...extra }

    const served = ctx.status?.https.mode === "self-signed" ? ctx.status.https.cert : undefined
    const runRegenerate = async () => {
        if (!await confirm({
            title: "Make a new certificate?",
            description: "Every browser and device that accepted the current one shows the warning again, and one-line node installs copied before now need a new command. The new certificate names this machine's addresses as they are now.",
            confirmLabel: "Make a new one",
        })) return
        regenerate.mutate()
    }

    const runTest = async () => {
        setTest("running")
        try {
            setTest(await settingsApi.testTLS(edit.draft.tls_cert_file ?? "", edit.draft.tls_key_file ?? ""))
        } catch {
            setTest({ success: false, error: "The test didn't run." })
        }
    }

    return (
        <SettingsGroup
            id={card.id} icon={card.icon} title={card.title} description={card.description}
            tone={toneOf(status)}
            edit={{ editing: edit.editing, onEdit: () => { setTest(null); edit.start() } }}
            footer={edit.footer(edit.changed.length ? { text: "Takes effect after a panel restart." } : null)}
        >
            {status && <StatusRow line={status} />}
            <SettingRow label={<><Hl text="Mode" />{edit.editing && <span className="ml-1.5 text-[11px] font-medium text-orange-500">restart</span>}</>}>
                {edit.editing ? (
                    <span role="radiogroup" aria-label="HTTPS mode" className="inline-flex flex-wrap gap-0.5 rounded-lg border border-border/50 bg-muted/50 p-[3px]">
                        {MODES.map(([m, label]) => (
                            <button
                                key={m} type="button" role="radio" aria-checked={mode === m}
                                onClick={() => { edit.set("https_mode", m); setTest(null) }}
                                className={cn("h-7 rounded-md px-3 text-[13px] font-medium transition-colors",
                                    mode === m ? "border border-border/80 bg-background text-foreground shadow-sm" : "border border-transparent text-muted-foreground hover:text-foreground")}
                            >{label}</button>
                        ))}
                    </span>
                ) : MODES.find(m => m[0] === mode)?.[1]}
            </SettingRow>
            {FIELDS.filter(f => f.show!(source)).map(f => <FieldRow key={f.key} card={card} f={f} edit={edit} />)}
            {edit.editing && mode === "self-signed" && (
                <SettingRow label="">
                    <p className="py-1.5 text-xs text-muted-foreground">
                        The panel makes its own certificate. Browsers warn on the first visit from each device, so compare the fingerprint they show with the one here. Subscription links stay on plain HTTP, where VPN apps can fetch them.
                    </p>
                </SettingRow>
            )}
            {!edit.editing && served && (
                <>
                    <SettingRow label={<Hl text="Fingerprint" />} hint="SHA-256. Your browser shows it in the certificate's details." action={<CopyValue text={served.fingerprint} />}>
                        <span className="font-mono text-xs [overflow-wrap:anywhere]">{served.fingerprint}</span>
                    </SettingRow>
                    {!!served.domains?.length && (
                        <SettingRow label={<Hl text="Names" />}>
                            <span className="[overflow-wrap:anywhere]">{served.domains.join(", ")}</span>
                        </SettingRow>
                    )}
                    <SettingRow label="" hint="For when this machine's addresses changed, or the key may have leaked.">
                        <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" onClick={runRegenerate} disabled={regenerate.isPending}>
                            {regenerate.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}Make a new certificate
                        </Button>
                    </SettingRow>
                </>
            )}
            {edit.editing && mode === "files" && (
                <SettingRow label="" hint={testHint(test)}>
                    <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" onClick={runTest} disabled={test === "running" || !edit.draft.tls_cert_file || !edit.draft.tls_key_file}>
                        {test === "running" ? <Loader2 className="size-3.5 animate-spin" /> : <ShieldCheck className="size-3.5" />}Test certificate
                    </Button>
                </SettingRow>
            )}
        </SettingsGroup>
    )
}

function testHint(test: TLSTestResult | "running" | null) {
    if (!test || test === "running") return undefined
    if (!test.success) return <span className="text-red-500">{test.error}</span>
    const text = `${test.issuer ?? "Valid"} · ${(test.domains ?? []).join(", ")} · expires ${day(test.not_after)}`
    return <span className={test.warning ? "text-amber-600 dark:text-amber-500" : "text-emerald-600 dark:text-emerald-500"}>{test.warning ? `${test.warning}. ` : "Loads fine. "}{text}</span>
}
