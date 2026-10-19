import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react"
import { Link } from "react-router"
import { ArrowRight, ArrowUpRight, Check, Copy } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { Checkbox } from "@/components/ui/checkbox"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { toast } from "@/lib/toast"
import { cn, copyToClipboard } from "@/lib/utils"
import { useSaveSettings } from "@/lib/queries/use-settings"
import { OnOff, SettingRow, SettingsGroup, Value, type GroupFooterProps } from "@/components/settings-page/settings-group"
import {
    RESTART_KEYS, cardById, cardKeys, on, secretTail,
    type CardDef, type Ctx, type Effect, type FieldDef, type StatusLine, type Values,
} from "./catalog"

// ---------- page context ----------

export type Secret = { mode: "keep" } | { mode: "replace"; value: string } | { mode: "remove" }

/** A card's unsaved edits. They live on the page, so a search that hides the
 * card or a move to another section on a phone doesn't throw them away. */
export interface Draft {
    edits: Values
    secrets: Record<string, Secret>
}

interface PageApi {
    ctx: Ctx
    query: string
    /** scroll to a card (opening its section on a phone) */
    goto: (cardId: string) => void
    /** a card reports whether it holds unsaved edits */
    report: (cardId: string, dirty: boolean) => void
    drafts: Record<string, Draft>
    setDraft: (cardId: string, fn: (d: Draft | undefined) => Draft | undefined) => void
}

const PageContext = createContext<PageApi | null>(null)
export const SettingsPageProvider = PageContext.Provider

export function usePage(): PageApi {
    const api = useContext(PageContext)
    if (!api) throw new Error("settings cards need SettingsPageProvider")
    return api
}

// ---------- small pieces ----------

/** Marks where the search query appears in text. */
export function Hl({ text }: { text: string }) {
    const { query } = usePage()
    const q = query.trim().toLowerCase()
    if (!q) return <>{text}</>
    const out: ReactNode[] = []
    const low = text.toLowerCase()
    let i = 0
    for (let j = low.indexOf(q); j >= 0; j = low.indexOf(q, i)) {
        out.push(text.slice(i, j), <mark key={j} className="rounded-sm bg-amber-500/30 px-px text-inherit">{text.slice(j, j + q.length)}</mark>)
        i = j + q.length
    }
    out.push(text.slice(i))
    return <>{out}</>
}

const DOT: Record<StatusLine["tone"], string> = {
    ok: "bg-emerald-500", warn: "bg-amber-500", danger: "bg-red-500", off: "bg-muted-foreground/60",
}

export function StatusRow({ line }: { line: StatusLine }) {
    return (
        <SettingRow label="Status" hint={line.hint}>
            <span className={cn("inline-flex min-w-0 items-start gap-2", line.tone === "danger" && "text-red-500")}>
                <span className={cn("mt-[7px] size-1.5 shrink-0 rounded-full", DOT[line.tone])} aria-hidden />
                <span className="[overflow-wrap:anywhere]">{line.text}</span>
            </span>
        </SettingRow>
    )
}

export function CopyValue({ text }: { text: string }) {
    const [done, setDone] = useState(false)
    return (
        <Button
            type="button" variant="ghost" size="icon" className="size-8 text-muted-foreground hover:text-foreground"
            aria-label="Copy"
            onClick={async () => {
                if (!await copyToClipboard(text)) return toast.error("Couldn't copy")
                setDone(true)
                setTimeout(() => setDone(false), 1500)
            }}
        >
            {done ? <Check className="size-3.5 text-emerald-500" /> : <Copy className="size-3.5" />}
        </Button>
    )
}

export function GotoLink({ cardId, label }: { cardId: string; label?: string }) {
    const { goto } = usePage()
    return (
        <button type="button" onClick={() => goto(cardId)} className="inline-flex items-center gap-1 text-xs font-medium text-foreground hover:underline hover:underline-offset-4">
            {label ?? cardById(cardId)?.title}<ArrowRight className="size-3" aria-hidden />
        </button>
    )
}

function EnvTag({ name }: { name: string }) {
    return (
        <span title={`Set in .env as ${name}`} className="rounded border border-border px-1.5 py-px font-mono text-[11px] text-muted-foreground">.env</span>
    )
}

const RestartTag = () => <span className="ml-1.5 text-[11px] font-medium text-orange-500">restart</span>

// ---------- editing state ----------

