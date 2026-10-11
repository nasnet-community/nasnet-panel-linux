import { describe, expect, it } from "vitest"
import { coreFormSchema } from "./core-schema"

const base = { node_id: 1, protocol: "mtproto" as const, port: 443, egress_mode: "hub" as const }

function issues(data: Record<string, unknown>): { path: string; message: string }[] {
    const r = coreFormSchema.safeParse(data)
    return r.success ? [] : r.error.issues.map((i) => ({ path: i.path.join("."), message: i.message }))
}

describe("MTProto node (hub egress) tunnels", () => {
    it("accepts MTProto with a cover site", () => {
        expect(issues({ ...base, mtproto_settings: { tls_domain: "www.example.ir" } })).toEqual([])
    })

    it("needs a cover site: the node presents it on its link too", () => {
        const got = issues({ ...base, mtproto_settings: {} })
        expect(got).toEqual([{ path: "mtproto_settings.tls_domain", message: "An MTProto node's tunnel needs a cover site." }])
    })

    it("needs the main cover site even when extras are given", () => {
        const got = issues({ ...base, mtproto_settings: { tls_domains_extra: ["a.example.ir"] } })
        expect(got).toEqual([{ path: "mtproto_settings.tls_domain", message: "An MTProto node's tunnel needs a cover site." }])
    })

    it("refuses the middle proxy and an ad tag, which would reach Telegram directly", () => {
        const proxy = issues({ ...base, mtproto_settings: { tls_domain: "a.example.ir", middle_proxy: true } })
        expect(proxy.map((i) => i.path)).toContain("egress_mode")
        const tag = issues({ ...base, mtproto_settings: { tls_domain: "a.example.ir", middle_proxy: true, adtag_default: "ab" } })
        expect(tag.some((i) => i.message.includes("MTProto node"))).toBe(true)
    })

    it("refuses any protocol but MTProto", () => {
        const got = issues({ node_id: 1, protocol: "ssh", port: 2222, egress_mode: "hub", ssh_settings: {} })
        expect(got).toEqual([{ path: "egress_mode", message: "An MTProto node runs only MTProto tunnels." }])
    })
})

describe("the other egress modes are unchanged", () => {
    it("keeps the Xray middle-proxy rule", () => {
        const got = issues({ ...base, egress_mode: "xray", mtproto_settings: { tls_domain: "a.example.ir", middle_proxy: true } })
        expect(got.map((i) => i.message)).toContain("The middle proxy and ad tag aren't available with Xray egress.")
    })

    it("does not demand a domain on a full node's direct tunnel", () => {
        expect(issues({ ...base, egress_mode: "direct", mtproto_settings: {} })).toEqual([])
    })
})
