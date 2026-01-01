import { describe, it, expect } from "vitest"
import { getContextConfig } from "../sidebar-context-config"

describe("getContextConfig", () => {
    it("matches exact prefixes", () => {
        expect(getContextConfig("/nodes")?.id).toBe("nodes")
        expect(getContextConfig("/payments")?.id).toBe("payments")
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

    it("returns null for routes with no panel", () => {
        expect(getContextConfig("/settings")).toBeNull()
        expect(getContextConfig("/settings/general")).toBeNull()
        expect(getContextConfig("/audit")).toBeNull()
        expect(getContextConfig("/backup")).toBeNull()
    })

    it("returns null for unknown routes", () => {
        expect(getContextConfig("/unknown")).toBeNull()
        expect(getContextConfig("/")).toBeNull()
    })

    it("prefers the longest matching prefix", () => {
        expect(getContextConfig("/accounts")?.id).toBe("customers")
    })
})
