/**
 * Config Link Parser for Xray protocols
 * Supports: vless://, vmess://, trojan://, ss://, socks://, http://
 */

import type { Outbound } from "@/lib/types"

export interface ParseResult {
    success: boolean
    outbound?: Partial<Outbound>
    error?: string
}

/**
 * Parse a config link string into an Outbound configuration
 */
export function parseConfigLink(link: string): ParseResult {
    const trimmed = link.trim()

    if (!trimmed) {
        return { success: false, error: "Empty link" }
    }

    try {
        // Determine protocol from scheme
        const colonIndex = trimmed.indexOf("://")
        if (colonIndex === -1) {
            return { success: false, error: "Invalid link format - missing ://" }
        }

        const scheme = trimmed.substring(0, colonIndex).toLowerCase()

        switch (scheme) {
            case "vless":
            case "trojan":
                return parseVlessTrojan(trimmed, scheme)
            case "vmess":
                return parseVmess(trimmed)
            case "ss":
                return parseShadowsocks(trimmed)
            case "socks":
            case "http":
                return parseSocksHttp(trimmed, scheme)
            default:
                return { success: false, error: `Unsupported protocol: ${scheme}` }
        }
    } catch (e) {
        return { success: false, error: `Parse error: ${e instanceof Error ? e.message : "Unknown error"}` }
    }
}

/**
 * Parse VLESS and Trojan links
 * Format: vless://uuid@host:port?params#remark
 */
function parseVlessTrojan(link: string, protocol: string): ParseResult {
    try {
        const url = new URL(link)
        const params = url.searchParams

        // Extract UUID/Password from userinfo
        const auth = decodeURIComponent(url.username)
        if (!auth) {
            return { success: false, error: "Missing UUID/password in link" }
        }

        const port = parseInt(url.port) || 443
        const remark = url.hash ? decodeURIComponent(url.hash.slice(1)) : `Imported ${protocol.toUpperCase()}`

        const outbound: Partial<Outbound> = {
            tag: sanitizeTag(remark),
            protocol,
            address: url.hostname,
            port,
            remark,
            network: params.get("type") || "tcp",
            security: params.get("security") || "none",
        }

        // Protocol-specific settings
        if (protocol === "vless") {
            outbound.vless_settings = {
                uuid: auth,
                encryption: params.get("encryption") || "none",
                flow: params.get("flow") || "",
            }
        } else {
            // Trojan
            outbound.trojan_settings = {
                password: auth,
            }
        }

        // TLS Settings
        if (outbound.security === "tls") {
            outbound.tls_settings = {
                serverName: params.get("sni") || "",
                fingerprint: params.get("fp") || "chrome",
                alpn: parseALPN(params.get("alpn")),
            }
        } else if (outbound.security === "reality") {
            outbound.reality_settings = {
                serverNames: params.get("sni") ? [params.get("sni")!] : [],
                fingerprint: params.get("fp") || "chrome",
                publicKey: params.get("pbk") || "",
                shortId: params.get("sid") || "",
                spiderX: params.get("spiderx") || params.get("spx") || "",
            }
        }

        // Transport Settings
        outbound.transport_settings = parseTransportParams(outbound.network || "tcp", params)

        return { success: true, outbound }
    } catch (e) {
        return { success: false, error: `Invalid ${protocol} link: ${e instanceof Error ? e.message : "Parse error"}` }
    }
}

/**
 * Parse VMess links (base64 encoded JSON)
 * Format: vmess://base64(json)
 */
