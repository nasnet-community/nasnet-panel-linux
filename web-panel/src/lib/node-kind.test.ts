import { describe, expect, it } from "vitest"
import { isLiteNode, isLocalNode } from "./node-kind"

describe("node kinds", () => {
    it("tells an MTProto node from a full one", () => {
        expect(isLiteNode({ kind: "lite" })).toBe(true)
        expect(isLiteNode({ kind: "full" })).toBe(false)
        expect(isLiteNode({})).toBe(false) // rows from before MTProto nodes read as full
        expect(isLiteNode(null)).toBe(false)
    })

    it("an MTProto node is not the built-in one", () => {
        expect(isLocalNode({ connect_mode: "link" })).toBe(false)
    })
})
