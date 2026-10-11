import { useEffect, useState, type ReactNode } from "react"
import { Link, useNavigate } from "react-router"
import { Controller, type FieldPath } from "react-hook-form"
import { Activity, Cpu, FileText, IdCard, Plug, Power, RefreshCw, SatelliteDish, ScrollText, SlidersHorizontal, SquareTerminal, TriangleAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Form } from "@/components/ui/form"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { toast } from "@/lib/toast"
import { deleteNode } from "@/lib/api/nodes"
import { useNodeSettingsForm } from "@/hooks/use-node-settings-form"
import { useUnsavedChanges } from "@/hooks/use-unsaved-changes"
import type { NodeSettingsFormData } from "@/lib/validations/node-settings-schema"
import { DangerZone } from "@/components/node/danger-zone"
import { NodeSettingsBandwidth } from "./settings/node-settings-bandwidth"
import { AddressChange, AgentPortDialog, ConnectModeDialog } from "./settings/agent-moves"
import { SettingsPage, SettingsSection, type SettingsSectionMeta } from "@/components/settings-page/settings-page"
import { GroupFooter, OnOff, SettingRow, SettingsGroup, Value, type GroupFooterProps } from "@/components/settings-page/settings-group"
import { CountryLabel, CountryPicker } from "@/components/settings-page/country-picker"
import { isLocalNode } from "@/lib/node-kind"
import { cn } from "@/lib/utils"
import type { Node } from "@/lib/types"

type Field = FieldPath<NodeSettingsFormData>
type GroupId = "identity" | "availability" | "agent" | "ssh" | "logging" | "starlink" | "bandwidth"

const FIELDS: Record<GroupId, Field[]> = {
    identity: ["name", "country_code", "datacenter"],
    availability: ["is_active", "maintenance_mode", "maintenance_message"],
    agent: ["api_port"],
    ssh: ["ssh_enabled", "ssh_port"],
    logging: ["loglevel", "log_access", "log_error", "dnsLog", "enable_access_log"],
    starlink: ["starlink_enabled", "starlink_dish_address"],
    bandwidth: ["bandwidth_enabled", "bandwidth_interface", "bandwidth_total_bw"],
}
const LABEL: Record<GroupId, string> = {
    identity: "Identity", availability: "Availability", agent: "Agent", ssh: "SSH",
    logging: "Xray logging", starlink: "Starlink", bandwidth: "Bandwidth",
}
const LEVELS = [["debug", "Debug"], ["info", "Info"], ["warning", "Warning"], ["error", "Error"], ["none", "None"]] as const
const OFFLINE = "Needs the node online"

interface NodeSettingsProps {
    node: Node
    onRefresh: () => void
    onDirtyChange?: (dirty: boolean) => void
}

