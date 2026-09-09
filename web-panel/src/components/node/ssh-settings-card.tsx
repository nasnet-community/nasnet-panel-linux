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
    isOnline?: boolean
}

export function SSHSettingsCard({ settingsForm, isStealth, isOnline = true }: SSHSettingsCardProps) {
    const { form, sshLoading, sshStatus, sshError, fetchSSHStatus } = settingsForm
    const sshEnabled = form.watch("ssh_enabled")

    return (
        <Card className="border-border/60 bg-card shadow-none">
            <CardHeader>
                <div className="flex items-center justify-between">
                    <div>
                        <CardTitle className="flex items-center gap-2">
                            SSH access
                            {sshStatus && (
                                <Badge variant={sshStatus.is_active ? "success" : "secondary"} className="ml-2 text-xs">
                                    {sshStatus.is_active ? "Active" : "Inactive"}
                                </Badge>
                            )}
                        </CardTitle>
                        <CardDescription>Manage remote SSH access security</CardDescription>
                    </div>
                    <Button variant="ghost" size="icon" onClick={fetchSSHStatus} disabled={sshLoading || !isOnline || isStealth} aria-label="Refresh SSH status">
                        <RefreshCw className={`w-4 h-4 ${sshLoading ? "animate-spin" : ""}`} />
                    </Button>
                </div>
            </CardHeader>
            <CardContent className="space-y-6">
                {!isOnline ? <p className="text-sm text-muted-foreground">Restore the server connection to view and change SSH access.</p> : isStealth ? (
                    <p className="text-sm text-muted-foreground">
                        SSH configuration is not available for stealth nodes.
                    </p>
                ) : sshError || (!sshStatus && !sshLoading) ? (
                    <div className="text-center py-4 text-muted-foreground">
                        <p>{sshError || "Unable to retrieve SSH status."}</p><Button type="button" size="sm" variant="outline" className="mt-3" onClick={fetchSSHStatus}>Retry SSH status</Button>
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
                                <FormItem className="flex items-center justify-between gap-4 rounded-lg border border-border/60 p-4">
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
                                        name={field.name} ref={field.ref} onBlur={field.onBlur}
                                                value={field.value}
                                                onChange={(e) => field.onChange(Number(e.target.value))}
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
