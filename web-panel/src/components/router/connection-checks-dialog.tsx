import { useMemo, useState } from "react"
import { ChevronDown, Plus, X } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { ResponsiveDialog } from "@/components/ui/responsive-dialog"
import { useSettings, useUpdateSettings } from "@/lib/queries/use-settings"
import {
    CHECK_PRESETS,
    DEFAULT_LOSS_PCT,
    DEFAULT_RTT_MS,
    LEGACY_LOSS_KEY,
    MAX_TARGETS,
    checkRows,
    defaultPort,
    groupOf,
    groupLossKey,
    groupRttKey,
    groupTargetsKey,
    isPreset,
    parseTargets,
    protoLabel,
    sameTarget,
    serializeTargets,
    slotLossKey,
    slotRttKey,
    slotTargetsKey,
    splitAddress,
    targetError,
    type ProbeTarget,
} from "@/lib/router-checks"
import { cn } from "@/lib/utils"
import type { UplinkSlot } from "@/lib/types/network"

/** One other line in the same group, named the way the operator named it. */
export interface CheckSibling {
    slot: UplinkSlot
    label: string
}

type Scope = "group" | "line"
type Read = (key: string) => string | undefined

interface ChecksProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    slot: UplinkSlot
    lineLabel: string
    siblings?: CheckSibling[]
}

/** Reads the list a line is actually probed against, and says where it came
 *  from: a line's own key wins, and an empty one means it uses its group's. */
function readScope(slot: UplinkSlot, valueOf: Read): { scope: Scope; active: ProbeTarget[] } {
    const own = parseTargets(valueOf(slotTargetsKey(slot)))
    if (own.length > 0) return { scope: "line", active: own }
    const shared = parseTargets(valueOf(groupTargetsKey(slot)))
    return { scope: "group", active: shared.length > 0 ? shared : CHECK_PRESETS[groupOf(slot)] }
}

/** Which lines a save reaches. The whole point of the scope switch is that
 *  this sentence is never a guess. */
function reachSentence(scope: Scope, lineLabel: string, siblings: CheckSibling[]): string {
    const others = siblings.map((s) => s.label)
    if (scope === "line") {
        const rest =
            others.length === 0
                ? "No other line uses it."
                : `${listOf(others)} keep${others.length === 1 ? "s" : ""} the shared list.`
        return `Tested every 5 seconds. These checks apply to ${lineLabel} only. ${rest}`
    }
    return `Tested every 5 seconds. This is the shared list, so changes affect ${listOf([lineLabel, ...others])}.`
}

