import { useMemo } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { Checkbox } from "@/components/ui/checkbox"
import { setGlobalMaintenance } from "@/lib/api/maintenance"
import { queryKeys } from "@/lib/queries/keys"
import { OnOff, SettingRow, SettingsGroup } from "@/components/settings-page/settings-group"
import { on, type CardDef, type Effect, type Values } from "./catalog"
import { FieldRow, StatusRow, toneOf, useCardEdit, usePage } from "./settings-card"

/** Maintenance saves through its own endpoint, which also sends the notice. */
export function MaintenanceCard({ card, match = "all" }: { card: CardDef; match?: "all" | Set<string> }) {
    const { ctx } = usePage()
    const qc = useQueryClient()
    const wasOn = on(ctx.values.maintenance_mode_enabled)
    const reach = ctx.status?.maintenance.reach
    const extra = useMemo<Values>(() => ({ _notify: "true" }), [])

    const effectOf = (d: Values, changed: string[]): Effect | null => {
        if (!changed.length) return null
        const nowOn = on(d.maintenance_mode_enabled)
        if (nowOn && !wasOn) {
            const send = on(d._notify) && reach
                ? ` Sends the notice to ${reach} active ${reach === 1 ? "user" : "users"} on Telegram.`
                : ""
            return { danger: true, text: `Customers can't make changes until you turn it off.${send}` }
        }
        if (!nowOn && wasOn) return { text: "Customers can make changes again." }
        return { text: "Updates the notice. No new message is sent." }
    }

    const edit = useCardEdit(card, {
        extra,
        effect: effectOf,
        confirm: { title: "Turn maintenance on?", label: "Turn on" },
        // only a switch-on this page asked about may send the notice; the panel
        // decides from its own state, which a stale page can't overrule
        toPayload: d => ({
            maintenance_mode_enabled: d.maintenance_mode_enabled,
            maintenance_mode_message: d.maintenance_mode_message ?? "",
            _notify: on(d._notify) && on(d.maintenance_mode_enabled) && !wasOn ? "true" : "false",
        }),
        submit: async payload => {
            await setGlobalMaintenance({
                enabled: on(payload.maintenance_mode_enabled),
                message: payload.maintenance_mode_message,
                notify: on(payload._notify),
            })
            await qc.invalidateQueries({ queryKey: queryKeys.settings, exact: true })
            void qc.invalidateQueries({ queryKey: ["maintenance"] })
            void qc.invalidateQueries({ queryKey: queryKeys.settingsStatus() })
        },
    })

    const status = card.status?.(ctx)
    const source = edit.editing ? edit.draft : ctx.values
    const narrowed = match !== "all"
    const collapsed = !edit.editing && !narrowed && !wasOn
    const [enabled, message] = card.fields!
    const turningOn = edit.editing && on(edit.draft.maintenance_mode_enabled) && !wasOn

    return (
        <SettingsGroup
            id={card.id} icon={card.icon} title={card.title} description={card.description}
            tone={toneOf(status)}
            edit={{
                editing: edit.editing,
                // read the current state first: another admin or the bot may have switched it
                onEdit: () => { void qc.invalidateQueries({ queryKey: queryKeys.settings, exact: true }); edit.start() },
            }}
            footer={edit.footer(effectOf(edit.draft, edit.changed))}
        >
            {status && !narrowed && <StatusRow line={status} />}
            {collapsed
                ? <SettingRow label={card.master!.label} hint={card.master!.off}><OnOff on={false} /></SettingRow>
                : <>
                    <FieldRow card={card} f={enabled} edit={edit} />
                    {on(source.maintenance_mode_enabled) && <FieldRow card={card} f={message} edit={edit} />}
                    {turningOn && (
                        <SettingRow label="Telegram" hint={reach === 0 ? "No active user has Telegram." : undefined}>
                            <label className="flex items-center gap-2.5 text-sm">
                                <Checkbox checked={on(edit.draft._notify)} onCheckedChange={c => edit.set("_notify", c === true ? "true" : "false")} />
                                Send the notice to active users{reach ? ` (${reach})` : ""}
                            </label>
                        </SettingRow>
                    )}
                </>}
        </SettingsGroup>
    )
}
