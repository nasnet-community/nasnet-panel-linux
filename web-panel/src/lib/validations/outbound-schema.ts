import { z } from "zod"

export const outboundSchema = z
    .object({
        // allow any non-empty tag (≥2 chars); import/API tags can have dots/colons/spaces
        tag: z.string().trim().min(2, "Tag must be at least 2 characters"),
        remark: z.string().optional().or(z.literal("")),
        protocol: z.string().min(1, "Protocol is required"),
        address: z.string().optional().or(z.literal("")),
        port: z.number().int().min(0).max(65535).optional(),
        network: z.string().optional(),
        security: z.string().optional(),

        // Sub-settings
        tls_settings: z.any().optional(),
        reality_settings: z.any().optional(),
        transport_settings: z.any().optional(),
        sockopt_settings: z.any().optional(),
        freedom_settings: z.any().optional(),
        blackhole_settings: z.any().optional(),
        vmess_settings: z.any().optional(),
        vless_settings: z.any().optional(),
        trojan_settings: z.any().optional(),
        shadowsocks_settings: z.any().optional(),
        wireguard_settings: z.any().optional(),
        http_settings: z.any().optional(),
        socks_settings: z.any().optional(),
        // nullish, not optional: backend sends dns_settings: null for non-DNS
        // outbounds, and .optional() alone would reject that null.
        dns_settings: z.object({
            network: z.enum(["tcp", "udp", ""]).optional(),
            address: z.string().optional().or(z.literal("")),
            port: z.number().int().min(0).max(65535).optional(),
            userLevel: z.number().int().min(0).optional(),
            nonIPQuery: z.enum(["drop", "skip", "reject", ""]).optional(),
            blockTypes: z.array(z.number().int()).optional(),
        }).nullish(),
        loopback_settings: z.any().optional(),
        hysteria_settings: z.any().optional(),
        mux_settings: z.any().optional(),
        proxy_settings: z.any().optional(),
        send_through: z.string().optional().or(z.literal("")),
        finalmask: z.any().optional(),
    })
    .superRefine((data, ctx) => {
        // Protocols that require address + port
        const needsAddress = ["vmess", "vless", "trojan", "shadowsocks", "socks", "http", "hysteria2"]
        if (needsAddress.includes(data.protocol)) {
            if (!data.address) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Address is required for this protocol",
                    path: ["address"],
                })
            }
            if (!data.port || data.port <= 0) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Port is required for this protocol",
                    path: ["port"],
                })
            }
        }

        // VLESS requires UUID
        if (data.protocol === "vless") {
            if (!data.vless_settings?.uuid) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "UUID is required for VLESS outbound",
                    path: ["vless_settings", "uuid"],
                })
            }
        }

        // VMess requires UUID
        if (data.protocol === "vmess") {
            if (!data.vmess_settings?.uuid) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "UUID is required for VMess outbound",
                    path: ["vmess_settings", "uuid"],
                })
            }
        }

        // Trojan requires password
        if (data.protocol === "trojan") {
            if (!data.trojan_settings?.password) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Password is required for Trojan outbound",
                    path: ["trojan_settings", "password"],
                })
            }
        }

        // Shadowsocks requires method and password
        if (data.protocol === "shadowsocks") {
            if (!data.shadowsocks_settings?.method) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Method is required for Shadowsocks outbound",
                    path: ["shadowsocks_settings", "method"],
                })
            }
            if (!data.shadowsocks_settings?.password) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Password is required for Shadowsocks outbound",
                    path: ["shadowsocks_settings", "password"],
                })
            }
        }

        // Reality needs publicKey. serverName optional; xray falls back to dest.
        if (data.security === "reality") {
            const reality = data.reality_settings
            if (!reality?.publicKey) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Public Key is required for Reality outbound",
                    path: ["reality_settings", "publicKey"],
                })
            }
        }

        // Loopback requires inboundTag
        if (data.protocol === "loopback") {
            if (!data.loopback_settings?.inboundTag) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Inbound Tag is required for Loopback outbound",
                    path: ["loopback_settings", "inboundTag"],
                })
            }
        }

        // Hysteria2 requires auth
        if (data.protocol === "hysteria2") {
            if (!data.hysteria_settings?.auth) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Auth is required for Hysteria2 outbound",
                    path: ["hysteria_settings", "auth"],
                })
            }
        }

        // WireGuard requires secretKey and peers
        if (data.protocol === "wireguard") {
            if (!data.wireguard_settings?.secretKey) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Secret Key is required for WireGuard outbound",
                    path: ["wireguard_settings", "secretKey"],
                })
            }
            const peers = data.wireguard_settings?.peers
            if (!peers || peers.length === 0) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "At least one peer is required",
                    path: ["wireguard_settings", "peers"],
                })
            } else {
                peers.forEach((p: { publicKey?: string }, i: number) => {
                    if (!p.publicKey) {
                        ctx.addIssue({
                            code: z.ZodIssueCode.custom,
                            message: `Peer #${i + 1}: Public Key is required`,
                            path: ["wireguard_settings", "peers", i, "publicKey"],
                        })
                    }
                })
            }
        }
    })

export type OutboundFormData = z.infer<typeof outboundSchema>
