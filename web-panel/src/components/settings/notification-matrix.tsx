import { Checkbox } from "@/components/ui/checkbox"
import { notificationChannels, notificationMatrixSections } from "./settings-constants"
import { cn } from "@/lib/utils"
import type { Setting } from "@/lib/domain/setting"

interface NotificationMatrixProps {
    settings: Setting[]
    serverSettings: Setting[] | undefined
    onSettingChange: (category: string, key: string, value: string | boolean) => void
}

export function NotificationMatrix({ settings, serverSettings, onSettingChange }: NotificationMatrixProps) {
    const getSettingKey = (channel: string, eventKey: string) =>
        `notification_${channel}_${eventKey}`

    const isChecked = (channel: string, eventKey: string) => {
        const key = getSettingKey(channel, eventKey)
        return settings.find(s => s.key === key)?.value === "true"
    }

    const isRowModified = (eventKey: string) => {
        if (!serverSettings) return false
        return notificationChannels.some(ch => {
            const key = getSettingKey(ch.key, eventKey)
            const current = settings.find(s => s.key === key)?.value
            const server = serverSettings.find(s => s.key === key)?.value
            return current !== server
        })
    }

    return (
        <div className="rounded-lg border bg-card/50 overflow-hidden">
            {/* Header row */}
            <div className="grid grid-cols-[1fr_repeat(3,64px)] sm:grid-cols-[1fr_repeat(3,80px)] items-center px-3 py-2 border-b bg-muted/30">
                <span className="text-xs font-medium text-muted-foreground">Event</span>
                {notificationChannels.map(ch => (
                    <div key={ch.key} className="flex items-center justify-center gap-1.5">
                        <ch.icon className="w-3.5 h-3.5 text-muted-foreground" />
                        <span className="text-xs font-medium text-muted-foreground hidden sm:inline">{ch.label}</span>
                    </div>
                ))}
            </div>

            {/* Sections */}
            {notificationMatrixSections.map((section, sIdx) => (
                <div key={section.label}>
                    {/* Section header */}
                    <div className={cn(
                        "px-3 py-1.5 bg-muted/20",
                        sIdx > 0 && "border-t"
                    )}>
                        <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                            {section.label}
                        </span>
                    </div>

                    {/* Event rows */}
                    {section.events.map(event => {
                        const modified = isRowModified(event.key)
                        return (
                            <div
                                key={event.key}
                                className={cn(
                                    "grid grid-cols-[1fr_repeat(3,64px)] sm:grid-cols-[1fr_repeat(3,80px)] items-center px-3 py-2 border-t transition-colors",
                                    modified
                                        ? "bg-amber-500/5 border-amber-500/20"
                                        : "hover:bg-muted/30"
                                )}
                            >
                                <span className="text-sm">
                                    {event.label}
                                    {modified && <span className="inline-block w-1.5 h-1.5 rounded-full bg-amber-500 ml-1.5 align-middle" />}
                                </span>
                                {notificationChannels.map(ch => (
                                    <div key={ch.key} className="flex justify-center">
                                        <Checkbox
                                            checked={isChecked(ch.key, event.key)}
                                            onCheckedChange={(checked) =>
                                                onSettingChange("notification", getSettingKey(ch.key, event.key), checked === true)
                                            }
                                        />
                                    </div>
                                ))}
                            </div>
                        )
                    })}
                </div>
            ))}
        </div>
    )
}
