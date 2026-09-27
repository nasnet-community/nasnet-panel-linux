/**
 * Config Link Parser for Xray protocols
 * Supports: vless://, vmess://, trojan://, ss://, socks://, http://,
 *           wireguard:// (wg://), hysteria2:// (hy2://), hysteria://
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
            case "wireguard":
            case "wg":
                return parseWireGuard(trimmed)
            case "hysteria2":
            case "hy2":
            case "hysteria":
                return parseHysteria(trimmed, scheme)
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
            // Check if userinfo is base64 encoded. Decode percent-escapes first:
            // `new URL` re-encodes base64 padding as %3D, which makes atob throw
            // and dumps the whole blob into `method`.
            try {
                const decoded = atob(padBase64(safeDecode(url.username)))
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

/**
 * Parse WireGuard links
 * Format: wireguard://privateKey@peerHost:peerPort?publickey=..&address=10.0.0.2/32&mtu=1420&reserved=0,0,0#remark
 *
 * There is no single WireGuard URI standard, so key material is accepted from
 * either the userinfo slot or a query param, and param names are matched
 * case/separator-insensitively (publicKey, publickey, public_key, ...).
 */
function parseWireGuard(link: string): ParseResult {
    try {
        const parts = splitLink(link)
        if (!parts) {
            return { success: false, error: "Invalid WireGuard link format" }
        }
        const param = paramLookup(parts.query)

        const secretKey = (parts.userinfo ? safeDecode(parts.userinfo) : "") ||
            param("privatekey", "secretkey", "private_key")
        if (!secretKey) {
            return { success: false, error: "Missing private key in WireGuard link" }
        }

        const publicKey = param("publickey", "public_key", "peer_public_key", "pbk")
        if (!publicKey) {
            return { success: false, error: "Missing peer public key in WireGuard link" }
        }

        // Peer endpoint: from the authority, or from an explicit param when the
        // link carries no host (some generators emit wireguard://key@?endpoint=..).
        let peerEndpoint = ""
        if (parts.host) {
            peerEndpoint = `${parts.host}:${parts.port || "2408"}`
        } else {
            peerEndpoint = param("endpoint", "peer_endpoint")
        }
        if (!peerEndpoint) {
            return { success: false, error: "Missing peer endpoint in WireGuard link" }
        }

        const remark = parts.hash ? safeDecode(parts.hash) : "Imported WireGuard"
        const allowed = splitList(param("allowedips", "allowed_ips"))
        const keepAlive = parseInt(param("keepalive", "persistent_keepalive"))
        const mtu = parseInt(param("mtu"))
        const preSharedKey = param("presharedkey", "pre_shared_key", "psk")
        const reserved = parseReserved(param("reserved"))

        const settings: NonNullable<Outbound["wireguard_settings"]> = {
            secretKey,
            // Local tunnel addresses (Xray calls these the interface addresses).
            // Left empty when the link omits them so the user fills in real values
            // rather than inheriting a wrong guess.
            endpoint: splitList(param("address", "addresses", "ip", "local_address")),
            peers: [{
                publicKey,
                endpoint: peerEndpoint,
                allowedIps: allowed.length ? allowed : ["0.0.0.0/0", "::/0"],
                ...(preSharedKey ? { preSharedKey } : {}),
                ...(Number.isFinite(keepAlive) && keepAlive > 0 ? { keepAlive } : {}),
            }],
        }
        if (Number.isFinite(mtu) && mtu > 0) settings.mtu = mtu
        if (reserved) settings.reserved = reserved

        return {
            success: true,
            outbound: {
                tag: sanitizeTag(remark),
                protocol: "wireguard",
                remark,
                network: "",
                security: "",
                wireguard_settings: settings,
            },
        }
    } catch (e) {
        return { success: false, error: `Invalid WireGuard link: ${e instanceof Error ? e.message : "Parse error"}` }
    }
}

/**
 * Parse Hysteria2 links (and legacy hysteria:// v1, mapped onto hysteria2)
 * Format: hysteria2://auth@host:port?sni=example.com&insecure=1&up=100&down=200#remark
 *
 * obfs / obfs-password and port hopping (mport) have no representation in the
 * outbound model, so they are dropped rather than silently mis-mapped.
 */
