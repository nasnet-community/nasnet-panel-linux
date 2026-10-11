import { useEffect, useState, type ReactNode } from "react"
import * as SliderPrimitive from "@radix-ui/react-slider"
import { ArrowRight, Gauge, IdCard, Link2, Plug, Power, Send, SlidersHorizontal, Trash2, TriangleAlert, Wrench } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { CopyableText } from "@/components/ui/copyable-text"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useUnsavedChanges } from "@/hooks/use-unsaved-changes"
import { useMtprotoNode } from "@/lib/queries/use-mtproto"
import { MAX_STREAM_LIMIT, MIN_STREAM_LIMIT } from "@/lib/api/mtproto"
import type { Node } from "@/lib/types"
import { cn, formatDate } from "@/lib/utils"
import { SettingsPage, SettingsSection, type SettingsSectionMeta } from "@/components/settings-page/settings-page"
import { OnOff, SettingRow, SettingsGroup, StateBadge, Value, type GroupFooterProps } from "@/components/settings-page/settings-group"
import { DangerCard, DangerRow } from "@/components/settings-page/danger-card"
import { useMtprotoActions } from "./mtproto-actions"
import { useMtprotoSettingsForm, type MtprotoField } from "./settings/use-mtproto-settings-form"

export interface MtprotoNodeSettingsProps {
    node: Node
    /** runs after a change is saved, so the page shows it */
    onSaved?: () => void
    onDirtyChange?: (dirty: boolean) => void
    /** opens another tab of the node page */
    onNavigateTab?: (tab: string) => void
}

type GroupId = "identity" | "availability" | "repair" | "capacity"
const FIELDS: Record<GroupId, MtprotoField[]> = {
    identity: ["name"],
    availability: ["is_active", "maintenance_mode", "maintenance_message"],
    repair: ["ssh_port", "ssh_user"],
    capacity: ["max_streams"],
}
const LINK_DOWN = "Needs the link to the node"
const STEP = 100
const fmt = (n: number) => n.toLocaleString("en-US")

/** An MTProto node's settings, laid out like a full node's. */
export function MtprotoNodeSettings(props: MtprotoNodeSettingsProps) {
    // a different node starts from a clean form
    return <MtprotoNodeSettingsBody key={props.node.id} {...props} />
}

