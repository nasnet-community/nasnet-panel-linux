import { describe, it, expect } from "vitest"
import { inboundSchema } from "./inbound-schema"

// A minimal base inbound; override per-case.
function mk(over: Record<string, unknown> = {}) {
    return {
        tag: "in-1",
        port: 443,
        protocol: "vless",
        network: "tcp",
        security: "none",
        ...over,
    }
}

const KEY32 = "A".repeat(43) // base64url of 32 zero bytes
const oneCert = [{ certificateFile: "/c", keyFile: "/k" }]

function ok(data: Record<string, unknown>) {
    return inboundSchema.safeParse(data).success
}
function errMsgs(data: Record<string, unknown>): string[] {
    const r = inboundSchema.safeParse(data)
    return r.success ? [] : r.error.issues.map((i) => i.message)
}

describe("inbound schema", () => {
    it("relaxation: tag with dots/colons is allowed", () => {
        expect(ok(mk({ tag: "vless.tcp:reality", vless_settings: {} }))).toBe(true)
    })

    it("relaxation: TLS without serverName is allowed (cert only)", () => {
        expect(ok(mk({ security: "tls", vless_settings: {}, tls_settings: { certificates: oneCert } }))).toBe(true)
    })

    it("TLS without any certificate is rejected", () => {
        expect(errMsgs(mk({ security: "tls", vless_settings: {}, tls_settings: { serverName: "e.com" } })))
            .toContain("TLS requires at least one certificate")
    })

    it("relaxation: http + tls is allowed", () => {
        expect(ok(mk({ protocol: "http", security: "tls", tls_settings: { certificates: oneCert } }))).toBe(true)
    })

    it("http + reality is rejected", () => {
        expect(ok(mk({ protocol: "http", security: "reality" }))).toBe(false)
    })

    it("reality requires serverNames + dest + privateKey", () => {
        const msgs = errMsgs(mk({
            security: "reality",
            vless_settings: { flow: "xtls-rprx-vision" },
            reality_settings: { privateKey: KEY32 },
        }))
        expect(msgs.some((m) => m.includes("Server Name"))).toBe(true)
        expect(msgs.some((m) => m.includes("Destination"))).toBe(true)
    })

    it("reality valid config passes", () => {
        expect(ok(mk({
            security: "reality",
            vless_settings: { flow: "xtls-rprx-vision" },
            reality_settings: { privateKey: KEY32, serverNames: ["e.com"], dest: "e.com:443", shortId: "01ab" },
        }))).toBe(true)
    })

    it("reality odd-length shortId is rejected", () => {
        expect(errMsgs(mk({
            security: "reality",
            vless_settings: { flow: "xtls-rprx-vision" },
            reality_settings: { privateKey: KEY32, serverNames: ["e.com"], dest: "e.com:443", shortId: "abc" },
        })).some((m) => m.includes("Short ID"))).toBe(true)
    })

    it("vless flow xtls-rprx-vision-udp443 is rejected on inbound", () => {
        expect(ok(mk({ vless_settings: { flow: "xtls-rprx-vision-udp443" }, security: "tls", tls_settings: { certificates: oneCert } }))).toBe(false)
    })

    it("shadowsocks 2022 wrong-length key is rejected", () => {
        // aes-256 needs 32 bytes; give a 16-byte key
        expect(errMsgs(mk({
            protocol: "shadowsocks", network: "tcp", security: "none",
            shadowsocks_settings: { method: "2022-blake3-aes-256-gcm", password: btoa("0123456789012345") },
        })).some((m) => m.includes("32 bytes"))).toBe(true)
    })

    it("shadowsocks 2022 correct-length key passes", () => {
        expect(ok(mk({
            protocol: "shadowsocks", network: "tcp", security: "none",
            shadowsocks_settings: { method: "2022-blake3-aes-128-gcm", password: btoa("0123456789012345") },
        }))).toBe(true)
    })

    it("fallback with empty dest is rejected", () => {
        expect(errMsgs(mk({
            vless_settings: { fallbacks: [{ dest: "" }] },
        })).some((m) => m.includes("Fallback destination"))).toBe(true)
    })

    it("wireguard requires secretKey", () => {
        expect(errMsgs(mk({
            protocol: "wireguard", network: "tcp", security: "none",
            wireguard_settings: { endpoint: ["10.0.0.1/32"] },
        })).some((m) => m.includes("Secret key"))).toBe(true)
    })
})
