import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import {
    FormControl,
    FormDescription,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form"
import { InfoPopover } from "@/components/ui/info-popover"
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

interface NodeSettingsBandwidthProps {
    settingsForm: NodeSettingsForm
    isStealth?: boolean
}

export function NodeSettingsBandwidth({ settingsForm, isStealth }: NodeSettingsBandwidthProps) {
    const { form } = settingsForm
    const isEnabled = form.watch("bandwidth_enabled")

    return (
        <Card className="border-border/60 bg-card shadow-none">
            <CardHeader>
                <CardTitle className="text-base">Bandwidth Shaping</CardTitle>
                <CardDescription>
                    Enforce the speed limits of subscriptions with Linux TC (Traffic Control)
                </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
                {isStealth ? (
                    <p className="text-sm text-muted-foreground">
                        Bandwidth shaping is not available for stealth nodes.
                    </p>
                ) : (
                    <>
                        <FormField
                            control={form.control}
                            name="bandwidth_enabled"
                            render={({ field }) => (
                                <FormItem className="flex items-center justify-between gap-4 rounded-lg border border-border/60 p-4">
                                    <div className="space-y-0.5">
                                        <FormLabel className="flex items-center gap-2">
                                            Enable Bandwidth Shaping
                                            <InfoPopover>
                                                When enabled, the agent enforces the speed limits set on
                                                subscriptions. Each speed is one pipe on this node, shared by
                                                everyone on it: two users on 10 Mbps get 10 Mbps between them.
                                                Traffic through WireGuard outbounds isn&apos;t limited.
                                                Requires the agent to run as root.
                                            </InfoPopover>
                                        </FormLabel>
                                        <FormDescription>
                                            Enforce speed limits on this node
                                        </FormDescription>
                                    </div>
                                    <FormControl>
                                        <Switch
                                            checked={field.value}
                                            onCheckedChange={field.onChange}
                                        />
                                    </FormControl>
                                </FormItem>
                            )}
                        />

                        {isEnabled && (
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 ml-2 pl-4 border-l-2 border-muted">
                                <FormField
                                    control={form.control}
                                    name="bandwidth_interface"
                                    render={({ field }) => (
                                        <FormItem>
                                            <FormLabel>Network Interface</FormLabel>
                                            <FormControl>
                                                <Input
                                                    placeholder="eth0" name={field.name} ref={field.ref} onBlur={field.onBlur}
                                                    value={field.value}
                                                    onChange={field.onChange}
                                                />
                                            </FormControl>
                                            <FormDescription>
                                                Main uplink (e.g. eth0, ens3). Tunnels on the node are covered too.
                                            </FormDescription>
                                            <FormMessage />
                                        </FormItem>
                                    )}
                                />
                                <FormField
                                    control={form.control}
                                    name="bandwidth_total_bw"
                                    render={({ field }) => (
                                        <FormItem>
                                            <FormLabel>Total Link Bandwidth (Mbps)</FormLabel>
                                            <FormControl>
                                                <Input
                                                    type="number"
                                        name={field.name} ref={field.ref} onBlur={field.onBlur}
                                                    value={field.value}
                                                    onChange={(e) => field.onChange(Number(e.target.value))}
                                                />
                                            </FormControl>
                                            <FormDescription>
                                                Cap for all limited users together, usually the link speed
                                            </FormDescription>
                                            <FormMessage />
                                        </FormItem>
                                    )}
                                />
                            </div>
                        )}
                    </>
                )}
            </CardContent>
        </Card>
    )
}
