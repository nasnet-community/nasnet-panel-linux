import { describe, it, expect } from "vitest"
import { outboundSchema } from "./outbound-schema"

describe("outboundSchema", () => {
    // The backend serializes nil `*Settings` pointers as JSON `null` (no omitempty),
    // so a non-DNS outbound arrives with `dns_settings: null`. The schema must accept
    // it — otherwise editing e.g. a freedom outbound fails with a spurious
    // "Please fix validation errors" mapped to the Protocol tab.
    it("accepts a freedom outbound whose dns_settings is null", () => {
        const result = outboundSchema.safeParse({
            tag: "wg-ir",
            protocol: "freedom",
            network: "tcp",
            security: "none",
            freedom_settings: { domainStrategy: "UseIPv4" },
            dns_settings: null,
        })
        expect(result.success).toBe(true)
    })

    it("still accepts a valid dns_settings object", () => {
        const result = outboundSchema.safeParse({
            tag: "dns-out",
            protocol: "dns",
            dns_settings: { network: "udp", address: "8.8.8.8", port: 53, nonIPQuery: "drop" },
        })
        expect(result.success).toBe(true)
    })

    it("still rejects an invalid dns_settings enum value", () => {
        const result = outboundSchema.safeParse({
            tag: "dns-out",
            protocol: "dns",
            dns_settings: { nonIPQuery: "bogus" },
        })
        expect(result.success).toBe(false)
    })
})