function parseVmess(link: string): ParseResult {
    try {
        const b64 = link.replace(/^vmess:\/\//i, "")
        const decoded = atob(padBase64(b64))
        const json = JSON.parse(decoded)

        const port = typeof json.port === "string" ? parseInt(json.port) : json.port || 443
        const alterId = typeof json.aid === "string" ? parseInt(json.aid) : json.aid || 0
        const remark = json.ps || "Imported VMess"

        const outbound: Partial<Outbound> = {
            tag: sanitizeTag(remark),
            protocol: "vmess",
            address: json.add,
            port,
            remark,
            network: json.net || "tcp",
            security: json.tls || "none",
            vmess_settings: {
                uuid: json.id,
                alterId,
                security: json.scy || "auto",
            },
        }

        // TLS Settings
        if (json.tls === "tls") {
            outbound.tls_settings = {
                serverName: json.sni || "",
                fingerprint: json.fp || "chrome",
                alpn: parseALPN(json.alpn),
            }
        } else if (json.tls === "reality") {
            outbound.reality_settings = {
                serverNames: json.sni ? [json.sni] : [],
                fingerprint: json.fp || "chrome",
                publicKey: json.pbk || "",
                shortId: json.sid || "",
            }
        }

        // Transport Settings
        outbound.transport_settings = {
            path: json.path || "",
            host: json.host || "",
            serviceName: json.net === "grpc" ? json.path : "",
            headerType: json.type || "",
        }

        return { success: true, outbound }
    } catch (e) {
        return { success: false, error: `Invalid VMess link: ${e instanceof Error ? e.message : "Base64/JSON decode error"}` }
    }
}

/**
 * Parse Shadowsocks links
 * Format: ss://base64(method:password)@host:port#remark
 * Or: ss://method:password@host:port#remark
 */
function parseShadowsocks(link: string): ParseResult {
    try {
        // Try standard URL format first
        let url: URL
        try {
            url = new URL(link)
        } catch {
            // Try to decode the userinfo part
            const match = link.match(/^ss:\/\/([^@#]+)@([^:#]+):(\d+)(#.*)?$/i)
            if (match) {
                const decoded = atob(padBase64(match[1]))
                const [method, password] = decoded.split(":")
                const remark = match[4] ? decodeURIComponent(match[4].slice(1)) : "Imported SS"

                return {
                    success: true,
                    outbound: {
                        tag: sanitizeTag(remark),
                        protocol: "shadowsocks",
                        address: match[2],
                        port: parseInt(match[3]),
                        remark,
                        security: "none",
                        shadowsocks_settings: {
                            method,
                            password,
                            network: "tcp,udp",
                        },
                    },
                }
            }
            return { success: false, error: "Invalid Shadowsocks link format" }
        }

        // Standard URL format
        let method = ""
        let password = ""

        if (url.username) {
            // Check if userinfo is base64 encoded
            try {
                const decoded = atob(padBase64(url.username))
                if (decoded.includes(":")) {
                    [method, password] = decoded.split(":")
                } else {
                    method = decodeURIComponent(url.username)
                    password = decodeURIComponent(url.password || "")
                }
            } catch {
                method = decodeURIComponent(url.username)
                password = decodeURIComponent(url.password || "")
            }
        }

        const remark = url.hash ? decodeURIComponent(url.hash.slice(1)) : "Imported SS"

        return {
            success: true,
            outbound: {
                tag: sanitizeTag(remark),
                protocol: "shadowsocks",
                address: url.hostname,
                port: parseInt(url.port) || 443,
                remark,
                security: "none",
                shadowsocks_settings: {
                    method,
                    password,
                    network: "tcp,udp",
                },
            },
        }
    } catch (e) {
        return { success: false, error: `Invalid Shadowsocks link: ${e instanceof Error ? e.message : "Parse error"}` }
    }
}

/**
 * Parse SOCKS and HTTP proxy links
 * Format: socks://user:pass@host:port#remark
 */
function parseSocksHttp(link: string, protocol: string): ParseResult {
    try {
        const url = new URL(link)
        const remark = url.hash ? decodeURIComponent(url.hash.slice(1)) : `Imported ${protocol.toUpperCase()}`
        const user = url.username ? decodeURIComponent(url.username) : ""
        const pass = url.password ? decodeURIComponent(url.password) : ""

        const outbound: Partial<Outbound> = {
            tag: sanitizeTag(remark),
            protocol,
            address: url.hostname,
            port: parseInt(url.port) || (protocol === "http" ? 80 : 1080),
            remark,
            security: "none",
        }

        if (protocol === "socks") {
            outbound.socks_settings = {
                auth: user ? "password" : "noauth",
                accounts: user ? [{ user, pass }] : [],
            }
        } else {
            outbound.http_settings = {
                accounts: user ? [{ user, pass }] : [],
            }
        }

        return { success: true, outbound }
    } catch (e) {
        return { success: false, error: `Invalid ${protocol} link: ${e instanceof Error ? e.message : "Parse error"}` }
    }
}

// ============ Helper Functions ============

function parseTransportParams(network: string, params: URLSearchParams): Outbound["transport_settings"] {
    const transport: Outbound["transport_settings"] = {}

    switch (network) {
        case "ws":
            transport.path = params.get("path") || "/"
            transport.host = params.get("host") || ""
            break
        case "grpc":
            transport.serviceName = params.get("serviceName") || ""
            transport.mode = params.get("mode") || ""
            break
        case "h2":
        case "http":
            transport.path = params.get("path") || "/"
            transport.host = params.get("host") || ""
            break
        case "xhttp":
        case "splithttp":
            transport.path = params.get("path") || "/"
            transport.host = params.get("host") || ""
            transport.mode = params.get("mode") || ""
            break
        case "httpupgrade":
            transport.path = params.get("path") || "/"
            transport.host = params.get("host") || ""
            break
        case "tcp":
            transport.headerType = params.get("headerType") || ""
            break
    }

    return transport
}

function parseALPN(alpn: string | null): string[] | undefined {
    if (!alpn) return undefined
    return alpn
        .replace(/%2C/gi, ",")
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
}

function padBase64(str: string): string {
    const pad = str.length % 4
    if (pad) {
        return str + "=".repeat(4 - pad)
    }
    return str
}

function sanitizeTag(remark: string): string {
    // Create a valid tag from the remark
    return remark
        .toLowerCase()
        .replace(/[^a-z0-9\-_]/g, "-")
        .replace(/-+/g, "-")
        .replace(/^-|-$/g, "")
        .slice(0, 50) || "imported"
}

/**
 * Detect if a string is a config link. Only proxy-specific schemes match;
 * plain http:// URLs are excluded so a pasted page URL isn't mistaken for
 * an HTTP proxy outbound config.
 */
export function isConfigLink(text: string): boolean {
    const schemes = ["vless://", "vmess://", "trojan://", "ss://", "socks://"]
    const trimmed = text.trim().toLowerCase()
    return schemes.some((s) => trimmed.startsWith(s))
}

/**
 * Get protocol from a link
 */
export function getProtocolFromLink(link: string): string | null {
    const match = link.match(/^(\w+):\/\//i)
    if (match) {
        const scheme = match[1].toLowerCase()
        if (scheme === "ss") return "shadowsocks"
        return scheme
    }
    return null
}
