import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Loader2, RefreshCw } from "lucide-react"
import {
    FormControl,
    FormDescription,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form"
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

interface SSHSettingsCardProps {
    settingsForm: NodeSettingsForm
    isStealth?: boolean
}

export function SSHSettingsCard({ settingsForm, isStealth }: SSHSettingsCardProps) {
    const { form, sshLoading, sshStatus, fetchSSHStatus } = settingsForm
    const sshEnabled = form.watch("ssh_enabled")

    return (
        <Card className="bg-card/50 backdrop-blur-sm border-white/5">
            <CardHeader>
                <div className="flex items-center justify-between">
                    <div>
                        <CardTitle className="flex items-center gap-2">
                            SSH Configuration
                            {sshStatus && (
                                <Badge variant={sshStatus.is_active ? "success" : "secondary"} className="ml-2 text-xs">
                                    {sshStatus.is_active ? "Active" : "Inactive"}
                                </Badge>
                            )}
                        </CardTitle>
                        <CardDescription>Manage remote SSH access security</CardDescription>
                    </div>
                    <Button variant="ghost" size="icon" onClick={fetchSSHStatus} disabled={sshLoading} aria-label="Refresh SSH status">
                        <RefreshCw className={`w-4 h-4 ${sshLoading ? "animate-spin" : ""}`} />
                    </Button>
                </div>
            </CardHeader>
            <CardContent className="space-y-6">
                {isStealth ? (
                    <p className="text-sm text-muted-foreground">
                        SSH configuration is not available for stealth nodes.
                    </p>
                ) : !sshStatus && !sshLoading ? (
                    <div className="text-center py-4 text-muted-foreground">
                        <p>Unable to retrieve SSH status. Is the agent updated?</p>
                    </div>
                ) : sshLoading && !sshStatus ? (
                    <div className="flex items-center justify-center py-8">
                        <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
                    </div>
                ) : (
                    <>
                        <FormField
                            control={form.control}
                            name="ssh_enabled"
                            render={({ field }) => (
                                <FormItem className="flex items-center justify-between rounded-lg border p-3">
                                    <div className="space-y-0.5">
                                        <FormLabel>Enable SSH Service</FormLabel>
                                        <FormDescription>
                                            Turn on/off the systemd sshd service
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

                        <FormField
                            control={form.control}
                            name="ssh_port"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>SSH Port</FormLabel>
                                    <div className="flex gap-2 items-center">
                                        <FormControl>
                                            <Input
                                                type="number"
                                                value={field.value}
                                                onChange={(e) => field.onChange(parseInt(e.target.value) || 22)}
                                                disabled={!sshEnabled}
                                                className="max-w-[150px]"
                                            />
                                        </FormControl>
                                        <FormDescription className="mt-0">
                                            Default: 22. Range: 1-65535
                                        </FormDescription>
                                    </div>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />
                    </>
                )}
            </CardContent>
        </Card>
    )
}
