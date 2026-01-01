import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import {
    FormControl,
    FormDescription,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form"
import type { Node } from "@/lib/types"
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

interface NodeSettingsGeneralProps {
    node: Node
    settingsForm: NodeSettingsForm
}

export function NodeSettingsGeneral({ node, settingsForm }: NodeSettingsGeneralProps) {
    const { form } = settingsForm

    return (
        <Card className="bg-card/50 backdrop-blur-sm border-white/5">
            <CardHeader>
                <CardTitle>General Settings</CardTitle>
                <CardDescription>Manage basic node information</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <FormField
                        control={form.control}
                        name="name"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Node Name</FormLabel>
                                <FormControl>
                                    <Input {...field} />
                                </FormControl>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                    <FormField
                        control={form.control}
                        name="ip"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel className="flex items-center gap-2">
                                    IP Address
                                    <span
                                        className={`inline-block w-2 h-2 rounded-full ${
                                            node.is_online ? "bg-green-500" : "bg-red-500"
                                        }`}
                                        title={node.is_online ? "Reachable" : "Unreachable"}
                                    />
                                </FormLabel>
                                <FormControl>
                                    <Input {...field} placeholder="1.2.3.4" />
                                </FormControl>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                    <FormField
                        control={form.control}
                        name="country_code"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Country Code</FormLabel>
                                <FormControl>
                                    <Input {...field} placeholder="DE" maxLength={2} className="uppercase" />
                                </FormControl>
                                <FormDescription>
                                    2-letter ISO code (e.g. DE, US, NL)
                                </FormDescription>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                    <FormField
                        control={form.control}
                        name="datacenter"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Datacenter</FormLabel>
                                <FormControl>
                                    <Input {...field} placeholder="Hetzner, DigitalOcean, etc." />
                                </FormControl>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                </div>
            </CardContent>
        </Card>
    )
}
