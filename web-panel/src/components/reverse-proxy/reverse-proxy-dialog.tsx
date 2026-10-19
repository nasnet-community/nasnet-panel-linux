import { useEffect } from "react"
import { useForm, Controller } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { ResponsiveDialog } from "@/components/ui/responsive-dialog"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { reverseProxySchema, type ReverseProxyFormData } from "@/lib/validations/reverse-proxy-schema"
import type { ReverseProxy } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { copyToClipboard } from "@/lib/utils"
import { toast } from "@/lib/toast"

// The bridge exit the panel manages: Freedom with private addresses blocked.
const MANAGED_EXIT_TAG = "rvs-exit"

interface ReverseProxyDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    mode: "create" | "edit"
    reverseProxy: ReverseProxy | null
    inboundTags: string[]
    outboundTags: string[]
    vlessInboundTags: string[]
    vlessOutboundTags: string[]
    existingCount: number
    onSave: (data: Partial<ReverseProxy>) => void
}

export function ReverseProxyDialog({
    open,
    onOpenChange,
    mode,
    reverseProxy,
    inboundTags,
    outboundTags,
    vlessInboundTags,
    vlessOutboundTags,
    existingCount,
    onSave,
}: ReverseProxyDialogProps) {
    const {
        register,
        handleSubmit,
        control,
        watch,
        setValue,
        reset,
        formState: { errors, isSubmitting },
    } = useForm<ReverseProxyFormData>({
        resolver: zodResolver(reverseProxySchema),
        defaultValues: {
            type: "bridge",
            tag: `reverse-${existingCount}`,
            interconnection_tag: "",
            outbound_tag: "",
            interconnection_tags: [],
            inbound_tags: [],
        },
    })

    const proxyType = watch("type")

    // Reset form when dialog opens
    useEffect(() => {
        if (open) {
            if (mode === "edit" && reverseProxy) {
                reset({
                    type: reverseProxy.type,
                    tag: reverseProxy.tag,
                    interconnection_tag: reverseProxy.interconnection_tag || "",
                    outbound_tag: reverseProxy.outbound_tag || "",
                    interconnection_tags: reverseProxy.interconnection_tags || [],
                    inbound_tags: reverseProxy.inbound_tags || [],
                })
            } else {
                reset({
                    type: "bridge",
                    tag: `reverse-${existingCount}`,
                    interconnection_tag: "",
                    outbound_tag: "",
                    interconnection_tags: [],
                    inbound_tags: [],
                })
            }
        }
    }, [open, mode, reverseProxy, existingCount, reset])

    // Clear type-specific fields when type changes
    const handleTypeChange = (newType: "bridge" | "portal") => {
        setValue("type", newType)
        if (newType === "bridge") {
            setValue("interconnection_tags", [])
            setValue("inbound_tags", [])
        } else {
            setValue("interconnection_tag", "")
            setValue("outbound_tag", "")
        }
    }

    const onSubmit = (data: ReverseProxyFormData) => {
        onSave(data as Partial<ReverseProxy>)
        onOpenChange(false)
    }

    const toggleArrayItem = (
        field: "interconnection_tags" | "inbound_tags",
        tag: string,
        current: string[]
    ) => {
        if (current.includes(tag)) {
            setValue(field, current.filter((t) => t !== tag))
        } else {
            setValue(field, [...current, tag])
        }
    }

    return (
        <ResponsiveDialog
            open={open}
            onOpenChange={onOpenChange}
            title={mode === "create" ? "Add Reverse Proxy" : "Edit Reverse Proxy"}
            description="Configure VLESS Reverse through a bridge or portal"
            onSave={handleSubmit(onSubmit)}
            saveLabel={isSubmitting ? "Saving..." : mode === "create" ? "Add Reverse" : "Save Changes"}
            saveDisabled={isSubmitting}
        >
            <div className="space-y-5">
                {/* Type */}
                <div className="space-y-2">
                    <Label>Type</Label>
                    <Controller
                        control={control}
                        name="type"
                        render={({ field }) => (
                            <Select
                                value={field.value}
                                onValueChange={(v) => handleTypeChange(v as "bridge" | "portal")}
                            >
                                <SelectTrigger className="w-full">
                                    <SelectValue placeholder="Select type..." />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="bridge">Bridge</SelectItem>
                                    <SelectItem value="portal">Portal</SelectItem>
                                </SelectContent>
                            </Select>
                        )}
                    />
                    {errors.type && (
                        <p className="text-xs text-destructive">{errors.type.message}</p>
                    )}
                </div>

                {/* Tag */}
                <div className="space-y-2">
                    <Label>Tag</Label>
                    <Input
                        {...register("tag")}
                        placeholder={`reverse-${existingCount}`}
                    />
                    {errors.tag && (
                        <p className="text-xs text-destructive">{errors.tag.message}</p>
                    )}
                    <p className="text-xs text-muted-foreground">Unique identifier for this reverse proxy</p>
                </div>

                {/* Bridge-specific fields */}
                {proxyType === "bridge" && (
                    <>
                        {/* Interconnection (outbound) */}
                        <div className="space-y-2">
                            <Label>Interconnection</Label>
                            {vlessOutboundTags.length === 0 ? (
                                <p className="text-sm text-muted-foreground border rounded-md px-3 py-2">
                                    No enabled VLESS outbounds configured
                                </p>
                            ) : (
                                <Controller
                                    control={control}
                                    name="interconnection_tag"
                                    render={({ field }) => (
                                        <Select
                                            value={field.value || ""}
                                            onValueChange={field.onChange}
                                        >
                                            <SelectTrigger className="w-full">
                                                <SelectValue placeholder="Select VLESS outbound..." />
                                            </SelectTrigger>
                                            <SelectContent>
                                                {vlessOutboundTags.map((tag) => (
                                                    <SelectItem key={tag} value={tag}>
                                                        {tag}
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    )}
                                />
                            )}
                            {errors.interconnection_tag && (
                                <p className="text-xs text-destructive">{errors.interconnection_tag.message}</p>
                            )}
                            <p className="text-xs text-muted-foreground">VLESS outbound authenticated as a user on the remote portal interconnection inbound</p>
                        </div>

                        {/* Outbound */}
                        <div className="space-y-2">
                            <Label>Outbound</Label>
                            <Controller
                                    control={control}
                                    name="outbound_tag"
                                    render={({ field }) => (
                                        <Select
                                            value={field.value || ""}
                                            onValueChange={field.onChange}
                                        >
                                            <SelectTrigger className="w-full">
                                                <SelectValue placeholder="Select outbound..." />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value={MANAGED_EXIT_TAG}>Direct, LAN blocked</SelectItem>
                                                {outboundTags.map((tag) => (
                                                    <SelectItem key={tag} value={tag}>
                                                        {tag}
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    )}
                                />
                            {errors.outbound_tag && (
                                <p className="text-xs text-destructive">{errors.outbound_tag.message}</p>
                            )}
                            <p className="text-xs text-muted-foreground">Where tunnel traffic leaves this node. Xray blocks it at a Freedom outbound without an Allow final rule; "Direct, LAN blocked" has one and keeps private addresses out.</p>
                        </div>
                    </>
                )}

                {/* Portal-specific fields */}
                {proxyType === "portal" && (
                    <>
                        {/* Interconnection (inbound multi-select) */}
                        <div className="space-y-2">
                            <Label>Interconnection</Label>
                            {vlessInboundTags.length === 0 ? (
                                <p className="text-sm text-muted-foreground border rounded-md px-3 py-2">
                                    No enabled VLESS inbounds configured
                                </p>
                            ) : (
                                <Controller
                                    control={control}
                                    name="interconnection_tags"
                                    render={({ field }) => (
                                        <div className="border rounded-md p-3 max-h-48 overflow-y-auto space-y-2">
                                            {vlessInboundTags.map((tag) => (
                                                <div key={tag} className="flex items-center gap-2">
                                                    <Checkbox
                                                        id={`interconnection-${tag}`}
                                                        checked={(field.value || []).includes(tag)}
                                                        onCheckedChange={() =>
                                                            toggleArrayItem(
                                                                "interconnection_tags",
                                                                tag,
                                                                field.value || []
                                                            )
                                                        }
                                                    />
                                                    <label
                                                        htmlFor={`interconnection-${tag}`}
                                                        className="text-sm font-mono cursor-pointer select-none"
                                                    >
                                                        {tag}
                                                    </label>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                />
                            )}
                            {errors.interconnection_tags && (
                                <p className="text-xs text-destructive">{errors.interconnection_tags.message}</p>
                            )}
                            <p className="text-xs text-muted-foreground">The entry gets one hidden client on each of these inbounds, and only that credential can open the tunnel. Customers on them keep working.</p>
                            {mode === "edit" && reverseProxy?.uuid && (
                                <div className="flex items-center justify-between gap-2 rounded-md border px-3 py-2">
                                    <div className="min-w-0">
                                        <p className="text-xs text-muted-foreground">Bridge credential (VLESS id)</p>
                                        <p className="font-mono text-xs truncate">{reverseProxy.uuid}</p>
                                    </div>
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="sm"
                                        onClick={async () => {
                                            if (await copyToClipboard(reverseProxy.uuid!)) toast.success("Copied")
                                        }}
                                    >
                                        Copy
                                    </Button>
                                </div>
                            )}
                        </div>

                        {/* Inbound (multi-select) */}
                        <div className="space-y-2">
                            <Label>Inbound</Label>
                            {inboundTags.length === 0 ? (
                                <p className="text-sm text-muted-foreground border rounded-md px-3 py-2">
                                    No inbounds configured
                                </p>
                            ) : (
                                <Controller
                                    control={control}
                                    name="inbound_tags"
                                    render={({ field }) => (
                                        <div className="border rounded-md p-3 max-h-48 overflow-y-auto space-y-2">
                                            {inboundTags.map((tag) => (
                                                <div key={tag} className="flex items-center gap-2">
                                                    <Checkbox
                                                        id={`inbound-${tag}`}
                                                        checked={(field.value || []).includes(tag)}
                                                        onCheckedChange={() =>
                                                            toggleArrayItem(
                                                                "inbound_tags",
                                                                tag,
                                                                field.value || []
                                                            )
                                                        }
                                                    />
                                                    <label
                                                        htmlFor={`inbound-${tag}`}
                                                        className="text-sm font-mono cursor-pointer select-none"
                                                    >
                                                        {tag}
                                                    </label>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                />
                            )}
                            {errors.inbound_tags && (
                                <p className="text-xs text-destructive">{errors.inbound_tags.message}</p>
                            )}
                            <p className="text-xs text-muted-foreground">Inbounds to accept traffic on</p>
                        </div>
                    </>
                )}
            </div>
        </ResponsiveDialog>
    )
}
