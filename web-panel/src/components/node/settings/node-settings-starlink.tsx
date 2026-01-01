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
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

interface NodeSettingsStarlinkProps {
    settingsForm: NodeSettingsForm
    isStealth?: boolean
}

export function NodeSettingsStarlink({ settingsForm, isStealth }: NodeSettingsStarlinkProps) {
    const { form } = settingsForm
    const isEnabled = form.watch("starlink_enabled")

    return (
        <Card className="bg-card/50 backdrop-blur-sm border-white/5">
            <CardHeader>
                <CardTitle>Starlink Monitoring</CardTitle>
                <CardDescription>
                    Monitor Starlink satellite dish performance metrics on this node
                </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
                {isStealth ? (
                    <p className="text-sm text-muted-foreground">
                        Starlink monitoring is not available for stealth nodes.
                    </p>
                ) : (
                    <>
                        <FormField
                            control={form.control}
                            name="starlink_enabled"
                            render={({ field }) => (
                                <FormItem className="flex items-center justify-between rounded-lg border p-3">
                                    <div className="space-y-0.5">
                                        <FormLabel>
                                            Enable Starlink Monitoring
                                        </FormLabel>
                                        <FormDescription>
                                            Query the local Starlink dish for real-time metrics
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
                            <div className="ml-2 pl-4 border-l-2 border-muted">
                                <FormField
                                    control={form.control}
                                    name="starlink_dish_address"
                                    render={({ field }) => (
                                        <FormItem>
                                            <FormLabel>Dish Address</FormLabel>
                                            <FormControl>
                                                <Input
                                                    placeholder="192.168.100.1:9200"
                                                    value={field.value}
                                                    onChange={field.onChange}
                                                />
                                            </FormControl>
                                            <FormDescription>
                                                Starlink dish gRPC address (default: 192.168.100.1:9200)
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
