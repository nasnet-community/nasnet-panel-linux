import { afterEach, describe, expect, it, vi } from "vitest"
import type { Terminal } from "@xterm/xterm"
import { needsPasteReview, readTerminalPreferences, terminalOutput } from "./terminal-utils"

afterEach(() => { localStorage.clear(); vi.unstubAllGlobals() })

describe("terminal output and preferences", () => {
    it("preserves hard newlines and spaces across soft-wrapped exported output", () => {
        const source = [
            { text: "a command with ", wrapped: false },
            { text: "spaces   ", wrapped: true },
            { text: "second line   ", wrapped: false },
            { text: "   ", wrapped: false },
            { text: "last line", wrapped: false },
        ]
        const terminal = { buffer: { active: {
            length: source.length,
            getLine: (index: number) => source[index] && {
                isWrapped: source[index].wrapped,
                translateToString: (trim: boolean) => trim ? source[index].text.trimEnd() : source[index].text,
            },
        } } } as unknown as Terminal
        expect(terminalOutput(terminal)).toBe("a command with spaces\nsecond line\n\nlast line")
    })

    it("recovers from malformed preferences and clamps an unusable text size", () => {
        vi.stubGlobal("matchMedia", () => ({ matches: false }))
        localStorage.setItem("nasnet-terminal-preferences-v1", "broken json")
        expect(readTerminalPreferences()).toEqual({ fontSize: 14, touchKeys: false, screenReaderMode: false })
        localStorage.setItem("nasnet-terminal-preferences-v1", JSON.stringify({ fontSize: 300, touchKeys: "false", screenReaderMode: "false" }))
        expect(readTerminalPreferences()).toEqual({ fontSize: 20, touchKeys: false, screenReaderMode: false })
    })

    it("reviews execution-affecting paste controls while allowing ordinary single-line text", () => {
        expect(needsPasteReview("ls -la\t/tmp")).toBe(false)
        expect(needsPasteReview("pwd\nwhoami")).toBe(true)
        expect(needsPasteReview("pwd\r")).toBe(true)
        expect(needsPasteReview("\x1b[200~command")).toBe(true)
    })
})
