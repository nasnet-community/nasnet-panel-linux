import { Fragment } from "react"
import { Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { useRetentionStats, useRunRetentionCleanup } from "@/lib/queries/use-settings"
import { SettingsGroup } from "@/components/settings-page/settings-group"
import { cn, formatBytes } from "@/lib/utils"
import { RETENTION, day, type CardDef } from "./catalog"
import { Hl, useCardEdit, usePage } from "./settings-card"

const compact = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 })

/** Datasets with rows older than their window: the cleanup should have taken them. */
function pastWindow(keepDays: number, oldest?: string | null) {
    if (!keepDays || !oldest) return false
    return Date.now() - new Date(oldest).getTime() > (keepDays + 1) * 86_400_000
}

export function RetentionCard({ card }: { card: CardDef }) {
    const { ctx } = usePage()
    const confirm = useConfirm()
    const { data: stats } = useRetentionStats()
    const cleanup = useRunRetentionCleanup()
    const edit = useCardEdit(card, {
        validate: d => Object.fromEntries(RETENTION.flatMap(g => g.rows.map(([k]) => {
            const v = d[k] ?? ""
            return [k, /^\d+$/.test(v.trim()) ? "" : "Days, 0 keeps forever"]
        }))),
    })
    const byKey = new Map((stats ?? []).map(s => [s.setting_key, s]))
    const values = edit.editing ? edit.draft : ctx.values
    const effect = edit.changed.length ? card.effect?.(edit.draft, ctx.values, edit.changed, ctx) : null
    const dbSize = ctx.status?.database.size_bytes
    const hasSizes = (stats ?? []).some(s => s.size_bytes != null)

    const stale = RETENTION.flatMap(g => g.rows.filter(([k]) => pastWindow(Number(ctx.values[k]), byKey.get(k)?.oldest_at)).map(r => r[1]))
    const runCleanup = async () => {
        if (!await confirm({
            title: "Run the cleanup now?",
            description: (
                <span className="block space-y-2">
                    <span className="block">It deletes rows older than each dataset's window, the same as the scheduled cleanup every 6 hours. Deleted rows can't be brought back.</span>
                    {stale.length > 0 && <span className="block">Past their window now: {stale.join(", ")}.</span>}
                </span>
            ),
            confirmLabel: "Run cleanup",
            variant: "destructive",
        })) return
        cleanup.mutate()
    }

    return (
        <SettingsGroup
            id={card.id} icon={card.icon} title={card.title} description={card.description}
            edit={{ editing: edit.editing, onEdit: edit.start }}
            footer={edit.footer(effect)}
        >
            <div className="overflow-x-auto">
                <table className="w-full text-sm">
                    <thead>
                        <tr className="text-left text-xs text-muted-foreground">
                            <th className="px-4 py-2 font-medium">Dataset</th>
                            <th className="px-4 py-2 font-medium">Keep</th>
                            <th className="px-4 py-2 text-right font-medium @max-[559px]:hidden">Rows</th>
                            {hasSizes && <th className="px-4 py-2 text-right font-medium">Size</th>}
                            <th className="px-4 py-2 text-right font-medium @max-[559px]:hidden">Oldest</th>
                        </tr>
                    </thead>
                    <tbody>
                        {RETENTION.map(g => (
                            <Fragment key={g.group}>
                                <tr><td colSpan={5} className="border-t border-border/40 bg-muted/20 px-4 py-1.5 text-[12px] font-semibold uppercase tracking-wider text-muted-foreground">{g.group}</td></tr>
                                {g.rows.map(([key, name]) => {
                                    const st = byKey.get(key)
                                    const keep = Number(values[key] ?? 0)
                                    const late = !edit.editing && pastWindow(keep, st?.oldest_at)
                                    return (
                                        <tr key={key} className="border-t border-border/40">
                                            <td className="px-4 py-2 align-middle">
                                                <Hl text={name} />
                                                {late && <span className="block text-xs text-amber-600 dark:text-amber-500">Older rows weren't cleaned up</span>}
                                            </td>
                                            <td className="whitespace-nowrap px-4 py-1.5 align-middle">
                                                {edit.editing ? (
                                                    <span className="flex items-center gap-2">
                                                        <Input
                                                            type="number" min={0} inputMode="numeric" aria-label={`${name}: days to keep`}
                                                            aria-invalid={!!edit.errors[key]}
                                                            value={values[key] ?? ""}
                                                            onChange={e => edit.set(key, e.target.value)}
                                                            className={cn("h-8 w-20 font-mono text-[13px]", edit.changed.includes(key) && "border-amber-500/60")}
                                                        />
                                                        <span className="text-xs text-muted-foreground">days</span>
                                                    </span>
                                                ) : keep === 0
                                                    ? <span className="rounded-full border border-border px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Forever</span>
                                                    : `${keep} ${keep === 1 ? "day" : "days"}`}
                                            </td>
                                            <td className="px-4 py-2 text-right align-middle font-mono text-[13px] @max-[559px]:hidden">{st ? compact.format(st.rows) : "—"}</td>
                                            {hasSizes && <td className="whitespace-nowrap px-4 py-2 text-right align-middle font-mono text-[13px]">{st?.size_bytes != null ? formatBytes(st.size_bytes) : "—"}</td>}
                                            <td className="whitespace-nowrap px-4 py-2 text-right align-middle font-mono text-[13px] @max-[559px]:hidden">{st?.oldest_at ? day(st.oldest_at) : "—"}</td>
                                        </tr>
                                    )
                                })}
                            </Fragment>
                        ))}
                    </tbody>
                </table>
            </div>
            {!edit.editing && (
                <div className="flex flex-wrap items-center gap-3 border-t border-border/40 px-4 py-2.5 text-xs text-muted-foreground">
                    <span className="mr-auto">{dbSize ? `Database ${formatBytes(dbSize)} · ` : ""}Cleanup runs every 6 hours</span>
                    <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" onClick={runCleanup} disabled={cleanup.isPending}>
                        {cleanup.isPending && <Loader2 className="size-3.5 animate-spin" />}Run cleanup now…
                    </Button>
                </div>
            )}
            {edit.editing && <p className="border-t border-border/40 px-4 py-2 text-xs text-muted-foreground">Days. 0 keeps forever.</p>}
        </SettingsGroup>
    )
}