export interface CardEdit {
    editing: boolean
    /** saved values overlaid with the edits; a kept secret stays masked */
    draft: Values
    set: (key: string, value: string) => void
    secrets: Record<string, Secret>
    setSecret: (key: string, s: Secret) => void
    changed: string[]
    errors: Record<string, string>
    start: () => void
    cancel: () => void
    save: () => Promise<void>
    footer: (effect?: Effect | null) => GroupFooterProps | null
}

interface EditOptions {
    /** virtual draft keys (like the HTTPS mode) and their saved values */
    extra?: Values
    /** turn the changed keys into what gets sent */
    toPayload?: (draft: Values, changed: string[]) => Values
    /** save some other way than PUT /settings */
    submit?: (payload: Values, draft: Values) => Promise<void>
    validate?: (draft: Values) => Record<string, string>
    /** the card's own effect line, when it needs more than the catalog knows */
    effect?: (draft: Values, changed: string[]) => Effect | null
    /** wording for the confirm a danger effect asks for */
    confirm?: { title: string; label: string }
}

const WHOLE = /^-?\d+$/

/** "0042 " → "42"; anything else is left for validation to catch. */
export const wholeNumber = (v: string) => (WHOLE.test(v.trim()) ? String(Number(v.trim())) : v)

function fieldError(f: FieldDef, v: string): string {
    if (f.type !== "number") return ""
    if (v.trim() === "") return "Required"
    if (!WHOLE.test(v.trim())) return "A whole number"
    const n = Number(v)
    if (f.min !== undefined && n < f.min) return `At least ${f.min}`
    if (f.max !== undefined && n > f.max) return `At most ${f.max}`
    return ""
}

export function useCardEdit(card: CardDef, opts: EditOptions = {}): CardEdit {
    const { ctx, report, drafts, setDraft } = usePage()
    const confirm = useConfirm()
    const saveSettings = useSaveSettings()
    const saved = useMemo(() => ({ ...ctx.values, ...opts.extra }), [ctx.values, opts.extra])
    const held = drafts[card.id]
    const edits = held?.edits ?? null
    const secrets = useMemo(() => held?.secrets ?? {}, [held])
    const [errors, setErrors] = useState<Record<string, string>>({})
    const [saving, setSaving] = useState(false)
    const [failure, setFailure] = useState<string | null>(null)
    const editing = edits !== null

    const secretKeys = useMemo(() => new Set((card.fields ?? []).filter(f => f.type === "secret" && f.key).map(f => f.key!)), [card.fields])
    const numericKeys = useMemo(() => new Set([
        ...(card.fields ?? []).filter(f => f.type === "number" && f.key).map(f => f.key!),
        ...(card.kind === "retention" ? cardKeys(card) : []),
    ]), [card])
    const allKeys = useMemo(() => [...cardKeys(card, saved), ...Object.keys(opts.extra ?? {})], [card, saved, opts.extra])

    const draft = useMemo(() => {
        const d: Values = { ...saved, ...edits }
        for (const k of secretKeys) {
            const s = secrets[k]
            if (s?.mode === "replace" && s.value) d[k] = s.value
            else if (s?.mode === "remove") d[k] = ""
            else d[k] = saved[k] ?? ""
        }
        return d
    }, [saved, edits, secrets, secretKeys])

    // a field its switch has hidden isn't sent: it doesn't apply any more
    const keys = useMemo(() => allKeys.filter(k => {
        const f = (card.fields ?? []).find(f => f.key === k || f.items?.some(i => i[0] === k))
        return !f?.show || f.show(draft)
    }), [allKeys, card.fields, draft])

    const changed = useMemo(() => {
        if (!editing) return []
        return keys.filter(k => {
            if (secretKeys.has(k)) {
                const s = secrets[k]
                return (s?.mode === "replace" && s.value !== "") || (s?.mode === "remove" && !!saved[k])
            }
            return (draft[k] ?? "") !== (saved[k] ?? "")
        })
    }, [editing, keys, secretKeys, secrets, draft, saved])

    // no unmount cleanup: the draft outlives a hidden card, and so does the guard
    useEffect(() => { report(card.id, changed.length > 0) }, [report, card.id, changed.length])

    // any edit clears the errors; the next Save checks again
    const set = useCallback((key: string, value: string) => {
        setDraft(card.id, d => d && { ...d, edits: { ...d.edits, [key]: value } })
        setErrors({})
    }, [setDraft, card.id])
    const setSecret = useCallback((key: string, s: Secret) => {
        setDraft(card.id, d => d && { ...d, secrets: { ...d.secrets, [key]: s } })
        setErrors({})
    }, [setDraft, card.id])
    const start = useCallback(() => { setDraft(card.id, () => ({ edits: {}, secrets: {} })); setErrors({}); setFailure(null) }, [setDraft, card.id])
    const cancel = useCallback(() => { setDraft(card.id, () => undefined); setErrors({}); setFailure(null) }, [setDraft, card.id])

    const save = async () => {
        const errs: Record<string, string> = { ...card.validate?.(draft, saved), ...opts.validate?.(draft) }
        for (const f of card.fields ?? []) {
            if (f.key && (!f.show || f.show(draft)) && changed.includes(f.key)) {
                const e = fieldError(f, draft[f.key] ?? "")
                if (e) errs[f.key] = e
            }
        }
        for (const k of Object.keys(errs)) if (!errs[k]) delete errs[k]
        setErrors(errs)
        if (Object.keys(errs).length) return

        const effect = opts.effect ? opts.effect(draft, changed) : card.effect?.(draft, saved, changed, ctx)
        if (effect?.danger && !await confirm({
            title: (opts.confirm ?? card.confirm)?.title ?? `Save ${card.title}?`, description: effect.text,
            confirmLabel: (opts.confirm ?? card.confirm)?.label ?? "Save", variant: "destructive",
        })) return

        const payload: Values = opts.toPayload ? opts.toPayload(draft, changed) : {}
        if (!opts.toPayload) {
            for (const k of changed) {
                const s = secrets[k]
                payload[k] = s?.mode === "replace" ? s.value : s?.mode === "remove" ? "" : draft[k] ?? ""
                if (numericKeys.has(k)) payload[k] = wholeNumber(payload[k])
            }
        }
        setSaving(true)
        setFailure(null)
        try {
            if (opts.submit) await opts.submit(payload, draft)
            else await saveSettings.mutateAsync(payload)
            const restart = Object.keys(payload).some(k => RESTART_KEYS.has(k))
            if (restart) toast.warning(`${card.title} saved`, { description: "It takes effect after a panel restart." })
            else toast.success(`${card.title} saved`, effect ? { description: effect.text } : undefined)
            cancel()
        } catch (err) {
            setFailure(err instanceof Error ? err.message : "Couldn't save.")
        } finally {
            setSaving(false)
        }
    }

    const footer = (effect?: Effect | null): GroupFooterProps | null => editing ? {
        dirty: changed.length,
        effect,
        error: failure,
        invalid: Object.values(errors).some(Boolean),
        saving,
        onCancel: cancel,
        onSave: () => void save(),
    } : null

    return { editing, draft, set, secrets, setSecret, changed, errors, start, cancel, save, footer }
}

