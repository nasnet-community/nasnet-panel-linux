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

interface ReverseProxyDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    mode: "create" | "edit"
    reverseProxy: ReverseProxy | null
    inboundTags: string[]
    outboundTags: string[]
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
            domain: "reverse.xui",
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
                    domain: reverseProxy.domain,
                    interconnection_tag: reverseProxy.interconnection_tag || "",
                    outbound_tag: reverseProxy.outbound_tag || "",
                    interconnection_tags: reverseProxy.interconnection_tags || [],
                    inbound_tags: reverseProxy.inbound_tags || [],
                })
            } else {
                reset({
                    type: "bridge",
                    tag: `reverse-${existingCount}`,
                    domain: "reverse.xui",
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
            description="Configure a bridge or portal reverse proxy entry"
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

                {/* Domain */}
                <div className="space-y-2">
                    <Label>Domain</Label>
                    <Input
                        {...register("domain")}
                        placeholder="reverse.xui"
                    />
                    {errors.domain && (
                        <p className="text-xs text-destructive">{errors.domain.message}</p>
                    )}
                </div>

                {/* Bridge-specific fields */}
                {proxyType === "bridge" && (
                    <>
                        {/* Interconnection (outbound) */}
                        <div className="space-y-2">
                            <Label>Interconnection</Label>
                            {outboundTags.length === 0 ? (
                                <p className="text-sm text-muted-foreground border rounded-md px-3 py-2">
                                    No outbounds configured
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
                                                <SelectValue placeholder="Select outbound..." />
                                            </SelectTrigger>
                                            <SelectContent>
                                                {outboundTags.map((tag) => (
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
                            <p className="text-xs text-muted-foreground">Outbound used for the interconnection tunnel</p>
                        </div>

                        {/* Outbound */}
                        <div className="space-y-2">
                            <Label>Outbound</Label>
                            {outboundTags.length === 0 ? (
                                <p className="text-sm text-muted-foreground border rounded-md px-3 py-2">
                                    No outbounds configured
                                </p>
                            ) : (
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
                                                {outboundTags.map((tag) => (
                                                    <SelectItem key={tag} value={tag}>
                                                        {tag}
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    )}
                                />
                            )}
                            {errors.outbound_tag && (
                                <p className="text-xs text-destructive">{errors.outbound_tag.message}</p>
                            )}
                            <p className="text-xs text-muted-foreground">Outbound to forward traffic to</p>
                        </div>
                    </>
                )}

                {/* Portal-specific fields */}
                {proxyType === "portal" && (
                    <>
                        {/* Interconnection (inbound multi-select) */}
                        <div className="space-y-2">
                            <Label>Interconnection</Label>
                            {inboundTags.length === 0 ? (
                                <p className="text-sm text-muted-foreground border rounded-md px-3 py-2">
                                    No inbounds configured
                                </p>
                            ) : (
                                <Controller
                                    control={control}
                                    name="interconnection_tags"
                                    render={({ field }) => (
                                        <div className="border rounded-md p-3 max-h-48 overflow-y-auto space-y-2">
                                            {inboundTags.map((tag) => (
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
                            <p className="text-xs text-muted-foreground">Inbounds used for the interconnection tunnel</p>
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
