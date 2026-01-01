import { z } from "zod"

// Decoded byte length of a base64 / base64url string, or null if invalid.
function base64ByteLength(s: string): number | null {
    try {
        const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/"))
        return bin.length
    } catch {
        return null
    }
}

export const inboundSchema = z
    .object({
        // allow any non-empty tag; import tags can have dots/colons/spaces
        tag: z.string().trim().min(1, "Tag is required"),
        remark: z.string().optional().or(z.literal("")),
        listen: z.string().optional().or(z.literal("")),
        port: z
            .number()
            .int("Port must be an integer")
            .min(1, "Port must be between 1 and 65535")
            .max(65535, "Port must be between 1 and 65535"),
        // free-form; backend reads it independently of `port`
        port_range: z.string().optional().or(z.literal("")),
        protocol: z.enum(["vless", "vmess", "trojan", "shadowsocks", "wireguard", "http", "socks", "mixed", "dokodemo-door", "hysteria2"]),
        network: z.string(),
        security: z.string(),

        // Sub-settings, validated conditionally. z.any() also allows explicit
        // null, which the disable toggles send to clear the backend pointer.
        tls_settings: z.any().optional(),
        reality_settings: z.any().optional(),
        transport_settings: z.any().optional(),
        sniffing_settings: z.any().optional(),
        sockopt_settings: z.any().optional(),
        vless_settings: z.any().optional(),
        vmess_settings: z.any().optional(),
        shadowsocks_settings: z.any().optional(),
        wireguard_settings: z.any().optional(),
        http_settings: z.any().optional(),
        socks_settings: z.any().optional(),
        trojan_settings: z.any().optional(),
        dokodemo_settings: z.any().optional(),
        hysteria_settings: z.any().optional(),
        finalmask: z.any().optional(),
    })
    .superRefine((data, ctx) => {
        // TLS needs at least one cert (serverName is optional in xray).
        // Check here so it shows in-form instead of a 400.
        if (data.security === "tls") {
            const certs = data.tls_settings?.certificates
            if (!Array.isArray(certs) || certs.length === 0) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "TLS requires at least one certificate",
                    path: ["tls_settings", "certificates"],
                })
            }
        }

        // Reality needs serverNames, dest, and a base64url 32-byte privateKey;
        // shortId (if set) must be even-length hex, ≤16 chars.
        if (data.security === "reality") {
            const reality = data.reality_settings
            const serverNames = reality?.serverNames
            if (!Array.isArray(serverNames) || serverNames.filter((s: string) => s?.trim()).length === 0) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "At least one Server Name is required for Reality",
                    path: ["reality_settings", "serverNames"],
                })
            }
            if (!reality?.dest) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Destination is required for Reality",
                    path: ["reality_settings", "dest"],
                })
            }
            if (!reality?.privateKey) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Private Key is required for Reality",
                    path: ["reality_settings", "privateKey"],
                })
            } else if (!/^[A-Za-z0-9_-]{43}$/.test(reality.privateKey)) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Private Key must be a base64url-encoded 32-byte x25519 key",
                    path: ["reality_settings", "privateKey"],
                })
            }
            const shortId: string = reality?.shortId ?? ""
            if (shortId !== "" && !(/^([0-9a-fA-F]{2})*$/.test(shortId) && shortId.length <= 16)) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Short ID must be hex, even length, and ≤16 characters",
                    path: ["reality_settings", "shortId"],
                })
            }
        }

        // VLESS inbound flow. xray-core only accepts "" or "xtls-rprx-vision" on
        // inbounds; "xtls-rprx-vision-udp443" is outbound-only.
        if (data.protocol === "vless") {
            const flow = data.vless_settings?.flow ?? ""
            if (flow !== "" && flow !== "xtls-rprx-vision") {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: flow === "xtls-rprx-vision-udp443"
                        ? "xtls-rprx-vision-udp443 is outbound-only; use xtls-rprx-vision on the inbound"
                        : `unsupported vless flow: ${flow}`,
                    path: ["vless_settings", "flow"],
                })
            }
            if (flow === "xtls-rprx-vision") {
                const isTCPVision = (data.network === "tcp" || data.network === "raw") && (data.security === "tls" || data.security === "reality")
                const isXHTTPVision = data.network === "xhttp"
                if (!isTCPVision && !isXHTTPVision) {
                    ctx.addIssue({
                        code: z.ZodIssueCode.custom,
                        message: "XTLS Vision requires TCP (with TLS/Reality) or XHTTP",
                        path: ["vless_settings", "flow"],
                    })
                }
            }
            validateFallbacks(data.vless_settings?.fallbacks, ctx, "vless_settings")
        }

        if (data.protocol === "trojan") {
            validateFallbacks(data.trojan_settings?.fallbacks, ctx, "trojan_settings")
        }

        // Shadowsocks: method required. 2022-blake3-* needs a base64 key
        // matching the cipher size (16 bytes for aes-128, else 32).
        if (data.protocol === "shadowsocks") {
            const ss = data.shadowsocks_settings
            const method: string = ss?.method ?? ""
            if (!method) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Encryption method is required",
                    path: ["shadowsocks_settings", "method"],
                })
            }
            if (method.startsWith("2022-blake3-")) {
                const want = method.includes("aes-128") ? 16 : 32
                const pw: string = ss?.password ?? ""
                if (!pw) {
                    ctx.addIssue({
                        code: z.ZodIssueCode.custom,
                        message: "2022 methods require a base64 server key (password)",
                        path: ["shadowsocks_settings", "password"],
                    })
                } else if (base64ByteLength(pw) !== want) {
                    ctx.addIssue({
                        code: z.ZodIssueCode.custom,
                        message: `Key must be base64 of exactly ${want} bytes for ${method}`,
                        path: ["shadowsocks_settings", "password"],
                    })
                }
            }
        }

        // WireGuard requires secretKey + single-host CIDR addresses.
        if (data.protocol === "wireguard") {
            if (!data.wireguard_settings?.secretKey) {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: "Secret key is required",
                    path: ["wireguard_settings", "secretKey"],
                })
            }
            const addrs = (data.wireguard_settings?.endpoint as string[] | undefined) ?? []
            addrs.forEach((addr, i) => {
                if (!addr) {
                    ctx.addIssue({
                        code: z.ZodIssueCode.custom,
                        message: "Address is empty",
                        path: ["wireguard_settings", "endpoint", i],
                    })
                    return
                }
                const m = addr.match(/^([0-9a-fA-F:.]+)(?:\/(\d+))?$/)
                if (!m) {
                    ctx.addIssue({
                        code: z.ZodIssueCode.custom,
                        message: `Invalid IP or CIDR: ${addr}`,
                        path: ["wireguard_settings", "endpoint", i],
                    })
                    return
                }
                const ip = m[1]
                const prefix = m[2]
                const isV6 = ip.includes(":")
                if (prefix !== undefined) {
                    const want = isV6 ? 128 : 32
                    if (Number(prefix) !== want) {
                        ctx.addIssue({
                            code: z.ZodIssueCode.custom,
                            message: `Address must be /${want} (single host), got ${addr}`,
                            path: ["wireguard_settings", "endpoint", i],
                        })
                    }
                }
            })
        }

        // SOCKS/HTTP/Mixed/dokodemo-door are raw TCP; TLS is fine, Reality isn't.
        if (["socks", "http", "mixed", "dokodemo-door"].includes(data.protocol)) {
            if (data.network && data.network !== "" && data.network !== "tcp" && data.network !== "raw") {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: `${data.protocol} only supports TCP transport`,
                    path: ["network"],
                })
            }
            if (data.security === "reality") {
                ctx.addIssue({
                    code: z.ZodIssueCode.custom,
                    message: `${data.protocol} does not support Reality`,
                    path: ["security"],
                })
            }
        }
    })

