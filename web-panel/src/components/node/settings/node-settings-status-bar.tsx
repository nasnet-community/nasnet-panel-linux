import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Separator } from "@/components/ui/separator"
import { HiOutlineStatusOnline, HiOutlineStatusOffline } from "react-icons/hi"
import type { Node } from "@/lib/types"
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

interface NodeSettingsStatusBarProps {
    node: Node
    settingsForm: NodeSettingsForm
}

function timeAgo(dateStr: string): string {
    if (!dateStr) return "Never"
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffSec = Math.floor(diffMs / 1000)
    if (diffSec < 60) return `${diffSec}s ago`
    const diffMin = Math.floor(diffSec / 60)
    if (diffMin < 60) return `${diffMin}m ago`
    const diffHour = Math.floor(diffMin / 60)
    if (diffHour < 24) return `${diffHour}h ago`
    const diffDay = Math.floor(diffHour / 24)
    return `${diffDay}d ago`
}

export function NodeSettingsStatusBar({ node, settingsForm }: NodeSettingsStatusBarProps) {
    const { form } = settingsForm
    const isActive = form.watch("is_active")

    return (
        <div className="rounded-lg border bg-card/50 backdrop-blur-sm border-white/5 px-4 py-3 space-y-3">
            {/* Status line */}
            <div className="flex flex-wrap items-center gap-3">
                <Badge variant={node.is_online ? "success" : "danger"} className="gap-1.5">
                    {node.is_online ? (
                        <HiOutlineStatusOnline className="w-3.5 h-3.5" />
                    ) : (
                        <HiOutlineStatusOffline className="w-3.5 h-3.5" />
                    )}
                    {node.is_online ? "Online" : "Offline"}
                </Badge>

                {node.agent_version && (
                    <span className="text-sm text-muted-foreground">
                        Agent <span className="font-mono text-foreground">{node.agent_version}</span>
                    </span>
                )}

                {node.xray_version && (
                    <span className="text-sm text-muted-foreground">
                        Xray <span className="font-mono text-foreground">{node.xray_version}</span>
                    </span>
                )}

                {node.last_check && (
                    <span className="text-sm text-muted-foreground ml-auto">
                        Last check: {timeAgo(node.last_check)}
                    </span>
                )}
            </div>

            <Separator className="bg-white/5" />

            {/* Active toggle */}
            <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                    <p className="text-sm font-medium">Node Active</p>
                    <p className="text-xs text-muted-foreground">
                        Disabled nodes won&apos;t receive new connections
                    </p>
                </div>
                <Switch
                    checked={isActive}
                    onCheckedChange={(checked) => form.setValue("is_active", checked, { shouldDirty: true })}
                />
            </div>
        </div>
    )
}