// ---------- rows ----------

function readValue(f: FieldDef, v: string, ctx: Ctx): ReactNode {
    switch (f.type) {
        case "bool":
            return <OnOff on={on(v)} onText={f.onText} offText={f.offText} />
        case "number":
            return f.format ? f.format(v) : <span><span className={cn(f.mono && "font-mono text-[13px]")}>{v}</span>{f.unit && <span className="text-muted-foreground"> {f.unit}</span>}</span>
        case "select": {
            const o = f.options?.find(o => o[0] === v)
            return <Value mono={f.mono}>{o ? o[1] : v}</Value>
        }
        case "secret": {
            if (!v) return <Value />
            if (f.hashed) return "Set"
            return <span className="font-mono text-[13px]">••••{secretTail(v)}</span>
        }
        case "textarea":
            return v ? <span dir="auto" className="whitespace-pre-wrap [overflow-wrap:anywhere]">{v}</span> : <span className="text-muted-foreground">{f.placeholder || "Not set"}</span>
        case "readonly":
            return <Value mono={f.mono} wrap>{f.text?.(ctx)}</Value>
        case "multi": {
            const picked = (f.items ?? []).filter(i => on(ctx.values[i[0]]))
            return picked.length
                ? <span className="flex flex-wrap gap-1.5 py-1">{picked.map(i => <span key={i[0]} className="rounded-full border border-border px-2.5 py-0.5 text-xs">{i[1]}</span>)}</span>
                : <span className="text-muted-foreground">Nothing</span>
        }
        default:
            return f.format ? f.format(v) : <span dir={f.dir}><Value mono={f.mono} wrap>{v}</Value></span>
    }
}