// each fallback needs a dest, a path starting with "/", and xver ≤ 2
function validateFallbacks(
    fallbacks: unknown,
    ctx: z.RefinementCtx,
    settingsKey: string,
) {
    if (!Array.isArray(fallbacks)) return
    fallbacks.forEach((fb, i) => {
        const dest = fb?.dest
        const destEmpty = dest === undefined || dest === null || (typeof dest === "string" && dest.trim() === "")
        if (destEmpty) {
            ctx.addIssue({
                code: z.ZodIssueCode.custom,
                message: "Fallback destination is required",
                path: [settingsKey, "fallbacks", i, "dest"],
            })
        }
        if (typeof fb?.path === "string" && fb.path !== "" && !fb.path.startsWith("/")) {
            ctx.addIssue({
                code: z.ZodIssueCode.custom,
                message: 'Fallback path must start with "/"',
                path: [settingsKey, "fallbacks", i, "path"],
            })
        }
        if (typeof fb?.xver === "number" && fb.xver > 2) {
            ctx.addIssue({
                code: z.ZodIssueCode.custom,
                message: "xver only accepts 0, 1, 2",
                path: [settingsKey, "fallbacks", i, "xver"],
            })
        }
    })
}

export type InboundFormData = z.infer<typeof inboundSchema>
