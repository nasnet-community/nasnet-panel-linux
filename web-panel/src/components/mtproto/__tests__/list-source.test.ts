import { describe, expect, it } from "vitest"
import { listSource } from "../overview/status-cards"

describe("listSource", () => {
    it("names the host the list refreshes from", () => {
        expect(listSource({ prefixes: 14, placeholder: false, url: "https://core.telegram.org/resources/cidr.txt" }))
            .toBe("From core.telegram.org · refreshes daily")
    })
    it("says when the refresh is off or failing", () => {
        expect(listSource({ prefixes: 14, placeholder: false, url: "" })).toBe("Built-in list · daily refresh off")
        expect(listSource({ prefixes: 14, placeholder: false, url: "https://core.telegram.org/resources/cidr.txt", last_error: "timeout" }))
            .toBe("From core.telegram.org · last refresh failed")
    })
})
