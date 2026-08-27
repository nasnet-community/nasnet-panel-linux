import { describe, it, expect } from "vitest"
import { getContextConfig } from "../sidebar-context-config"

describe("getContextConfig", () => {
    it("matches exact prefixes", () => {
        expect(getContextConfig("/nodes")?.id).toBe("nodes")
        // expect(getContextConfig("/chats")?.id).toBe("chats")
        expect(getContextConfig("/alerts")?.id).toBe("alerts")
    })

    it("matches sub-routes via prefix", () => {
        expect(getContextConfig("/nodes/5")?.id).toBe("nodes")
        expect(getContextConfig("/nodes/5/inbounds")?.id).toBe("nodes")
        expect(getContextConfig("/hosts")?.id).toBe("nodes")
        expect(getContextConfig("/access-logs")?.id).toBe("nodes")
        expect(getContextConfig("/domains/new")?.id).toBe("certs")
    })

    // A route without its own panel used to render none, and the nav jumped by
    // the card's height. System status is true everywhere, so it stands in.
    it("falls back to system status rather than nothing", () => {
        for (const path of [
            "/settings",
            "/settings/general",
            "/audit",
            "/backup",
            "/router",
            "/router/flow",
            "/xray-binaries",
            "/chats",
        ]) {
            expect(getContextConfig(path)?.id).toBe("system")
        }
    })

    it("falls back for unknown routes too", () => {
        expect(getContextConfig("/unknown")?.id).toBe("system")
        expect(getContextConfig("/")?.id).toBe("system")
    })

    // Every route resolves, so the panel can never be absent.
    it("never returns null", () => {
        for (const path of ["/", "/dashboard", "/nope", "/router/flow/deep/nested"]) {
            expect(getContextConfig(path)).not.toBeNull()
        }
    })

    it("prefers the longest matching prefix", () => {
        expect(getContextConfig("/accounts")?.id).toBe("customers")
    })
})
