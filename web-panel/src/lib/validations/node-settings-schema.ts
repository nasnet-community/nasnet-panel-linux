import { z } from "zod"

export const nodeGeneralSchema = z.object({
    name: z.string().min(1, "Name is required"),
    ip: z
        .string()
        .min(1, "IP address is required")
        .regex(
            /^(\d{1,3}\.){3}\d{1,3}$/,
            "Must be a valid IPv4 address"
        )
        .refine(
            (ip) =>
                ip.split(".").every((octet) => {
                    const n = parseInt(octet, 10)
                    return n >= 0 && n <= 255
                }),
            "Each octet must be between 0 and 255"
        ),
    country_code: z
        .string()
        .max(2, "Country code must be 2 characters (e.g. DE, US)")
        .optional()
        .or(z.literal("")),
    datacenter: z.string().optional().or(z.literal("")),
    is_active: z.boolean(),
})

export type NodeGeneralFormData = z.infer<typeof nodeGeneralSchema>

export const nodeAgentSchema = z.object({
    agent_port: z
        .number()
        .int()
        .min(1, "Port must be between 1 and 65535")
        .max(65535, "Port must be between 1 and 65535"),
    api_port: z
        .number()
        .int()
        .min(1, "Port must be between 1 and 65535")
        .max(65535, "Port must be between 1 and 65535"),
    is_stealth: z.boolean(),
    is_persistent_stealth: z.boolean(),
})

export type NodeAgentFormData = z.infer<typeof nodeAgentSchema>

export const nodeXrayLogSchema = z.object({
    loglevel: z.enum(["debug", "info", "warning", "error", "none"]),
    access: z.string().optional().or(z.literal("")),
    error: z.string().optional().or(z.literal("")),
    dnsLog: z.boolean(),
})

export type NodeXrayLogFormData = z.infer<typeof nodeXrayLogSchema>

export const nodeBandwidthSchema = z.object({
    bandwidth_enabled: z.boolean(),
    bandwidth_interface: z.string().min(1, "Interface name is required").optional().or(z.literal("")),
    bandwidth_total_bw: z
        .number()
        .int()
        .min(1, "Must be at least 1 Mbps")
        .max(100000, "Must be at most 100000 Mbps"),
})

export type NodeBandwidthFormData = z.infer<typeof nodeBandwidthSchema>

export const nodeStarlinkSchema = z.object({
    starlink_enabled: z.boolean(),
    starlink_dish_address: z.string().optional().or(z.literal("")),
})

export type NodeStarlinkFormData = z.infer<typeof nodeStarlinkSchema>

export const nodeCrashRecoverySchema = z.object({
    crash_recovery_enabled: z.boolean(),
    crash_recovery_command: z.string().max(1000).optional().or(z.literal("")),
    crash_recovery_command_timeout: z.number().int().min(5, "Min 5 seconds").max(300, "Max 300 seconds"),
    crash_recovery_cooldown: z.number().int().min(1, "Min 1 minute").max(1440, "Max 1440 minutes"),
    crash_recovery_max_attempts: z.number().int().min(0, "Min 0 (unlimited)").max(100, "Max 100"),
})

export type NodeCrashRecoveryFormData = z.infer<typeof nodeCrashRecoverySchema>

export const nodeSSHSchema = z.object({
    ssh_enabled: z.boolean(),
    ssh_port: z
        .number()
        .int()
        .min(1, "Port must be between 1 and 65535")
        .max(65535, "Port must be between 1 and 65535"),
})

export type NodeSSHFormData = z.infer<typeof nodeSSHSchema>

// Unified schema combining all sections
export const nodeSettingsSchema = nodeGeneralSchema
    .merge(nodeAgentSchema)
    .merge(nodeXrayLogSchema.omit({ access: true, error: true }).extend({
        log_access: z.string().optional().or(z.literal("")),
        log_error: z.string().optional().or(z.literal("")),
        enable_access_log: z.boolean(),
    }))
    .merge(nodeBandwidthSchema)
    .merge(nodeStarlinkSchema)
    .merge(nodeCrashRecoverySchema)
    .merge(nodeSSHSchema)

export type NodeSettingsFormData = z.infer<typeof nodeSettingsSchema>
