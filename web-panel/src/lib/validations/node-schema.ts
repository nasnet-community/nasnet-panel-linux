import { z } from "zod"

const ipRegex = /^(\d{1,3}\.){3}\d{1,3}$|^([a-fA-F0-9:]+)$|^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$/

export const nodeSchema = z.object({
    name: z.string().min(1, "Name is required").max(100, "Name too long"),
    ip: z
        .string()
        .min(1, "IP address is required")
        .regex(ipRegex, "Invalid IP address or hostname"),
    api_port: z.string().optional().default(""),
    agent_port: z.string().min(1, "Agent Port is required").default("9090"),
    country: z.string().optional().default(""),
    datacenter: z.string().optional().default(""),
    is_stealth: z.boolean().optional().default(false),
    is_persistent_stealth: z.boolean().optional().default(false),
})

export type NodeFormData = z.infer<typeof nodeSchema>
