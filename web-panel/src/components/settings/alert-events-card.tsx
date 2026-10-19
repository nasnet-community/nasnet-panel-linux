import { Fragment } from "react"
import { Checkbox } from "@/components/ui/checkbox"
import { SettingRow, SettingsGroup } from "@/components/settings-page/settings-group"
import { CHANNEL_LABEL, EVENTS, eventKey, liveChannels, on, type CardDef, type Channel } from "./catalog"
import { GotoLink, Hl, useCardEdit, usePage } from "./settings-card"

const EVENT_COUNT = EVENTS.reduce((n, g) => n + g.items.length, 0)

const ALL: Channel[] = ["telegram", "discord", "webhook"]

/** Reads as the events each channel sends; Edit shows the grid. */
export function AlertEventsCard({ card }: { card: CardDef }) {
    const { ctx } = usePage()
    const edit = useCardEdit(card)
    const live = liveChannels(ctx.values)
    const values = edit.editing ? edit.draft : ctx.values
    const effect = edit.changed.length ? card.effect?.(edit.draft, ctx.values, edit.changed, ctx) : null

    return (
        <SettingsGroup
            id={card.id} icon={card.icon} title={card.title} description={card.description}
            edit={{ editing: edit.editing, onEdit: edit.start }}
            footer={edit.footer(effect)}
        >
            {!edit.editing && ALL.map(ch => {
                if (!live.includes(ch)) {
                    return (
                        <SettingRow key={ch} label={CHANNEL_LABEL[ch]} action={<GotoLink cardId="alert-channels" label="Add a URL" />}>
                            <span className="text-muted-foreground">{ch === "discord" ? "No webhook URL" : "No URL"}</span>
                        </SettingRow>
                    )
                }
                const picked = EVENTS.flatMap(g => g.items.filter(([ev]) => on(values[eventKey(ch, ev)])))
                return (
                    <SettingRow key={ch} label={CHANNEL_LABEL[ch]}>
                        <span className="flex flex-col gap-1.5 py-1">
                            <span>{picked.length} of {EVENT_COUNT} events</span>
                            {picked.length > 0 && (
                                <span className="flex flex-wrap gap-1.5">
                                    {picked.map(([ev, label]) => <span key={ev} className="rounded-full border border-border px-2.5 py-0.5 text-xs"><Hl text={label} /></span>)}
                                </span>
                            )}
                        </span>
                    </SettingRow>
                )
            })}
            {edit.editing && (
                <div className="px-4 py-2">
                    <div className="grid items-center" style={{ gridTemplateColumns: `minmax(0,1fr) repeat(${live.length}, 84px)` }}>
                        <span className="py-1.5 text-xs text-muted-foreground">Event</span>
                        {live.map(ch => <span key={ch} className="py-1.5 text-center text-xs text-muted-foreground">{CHANNEL_LABEL[ch]}</span>)}
                        {EVENTS.map(g => (
                            <Fragment key={g.group}>
                                <span className="col-span-full mt-1 border-t border-border/40 pb-1 pt-2.5 text-[12px] font-semibold uppercase tracking-wider text-muted-foreground">{g.group}</span>
                                {g.items.map(([ev, label]) => (
                                    <Fragment key={ev}>
                                        <span className="py-1.5 text-sm">{label}</span>
                                        {live.map(ch => {
                                            const key = eventKey(ch, ev)
                                            return (
                                                <span key={ch} className="flex justify-center py-1.5">
                                                    <Checkbox
                                                        aria-label={`${label} on ${CHANNEL_LABEL[ch]}`}
                                                        checked={on(values[key])}
                                                        onCheckedChange={c => edit.set(key, c === true ? "true" : "false")}
                                                    />
                                                </span>
                                            )
                                        })}
                                    </Fragment>
                                ))}
                            </Fragment>
                        ))}
                    </div>
                    {live.length < 3 && (
                        <p className="mt-2 border-t border-border/40 pt-2.5 text-xs text-muted-foreground">
                            {ALL.filter(ch => !live.includes(ch)).map(ch => CHANNEL_LABEL[ch]).join(" and ")} {live.length === 1 ? "get columns" : "gets a column"} here once {live.length === 1 ? "they have" : "it has"} a URL in Alert channels.
                        </p>
                    )}
                </div>
            )}
        </SettingsGroup>
    )
}
