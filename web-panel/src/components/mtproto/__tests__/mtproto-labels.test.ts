import { describe, expect, it } from "vitest"
import { stepLabel } from "../mtproto-labels"

describe("stepLabel", () => {
    it("names a step for the job it is in", () => {
        expect(stepLabel("verify", "install")).toBe("Verifying what will be uploaded")
        expect(stepLabel("verify", "repair")).toBe("Verifying what will be uploaded")
        expect(stepLabel("verify", "rotate")).toBe("Checking the new keys work")
    })

    it("falls back to the shared label, then to the raw name", () => {
        expect(stepLabel("rotate", "rotate")).toBe("Switching keys")
        expect(stepLabel("check")).toBe("Checking the node")
        expect(stepLabel("new_step", "rotate")).toBe("new step")
    })
})
