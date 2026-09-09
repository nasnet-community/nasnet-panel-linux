import { z } from "zod"

export const nodeGeneralSchema = z.object({
    name: z.string().trim().min(1, "Name is required").max(255, "Use at most 255 characters"),
    ip: z.union([z.ipv4(), z.ipv6()], { error: "Enter a valid IPv4 or IPv6 address" }),
    country_code: z
        .string()
        .regex(/^(?:[A-Za-z]{2})?$/, "Use a 2-letter country code, such as DE")
        .optional()
        .or(z.literal("")),
    datacenter: z.string().max(255).optional().or(z.literal("")),
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
    bandwidth_total_bw: z.number().int(),
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
    crash_recovery_command_timeout: z.number().int(),
    crash_recovery_cooldown: z.number().int(),
    crash_recovery_max_attempts: z.number().int(),
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
    .extend({
        maintenance_mode: z.boolean(),
        maintenance_message: z.string().max(2000, "Use at most 2000 characters"),
    })
    .superRefine((values, ctx) => {
        const issue = (field: string, message: string) => ctx.addIssue({ code: "custom", path: [field], message })
        if (values.crash_recovery_enabled) {
            if (values.crash_recovery_command_timeout < 5 || values.crash_recovery_command_timeout > 300) issue("crash_recovery_command_timeout", "Use 5 to 300 seconds")
            if (values.crash_recovery_cooldown < 1 || values.crash_recovery_cooldown > 1440) issue("crash_recovery_cooldown", "Use 1 to 1440 minutes")
            if (values.crash_recovery_max_attempts < 0 || values.crash_recovery_max_attempts > 100) issue("crash_recovery_max_attempts", "Use 0 to 100 attempts (0 means unlimited)")
        }
        if (values.starlink_enabled) {
            const address = values.starlink_dish_address || ""
            const match = address.match(/^(?:\[([a-fA-F0-9:]+)\]|([a-zA-Z0-9.-]+)):(\d+)$/)
            if (!match || Number(match[3]) < 1 || Number(match[3]) > 65535) {
                issue("starlink_dish_address", "Enter a host and port, such as 192.168.100.1:9200")
            }
        }
        if (values.crash_recovery_enabled && !values.crash_recovery_command?.trim()) {
            issue("crash_recovery_command", "Enter the command to run during recovery")
        }
    })

export type NodeSettingsFormData = z.infer<typeof nodeSettingsSchema>
