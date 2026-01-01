import { z } from "zod"

export const reverseProxySchema = z.object({
    type: z.enum(["bridge", "portal"]),
    tag: z.string().min(1, "Tag is required").max(100),
    domain: z.string().min(1, "Domain is required").max(200),
    interconnection_tag: z.string(),
    outbound_tag: z.string(),
    interconnection_tags: z.array(z.string()),
    inbound_tags: z.array(z.string()),
}).superRefine((data, ctx) => {
    if (data.type === "bridge") {
        if (!data.interconnection_tag) {
            ctx.addIssue({
                code: z.ZodIssueCode.custom,
                message: "Interconnection outbound is required for Bridge",
                path: ["interconnection_tag"],
            })
        }
        if (!data.outbound_tag) {
            ctx.addIssue({
                code: z.ZodIssueCode.custom,
                message: "Outbound is required for Bridge",
                path: ["outbound_tag"],
            })
        }
    } else {
        if (!data.interconnection_tags || data.interconnection_tags.length === 0) {
            ctx.addIssue({
                code: z.ZodIssueCode.custom,
                message: "At least one interconnection inbound is required for Portal",
                path: ["interconnection_tags"],
            })
        }
        if (!data.inbound_tags || data.inbound_tags.length === 0) {
            ctx.addIssue({
                code: z.ZodIssueCode.custom,
                message: "At least one inbound is required for Portal",
                path: ["inbound_tags"],
            })
        }
    }
})

export type ReverseProxyFormData = z.infer<typeof reverseProxySchema>