function parseHysteria(link: string, scheme: string): ParseResult {
    try {
        const parts = splitLink(link)
        if (!parts || !parts.host) {
            return { success: false, error: "Invalid Hysteria2 link - missing host" }
        }
        const param = paramLookup(parts.query)

        // Per the hysteria2 URI scheme the whole userinfo is the auth string,
        // so a "user:pass" form is kept intact. v1 carries it as a query param.
        const auth = (parts.userinfo ? safeDecode(parts.userinfo) : "") ||
            param("auth", "auth_str")
        if (!auth) {
            return { success: false, error: "Missing auth in Hysteria2 link" }
        }

        const port = parseInt(parts.port) || 443
        const label = scheme === "hysteria" ? "Imported Hysteria" : "Imported Hysteria2"
        const remark = parts.hash ? safeDecode(parts.hash) : label
        const insecure = param("insecure", "allowinsecure")
        const address = stripBrackets(parts.host)

        const outbound: Partial<Outbound> = {
            tag: sanitizeTag(remark),
            protocol: "hysteria2",
            address,
            port,
            remark,
            // Hysteria2 is QUIC-native: no stream transport, TLS always on.
            network: "",
            security: "tls",
            tls_settings: {
                serverName: param("sni", "peer") || address,
                alpn: parseALPN(param("alpn") || null),
                allowInsecure: insecure === "1" || insecure.toLowerCase() === "true",
            },
            hysteria_settings: { auth },
        }

        const up = param("up", "upmbps", "up_mbps")
        const down = param("down", "downmbps", "down_mbps")
        if (up) outbound.hysteria_settings!.up = up
        if (down) outbound.hysteria_settings!.down = down
        const congestion = param("congestion", "congestion_control")
        if (congestion) outbound.hysteria_settings!.congestion = congestion

        return { success: true, outbound }
    } catch (e) {
        return { success: false, error: `Invalid Hysteria2 link: ${e instanceof Error ? e.message : "Parse error"}` }
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
            // HTTP obfuscation carries the request path and Host header.
            if (transport.headerType === "http") {
                transport.path = params.get("path") || ""
                transport.host = params.get("host") || ""
            }
            break
    }

    return transport
}

interface LinkParts {
    scheme: string
    userinfo: string
    host: string
    port: string
    path: string
    query: string
    hash: string
}

// Manual split instead of `new URL`: WireGuard private keys are standard
// base64 and may contain "+", "/" or "=", which the URL parser treats as the
// end of the authority (or re-encodes), corrupting the key. Splitting on the
// LAST "@" before the query/fragment keeps such keys intact.
function splitLink(link: string): LinkParts | null {
    const schemeEnd = link.indexOf("://")
    if (schemeEnd <= 0) return null
    const scheme = link.slice(0, schemeEnd).toLowerCase()
    if (!/^[a-z][a-z0-9+.-]*$/.test(scheme)) return null

    let rest = link.slice(schemeEnd + 3)

    // Trailing fragment, then query — both may contain "@" and "/", so peel
    // them off before touching the authority.
    let hash = ""
    const hashIdx = rest.indexOf("#")
    if (hashIdx !== -1) {
        hash = rest.slice(hashIdx + 1)
        rest = rest.slice(0, hashIdx)
    }
    let query = ""
    const qIdx = rest.indexOf("?")
    if (qIdx !== -1) {
        query = rest.slice(qIdx + 1)
        rest = rest.slice(0, qIdx)
    }

    let userinfo = ""
    const atIdx = rest.lastIndexOf("@")
    if (atIdx !== -1) {
        userinfo = rest.slice(0, atIdx)
        rest = rest.slice(atIdx + 1)
    }

    let path = ""
    const slashIdx = rest.indexOf("/")
    if (slashIdx !== -1) {
        path = rest.slice(slashIdx)
        rest = rest.slice(0, slashIdx)
    }

    // host[:port], with IPv6 literals kept bracketed
    let host = rest
    let port = ""
    if (host.startsWith("[")) {
        const close = host.indexOf("]")
        if (close === -1) return null
        const after = host.slice(close + 1)
        host = host.slice(0, close + 1)
        if (after.startsWith(":")) port = after.slice(1)
    } else {
        const colon = host.lastIndexOf(":")
        if (colon !== -1) {
            port = host.slice(colon + 1)
            host = host.slice(0, colon)
        }
    }
    if (port && (!/^\d+$/.test(port) || Number(port) > 65535)) return null

    return { scheme, userinfo, host, port, path, query, hash }
}

// Query params, looked up ignoring case and -/_ separators, since link
// generators disagree on publicKey vs publickey vs public_key.
function paramLookup(query: string): (...names: string[]) => string {
    const norm = (s: string) => s.toLowerCase().replace(/[-_]/g, "")
    const map = new Map<string, string>()
    for (const [k, v] of new URLSearchParams(query)) {
        const key = norm(k)
        if (!map.has(key)) map.set(key, v)
    }
    return (...names: string[]): string => {
        for (const n of names) {
            const v = map.get(norm(n))
            if (v) return v
        }
        return ""
    }
}

function safeDecode(s: string): string {
    try {
        return decodeURIComponent(s)
    } catch {
        return s
    }
}

function splitList(value: string): string[] {
    if (!value) return []
    return value
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
}

function stripBrackets(host: string): string {
    return host.startsWith("[") && host.endsWith("]") ? host.slice(1, -1) : host
}

// WARP-style reserved: either "0,0,0" or a 3-byte base64 blob.
function parseReserved(value: string): number[] | undefined {
    if (!value) return undefined
    const csv = splitList(value)
    if (csv.length && csv.every((s) => /^\d+$/.test(s))) {
        return csv.map(Number)
    }
    try {
        const raw = atob(padBase64(value.replace(/-/g, "+").replace(/_/g, "/")))
        return Array.from(raw, (c) => c.charCodeAt(0))
    } catch {
        return undefined
    }
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
    const schemes = [
        "vless://", "vmess://", "trojan://", "ss://", "socks://",
        "wireguard://", "wg://", "hysteria2://", "hy2://", "hysteria://",
    ]
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
        if (scheme === "wg") return "wireguard"
        if (scheme === "hy2" || scheme === "hysteria") return "hysteria2"
        return scheme
    }
    return null
}
