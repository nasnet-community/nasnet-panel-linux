import { describe, expect, it } from "vitest"
import { coreFormSchema } from "./core-schema"

const base = { node_id: 1, protocol: "mtproto" as const, port: 443 }

function issues(data: Record<string, unknown>): { path: string; message: string }[] {
    const r = coreFormSchema.safeParse(data)
    return r.success ? [] : r.error.issues.map((i) => ({ path: i.path.join("."), message: i.message }))
}

describe("MTProto tunnel egress", () => {
    it("keeps the Xray middle-proxy rule", () => {
        const got = issues({ ...base, egress_mode: "xray", mtproto_settings: { tls_domain: "a.example.ir", middle_proxy: true } })
        expect(got.map((i) => i.message)).toContain("The middle proxy and ad tag aren't available with Xray egress.")
    })

    it("does not demand a domain on a direct tunnel", () => {
        expect(issues({ ...base, egress_mode: "direct", mtproto_settings: {} })).toEqual([])
    })

    it("takes only the direct and Xray modes", () => {
        expect(issues({ ...base, egress_mode: "hub", mtproto_settings: {} }).map((i) => i.path)).toEqual(["egress_mode"])
    })
})
