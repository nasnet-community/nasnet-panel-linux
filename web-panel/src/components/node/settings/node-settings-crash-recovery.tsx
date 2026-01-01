import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Switch } from "@/components/ui/switch"
import {
    FormControl,
    FormDescription,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form"
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

interface NodeSettingsCrashRecoveryProps {
    settingsForm: NodeSettingsForm
    isStealth?: boolean
}

export function NodeSettingsCrashRecovery({ settingsForm, isStealth }: NodeSettingsCrashRecoveryProps) {
    const { form } = settingsForm
    const isEnabled = form.watch("crash_recovery_enabled")

    return (
        <Card className="bg-card/50 backdrop-blur-sm border-white/5">
            <CardHeader>
                <CardTitle>Crash Recovery Command</CardTitle>
                <CardDescription>
                    Run a custom command on the node when xray auto-disable triggers after too many crashes, then attempt one final restart
                </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
                {isStealth ? (
                    <p className="text-sm text-muted-foreground">
                        Crash recovery command is not available for stealth nodes.
                    </p>
                ) : (
                    <>
                        <FormField
                            control={form.control}
                            name="crash_recovery_enabled"
                            render={({ field }) => (
                                <FormItem className="flex items-center justify-between rounded-lg border p-3">
                                    <div className="space-y-0.5">
                                        <FormLabel>
                                            Enable Crash Recovery Command
                                        </FormLabel>
                                        <FormDescription>
                                            Execute a shell command on the node before the final xray restart attempt
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
                            <div className="ml-2 pl-4 border-l-2 border-muted space-y-4">
                                <FormField
                                    control={form.control}
                                    name="crash_recovery_command"
                                    render={({ field }) => (
                                        <FormItem>
                                            <FormLabel>Command</FormLabel>
                                            <FormControl>
                                                <Textarea
                                                    placeholder="systemctl restart networking"
                                                    className="font-mono text-sm min-h-[80px]"
                                                    value={field.value}
                                                    onChange={field.onChange}
                                                />
                                            </FormControl>
                                            <FormDescription>
                                                Shell command to run on the node (executed via sh -c)
                                            </FormDescription>
                                            <FormMessage />
                                        </FormItem>
                                    )}
                                />

                                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                                    <FormField
                                        control={form.control}
                                        name="crash_recovery_command_timeout"
                                        render={({ field }) => (
                                            <FormItem>
                                                <FormLabel>Timeout (seconds)</FormLabel>
                                                <FormControl>
                                                    <Input
                                                        type="number"
                                                        min={5}
                                                        max={300}
                                                        value={field.value}
                                                        onChange={(e) => field.onChange(Number(e.target.value))}
                                                    />
                                                </FormControl>
                                                <FormDescription>
                                                    Max execution time
                                                </FormDescription>
                                                <FormMessage />
                                            </FormItem>
                                        )}
                                    />

                                    <FormField
                                        control={form.control}
                                        name="crash_recovery_cooldown"
                                        render={({ field }) => (
                                            <FormItem>
                                                <FormLabel>Cooldown (minutes)</FormLabel>
                                                <FormControl>
                                                    <Input
                                                        type="number"
                                                        min={1}
                                                        max={1440}
                                                        value={field.value}
                                                        onChange={(e) => field.onChange(Number(e.target.value))}
                                                    />
                                                </FormControl>
                                                <FormDescription>
                                                    Min time between runs
                                                </FormDescription>
                                                <FormMessage />
                                            </FormItem>
                                        )}
                                    />

                                    <FormField
                                        control={form.control}
                                        name="crash_recovery_max_attempts"
                                        render={({ field }) => (
                                            <FormItem>
                                                <FormLabel>Max Attempts</FormLabel>
                                                <FormControl>
                                                    <Input
                                                        type="number"
                                                        min={0}
                                                        max={100}
                                                        value={field.value}
                                                        onChange={(e) => field.onChange(Number(e.target.value))}
                                                    />
                                                </FormControl>
                                                <FormDescription>
                                                    0 = unlimited
                                                </FormDescription>
                                                <FormMessage />
                                            </FormItem>
                                        )}
                                    />
                                </div>
                            </div>
                        )}
                    </>
                )}
            </CardContent>
        </Card>
    )
}