function listOf(names: string[]): string {
    if (names.length <= 1) return names[0] ?? ""
    return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`
}

// The bulk endpoint takes whole rows, and an unseeded key is created on write.
function setting(key: string, value: string, type = "string") {
    return { key, value, type, category: "router", description: "", label: "" }
}

/** What a line is tested against, and whether that list is its own or its
 *  group's. The form mounts on open, so it is always seeded from storage. */
export function ConnectionChecksDialog(props: ChecksProps) {
    const settings = useSettings()

    const valueOf = useMemo<Read>(() => {
        const byKey = new Map((settings.data?.router ?? []).map((s) => [s.key, s.value]))
        return (key) => byKey.get(key)
    }, [settings.data])

    // Only the keys this form reads. Remounting on a real change to them
    // reseeds the fields; an unrelated refetch leaves the typing alone.
    const stored = useMemo(
        () =>
            JSON.stringify([
                valueOf(slotTargetsKey(props.slot)),
                valueOf(groupTargetsKey(props.slot)),
                valueOf(slotRttKey(props.slot)),
                valueOf(groupRttKey(props.slot)),
                valueOf(slotLossKey(props.slot)),
                valueOf(groupLossKey(props.slot)),
                valueOf(LEGACY_LOSS_KEY),
            ]),
        [valueOf, props.slot],
    )

    if (!props.open) return null
    return <ChecksForm key={`${props.slot}:${stored}`} {...props} valueOf={valueOf} />
}

function ChecksForm({
    onOpenChange,
    slot,
    lineLabel,
    siblings = [],
    valueOf,
}: ChecksProps & { valueOf: Read }) {
    const save = useUpdateSettings()
    const group = groupOf(slot)
    const initial = readScope(slot, valueOf)

    const [scope, setScope] = useState<Scope>(initial.scope)
    const [checked, setChecked] = useState<ProbeTarget[]>(initial.active)
    const [rows, setRows] = useState<ProbeTarget[]>(() => checkRows(slot, initial.active))
    const [rtt, setRtt] = useState(
        () =>
            valueOf(slotRttKey(slot)) || valueOf(groupRttKey(slot)) || String(DEFAULT_RTT_MS[group]),
    )
    const [loss, setLoss] = useState(
        () => valueOf(slotLossKey(slot)) || valueOf(groupLossKey(slot)) ||
            valueOf(LEGACY_LOSS_KEY) || String(DEFAULT_LOSS_PCT),
    )
    const [advanced, setAdvanced] = useState(false)
    const [draft, setDraft] = useState(() => {
        const proto = group === "domestic" ? ("dns" as const) : ("tcp" as const)
        return { label: "", host: "", proto, port: defaultPort(proto) }
    })
    const [addError, setAddError] = useState<string | null>(null)

    const isChecked = (t: ProbeTarget) => checked.some((c) => sameTarget(c, t))
    const atCap = checked.length >= MAX_TARGETS

    function toggle(t: ProbeTarget) {
        setChecked((prev) => {
            if (prev.some((c) => sameTarget(c, t))) {
                // One has to stay, or the prober silently keeps the defaults
                // and the list on screen stops being the truth.
                return prev.length === 1 ? prev : prev.filter((c) => !sameTarget(c, t))
            }
            return prev.length >= MAX_TARGETS ? prev : [...prev, t]
        })
    }

    function add() {
        const err = targetError(draft.host, draft.port)
        if (err) {
            setAddError(err)
            return
        }
        const t: ProbeTarget = {
            address: `${draft.host.trim()}:${draft.port.trim()}`,
            proto: draft.proto,
            ...(draft.label.trim() ? { label: draft.label.trim() } : {}),
        }
        if (rows.some((r) => sameTarget(r, t))) {
            setAddError("That server is already listed")
            return
        }
        setRows((prev) => [...prev, t])
        setChecked((prev) => (prev.length >= MAX_TARGETS ? prev : [...prev, t]))
        setDraft({ label: "", host: "", proto: draft.proto, port: defaultPort(draft.proto) })
        setAddError(null)
    }

    function remove(t: ProbeTarget) {
        setRows((prev) => prev.filter((r) => !sameTarget(r, t)))
        setChecked((prev) => prev.filter((c) => !sameTarget(c, t)))
    }

    /** Back to the shared list: empty this line's keys so the group's win. */
    async function backToShared() {
        try {
            await save.mutateAsync(
                [slotTargetsKey(slot), slotRttKey(slot), slotLossKey(slot)].map((k) =>
                    setting(k, ""),
                ),
            )
            toast.success(`${lineLabel} is back on the shared list`)
            onOpenChange(false)
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to save the checks")
        }
    }

    function restoreDefaults() {
        const presets = CHECK_PRESETS[group]
        setRows(presets)
        setChecked(presets)
        setRtt(String(DEFAULT_RTT_MS[group]))
        setLoss(String(DEFAULT_LOSS_PCT))
    }

    async function commit() {
        if (checked.length === 0) return
        const blob = serializeTargets(checked)
        const payload =
            scope === "line"
                ? [
                      setting(slotTargetsKey(slot), blob, "json"),
                      setting(slotRttKey(slot), rtt, "int"),
                      setting(slotLossKey(slot), loss, "int"),
                  ]
                : [
                      setting(groupTargetsKey(slot), blob, "json"),
                      setting(groupRttKey(slot), rtt, "int"),
                      setting(groupLossKey(slot), loss, "int"),
                      // A stale override left behind would make the shared list
                      // look ignored on this line.
                      setting(slotTargetsKey(slot), ""),
                      setting(slotRttKey(slot), ""),
                      setting(slotLossKey(slot), ""),
                  ]
        try {
            await save.mutateAsync(payload)
            toast.success(
                scope === "line"
                    ? `Checks updated for ${lineLabel}`
                    : `Checks updated for all ${group} lines`,
            )
            onOpenChange(false)
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to save the checks")
        }
    }

    const scopeOptions: { value: Scope; label: string }[] = [
        { value: "group", label: group === "domestic" ? "All domestic lines" : "All foreign lines" },
        { value: "line", label: `Just ${lineLabel}` },
    ]

    return (
        <ResponsiveDialog
            open
            onOpenChange={onOpenChange}
            title={`Connection checks · ${lineLabel}`}
            description={reachSentence(scope, lineLabel, siblings)}
            onSave={() => void commit()}
            saveDisabled={save.isPending || checked.length === 0}
        >
            <div className="space-y-6">
                <div className="border-border/60 flex items-center justify-between gap-3 border-b pb-4">
                    <span className="text-sm font-medium">Applies to</span>
                    <div
                        className="divide-border border-border flex w-fit divide-x overflow-hidden rounded-md border"
                        role="group"
                        aria-label="Which lines use this list"
                    >
                        {scopeOptions.map((o) => (
                            <button
                                key={o.value}
                                type="button"
                                aria-pressed={scope === o.value}
                                onClick={() => setScope(o.value)}
                                className={cn(
                                    "px-3 py-1.5 text-[13px] transition-colors",
                                    scope === o.value
                                        ? "bg-surface-3 text-text-primary font-medium"
                                        : "text-text-tertiary hover:text-text-secondary hover:bg-surface-3/50",
                                )}
                            >
                                {o.label}
                            </button>
                        ))}
                    </div>
                </div>

                <div className="space-y-2.5">
                    <div className="flex items-center justify-between gap-3">
                        <span className="text-sm font-medium">Servers to test</span>
                        {scope === "line" ? (
                            <Button
                                variant="ghost"
                                size="xs"
                                disabled={save.isPending}
                                onClick={() => void backToShared()}
                            >
                                Use the shared list
                            </Button>
                        ) : (
                            <Button variant="ghost" size="xs" onClick={restoreDefaults}>
                                Restore defaults
                            </Button>
                        )}
                    </div>

                    <div className="divide-border-subtle divide-y">
                        {rows.map((t) => {
                            const on = isChecked(t)
                            return (
                                <div
                                    key={t.address}
                                    data-target={t.address}
                                    className="flex items-center gap-3 py-2.5"
                                >
                                    <Checkbox
                                        checked={on}
                                        disabled={!on && atCap}
                                        onCheckedChange={() => toggle(t)}
                                        aria-label={t.label || t.address}
                                    />
                                    <div className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1">
                                        <span className="text-sm">{t.label || t.address}</span>
                                        <span className="text-text-tertiary font-mono text-xs">
                                            {t.address}
                                        </span>
                                        <span className="bg-surface-3 text-text-tertiary rounded px-1.5 font-mono text-[11px]">
                                            {protoLabel(t.proto)}
                                        </span>
                                        {!splitAddress(t.address).port && (
                                            <span className="text-status-warning text-[11px]">
                                                no port
                                            </span>
                                        )}
                                    </div>
                                    {!isPreset(slot, t) && (
                                        <Button
                                            variant="ghost"
                                            size="icon"
                                            className="ml-auto h-6 w-6 shrink-0"
                                            aria-label={`Remove ${t.label || t.address}`}
                                            onClick={() => remove(t)}
                                        >
                                            <X className="h-3.5 w-3.5" />
                                        </Button>
                                    )}
                                </div>
                            )
                        })}
                    </div>

                    <div className="flex flex-wrap items-center gap-2">
                        <Input
                            value={draft.label}
                            onChange={(e) => setDraft({ ...draft, label: e.target.value })}
                            placeholder="Name"
                            aria-label="Name for the server you are adding"
                            maxLength={31}
                            className="min-w-[8.75rem] flex-1"
                        />
                        <Input
                            value={draft.host}
                            onChange={(e) => setDraft({ ...draft, host: e.target.value })}
                            placeholder="Address"
                            aria-label="Address of the server you are adding"
                            className="min-w-[9.375rem] flex-1"
                        />
                        <div
                            className="divide-border border-border flex w-fit shrink-0 divide-x overflow-hidden rounded-md border"
                            role="group"
                            aria-label="How to test it"
                        >
                            {(["tcp", "dns"] as const).map((p) => (
                                <button
                                    key={p}
                                    type="button"
                                    aria-pressed={draft.proto === p}
                                    onClick={() =>
                                        setDraft({ ...draft, proto: p, port: defaultPort(p) })
                                    }
                                    className={cn(
                                        "px-2.5 py-1.5 text-[13px] transition-colors",
                                        draft.proto === p
                                            ? "bg-surface-3 text-text-primary font-medium"
                                            : "text-text-tertiary hover:text-text-secondary hover:bg-surface-3/50",
                                    )}
                                >
                                    {protoLabel(p)}
                                </button>
                            ))}
                        </div>
                        <Input
                            value={draft.port}
                            onChange={(e) => setDraft({ ...draft, port: e.target.value })}
                            aria-label="Port"
                            className="w-[4.75rem] shrink-0 tabular-nums"
                        />
                        <Button
                            variant="outline"
                            size="icon"
                            className="shrink-0"
                            disabled={atCap}
                            aria-label="Add this server"
                            onClick={add}
                        >
                            <Plus className="h-4 w-4" />
                        </Button>
                    </div>
                    {checked.length === 0 && (
                        <p className="text-status-warning text-xs">Select at least one server before saving.</p>
                    )}
                    {addError && <p className="text-status-danger text-xs">{addError}</p>}
                </div>

                <div className="space-y-2.5">
                    <button
                        type="button"
                        aria-expanded={advanced}
                        onClick={() => setAdvanced((v) => !v)}
                        className="flex w-full items-center justify-between gap-2 text-left"
                    >
                        <span className="text-sm font-medium">
                            Advanced · when does a line count as slow?
                        </span>
                        <ChevronDown
                            className={cn(
                                "text-text-tertiary h-4 w-4 transition-transform duration-150",
                                advanced && "rotate-180",
                            )}
                        />
                    </button>
                    {advanced && (
                        <div className="space-y-2.5">
                            <label className="text-text-secondary flex items-center justify-between gap-3 text-sm">
                                More than this share of checks failing
                                <span className="flex items-center gap-2">
                                    <Input
                                        value={loss}
                                        onChange={(e) => setLoss(e.target.value)}
                                        aria-label="Failing share that counts as slow"
                                        className="w-[4.75rem] tabular-nums"
                                    />
                                    <span className="text-text-tertiary">%</span>
                                </span>
                            </label>
                            <label className="text-text-secondary flex items-center justify-between gap-3 text-sm">
                                Answers slower than
                                <span className="flex items-center gap-2">
                                    <Input
                                        value={rtt}
                                        onChange={(e) => setRtt(e.target.value)}
                                        aria-label="Answer time that counts as slow"
                                        className="w-[4.75rem] tabular-nums"
                                    />
                                    <span className="text-text-tertiary">ms</span>
                                </span>
                            </label>
                            <p className="text-text-tertiary text-xs">
                                A slow line stays in use. It is only marked so you can see it.
                            </p>
                        </div>
                    )}
                </div>
            </div>
        </ResponsiveDialog>
    )
}
