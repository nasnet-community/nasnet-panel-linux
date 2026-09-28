import { useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import {
    FormControl,
    FormDescription,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form"
import { InfoPopover } from "@/components/ui/info-popover"
import { Loader2 } from "lucide-react"
import { toast } from "@/lib/toast"
import { switchNodeConnectMode } from "@/lib/api/nodes"
import type { Node } from "@/lib/types"
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

interface NodeSettingsAgentProps {
    node: Node
    settingsForm: NodeSettingsForm
    onRefresh?: () => void
}

export function NodeSettingsAgent({ node, settingsForm, onRefresh }: NodeSettingsAgentProps) {
    const { form } = settingsForm
    const [switchDialogOpen, setSwitchDialogOpen] = useState(false)
    const [isSwitching, setIsSwitching] = useState(false)

    const currentMode = node.connect_mode || "direct"
    const targetMode = currentMode === "direct" ? "reverse" : "direct"

    const handleSwitchConnectMode = async () => {
        try {
            setIsSwitching(true)
            const res = await switchNodeConnectMode(node.id, targetMode)
            if (res.success) {
                toast.success(`Switching to ${targetMode} mode. Agent is restarting...`)
                setSwitchDialogOpen(false)
                // Refresh node data after a short delay to let the agent restart
                setTimeout(() => onRefresh?.(), 3000)
            } else {
                toast.error(res.error || "Failed to switch connect mode")
            }
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Failed to switch connect mode")
        } finally {
            setIsSwitching(false)
        }
    }

    return (
        <Card className="border-border/60 bg-card shadow-none">
            <CardHeader>
                <CardTitle className="text-base">Agent connection</CardTitle>
                <CardDescription>Configure how the panel reaches this node.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
                <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2">
                    <FormField
                        control={form.control}
                        name="agent_port"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel className="flex items-center gap-2">
                                    Agent Port
                                    {node.is_online && (
                                        <span
                                            className="inline-block w-2 h-2 rounded-full bg-green-500"
                                            title="Agent reachable"
                                        />
                                    )}
                                </FormLabel>
                                <FormControl>
                                    <Input
                                        type="number"
                                        name={field.name} ref={field.ref} onBlur={field.onBlur}
                                        value={field.value}
                                        onChange={(e) => field.onChange(Number(e.target.value))}
                                    />
                                </FormControl>
                                <FormDescription>
                                    The port the panel dials. Changing this does not move the agent listener.
                                </FormDescription>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                    <FormField
                        control={form.control}
                        name="api_port"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Xray API Port</FormLabel>
                                <FormControl>
                                    <Input
                                        type="number"
                                        name={field.name} ref={field.ref} onBlur={field.onBlur}
                                        value={field.value}
                                        onChange={(e) => field.onChange(Number(e.target.value))}
                                    />
                                </FormControl>
                                <FormDescription>
                                    Changing this updates the API listener and restarts Xray.
                                </FormDescription>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                </div>

                {settingsForm.isDirty && <p className="text-xs text-muted-foreground">Save or discard your changes before switching connection mode. Switching applies immediately and restarts the agent.</p>}
                {/* Connect Mode */}
                <div className="flex flex-col items-start justify-between gap-4 rounded-lg border border-border/60 p-4 sm:flex-row sm:items-center">
                    <div className="space-y-0.5">
                        <div className="flex flex-wrap items-center gap-2 text-sm font-medium leading-none">
                            Connection Mode
                            <InfoPopover>
                                <strong>Direct:</strong> The panel connects to the agent via its IP and port.
                                Requires the agent port to be open and reachable.
                                <br /><br />
                                <strong>Reverse:</strong> The agent dials this panel's hub.
                                Useful when the node is behind NAT or a firewall.
                            </InfoPopover>
                            <Badge
                                variant={currentMode === "reverse" ? "default" : "secondary"}
                                className="text-[11px] uppercase font-bold tracking-wider"
                            >
                                {currentMode}
                            </Badge>
                        </div>
                        <p className="text-[0.8625rem] text-muted-foreground">
                            {currentMode === "direct"
                                ? "The panel connects directly to the agent"
                                : "The agent dials this panel's hub"}
                        </p>
                    </div>
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={!node.is_online || settingsForm.isDirty || isSwitching}
                        onClick={() => setSwitchDialogOpen(true)}
                    >
                        Switch to {targetMode}
                    </Button>
                </div>

            </CardContent>

            {/* Connect Mode Switch Confirmation Dialog */}
            <Dialog open={switchDialogOpen} onOpenChange={setSwitchDialogOpen}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Switch to {targetMode} mode?</DialogTitle>
                        <DialogDescription>
                            {targetMode === "reverse"
                                ? "The agent will be reconfigured to dial this panel's hub. This is useful when the node is behind NAT or a firewall; the hub must be on and reachable at the Agent Hub Address."
                                : `The agent will be reconfigured to listen on port ${node.agent_port} for direct connections from the panel. The node must be directly reachable.`}
                        </DialogDescription>
                    </DialogHeader>
                    <div className="text-sm text-muted-foreground space-y-1 rounded-lg bg-muted/50 p-3">
                        <p>This will:</p>
                        <ul className="list-disc list-inside space-y-1 ml-1">
                            <li>Rewrite the agent config on the node</li>
                            <li>Restart the agent service</li>
                            <li>Temporarily disconnect the node</li>
                        </ul>
                    </div>
                    <DialogFooter>
                        <Button
                            variant="outline"
                            onClick={() => setSwitchDialogOpen(false)}
                            disabled={isSwitching}
                        >
                            Cancel
                        </Button>
                        <Button
                            onClick={handleSwitchConnectMode}
                            disabled={isSwitching}
                        >
                            {isSwitching && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
                            Switch to {targetMode}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </Card>
    )
}
