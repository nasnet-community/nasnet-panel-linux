import { useState, type ReactNode } from "react"
import { Link, useNavigate } from "react-router"
import { ArrowUpRight, CloudDownload, Loader2, RefreshCw, RotateCcw, Upload } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { SettingRow, SettingsGroup, StateBadge } from "@/components/settings-page/settings-group"
import type { NodeXray, UpdateStatus } from "@/lib/api/updates"
import { useCancelSchedule, useCheckUpdates, useRollback, useUpdates } from "@/lib/queries/use-updates"
import { dateTime, isPrerelease, mb, panelLine, plainNotes, routeLabel, took, weekdayTime } from "@/lib/updates-state"
import { useNodesStore } from "@/store/nodes-store"
import { day, since, type CardDef, type StatusLine } from "./catalog"
import { CopyValue, StatusRow } from "./settings-card"
import { UpdateDialog, UpdateProgressDialog, UploadDialog } from "./update-dialogs"

const RELEASES = "https://github.com/nasnet-community/nasnet-panel-linux/releases"

function Version({ v }: { v: string }) {
    return (
        <span className="inline-flex items-center gap-2">
            <span className="font-mono text-[13px]">{v}</span>
            {isPrerelease(v) && <StateBadge tone="muted">beta</StateBadge>}
        </span>
    )
}

function Loading({ card }: { card: CardDef }) {
    const { isError } = useUpdates()
    return (
        <SettingsGroup id={card.id} icon={card.icon} title={card.title} description={card.description}>
            <SettingRow label="Status">
                <span className="text-muted-foreground">{isError ? "Couldn't load the update status." : "Loading…"}</span>
            </SettingRow>
        </SettingsGroup>
    )
}

/** The rows that differ by install kind: a command to run instead of a button. */
function commandRow(st: UpdateStatus) {
    const k = st.kind
    if (k.name === "systemd" || !k.command) return null
    const label = k.name === "docker" ? "On the host" : k.name === "not_root" ? "Once, from the CLI" : "From the CLI"
    const hint = k.name === "docker" ? "Run it in the folder with docker-compose.yml."
        : k.name === "not_root" ? "Choose Auto-Update. It also switches the install to root."
            : "Choose Update. It rebuilds from git."
    return (
        <SettingRow label={label} hint={hint} action={<CopyValue text={k.command} />}>
            <span className="font-mono text-[13px] [overflow-wrap:anywhere]">{k.command}</span>
        </SettingRow>
    )
}