function SecretInput({ f, edit, id }: { f: FieldDef; edit: CardEdit; id: string }) {
    const key = f.key!
    const s = edit.secrets[key] ?? { mode: "keep" }
    const saved = usePage().ctx.values[key]
    if (s.mode === "replace" || !saved) {
        return (
            <div className="flex w-full max-w-[420px] items-center gap-2">
                <Input
                    id={id} autoFocus={!!saved} autoComplete="off" spellCheck={false}
                    className="h-9 font-mono text-[13px]"
                    placeholder={saved ? "New value · empty keeps the current one" : f.placeholder}
                    value={s.mode === "replace" ? s.value : ""}
                    aria-invalid={!!edit.errors[key]}
                    onChange={e => edit.setSecret(key, { mode: "replace", value: e.target.value })}
                />
                {saved && <Button type="button" variant="ghost" size="sm" className="h-8 shrink-0" onClick={() => edit.setSecret(key, { mode: "keep" })}>Keep</Button>}
            </div>
        )
    }
    if (s.mode === "remove") {
        return (
            <span className="flex items-center gap-2">
                <span className="text-muted-foreground line-through">{f.hashed ? "Set" : `••••${secretTail(saved)}`}</span>
                <span className="text-xs text-red-500">Removed on save</span>
                <Button type="button" variant="ghost" size="sm" className="h-7 px-2" onClick={() => edit.setSecret(key, { mode: "keep" })}>Undo</Button>
            </span>
        )
    }
    return (
        <span className="flex items-center gap-2">
            <span className="font-mono text-[13px]">{f.hashed ? "Set" : `••••${secretTail(saved)}`}</span>
            <Button type="button" variant="outline" size="sm" className="h-7 px-2.5" onClick={() => edit.setSecret(key, { mode: "replace", value: "" })}>Replace</Button>
            {f.clearable && <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-muted-foreground" onClick={() => edit.setSecret(key, { mode: "remove" })}>Remove</Button>}
        </span>
    )
}

function EditControl({ f, edit, id }: { f: FieldDef; edit: CardEdit; id: string }) {
    const key = f.key!
    const v = edit.draft[key] ?? ""
    const invalid = !!edit.errors[key]
    const dirty = edit.changed.includes(key) && "border-amber-500/60"
    switch (f.type) {
        case "bool":
            return (
                <span className="flex items-center gap-2.5">
                    <Switch id={id} checked={on(v)} onCheckedChange={c => edit.set(key, c ? "true" : "false")} />
                    <span className="text-sm">{on(v) ? (f.onText ?? "On") : (f.offText ?? "Off")}</span>
                </span>
            )
        case "number":
            return (
                <span className="flex items-center gap-2">
                    <Input
                        id={id} type="number" inputMode="numeric" min={f.min} max={f.max} value={v} aria-invalid={invalid}
                        onChange={e => edit.set(key, e.target.value)}
                        className={cn("h-9 w-28", f.mono && "font-mono text-[13px]", dirty)}
                    />
                    {f.unit && <span className="text-sm text-muted-foreground">{f.unit}</span>}
                </span>
            )
        case "select":
            return (
                <Select value={v} onValueChange={val => edit.set(key, val)}>
                    <SelectTrigger id={id} className={cn("h-9 w-full max-w-[240px]", f.mono && "font-mono text-[13px]", dirty)}><SelectValue /></SelectTrigger>
                    <SelectContent>{f.options?.map(o => <SelectItem key={o[0]} value={o[0]}>{o[1]}</SelectItem>)}</SelectContent>
                </Select>
            )
        case "secret":
            return <SecretInput f={f} edit={edit} id={id} />
        case "textarea":
            return (
                <Textarea
                    id={id} dir="auto" rows={3} value={v} placeholder={f.placeholder} aria-invalid={invalid}
                    onChange={e => edit.set(key, e.target.value)} className={cn("w-full max-w-[520px]", dirty)}
                />
            )
        default:
            return (
                <Input
                    id={id} value={v} placeholder={f.placeholder} dir={f.dir} aria-invalid={invalid} spellCheck={false}
                    onChange={e => edit.set(key, e.target.value)}
                    className={cn("h-9 w-full max-w-[420px]", f.mono && "font-mono text-[13px]", dirty)}
                />
            )
    }
}

/** One row of a card, read or edited. */
export function FieldRow({ card, f, edit }: { card: CardDef; f: FieldDef; edit: CardEdit }) {
    const { ctx } = usePage()
    const id = `setting-${card.id}-${f.key ?? f.label}`.replace(/\s+/g, "-")
    const hint = typeof f.hint === "function" ? f.hint(ctx) : f.hint
    const label = <><Hl text={f.label} />{edit.editing && f.key && RESTART_KEYS.has(f.key) && <RestartTag />}</>

    if (f.type === "multi") {
        return (
            <SettingRow label={label}>
                {edit.editing ? (
                    <span className="grid w-full grid-cols-1 gap-x-6 gap-y-1.5 py-1 @min-[560px]:grid-cols-2">
                        {f.items!.map(([k, l]) => (
                            <label key={k} className="flex items-center gap-2.5 text-sm">
                                <Checkbox checked={on(edit.draft[k])} onCheckedChange={c => edit.set(k, c === true ? "true" : "false")} />{l}
                            </label>
                        ))}
                    </span>
                ) : readValue(f, "", ctx)}
            </SettingRow>
        )
    }

    if (!edit.editing || !f.key) {
        const v = f.key ? ctx.values[f.key] ?? "" : ""
        let action: ReactNode = null
        if (f.copy && v) action = <CopyValue text={v} />
        else if (f.env) action = <EnvTag name={f.env} />
        if (f.link) action = <>{action}<Link to={f.link.to} className="inline-flex items-center gap-1 text-xs font-medium hover:underline hover:underline-offset-4">{f.link.label}<ArrowUpRight className="size-3" aria-hidden /></Link></>
        if (f.goto) action = <>{action}<GotoLink cardId={f.goto} /></>
        const envHint = edit.editing && f.env ? `Set ${f.env} in .env and restart the panel to change it.` : hint
        return <SettingRow label={label} hint={envHint} action={action}>{readValue(f, v, ctx)}</SettingRow>
    }

    return (
        <SettingRow label={label} htmlFor={f.type === "bool" || f.type === "secret" ? undefined : id} hint={hint} error={edit.errors[f.key] || undefined}>
            <EditControl f={f} edit={edit} id={id} />
        </SettingRow>
    )
}

// ---------- cards ----------

export function toneOf(line: StatusLine | null | undefined) {
    return line && line.tone !== "off" ? line.tone : undefined
}

export function CardLink({ card }: { card: CardDef }) {
    if (!card.link) return null
    return (
        <Button asChild variant="ghost" size="sm" className="h-8 gap-1 px-2 text-muted-foreground hover:text-foreground">
            <Link to={card.link.to}>{card.link.label}<ArrowUpRight className="size-3.5" aria-hidden /></Link>
        </Button>
    )
}

/** The card's note, or undefined so the card draws no empty box. */
export function cardNotice(card: CardDef, ctx: Ctx): ReactNode | undefined {
    const n = card.note?.(ctx)
    if (!n) return undefined
    return (
        <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className={cn(n.warn && "text-amber-600 dark:text-amber-500")}>{n.text}</span>
            {n.goto && <GotoLink cardId={n.goto} />}
        </span>
    )
}

/** A card built from its catalog fields. */
export function SettingsCard({ card, match = "all", edit: given }: { card: CardDef; match?: "all" | Set<string>; edit?: CardEdit }) {
    const { ctx } = usePage()
    const own = useCardEdit(card)
    const edit = given ?? own
    const status = card.status?.(ctx)
    const narrowed = match !== "all"
    const collapsed = !edit.editing && !narrowed && !!card.master && !on(ctx.values[card.master.key])
    const source = edit.editing ? edit.draft : ctx.values
    const effect = edit.changed.length ? card.effect?.(edit.draft, ctx.values, edit.changed, ctx) : null

    let rows: ReactNode
    if (collapsed) {
        rows = <SettingRow label={card.master!.label} hint={card.master!.off}><OnOff on={false} /></SettingRow>
    } else {
        rows = (card.fields ?? [])
            .filter(f => !narrowed || (match as Set<string>).has(f.key ?? f.label))
            .filter(f => (!f.editOnly || edit.editing) && (narrowed || !f.show || f.show(source)))
            .map(f => <FieldRow key={f.key ?? f.label} card={card} f={f} edit={edit} />)
    }

    return (
        <SettingsGroup
            id={card.id}
            icon={card.icon}
            title={card.title}
            description={card.description}
            tone={toneOf(status)}
            action={!edit.editing ? <CardLink card={card} /> : undefined}
            edit={{ editing: edit.editing, onEdit: edit.start }}
            notice={!collapsed && !edit.editing ? cardNotice(card, ctx) : undefined}
            footer={edit.footer(effect)}
        >
            {status && !narrowed && <StatusRow line={status} />}
            {rows}
        </SettingsGroup>
    )
}
