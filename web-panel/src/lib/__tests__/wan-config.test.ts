import { describe, expect, it } from "vitest"
import { defaultWAN, normalizeWAN, recoveryURL, validateWAN } from "@/lib/wan-config"
import { confirmUrls } from "@/lib/api/network"

describe("WAN addressing and recovery", () => {
    it("accepts both /31 endpoints and requires explicit on-link /32 gateways", () => {
        const base = { ...defaultWAN("domestic"), method: "static" as const }
        expect(
            validateWAN({ ...base, static_address: "10.0.0.0/31", static_gateway: "10.0.0.1" }),
        ).toEqual({})
        expect(
            validateWAN({ ...base, static_address: "10.0.0.1/31", static_gateway: "10.0.0.0" }),
        ).toEqual({})
        expect(
            validateWAN({ ...base, static_address: "10.0.0.5/32", static_gateway: "10.0.0.1" })
                .gateway,
        ).toBeTruthy()
        expect(
            validateWAN({
                ...base,
                static_address: "10.0.0.5/32",
                static_gateway: "10.0.0.1",
                gateway_on_link: true,
            }),
        ).toEqual({})
    })

    it("clears hidden static fields and custom resolvers on returning to defaults", () => {
        expect(
            normalizeWAN({
                ...defaultWAN("domestic"),
                static_address: "10.0.0.5/32",
                static_gateway: "10.0.0.1",
                gateway_on_link: true,
                dns_servers: ["1.1.1.1"],
            }),
        ).toEqual(defaultWAN("domestic"))
        expect(defaultWAN("secondary").dns_mode).toBe("vpn")
    })

    it("retains the panel path, protocol and port when offering a new address", () => {
        const config = {
            ...defaultWAN("domestic"),
            method: "static" as const,
            static_address: "10.0.0.5/24",
        }
        expect(recoveryURL(config, "https://router.example:8443/panel/router")).toBe(
            "https://10.0.0.5:8443/panel/router",
        )
        expect(
            recoveryURL(
                { ...config, static_address: "invalid/24" },
                "https://router.example/router",
            ),
        ).toBeNull()
        expect(confirmUrls("https://router.example:8443", "", "/panel/")).toEqual([
            "https://router.example:8443/panel/api/v1/network/confirm",
        ])
    })
})
