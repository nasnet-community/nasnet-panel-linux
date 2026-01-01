import { useState, useEffect, useCallback } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { Card, CardContent, CardDescription, CardHeader, CardTitle, CardFooter } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Loader2 } from "lucide-react"
import { HiOutlineSave, HiOutlineRefresh, HiOutlineExclamationCircle } from "react-icons/hi"
import { toast } from "sonner"
import {
    Form,
    FormControl,
    FormDescription,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form"
import { getXrayConfig, updateXrayConfig } from "@/lib/admin-api"
import { nodeXrayLogSchema, type NodeXrayLogFormData } from "@/lib/validations/node-settings-schema"
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

// Integrated mode: used from node-settings with shared form
interface IntegratedProps {
    isOnline: boolean
    settingsForm: NodeSettingsForm
    nodeId?: never
}

// Standalone mode: used from node-network-config with own form
interface StandaloneProps {
    nodeId: number
    isOnline: boolean
    settingsForm?: never
}

type NodeSettingsXrayProps = IntegratedProps | StandaloneProps

export function NodeSettingsXray(props: NodeSettingsXrayProps) {
    const { isOnline } = props

    if (props.settingsForm) {
        return <IntegratedXray isOnline={isOnline} settingsForm={props.settingsForm} />
    }
    return <StandaloneXray nodeId={props.nodeId} isOnline={isOnline} />
}

// Integrated version — uses shared form from parent
function IntegratedXray({ isOnline, settingsForm }: { isOnline: boolean; settingsForm: NodeSettingsForm }) {
    const { form, xrayLoading, fullXrayConfig, fetchXrayConfig } = settingsForm

    if (!isOnline) return <XrayOfflineCard />
    if (xrayLoading && !fullXrayConfig) return <XrayLoadingCard />

    return (
        <Card className="bg-card/50 backdrop-blur-sm border-white/5">
            <CardHeader>
                <div className="flex items-center justify-between">
                    <div>
                        <CardTitle>Xray Logging</CardTitle>
                        <CardDescription>Control how Xray logs events and errors</CardDescription>
                    </div>
                    <Button variant="ghost" size="icon" onClick={fetchXrayConfig} disabled={xrayLoading} aria-label="Refresh Xray config">
                        <HiOutlineRefresh className={`w-4 h-4 ${xrayLoading ? "animate-spin" : ""}`} />
                    </Button>
                </div>
            </CardHeader>
            <CardContent className="space-y-6">
                <XrayFormFields form={form} fieldNames={{ access: "log_access", error: "log_error" }} showAccessLogToggle />
            </CardContent>
        </Card>
    )
}

// Standalone version — manages its own form and save
function StandaloneXray({ nodeId, isOnline }: { nodeId: number; isOnline: boolean }) {
    const [isLoading, setIsLoading] = useState(false)
    const [saving, setSaving] = useState(false)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const [fullConfig, setFullConfig] = useState<any>(null)

    const form = useForm<NodeXrayLogFormData>({
        resolver: zodResolver(nodeXrayLogSchema),
        defaultValues: {
            loglevel: "warning",
            access: "",
            error: "",
            dnsLog: false,
        },
    })

    const fetchConfig = useCallback(async () => {
        if (!isOnline) return
        try {
            setIsLoading(true)
            const res = await getXrayConfig(nodeId)
            if (res.success && res.data) {
                try {
                    const parsed = JSON.parse(res.data)
                    setFullConfig(parsed)
                    if (parsed.log) {
                        form.reset({
                            loglevel: parsed.log.loglevel || "warning",
                            access: parsed.log.access || "",
                            error: parsed.log.error || "",
                            dnsLog: parsed.log.dnsLog || false,
                        })
                    }
                } catch {
                    toast.error("Failed to parse Xray configuration")
                }
            } else {
                toast.error(res.error || "Failed to load config")
            }
        } catch {
            toast.error("Failed to load config")
        } finally {
            setIsLoading(false)
        }
    }, [nodeId, isOnline, form])

    useEffect(() => {
        fetchConfig()
    }, [fetchConfig])

    const onSubmit = async (data: NodeXrayLogFormData) => {
        if (!fullConfig) return
        try {
            setSaving(true)
            const updatedConfig = {
                ...fullConfig,
                log: {
                    ...fullConfig.log,
                    loglevel: data.loglevel,
                    access: data.access,
                    error: data.error,
                    dnsLog: data.dnsLog,
                },
            }
            const configStr = JSON.stringify(updatedConfig, null, 2)
            const res = await updateXrayConfig(nodeId, configStr)
            if (res.success) {
                setFullConfig(updatedConfig)
                form.reset(data)
                toast.success("Xray log settings saved. Restart Xray to apply changes.")
            } else {
                toast.error(res.error || "Failed to save settings")
            }
        } catch {
            toast.error("Failed to save settings")
        } finally {
            setSaving(false)
        }
    }

    if (!isOnline) return <XrayOfflineCard />
    if (isLoading && !fullConfig) return <XrayLoadingCard />

    return (
        <Card className="bg-card/50 backdrop-blur-sm border-white/5">
            <CardHeader>
                <div className="flex items-center justify-between">
                    <div>
                        <CardTitle>Xray Logging</CardTitle>
                        <CardDescription>Control how Xray logs events and errors</CardDescription>
                    </div>
                    <Button variant="ghost" size="icon" onClick={fetchConfig} disabled={isLoading || saving} aria-label="Refresh Xray config">
                        <HiOutlineRefresh className={`w-4 h-4 ${isLoading ? "animate-spin" : ""}`} />
                    </Button>
                </div>
            </CardHeader>
            <Form {...form}>
                <form onSubmit={form.handleSubmit(onSubmit)}>
                    <CardContent className="space-y-6">
                        <XrayFormFields form={form} fieldNames={{ access: "access", error: "error" }} />
                    </CardContent>
                    <CardFooter className="border-t bg-muted/10 px-6 py-4">
                        <Button type="submit" disabled={saving || !form.formState.isDirty}>
                            {saving && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
                            <HiOutlineSave className="w-4 h-4 mr-2" />
                            Save Changes
                        </Button>
                    </CardFooter>
                </form>
            </Form>
        </Card>
    )
}

// Shared form fields
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function XrayFormFields({ form, fieldNames, showAccessLogToggle = false }: { form: any; fieldNames: { access: string; error: string }; showAccessLogToggle?: boolean }) {
    return (
        <>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <FormField
                    control={form.control}
                    name="loglevel"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Log Level</FormLabel>
                            <Select value={field.value} onValueChange={field.onChange}>
                                <FormControl>
                                    <SelectTrigger>
                                        <SelectValue placeholder="Select level" />
                                    </SelectTrigger>
                                </FormControl>
                                <SelectContent>
                                    <SelectItem value="debug">Debug</SelectItem>
                                    <SelectItem value="info">Info</SelectItem>
                                    <SelectItem value="warning">Warning</SelectItem>
                                    <SelectItem value="error">Error</SelectItem>
                                    <SelectItem value="none">None</SelectItem>
                                </SelectContent>
                            </Select>
                            <FormDescription>
                                Recommended: Warning for production
                            </FormDescription>
                            <FormMessage />
                        </FormItem>
                    )}
                />

                <div />

                <FormField
                    control={form.control}
                    name={fieldNames.access}
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Access Log Path</FormLabel>
                            <FormControl>
                                <Input value={field.value as string} onChange={field.onChange} onBlur={field.onBlur} name={field.name} ref={field.ref} placeholder="/var/log/xray/access.log" />
                            </FormControl>
                            <FormDescription>
                                Leave empty to disable file logging
                            </FormDescription>
                            <FormMessage />
                        </FormItem>
                    )}
                />
                <FormField
                    control={form.control}
                    name={fieldNames.error}
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>Error Log Path</FormLabel>
                            <FormControl>
                                <Input value={field.value as string} onChange={field.onChange} onBlur={field.onBlur} name={field.name} ref={field.ref} placeholder="/var/log/xray/error.log" />
                            </FormControl>
                            <FormDescription>
                                Leave empty to disable file logging
                            </FormDescription>
                            <FormMessage />
                        </FormItem>
                    )}
                />
            </div>

            <div className="flex flex-wrap gap-4">
                <FormField
                    control={form.control}
                    name="dnsLog"
                    render={({ field }) => (
                        <FormItem className="flex items-center justify-between rounded-lg border p-3 w-fit">
                            <div className="space-y-0.5 mr-4">
                                <FormLabel>DNS Logging</FormLabel>
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

                {showAccessLogToggle && (
                    <FormField
                        control={form.control}
                        name="enable_access_log"
                        render={({ field }) => (
                            <FormItem className="flex items-center justify-between rounded-lg border p-3 w-fit">
                                <div className="space-y-0.5 mr-4">
                                    <FormLabel>Access Log Capture</FormLabel>
                                    <FormDescription className="text-xs">
                                        Per-user destination tracking
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
                )}
            </div>

            <Alert>
                <HiOutlineExclamationCircle className="h-4 w-4" />
                <AlertTitle>Note</AlertTitle>
                <AlertDescription>
                    Changes require Xray restart to take effect.
                </AlertDescription>
            </Alert>
        </>
    )
}

function XrayOfflineCard() {
    return (
        <Card className="bg-card/50 backdrop-blur-sm border-white/5">
            <CardContent className="py-12">
                <div className="flex flex-col items-center justify-center text-muted-foreground">
                    <HiOutlineExclamationCircle className="w-12 h-12 mb-4 opacity-50" />
                    <h3 className="text-lg font-medium text-foreground">Node is Offline</h3>
                    <p>Start the node agent to edit Xray settings.</p>
                </div>
            </CardContent>
        </Card>
    )
}

function XrayLoadingCard() {
    return (
        <Card className="bg-card/50 backdrop-blur-sm border-white/5">
            <CardContent className="py-12">
                <div className="flex items-center justify-center">
                    <Loader2 className="w-8 h-8 animate-spin text-muted-foreground" />
                </div>
            </CardContent>
        </Card>
    )
}