function MtprotoNodeSettingsBody({ node, onSaved, onDirtyChange, onNavigateTab }: MtprotoNodeSettingsProps) {
    const mtproto = useMtprotoNode(node.id)
    const r = mtproto.data
    const form = useMtprotoSettingsForm(node, r, onSaved)
    const actions = useMtprotoActions(node, r, () => {
        void mtproto.refetch()
        onSaved?.()
    })
    const unsaved = useUnsavedChanges(form.isDirty)
    const [editing, setEditing] = useState<Partial<Record<GroupId, boolean>>>({})
    const [errors, setErrors] = useState<Partial<Record<GroupId, string>>>({})

    useEffect(() => { onDirtyChange?.(form.isDirty) }, [form.isDirty, onDirtyChange])
    useEffect(() => () => onDirtyChange?.(false), [onDirtyChange])

    const dirtyIn = (g: GroupId) => FIELDS[g].filter(f => form.dirtyFields.includes(f))
    const isEditing = (g: GroupId) => !!editing[g]
    const startEdit = (g: GroupId) => { setEditing(e => ({ ...e, [g]: true })); setErrors(e => ({ ...e, [g]: undefined })) }
    const cancel = (g: GroupId) => {
        form.discard(FIELDS[g])
        setEditing(e => ({ ...e, [g]: false }))
        setErrors(e => ({ ...e, [g]: undefined }))
    }
    const save = async (g: GroupId) => {
        setErrors(e => ({ ...e, [g]: undefined }))
        const res = await form.save(FIELDS[g])
        if (res.invalid) requestAnimationFrame(() => document.getElementById(`mtproto-${res.invalid}`)?.focus())
        else if (res.ok) setEditing(e => ({ ...e, [g]: false }))
        else if (res.error) setErrors(e => ({ ...e, [g]: res.error }))
    }
    const footer = (g: GroupId, effect?: GroupFooterProps["effect"]): GroupFooterProps | null => isEditing(g) ? {
        dirty: dirtyIn(g).length,
        effect,
        error: errors[g],
        invalid: dirtyIn(g).some(f => !!form.shownErrors[f]),
        saving: form.isSaving,
        onCancel: () => cancel(g),
        onSave: () => void save(g),
    } : null
    const edit = (g: GroupId, disabledReason?: string) => ({ editing: isEditing(g), onEdit: () => startEdit(g), disabledReason })
    const textInput = (f: MtprotoField, extra: { mono?: boolean; numeric?: boolean } = {}) => (
        <Input
            id={`mtproto-${f}`}
            value={String(form.values[f])}
            onChange={e => form.set(f, e.target.value)}
            onBlur={() => form.touch(f)}
            inputMode={extra.numeric ? "numeric" : undefined}
            aria-invalid={!!form.shownErrors[f]}
            spellCheck={false}
            className={cn("h-9 max-w-[360px]", extra.mono && "font-mono", extra.numeric && "max-w-[160px]", form.dirtyFields.includes(f) && "border-amber-500/60")}
        />
    )

    // groups that need the node's own numbers wait for them
    let pending: ReactNode = null
    if (!r) pending = mtproto.isError
        ? (
            <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                {(mtproto.error as Error | null)?.message ?? "Couldn't load this MTProto node."}
                <Button type="button" size="sm" variant="outline" className="h-7" onClick={() => void mtproto.refetch()}>Retry</Button>
            </span>
        )
        : "Loading the node's settings…"

    const linkUp = actions.linkUp
    const sections: SettingsSectionMeta[] = [
        { id: "general", title: "General", icon: SlidersHorizontal, dirty: [...dirtyIn("identity"), ...dirtyIn("availability")].length > 0 },
        { id: "connection", title: "Connection", icon: Plug, dirty: dirtyIn("repair").length > 0 },
        { id: "capacity", title: "Capacity", icon: Gauge, dirty: dirtyIn("capacity").length > 0 },
        { id: "danger", title: "Danger zone", icon: TriangleAlert, danger: true },
    ]

    const identity = (
        <SettingsGroup id="identity" icon={IdCard} title="Identity" description="How this node shows up in the panel." edit={edit("identity")} footer={footer("identity")}>
            <SettingRow label="Name" htmlFor={isEditing("identity") ? "mtproto-name" : undefined} error={isEditing("identity") ? form.shownErrors.name : undefined}>
                {isEditing("identity") ? textInput("name") : <Value>{form.saved.name}</Value>}
            </SettingRow>
            <SettingRow label="Address" hint="Part of every user's proxy link." action={
                <Button type="button" variant="outline" size="sm" className="h-8" onClick={actions.changeAddress} disabled={isEditing("identity")}>Change</Button>
            }>
                <Value mono>{node.ip}</Value>
            </SettingRow>
            <SettingRow label="Node"><Value mono>#{node.id}</Value></SettingRow>
        </SettingsGroup>
    )

    const active = form.saved.is_active ?? node.is_active
    const maintSaved = !!form.saved.maintenance_mode
    const maintOn = isEditing("availability") ? form.values.maintenance_mode : maintSaved
    const availability = (
        <SettingsGroup
            id="availability" icon={Power} title="Availability" description="Whether users can connect, and the notice they see."
            tone={!active ? "danger" : maintSaved ? "warn" : "ok"}
            edit={edit("availability")}
            footer={footer("availability", dirtyIn("availability").includes("is_active")
                ? (form.values.is_active ? { text: "The panel opens the link again" } : { text: "Saving cuts this node's users off", danger: true })
                : dirtyIn("availability").includes("maintenance_mode") ? { text: form.values.maintenance_mode ? "Users see the notice right away" : "The notice goes away" } : null)}
        >
            <SettingRow label="In service" hint={isEditing("availability")
                ? (form.values.is_active ? "Users can connect through this node." : "The link closes and its proxy links are hidden.")
                : active ? undefined : "Nobody can connect and its links are hidden."}>
                {isEditing("availability")
                    ? <Switch id="mtproto-is_active" checked={form.values.is_active} onCheckedChange={v => form.set("is_active", v)} aria-label="In service" />
                    : <OnOff on={active} />}
            </SettingRow>
            <SettingRow
                label="Maintenance notice"
                hint={isEditing("availability")
                    ? "A banner on users' subscription page, and purchases pause. Traffic is untouched."
                    : maintSaved && node.maintenance_since ? `On since ${new Date(node.maintenance_since).toLocaleString()}` : undefined}
            >
                {isEditing("availability")
                    ? <Switch id="mtproto-maintenance_mode" checked={form.values.maintenance_mode} onCheckedChange={v => form.set("maintenance_mode", v)} aria-label="Maintenance notice" />
                    : <OnOff on={maintSaved} />}
            </SettingRow>
            {maintOn && (
                <SettingRow label="Notice" htmlFor={isEditing("availability") ? "mtproto-maintenance_message" : undefined}
                    error={isEditing("availability") ? form.shownErrors.maintenance_message : undefined}
                    hint={isEditing("availability") ? "Leave it empty for the default notice, in the user's language." : undefined}>
                    {isEditing("availability")
                        ? <Textarea
                            id="mtproto-maintenance_message" dir="auto" rows={2} placeholder="Back in about an hour."
                            value={form.values.maintenance_message} onChange={e => form.set("maintenance_message", e.target.value)} onBlur={() => form.touch("maintenance_message")}
                            aria-invalid={!!form.shownErrors.maintenance_message}
                            className={cn("min-h-16", form.dirtyFields.includes("maintenance_message") && "border-amber-500/60")}
                        />
                        : <Value empty="Default notice">{form.saved.maintenance_message}</Value>}
                </SettingRow>
            )}
        </SettingsGroup>
    )

    const proxy = (
        <SettingsGroup
            id="proxy" icon={Send} title="Proxy" description="Port, cover site and user limits are set on the Tunnels tab."
            action={onNavigateTab && (
                <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" onClick={() => onNavigateTab("tunnels")}>
                    Open Tunnels<ArrowRight className="size-3.5" aria-hidden />
                </Button>
            )}
        />
    )

    const link = (
        <SettingsGroup
            id="link" icon={Link2} title="Link" description="The panel reaches the node on the link port. Users never see it."
            tone={r ? (linkUp ? "ok" : "warn") : undefined}
            badge={r && <StateBadge tone={linkUp ? "ok" : "warn"}>{linkUp ? "Up" : "Down"}</StateBadge>}
            unavailable={pending}
            advanced={r ? {
                count: 2,
                children: (<>
                    <SettingRow label="Capture port" hint="Local to the node."><Value mono>{r.capture_port}</Value></SettingRow>
                    <SettingRow label="Agent ID"><CopyableText text={r.agent_id} className="font-mono text-[13px]" /></SettingRow>
                </>),
            } : undefined}
        >
            <SettingRow
                label="Link port"
                hint={!linkUp && r ? LINK_DOWN : undefined}
                action={
                    <span title={linkUp ? undefined : LINK_DOWN}>
                        <Button type="button" variant="outline" size="sm" className="h-8" disabled={!linkUp} onClick={actions.changeLinkPort}>Change</Button>
                    </span>
                }
            >
                <Value mono>{r?.hub_port}</Value>
            </SettingRow>
            <SettingRow
                label="Link keys"
                hint="Rotating makes new keys for the link."
                action={
                    <span title={linkUp ? undefined : LINK_DOWN}>
                        <Button type="button" variant="outline" size="sm" className="h-8" disabled={!linkUp} onClick={() => void actions.rotateKeys()}>Rotate</Button>
                    </span>
                }
            >
                <Value>{!r ? undefined
                    : r.keys_rotated_at ? `Rotated ${formatDate(r.keys_rotated_at, "long")}`
                    : r.installed_at ? `From the install, ${formatDate(r.installed_at, "long")}`
                    : "From the install"}</Value>
            </SettingRow>
        </SettingsGroup>
    )

    const repair = (
        <SettingsGroup
            id="repair" icon={Wrench} title="Repair access" description="Used only to repair or remove the node. Passwords are never stored."
            unavailable={pending}
            edit={edit("repair", r ? undefined : "Waiting for the node's settings")}
            advanced={{
                count: 2,
                held: dirtyIn("repair").length > 0,
                openWhenEditing: true,
                children: (<>
                    <SettingRow label="SSH port" htmlFor={isEditing("repair") ? "mtproto-ssh_port" : undefined} error={isEditing("repair") ? form.shownErrors.ssh_port : undefined}>
                        {isEditing("repair") ? textInput("ssh_port", { mono: true, numeric: true }) : <Value mono>{form.saved.ssh_port}</Value>}
                    </SettingRow>
                    <SettingRow label="SSH user" htmlFor={isEditing("repair") ? "mtproto-ssh_user" : undefined} error={isEditing("repair") ? form.shownErrors.ssh_user : undefined}>
                        {isEditing("repair") ? textInput("ssh_user", { mono: true }) : <Value mono>{form.saved.ssh_user}</Value>}
                    </SettingRow>
                </>),
            }}
            footer={footer("repair", dirtyIn("repair").length ? { text: "Used the next time you repair or remove the node" } : null)}
        >
            <SettingRow label="Host key" hint="Checked before every repair.">
                {r?.host_key ? <CopyableText text={r.host_key} className="min-w-0 truncate font-mono text-[13px]" /> : <Value mono />}
            </SettingRow>
        </SettingsGroup>
    )

    const typed = Number(form.values.max_streams)
    const sliderValue = Number.isFinite(typed) ? Math.min(MAX_STREAM_LIMIT, Math.max(MIN_STREAM_LIMIT, typed)) : MIN_STREAM_LIMIT
    const capacity = (
        <SettingsGroup
            id="capacity" icon={Gauge} title="Stream limit" description="How many Telegram connections the agent carries at once."
            unavailable={pending}
            edit={edit("capacity", r ? undefined : "Waiting for the node's settings")}
            footer={footer("capacity", dirtyIn("capacity").length ? { text: linkUp ? "Applies at once; streams past the limit are refused" : "Applies once the link is back" } : null)}
        >
            <SettingRow
                label="Max streams" htmlFor={isEditing("capacity") ? "mtproto-max_streams" : undefined}
                error={isEditing("capacity") ? form.shownErrors.max_streams : undefined}
                hint={`Tested up to ${fmt(MAX_STREAM_LIMIT)}.`}
            >
                {isEditing("capacity") ? (
                    <div className="flex w-full max-w-[460px] items-center gap-4">
                        {textInput("max_streams", { mono: true, numeric: true })}
                        <SliderPrimitive.Root
                            className="relative flex h-9 w-full touch-none select-none items-center"
                            min={MIN_STREAM_LIMIT} max={MAX_STREAM_LIMIT} step={STEP}
                            value={[sliderValue]}
                            onValueChange={([v]) => form.set("max_streams", String(v))}
                            onValueCommit={() => form.touch("max_streams")}
                        >
                            <SliderPrimitive.Track className="relative h-1.5 w-full grow overflow-hidden rounded-full bg-primary/20">
                                <SliderPrimitive.Range className="absolute h-full bg-primary" />
                            </SliderPrimitive.Track>
                            <SliderPrimitive.Thumb aria-label="Stream limit" className="block size-4 rounded-full border border-primary/50 bg-background shadow transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" />
                        </SliderPrimitive.Root>
                    </div>
                ) : <Value mono>{form.saved.max_streams !== undefined ? fmt(Number(form.saved.max_streams)) : undefined}</Value>}
            </SettingRow>
        </SettingsGroup>
    )

    return (
        <>
            <SettingsPage sections={sections}>
                <SettingsSection id="general" title="General" icon={SlidersHorizontal}>{identity}{availability}{proxy}</SettingsSection>
                <SettingsSection id="connection" title="Connection" icon={Plug}>{link}{repair}</SettingsSection>
                <SettingsSection id="capacity" title="Capacity" icon={Gauge}>{capacity}</SettingsSection>
                <SettingsSection id="danger" title="Danger zone" icon={TriangleAlert} hideTitle>
                    <DangerCard>
                        <DangerRow
                            icon={Trash2} tone="danger" title="Remove node"
                            text="Uninstalls the agent and the proxy, then deletes the node from the panel. Its users lose this node."
                            why={linkUp ? undefined : "The link is down, so removing asks for an SSH login."}
                            button="Remove…" onClick={actions.remove}
                        />
                    </DangerCard>
                </SettingsSection>
            </SettingsPage>
            {actions.dialogs}

            <Dialog open={unsaved.showDialog} onOpenChange={o => { if (!o) unsaved.cancelNavigation() }}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Leave with unsaved changes?</DialogTitle>
                        <DialogDescription>Edits you haven't saved are lost.</DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={unsaved.cancelNavigation}>Keep editing</Button>
                        <Button variant="destructive" onClick={unsaved.confirmNavigation}>Discard and leave</Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    )
}