/** A full node's settings: one page of groups, each edited and saved on its own. */
export function NodeSettings({ node, onRefresh, onDirtyChange }: NodeSettingsProps) {
    const navigate = useNavigate()
    const confirm = useConfirm()
    const settings = useNodeSettingsForm(node, { onRefresh })
    const { form, savedValues: saved, dirtyFields } = settings
    const unsaved = useUnsavedChanges(settings.isDirty)
    const [editing, setEditing] = useState<Partial<Record<GroupId, boolean>>>({})
    const [errors, setErrors] = useState<Partial<Record<GroupId, string>>>({})
    const [forceDelete, setForceDelete] = useState(false)
    const [modeOpen, setModeOpen] = useState(false)
    const [portOpen, setPortOpen] = useState(false)
    const [addrOpen, setAddrOpen] = useState(false)
    const local = isLocalNode(node)
    const online = node.is_online
    const stealth = !!node.is_stealth
    const direct = (node.connect_mode || "direct") === "direct"

    useEffect(() => { onDirtyChange?.(settings.isDirty) }, [settings.isDirty, onDirtyChange])
    useEffect(() => () => onDirtyChange?.(false), [onDirtyChange])

    const draft = form.watch()
    const fieldError = (f: Field) => form.formState.errors[f]?.message as string | undefined
    const dirtyIn = (g: GroupId) => FIELDS[g].filter(f => dirtyFields.includes(f))
    const dirtyCls = (f: Field) => dirtyFields.includes(f) && "border-amber-500/60"

    const startEdit = (g: GroupId) => { setEditing(e => ({ ...e, [g]: true })); setErrors(e => ({ ...e, [g]: undefined })) }
    const cancel = (g: GroupId) => {
        settings.discard(FIELDS[g])
        setEditing(e => ({ ...e, [g]: false }))
        setErrors(e => ({ ...e, [g]: undefined }))
    }
    const save = async (g: GroupId) => {
        setErrors(e => ({ ...e, [g]: undefined }))
        if (g === "availability" && saved.is_active && !draft.is_active && !await confirm({
            title: `Take ${node.name} out of service?`,
            description: "Its users are removed from Xray right away, so nobody can connect through it, and its links leave their subscriptions. Xray keeps running, and turning it back on brings everyone back.",
            confirmLabel: "Take out of service",
            variant: "destructive",
        })) return
        const res = await settings.save(FIELDS[g], LABEL[g])
        if (res.invalid) requestAnimationFrame(() => form.setFocus(res.invalid!))
        else if (res.ok) setEditing(e => ({ ...e, [g]: false }))
        else setErrors(e => ({ ...e, [g]: res.error }))
    }
    const footer = (g: GroupId, effect?: GroupFooterProps["effect"]): GroupFooterProps | null => editing[g] ? {
        dirty: dirtyIn(g).length,
        effect,
        error: errors[g],
        invalid: FIELDS[g].some(f => !!form.formState.errors[f]),
        saving: settings.isSaving,
        onCancel: () => cancel(g),
        onSave: () => void save(g),
    } : null
    const edit = (g: GroupId, disabledReason?: string) => ({ editing: !!editing[g], onEdit: () => startEdit(g), disabledReason })

    const handleDelete = async () => {
        if (!await confirm({
            title: forceDelete ? "Force delete node?" : "Delete node?",
            description: `Remove ${node.name} from the panel and uninstall its agent and Xray.${forceDelete ? " Linked accounts and configuration are deleted too." : " A node with linked accounts or inbounds needs force deletion."}`,
            variant: "destructive", typeToConfirm: node.name, confirmLabel: forceDelete ? "Force delete" : "Delete node",
        })) return
        try {
            const res = await deleteNode(node.id, forceDelete)
            if (res.success) {
                toast.success("Node deleted")
                navigate("/nodes")
            } else if (res.error?.includes("has active")) {
                setForceDelete(true)
                toast.error("The node has active accounts. Use force delete to remove it.")
            } else {
                toast.error(res.error || "Couldn't delete the node")
            }
        } catch {
            toast.error("Couldn't delete the node")
        }
    }

    // ---- what each group shows while reading and while editing ----
    const isEditing = (g: GroupId) => !!editing[g]
    const textInput = (f: Field, id: string, { className, ...props }: React.ComponentProps<"input"> = {}) => (
        <Input id={id} {...form.register(f)} aria-invalid={!!fieldError(f)} className={cn("h-9 max-w-[360px]", dirtyCls(f), className)} {...props} />
    )
    const numberInput = (f: Field, id: string, extra: { disabled?: boolean } = {}) => (
        <Controller control={form.control} name={f} render={({ field }) => (
            <Input
                id={id} type="number" inputMode="numeric" name={field.name} ref={field.ref} onBlur={field.onBlur}
                value={field.value as number} onChange={e => field.onChange(e.target.value === "" ? Number.NaN : Number(e.target.value))}
                aria-invalid={!!fieldError(f)} disabled={extra.disabled}
                className={cn("h-9 max-w-[160px] font-mono", dirtyCls(f))}
            />
        )} />
    )
    const switchInput = (f: Field, id: string, label: string) => (
        <Controller control={form.control} name={f} render={({ field }) => (
            <Switch id={id} ref={field.ref} checked={!!field.value} onCheckedChange={field.onChange} aria-label={label} />
        )} />
    )

    const sections: SettingsSectionMeta[] = [
        { id: "general", title: "General", icon: SlidersHorizontal, dirty: [...dirtyIn("identity"), ...dirtyIn("availability")].length > 0 },
        { id: "connection", title: "Connection", icon: Plug, dirty: [...dirtyIn("agent"), ...dirtyIn("ssh")].length > 0 },
        { id: "logging", title: "Logging", icon: ScrollText, dirty: dirtyIn("logging").length > 0 },
        { id: "monitoring", title: "Monitoring", icon: Activity, dirty: [...dirtyIn("starlink"), ...dirtyIn("bandwidth")].length > 0 },
        // the router's Wipe and Nuke delete the panel too, so they live in the panel's Settings
        ...(local ? [] : [{ id: "danger", title: "Danger zone", icon: TriangleAlert, danger: true }]),
    ]

    // ---- groups ----
    const identity = (
        <SettingsGroup
            id="identity" icon={IdCard} title="Identity" description="How this node shows up in the panel and in users' apps."
            edit={edit("identity")}
            footer={footer("identity", dirtyIn("identity").some(f => f === "name" || f === "country_code") ? { text: "Users see it after their app refreshes" } : null)}
        >
            <SettingRow label="Name" htmlFor={isEditing("identity") ? "node-name" : undefined} error={isEditing("identity") ? fieldError("name") : undefined}>
                {isEditing("identity") ? textInput("name", "node-name") : <Value>{saved.name}</Value>}
            </SettingRow>
            <SettingRow
                label="Address"
                hint={local ? "This router." : "Users' links use it unless an inbound sets its own."}
                action={!local && (
                    <Button type="button" variant="outline" size="sm" className="h-8" disabled={isEditing("identity")} onClick={() => setAddrOpen(true)}>Change</Button>
                )}
            >
                <Value mono>{node.ip}</Value>
            </SettingRow>
            <SettingRow label="Country" htmlFor={isEditing("identity") ? "node-country" : undefined} error={isEditing("identity") ? fieldError("country_code") : undefined}
                hint={isEditing("identity") ? "The flag in users' apps and on their subscription page." : undefined}>
                {isEditing("identity")
                    ? <Controller control={form.control} name="country_code" render={({ field }) => (
                        <CountryPicker id="node-country" value={field.value ?? ""} onChange={field.onChange} blank="Detect from the address" noFlag dirty={dirtyFields.includes("country_code")} />
                    )} />
                    : <CountryLabel code={saved.country_code} blank="Not set" />}
            </SettingRow>
            <SettingRow label="Datacenter" htmlFor={isEditing("identity") ? "node-datacenter" : undefined} hint={isEditing("identity") ? "Only shown in the panel." : undefined}>
                {isEditing("identity") ? textInput("datacenter", "node-datacenter", { placeholder: "Hetzner, DigitalOcean…" }) : <Value>{saved.datacenter}</Value>}
            </SettingRow>
            <SettingRow label="Node"><Value mono>#{node.id}</Value></SettingRow>
        </SettingsGroup>
    )

    const maintOn = isEditing("availability") ? draft.maintenance_mode : saved.maintenance_mode
    const availability = (
        <SettingsGroup
            id="availability" icon={Power} title="Availability"
            description={local ? "The notice users of this router see." : "Whether the node is offered, and the notice users see."}
            tone={!local && !saved.is_active ? "danger" : saved.maintenance_mode ? "warn" : "ok"}
            edit={edit("availability")}
            footer={footer("availability", dirtyIn("availability").includes("is_active")
                ? (draft.is_active ? { text: "Its users can connect again" } : { text: "Its users are cut off right away", danger: true })
                : dirtyIn("availability").includes("maintenance_mode") ? { text: draft.maintenance_mode ? "Users see the notice right away" : "The notice goes away" } : null)}
        >
            {!local && (
                <SettingRow label="In service" hint={isEditing("availability")
                    ? (draft.is_active ? "Users can connect, and the panel checks and manages it." : "Nobody can connect and its links are hidden. Xray keeps running.")
                    : saved.is_active ? undefined : "Nobody can connect and its links are hidden."}>
                    {isEditing("availability") ? switchInput("is_active", "node-active", "In service") : <OnOff on={saved.is_active} />}
                </SettingRow>
            )}
            <SettingRow
                label="Maintenance notice"
                hint={isEditing("availability")
                    ? "A banner on users' subscription page, and purchases pause. Traffic is untouched."
                    : saved.maintenance_mode && node.maintenance_since ? `On since ${new Date(node.maintenance_since).toLocaleString()}` : undefined}
            >
                {isEditing("availability") ? switchInput("maintenance_mode", "node-maintenance", "Maintenance notice") : <OnOff on={saved.maintenance_mode} />}
            </SettingRow>
            {maintOn && (
                <SettingRow label="Notice" htmlFor={isEditing("availability") ? "node-notice" : undefined}
                    error={isEditing("availability") ? fieldError("maintenance_message") : undefined}
                    hint={isEditing("availability") ? "Leave it empty for the default notice, in the user's language." : undefined}>
                    {isEditing("availability")
                        ? <Textarea id="node-notice" dir="auto" rows={2} placeholder="Back in about an hour." {...form.register("maintenance_message")} className={cn("min-h-16", dirtyCls("maintenance_message"))} />
                        : <Value empty="Default notice">{saved.maintenance_message}</Value>}
                </SettingRow>
            )}
        </SettingsGroup>
    )

    const agent = !local && (
        <SettingsGroup
            id="agent" icon={Cpu} title="Agent" description="How the panel reaches this node."
            edit={edit("agent", online ? undefined : OFFLINE)}
            advanced={{
                count: 1,
                held: dirtyIn("agent").includes("api_port") || !!fieldError("api_port"),
                openWhenEditing: true,
                children: (
                    <SettingRow label="Xray API port" htmlFor={isEditing("agent") ? "node-api-port" : undefined}
                        error={isEditing("agent") ? fieldError("api_port") : undefined} hint="Local to the node.">
                        {isEditing("agent") ? numberInput("api_port", "node-api-port") : <Value mono>{saved.api_port}</Value>}
                    </SettingRow>
                ),
            }}
            footer={footer("agent", dirtyIn("agent").includes("api_port") ? { text: "Saving restarts Xray" } : null)}
        >
            <SettingRow
                label="Connection mode"
                hint={direct ? "The panel connects to the agent." : "The agent connects to the panel, so the node needs no open port."}
                action={
                    <span title={!online ? OFFLINE : isEditing("agent") ? "Save or cancel your edits first" : undefined}>
                        <Button type="button" variant="outline" size="sm" className="h-8" disabled={!online || isEditing("agent")} onClick={() => setModeOpen(true)}>
                            Switch to {direct ? "reverse" : "direct"}
                        </Button>
                    </span>
                }
            >
                <Value>{direct ? "Direct" : "Reverse"}</Value>
            </SettingRow>
            {direct && (
                <SettingRow
                    label="Agent port"
                    hint={online ? undefined : OFFLINE}
                    action={
                        <span title={!online ? OFFLINE : isEditing("agent") ? "Save or cancel your edits first" : undefined}>
                            <Button type="button" variant="outline" size="sm" className="h-8" disabled={!online || isEditing("agent")} onClick={() => setPortOpen(true)}>Change</Button>
                        </span>
                    }
                >
                    <Value mono>{node.agent_port}</Value>
                </SettingRow>
            )}
        </SettingsGroup>
    )

    let sshUnavailable: ReactNode = null
    if (!local && !online) sshUnavailable = "Can't read SSH while the node is offline. It loads again when the node is back."
    else if (stealth) sshUnavailable = "SSH configuration is not available for stealth nodes."
    else if (settings.sshError || (!settings.sshStatus && !settings.sshLoading)) sshUnavailable = (
        <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
            {settings.sshError || "Couldn't read SSH from the node."}
            <Button type="button" variant="outline" size="sm" className="h-7" onClick={() => void settings.fetchSSHStatus()}>Retry SSH status</Button>
        </span>
    )
    else if (!settings.sshStatus) sshUnavailable = "Reading SSH from the node…"
    const ssh = (
        <SettingsGroup
            id="ssh" icon={SquareTerminal} title={local ? "SSH on this router" : "SSH"}
            description={local ? "The router's own SSH server." : "The SSH server on the node."}
            tone={!sshUnavailable && saved.ssh_enabled ? "ok" : undefined}
            unavailable={sshUnavailable}
            action={!sshUnavailable && !isEditing("ssh") && (
                <Button type="button" variant="ghost" size="icon" className="size-8 text-muted-foreground" onClick={() => void settings.fetchSSHStatus()} disabled={settings.sshLoading} aria-label="Refresh SSH status">
                    <RefreshCw className={cn("size-3.5", settings.sshLoading && "animate-spin")} />
                </Button>
            )}
            edit={edit("ssh", sshUnavailable ? (online ? "SSH isn't available right now" : OFFLINE) : undefined)}
            footer={footer("ssh", dirtyIn("ssh").includes("ssh_enabled") && !draft.ssh_enabled
                ? { text: "Saving stops SSH on the server" }
                : dirtyIn("ssh").length ? { text: "Saving restarts SSH" } : null)}
        >
            <SettingRow label="SSH server" hint={isEditing("ssh") ? (draft.ssh_enabled ? "Running. Turning it off stops and disables sshd." : "Stopped. Turn it on to start sshd again.") : undefined}>
                {isEditing("ssh") ? switchInput("ssh_enabled", "node-ssh", "SSH server") : <OnOff on={saved.ssh_enabled} onText="Running" offText="Stopped" />}
            </SettingRow>
            <SettingRow label="SSH port" htmlFor={isEditing("ssh") ? "node-ssh-port" : undefined}
                error={isEditing("ssh") ? fieldError("ssh_port") : undefined}
                hint={isEditing("ssh") ? "Allow a new port in any firewall first." : undefined}>
                {isEditing("ssh") ? numberInput("ssh_port", "node-ssh-port", { disabled: !draft.ssh_enabled }) : <Value mono>{saved.ssh_port}</Value>}
            </SettingRow>
        </SettingsGroup>
    )

    let logUnavailable: ReactNode = null
    if (!online) logUnavailable = "Can't read Xray's settings while the node is offline. They load again when the node is back."
    else if (settings.xrayError || (!settings.fullXrayConfig && !settings.xrayLoading)) logUnavailable = (
        <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
            {settings.xrayError || "Couldn't read Xray's settings."}
            <Button type="button" variant="outline" size="sm" className="h-7" onClick={() => void settings.fetchXrayConfig()}>Retry Xray settings</Button>
        </span>
    )
    else if (!settings.fullXrayConfig) logUnavailable = "Reading Xray's settings…"
    const levelLabel = (v: string) => LEVELS.find(l => l[0] === v)?.[1] ?? v
    const logging = (
        <SettingsGroup
            id="logging" icon={FileText} title="Xray logging" description="What Xray writes down, and per-user destinations for Access Logs."
            unavailable={logUnavailable}
            edit={edit("logging", logUnavailable ? (online ? "Xray's settings haven't loaded" : OFFLINE) : undefined)}
            advanced={{
                count: 2,
                held: ["log_access", "log_error"].some(f => dirtyIn("logging").includes(f as Field)),
                children: (<>
                    <SettingRow label="Access log file" htmlFor={isEditing("logging") ? "node-log-access" : undefined} hint={isEditing("logging") ? "Empty stops writing this file." : undefined}>
                        {isEditing("logging") ? textInput("log_access", "node-log-access", { className: "font-mono", placeholder: "/var/log/xray/access.log" }) : <Value mono empty="Not written">{saved.log_access}</Value>}
                    </SettingRow>
                    <SettingRow label="Error log file" htmlFor={isEditing("logging") ? "node-log-error" : undefined} hint={isEditing("logging") ? "Empty stops writing this file." : undefined}>
                        {isEditing("logging") ? textInput("log_error", "node-log-error", { className: "font-mono", placeholder: "/var/log/xray/error.log" }) : <Value mono empty="Not written">{saved.log_error}</Value>}
                    </SettingRow>
                </>),
            }}
            footer={footer("logging", dirtyIn("logging").some(f => f !== "enable_access_log") ? { text: "Saving restarts Xray" } : null)}
        >
            <SettingRow label="Log level" htmlFor={isEditing("logging") ? "node-log-level" : undefined} hint={isEditing("logging") ? "Warning suits most nodes." : undefined}>
                {isEditing("logging")
                    ? <Controller control={form.control} name="loglevel" render={({ field }) => (
                        <Select value={field.value} onValueChange={field.onChange}>
                            <SelectTrigger id="node-log-level" aria-label="Log level" className={cn("h-9 w-44", dirtyCls("loglevel"))}><SelectValue /></SelectTrigger>
                            <SelectContent>{LEVELS.map(([v, t]) => <SelectItem key={v} value={v}>{t}</SelectItem>)}</SelectContent>
                        </Select>
                    )} />
                    : <Value>{levelLabel(saved.loglevel)}</Value>}
            </SettingRow>
            <SettingRow label="DNS logging" hint={isEditing("logging") ? "Log every DNS lookup Xray makes." : undefined}>
                {isEditing("logging") ? switchInput("dnsLog", "node-dns-log", "DNS logging") : <OnOff on={saved.dnsLog} />}
            </SettingRow>
            <SettingRow label="Access-log capture" hint={isEditing("logging") ? "Per-user destinations on the Access Logs tab." : undefined}>
                {isEditing("logging") ? switchInput("enable_access_log", "node-capture", "Access-log capture") : <OnOff on={saved.enable_access_log} />}
            </SettingRow>
        </SettingsGroup>
    )

    const starlinkOn = isEditing("starlink") ? draft.starlink_enabled : saved.starlink_enabled
    const starlink = (
        <SettingsGroup
            id="starlink" icon={SatelliteDish} title="Starlink" description="Read dish stats from a Starlink terminal on this network."
            tone={saved.starlink_enabled && !stealth ? "ok" : undefined}
            unavailable={stealth ? "Starlink monitoring is not available for stealth nodes." : undefined}
            edit={edit("starlink", stealth ? "Not available on this node" : undefined)}
            footer={footer("starlink")}
        >
            <SettingRow label="Starlink monitoring" hint={isEditing("starlink") ? "Adds a Starlink tab with dish health." : undefined}>
                {isEditing("starlink") ? switchInput("starlink_enabled", "node-starlink", "Starlink monitoring") : <OnOff on={saved.starlink_enabled} />}
            </SettingRow>
            {starlinkOn && (
                <SettingRow label="Dish address" htmlFor={isEditing("starlink") ? "node-dish" : undefined} error={isEditing("starlink") ? fieldError("starlink_dish_address") : undefined}>
                    {isEditing("starlink") ? textInput("starlink_dish_address", "node-dish", { className: "font-mono", placeholder: "192.168.100.1:9200" }) : <Value mono>{saved.starlink_dish_address}</Value>}
                </SettingRow>
            )}
        </SettingsGroup>
    )

    const bandwidthDirty = dirtyIn("bandwidth").length
    const bandwidth = !local && (
        <div className="space-y-0">
            <Form {...form}><NodeSettingsBandwidth settingsForm={settings} isStealth={stealth} /></Form>
            {bandwidthDirty > 0 && (
                <div className="-mt-px overflow-hidden rounded-b-xl border border-border/60">
                    <GroupFooter dirty={bandwidthDirty} error={errors.bandwidth} saving={settings.isSaving}
                        onCancel={() => cancel("bandwidth")} onSave={() => void save("bandwidth")} />
                </div>
            )}
        </div>
    )

    return (
        <>
            <SettingsPage sections={sections}>
                <SettingsSection id="general" title="General" icon={SlidersHorizontal}>{identity}{availability}</SettingsSection>
                <SettingsSection id="connection" title="Connection" icon={Plug}>{agent}{ssh}</SettingsSection>
                <SettingsSection id="logging" title="Logging" icon={ScrollText}>{logging}</SettingsSection>
                <SettingsSection id="monitoring" title="Monitoring" icon={Activity}>{starlink}{bandwidth}</SettingsSection>
                {local ? (
                    <p className="text-xs text-muted-foreground">
                        Wiping or nuking this router deletes the panel too, so it's in{" "}
                        <Link to="/settings?category=danger" className="font-medium text-foreground underline underline-offset-2">Settings → Danger zone</Link>.
                    </p>
                ) : (
                    <SettingsSection id="danger" title="Danger zone" icon={TriangleAlert} hideTitle>
                        <DangerZone nodeId={node.id} nodeName={node.name} onDelete={handleDelete}
                            deleteLabel={forceDelete ? "Force delete node" : "Delete node"} online={online} />
                    </SettingsSection>
                )}
            </SettingsPage>

            <ConnectModeDialog node={node} open={modeOpen} onOpenChange={setModeOpen} onSwitched={onRefresh} />
            <AgentPortDialog node={node} open={portOpen} onOpenChange={setPortOpen} onMoved={onRefresh} />
            {!local && <AddressChange node={node} open={addrOpen} onOpenChange={setAddrOpen} onMoved={onRefresh} />}

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
