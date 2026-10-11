import { describe, expect, it } from "vitest"
import { logFileName, parseLogLine } from "../logs/agent-log"

describe("parseLogLine", () => {
    it("reads telemt's tracing lines", () => {
        const l = parseLogLine("2026-10-02T10:15:57.148698Z  INFO telemt::maestro: Log level: normal")
        expect(l.time).toBe(Date.parse("2026-10-02T10:15:57.148Z"))
        expect(l.level).toBe("info")
        expect(l.message).toBe("telemt::maestro: Log level: normal")
    })

    it("maps every level word", () => {
        const level = (w: string) => parseLogLine(`2026-10-02T10:15:57Z ${w} x`).level
        expect(level("WARN")).toBe("warning")
        expect(level("WARNING")).toBe("warning")
        expect(level("ERROR")).toBe("error")
        expect(level("DEBUG")).toBe("debug")
        expect(level("TRACE")).toBe("debug")
        expect(level("[Info]")).toBe("info")
    })

    it("keeps a stamped line with no level word as a message", () => {
        const l = parseLogLine("2026-10-02T10:15:57+03:30 the capture rules are back")
        expect(l.time).toBe(Date.parse("2026-10-02T10:15:57+03:30"))
        expect(l.level).toBeUndefined()
        expect(l.message).toBe("the capture rules are back")
    })

    it("reads slog's key=value lines", () => {
        const l = parseLogLine('time=2026-10-02T10:15:57.148+00:00 level=WARN msg="push failed" err="i/o timeout"')
        expect(l.time).toBe(Date.parse("2026-10-02T10:15:57.148Z"))
        expect(l.level).toBe("warning")
        expect(l.message).toBe('push failed err="i/o timeout"')
    })

    it("leaves anything else raw", () => {
        for (const raw of ["MAESTRO: User: manual_0_8b613f6e", "the capture rules are gone: telemt is stopped until they are back", ""]) {
            expect(parseLogLine(raw)).toEqual({ raw, message: raw })
        }
    })

    it("drops terminal colour codes", () => {
        const esc = String.fromCharCode(27)
        const l = parseLogLine(`${esc}[2m2026-10-02T10:15:57Z${esc}[0m ${esc}[32m INFO${esc}[0m ready`)
        expect(l.level).toBe("info")
        expect(l.message).toBe("ready")
    })
})

describe("logFileName", () => {
    it("names the file after the node and the day", () => {
        expect(logFileName("VPS Tehran #2", new Date(2026, 9, 2, 23, 59))).toBe("VPS-Tehran-2-mtproto-agent-2026-10-02.log")
        expect(logFileName("91.107.156.216", new Date(2026, 0, 5))).toBe("91.107.156.216-mtproto-agent-2026-01-05.log")
        expect(logFileName("سرور ۱", new Date(2026, 0, 5))).toBe("سرور-۱-mtproto-agent-2026-01-05.log")
        expect(logFileName("///", new Date(2026, 0, 5))).toBe("node-mtproto-agent-2026-01-05.log")
    })
})
