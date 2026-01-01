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
        <Card className="bg-card/50 backdrop-blur-sm border-white/5">
            <CardHeader>
                <CardTitle>Bandwidth Shaping</CardTitle>
                <CardDescription>
                    Control per-user bandwidth limits using Linux TC (Traffic Control)
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
                                <FormItem className="flex items-center justify-between rounded-lg border p-3">
                                    <div className="space-y-0.5">
                                        <FormLabel className="flex items-center gap-2">
                                            Enable Bandwidth Shaping
                                            <InfoPopover>
                                                When enabled, the agent sets up Linux TC (HTB) rules to
                                                enforce per-user speed limits. Users assigned to bandwidth-limited
                                                plans will have their traffic shaped according to the plan&apos;s limit.
                                                Requires the agent to run as root.
                                            </InfoPopover>
                                        </FormLabel>
                                        <FormDescription>
                                            Apply TC rate limiting on this node
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
                                                    placeholder="eth0"
                                                    value={field.value}
                                                    onChange={field.onChange}
                                                />
                                            </FormControl>
                                            <FormDescription>
                                                Egress interface for TC shaping (e.g. eth0, ens3)
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
                                                    value={field.value}
                                                    onChange={(e) => field.onChange(parseInt(e.target.value) || 1000)}
                                                />
                                            </FormControl>
                                            <FormDescription>
                                                Total bandwidth of the server&apos;s link
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
