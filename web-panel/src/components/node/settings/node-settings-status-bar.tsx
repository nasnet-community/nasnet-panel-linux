import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form"
import type { Node } from "@/lib/types"
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

export function NodeSettingsStatusBar({ node, settingsForm }: { node: Node; settingsForm: NodeSettingsForm }) {
    const { form } = settingsForm
    const maintenance = form.watch("maintenance_mode")
    return (
        <Card className="border-border/60 bg-card shadow-none">
            <CardHeader className="pb-4">
                <CardTitle className="text-base">Availability</CardTitle>
                <CardDescription>Control server availability and the notice users see.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
                <div className="divide-y divide-border/60 rounded-lg border border-border/60">
                    <FormField control={form.control} name="is_active" render={({ field }) => (
                        <FormItem className="flex items-center justify-between gap-5 p-4">
                            <div className="space-y-1"><FormLabel>Enable server</FormLabel><FormDescription>Include this server in health checks and service management.</FormDescription></div>
                            <FormControl><Switch checked={field.value} onCheckedChange={field.onChange} /></FormControl>
                        </FormItem>
                    )} />
                    <FormField control={form.control} name="maintenance_mode" render={({ field }) => (
                        <FormItem className="flex items-center justify-between gap-5 p-4">
                            <div className="space-y-1"><FormLabel>Maintenance mode</FormLabel><FormDescription>Show a maintenance notice to users of this server.</FormDescription></div>
                            <FormControl><Switch checked={field.value} onCheckedChange={field.onChange} /></FormControl>
                        </FormItem>
                    )} />
                </div>
                {maintenance && <FormField control={form.control} name="maintenance_message" render={({ field }) => (
                    <FormItem>
                        <FormLabel>User notice <span className="font-normal text-muted-foreground">(optional)</span></FormLabel>
                        <FormControl><Textarea {...field} dir="auto" rows={3} placeholder="We’re performing scheduled maintenance. Please check back shortly." /></FormControl>
                        <FormDescription>Leave empty to use the default translated notice. Saved with your other settings.</FormDescription>
                        <FormMessage />
                    </FormItem>
                )} />}
                {node.maintenance_mode && node.maintenance_since && <p className="text-xs text-muted-foreground">In maintenance since {new Date(node.maintenance_since).toLocaleString()}</p>}
            </CardContent>
        </Card>
    )
}