export function UpdatePanelCard({ card }: { card: CardDef }) {
    const { data: st } = useUpdates()
    const check = useCheckUpdates()
    const rollback = useRollback()
    const cancel = useCancelSchedule()
    const confirm = useConfirm()
    const [dialog, setDialog] = useState<"update" | "upload" | null>(null)
    const [jobId, setJobId] = useState<string | null>(null)

    if (!st) return <Loading card={card} />
    const line = panelLine(st)
    const job = st.job && st.job.phase !== "done" ? st.job : undefined
    const offer = st.available
    const systemd = st.kind.name === "systemd"
    const status: StatusLine = { tone: line.tone, text: line.text, hint: line.hint }
    const step = job?.steps.find(s => s.status === "running")
    const follow = (id: string) => setJobId(id)

    const runRollback = async () => {
        if (!st.rollback) return
        if (!await confirm({
            title: `Roll back to ${st.rollback.version}?`,
            description: `The panel restarts on ${st.rollback.version}, with its node agents, Xray and system files. The database stays as it is; ${st.rollback.version} ignores what ${st.current} added. The panel, the bot, subscription pages and this router's Xray are down for about a minute.`,
            confirmLabel: "Roll back",
            variant: "warning",
        })) return
        rollback.mutate(undefined, { onSuccess: follow })
    }
    const runCancel = async () => {
        if (!st.schedule) return
        if (!await confirm({
            title: "Cancel the scheduled update?",
            description: `${st.schedule.version} won't install at ${weekdayTime(st.schedule.at)}. You can update or schedule it again any time.`,
            confirmLabel: "Cancel update",
            cancelLabel: "Keep it",
        })) return
        cancel.mutate()
    }

    const notice = st.net_pending && systemd && offer
        ? <span className="flex flex-wrap items-center gap-x-2 gap-y-1">Updates wait while a network change is armed. Keep or revert it first.
            <Link to="/router" className="inline-flex items-center gap-1 text-xs font-medium text-foreground hover:underline hover:underline-offset-4">Open Router<ArrowUpRight className="size-3" aria-hidden /></Link></span>
        : st.kind.name === "not_root"
            ? "This install runs the panel as a normal user. Update it once with sudo nasnet → Auto-Update, which also switches it to root. After that, updates run from here."
            : undefined

    const lastCheck = st.check
    const checkHint = lastCheck?.error ? `Failed: ${lastCheck.error}` : lastCheck ? `Through ${routeLabel(lastCheck.route, st.mirror_url)}` : undefined

    return (
        <>
            <SettingsGroup
                id={card.id} icon={card.icon} title={card.title}
                description={st.check_enabled ? card.description : "Automatic checks are off"}
                tone={line.tone === "off" ? undefined : line.tone}
                notice={notice}
            >
                <StatusRow line={status} />
                {job && (
                    <SettingRow
                        label="Progress"
                        hint={job.route ? `Through ${routeLabel(job.route, st.mirror_url)}` : undefined}
                        action={<Button type="button" variant="outline" size="sm" className="h-8" onClick={() => follow(job.id)}>Show progress</Button>}
                    >
                        <span className="inline-flex items-center gap-2">
                            <Loader2 className="size-3.5 animate-spin text-blue-500" aria-hidden />
                            {step ? `${step.label}${step.detail ? ` · ${step.detail}` : ""}` : "Working…"}
                        </span>
                    </SettingRow>
                )}
                <SettingRow label="Running" hint={st.kind.name === "docker" ? "In Docker" : st.built_at ? `Built ${day(st.built_at)}` : undefined}>
                    <Version v={st.current} />
                </SettingRow>
                {st.rollback && !job && (
                    <SettingRow
                        label="Previous"
                        hint={`You can go back until ${day(st.rollback.until)}. The database stays as it is.`}
                        action={<Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" onClick={runRollback} disabled={rollback.isPending}><RotateCcw className="size-3.5" />Roll back…</Button>}
                    >
                        <Version v={st.rollback.version} />
                    </SettingRow>
                )}
                {offer && !job && (
                    <SettingRow
                        label="Available"
                        hint={`Released ${day(offer.built_at)}${systemd ? ` · ${mb(offer.size)} to download` : ""}`}
                        action={systemd && (
                            <Button
                                type="button" size="sm" variant={line.state === "failed" ? "outline" : "default"} className="h-8 gap-1.5"
                                onClick={() => setDialog("update")} disabled={!!st.net_pending}
                            >
                                <CloudDownload className="size-3.5" />{line.state === "failed" ? "Try again…" : st.schedule ? "Update now…" : "Update…"}
                            </Button>
                        )}
                    >
                        <span className="font-semibold"><Version v={offer.version} /></span>
                    </SettingRow>
                )}
                {st.schedule && !job && (
                    <SettingRow
                        label="Scheduled"
                        hint="Your time. If the panel is down then, it skips the update and tells you."
                        action={<Button type="button" variant="outline" size="sm" className="h-8" onClick={runCancel} disabled={cancel.isPending}>Cancel…</Button>}
                    >
                        {weekdayTime(st.schedule.at)}
                    </SettingRow>
                )}
                {commandRow(st)}
                <SettingRow
                    label="Last check"
                    hint={checkHint}
                    action={
                        <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" onClick={() => check.mutate()} disabled={check.isPending || !!job}>
                            {check.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}Check now
                        </Button>
                    }
                >
                    {lastCheck ? `${since(lastCheck.checked_at)} ago` : <span className="text-muted-foreground">Never</span>}
                </SettingRow>
                {systemd && !job && (
                    <SettingRow
                        label="Offline"
                        hint="For a server that can't reach GitHub. Checked before anything installs."
                        action={<Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" onClick={() => setDialog("upload")}><Upload className="size-3.5" />Upload…</Button>}
                    >
                        Upload the update file
                    </SettingRow>
                )}
            </SettingsGroup>
            {offer && (
                <UpdateDialog open={dialog === "update"} onOpenChange={o => setDialog(o ? "update" : null)} status={st} offer={offer} onStarted={follow} />
            )}
            <UploadDialog open={dialog === "upload"} onOpenChange={o => setDialog(o ? "upload" : null)} status={st} arch={st.arch} onStarted={follow} />
            <UpdateProgressDialog open={!!jobId} onOpenChange={o => { if (!o) setJobId(null) }} jobId={jobId} />
        </>
    )
}

/** The notes of the release on offer, folded to their lines. */
export function UpdateNotesCard({ card }: { card: CardDef }) {
    const { data: st } = useUpdates()
    const offer = st?.available ?? (st?.check?.blocked ? st.check.latest : undefined)
    if (!offer) return null
    const lines = plainNotes(offer.notes)
    return (
        <SettingsGroup
            id={card.id} icon={card.icon} title={`What's new in ${offer.version}`} description={card.description}
            action={
                <Button asChild variant="ghost" size="sm" className="h-8 gap-1 px-2 text-muted-foreground hover:text-foreground">
                    <a href={`${RELEASES}/tag/${offer.version}`} target="_blank" rel="noreferrer">On GitHub<ArrowUpRight className="size-3.5" aria-hidden /></a>
                </Button>
            }
        >
            {lines.length
                ? <ul className="list-disc space-y-1 py-3 pl-9 pr-4 text-sm">{lines.map((l, i) => <li key={i}>{l}</li>)}</ul>
                : <p className="px-4 py-3 text-sm text-muted-foreground">This release has no notes.</p>}
        </SettingsGroup>
    )
}

function names(xs: NodeXray[]) {
    const n = xs.map(x => (x.local ? "the built-in node" : x.name))
    return n.length <= 2 ? n.join(" and ") : `${n.slice(0, 2).join(", ")} and ${n.length - 2} more`
}

/** What updates elsewhere. Every row is a link; nothing updates from here. */
export function UpdatePartsCard({ card }: { card: CardDef }) {
    const { data: st } = useUpdates()
    const navigate = useNavigate()
    const setSelected = useNodesStore(s => s.setSelected)
    if (!st) return null
    const { agents, xray, geofiles } = st.parts
    const open = (ids: number[]) => {
        setSelected(ids)
        navigate("/nodes")
    }
    const openLink = (ids: number[]) => (
        <button type="button" onClick={() => open(ids)} className="inline-flex items-center gap-1 text-xs font-medium text-foreground hover:underline hover:underline-offset-4">
            Open Nodes<ArrowUpRight className="size-3" aria-hidden />
        </button>
    )

    let agentLine: StatusLine = { tone: "off", text: "No remote nodes yet" }
    if (agents && agents.total > 0) {
        agentLine = agents.due.length
            ? { tone: "warn", text: `${agents.due.length} of ${agents.total} remote nodes need an update`, hint: "Their agent is older than the one this panel ships. Push it from Nodes." }
            : { tone: "ok", text: agents.total === 1 ? "The remote node is current" : `All ${agents.total} remote nodes are current`, hint: "They ship with the panel. Push them from Nodes." }
    }

    let xrayLine: StatusLine = { tone: "off", text: "No node has reported its Xray yet", hint: `Tested with ${st.tested_xray}.` }
    if (xray && (xray.current || xray.older.length || xray.newer.length)) {
        const newer = xray.newer.length
            ? ` ${names(xray.newer)} ${xray.newer.length === 1 ? `runs ${xray.newer[0].version}, which isn't` : "run newer versions, which aren't"} tested with this panel.`
            : ""
        if (xray.older.length) {
            xrayLine = { tone: "warn", text: `${xray.older.length === 1 ? "1 node runs" : `${xray.older.length} nodes run`} an older Xray`, hint: `Tested with ${xray.tested}.${newer}` }
        } else if (xray.newer.length) {
            xrayLine = { tone: "warn", text: `${xray.newer.length === 1 ? xray.newer[0].local ? "The built-in node runs" : `${xray.newer[0].name} runs` : `${xray.newer.length} nodes run`} a newer Xray`, hint: `${newer.trim()} This panel is tested with ${xray.tested}.` }
        } else {
            xrayLine = { tone: "ok", text: `All nodes run the tested ${xray.tested}` }
        }
    }

    return (
        <SettingsGroup id={card.id} icon={card.icon} title={card.title} description={card.description}>
            <PartRow label="Node agents" line={agentLine} action={agents?.due.length ? openLink(agents.due) : undefined} />
            <PartRow label="Xray" line={xrayLine} action={xray?.older.length ? openLink(xray.older.map(x => x.id)) : undefined} />
            <PartRow
                label="Geofiles"
                line={{ tone: "ok", text: geofiles.built_at ? `Built in, from ${day(geofiles.built_at)}` : "Built in", hint: "They update with the panel." }}
            />
        </SettingsGroup>
    )
}

function PartRow({ label, line, action }: { label: string; line: StatusLine; action?: ReactNode }) {
    const dot = { ok: "bg-emerald-500", warn: "bg-amber-500", danger: "bg-red-500", off: "bg-muted-foreground/60" }[line.tone]
    return (
        <SettingRow label={label} hint={line.hint} action={action}>
            <span className="inline-flex min-w-0 items-start gap-2">
                <span className={`mt-[7px] size-1.5 shrink-0 rounded-full ${dot}`} aria-hidden />
                <span className="[overflow-wrap:anywhere]">{line.text}</span>
            </span>
        </SettingRow>
    )
}

const RESULT = {
    ok: { tone: "ok", update: "Updated", rollback: "Rolled back" },
    rolled_back: { tone: "danger", update: "Rolled back", rollback: "Rolled back" },
    failed: { tone: "danger", update: "Failed", rollback: "Failed" },
} as const

export function UpdateHistoryCard({ card }: { card: CardDef }) {
    const { data: st } = useUpdates()
    if (!st) return null
    return (
        <SettingsGroup id={card.id} icon={card.icon} title={card.title} description={card.description}>
            {st.history.length === 0 && (
                <SettingRow label="">
                    <span className="text-muted-foreground">No updates from the panel yet.</span>
                </SettingRow>
            )}
            {st.history.map(h => {
                const r = RESULT[h.result] ?? RESULT.failed
                return (
                    <SettingRow
                        key={h.id}
                        label={dateTime(h.finished_at)}
                        hint={h.result === "ok" ? `Took ${took(h)}` : h.reason}
                        action={<StateBadge tone={r.tone}>{r[h.kind]}</StateBadge>}
                    >
                        <span className="font-mono text-[13px]">{h.from} → {h.to}</span>
                    </SettingRow>
                )
            })}
        </SettingsGroup>
    )
}
