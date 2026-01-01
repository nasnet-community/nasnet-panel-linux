// @vitest-environment node
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import {
  formatBytes,
  formatDataLimit,
  formatSpeed,
  formatUptime,
  formatCompact,
  getRelativeTime,
  getExpiryInfo,
  formatDate,
  formatCurrency,
  formatDateTime,
  generateUUID,
  formatRelativeTime,
} from "@/lib/utils"

// ---------------------------------------------------------------------------
// formatBytes
// ---------------------------------------------------------------------------
describe("formatBytes", () => {
  it("returns '0 Bytes' for 0", () => {
    expect(formatBytes(0)).toBe("0 Bytes")
  })

  it("returns '0 Bytes' for NaN-like falsy values", () => {
    // !+bytes is true for 0 and NaN
    expect(formatBytes(NaN)).toBe("0 Bytes")
  })

  it("formats bytes", () => {
    expect(formatBytes(500)).toBe("500 Bytes")
  })

  it("formats kilobytes with default 2 decimals", () => {
    expect(formatBytes(1024)).toBe("1 KB")
  })

  it("formats megabytes", () => {
    expect(formatBytes(1024 * 1024)).toBe("1 MB")
  })

  it("formats gigabytes", () => {
    expect(formatBytes(1024 * 1024 * 1024)).toBe("1 GB")
  })

  it("formats terabytes", () => {
    expect(formatBytes(1024 ** 4)).toBe("1 TB")
  })

  it("respects custom decimals", () => {
    expect(formatBytes(1500, 1)).toBe("1.5 KB")
  })

  it("uses 0 decimals when decimals < 0", () => {
    expect(formatBytes(1536, -1)).toBe("2 KB")
  })

  it("formats non-round values with 2 decimals by default", () => {
    expect(formatBytes(1536)).toBe("1.5 KB")
  })
})

// ---------------------------------------------------------------------------
// formatDataLimit
// ---------------------------------------------------------------------------
describe("formatDataLimit", () => {
  it("returns 'Unlimited' for 0", () => {
    expect(formatDataLimit(0)).toBe("Unlimited")
  })

  it("formats exactly 1 GB", () => {
    expect(formatDataLimit(1024 * 1024 * 1024)).toBe("1 GB")
  })

  it("formats multiple GBs", () => {
    expect(formatDataLimit(10 * 1024 * 1024 * 1024)).toBe("10 GB")
  })

  it("formats fractional GBs rounded to 0 decimals", () => {
    // 1.5 GB -> toFixed(0) -> "2 GB"
    expect(formatDataLimit(1.5 * 1024 * 1024 * 1024)).toBe("2 GB")
  })

  it("formats MB when less than 1 GB", () => {
    expect(formatDataLimit(500 * 1024 * 1024)).toBe("500 MB")
  })

  it("formats 1 MB", () => {
    expect(formatDataLimit(1024 * 1024)).toBe("1 MB")
  })
})

// ---------------------------------------------------------------------------
// formatSpeed
// ---------------------------------------------------------------------------
describe("formatSpeed", () => {
  it("returns '0 B/s' for 0", () => {
    expect(formatSpeed(0)).toBe("0 B/s")
  })

  it("returns '0 B/s' for NaN", () => {
    expect(formatSpeed(NaN)).toBe("0 B/s")
  })

  it("formats bytes per second", () => {
    expect(formatSpeed(500)).toBe("500 B/s")
  })

  it("formats KB/s", () => {
    expect(formatSpeed(1024)).toBe("1 KB/s")
  })

  it("formats MB/s", () => {
    expect(formatSpeed(1024 * 1024)).toBe("1 MB/s")
  })

  it("formats GB/s", () => {
    expect(formatSpeed(1024 * 1024 * 1024)).toBe("1 GB/s")
  })

  it("formats fractional KB/s with 1 decimal", () => {
    expect(formatSpeed(1536)).toBe("1.5 KB/s")
  })
})

// ---------------------------------------------------------------------------
// formatUptime
// ---------------------------------------------------------------------------
describe("formatUptime", () => {
  it("formats seconds only (< 60)", () => {
    expect(formatUptime(45)).toBe("45s")
  })

  it("formats exactly 59 seconds", () => {
    expect(formatUptime(59)).toBe("59s")
  })

  it("formats minutes and seconds (< 3600)", () => {
    expect(formatUptime(90)).toBe("1m 30s")
  })

  it("formats exactly 60 seconds as 1m 0s", () => {
    expect(formatUptime(60)).toBe("1m 0s")
  })

  it("formats hours and minutes (< 86400)", () => {
    expect(formatUptime(3661)).toBe("1h 1m")
  })

  it("formats exactly 1 hour", () => {
    expect(formatUptime(3600)).toBe("1h 0m")
  })

  it("formats days and hours", () => {
    expect(formatUptime(86400 + 3600)).toBe("1d 1h")
  })

  it("formats exactly 1 day", () => {
    expect(formatUptime(86400)).toBe("1d 0h")
  })

  it("formats multiple days", () => {
    expect(formatUptime(3 * 86400 + 2 * 3600)).toBe("3d 2h")
  })
})

// ---------------------------------------------------------------------------
// formatCompact
// ---------------------------------------------------------------------------
describe("formatCompact", () => {
  it("returns '0' for 0", () => {
    expect(formatCompact(0)).toBe("0")
  })

  it("formats GB with 1 decimal when < 10 GB", () => {
    expect(formatCompact(1.5 * 1024 * 1024 * 1024)).toBe("1.5GB")
  })

  it("formats GB rounded when >= 10 GB", () => {
    expect(formatCompact(10 * 1024 * 1024 * 1024)).toBe("10GB")
  })

  it("formats MB when < 1 GB", () => {
    expect(formatCompact(500 * 1024 * 1024)).toBe("500MB")
  })

  it("formats exactly 1 MB", () => {
    expect(formatCompact(1024 * 1024)).toBe("1MB")
  })

  it("formats KB when < 1 MB", () => {
    expect(formatCompact(512 * 1024)).toBe("512KB")
  })

  it("formats 1 KB", () => {
    expect(formatCompact(1024)).toBe("1KB")
  })

  it("formats small bytes as KB (rounds)", () => {
    // 500 bytes -> 500/1024 = 0.488... rounds to 0 KB
    expect(formatCompact(500)).toBe("0KB")
  })
})

// ---------------------------------------------------------------------------
// getRelativeTime
// ---------------------------------------------------------------------------
describe("getRelativeTime", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date("2024-01-01T12:00:00Z"))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it("returns 'just now' for timestamps within 5 seconds", () => {
    const now = Date.now()
    expect(getRelativeTime(now - 3000)).toBe("just now")
    expect(getRelativeTime(now)).toBe("just now")
  })

  it("returns seconds ago for timestamps 5-59 seconds old", () => {
    const now = Date.now()
    expect(getRelativeTime(now - 10000)).toBe("10s ago")
    expect(getRelativeTime(now - 59000)).toBe("59s ago")
  })

  it("returns minutes ago for timestamps 1-59 minutes old", () => {
    const now = Date.now()
    expect(getRelativeTime(now - 60000)).toBe("1m ago")
    expect(getRelativeTime(now - 90000)).toBe("1m ago")
    expect(getRelativeTime(now - 3599000)).toBe("59m ago")
  })

  it("returns hours ago for timestamps 1-23 hours old", () => {
    const now = Date.now()
    expect(getRelativeTime(now - 3600000)).toBe("1h ago")
    expect(getRelativeTime(now - 7200000)).toBe("2h ago")
  })

  it("returns days ago for timestamps >= 1 day old", () => {
    const now = Date.now()
    expect(getRelativeTime(now - 86400000)).toBe("1d ago")
    expect(getRelativeTime(now - 2 * 86400000)).toBe("2d ago")
  })
})

// ---------------------------------------------------------------------------
// formatRelativeTime
// ---------------------------------------------------------------------------
describe("formatRelativeTime", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date("2024-01-15T12:00:00Z"))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it("returns 'just now' for times within 60 seconds", () => {
    const now = new Date("2024-01-15T12:00:00Z")
    expect(formatRelativeTime(new Date(now.getTime() - 30000).toISOString())).toBe("just now")
    expect(formatRelativeTime(new Date(now.getTime() + 30000).toISOString())).toBe("just now")
  })

  it("returns 'Xm ago' for past times within an hour", () => {
    const past = new Date("2024-01-15T11:30:00Z") // 30 minutes ago
    expect(formatRelativeTime(past.toISOString())).toBe("30m ago")
  })

  it("returns 'in Xm' for future times within an hour", () => {
    const future = new Date("2024-01-15T12:45:00Z") // 45 minutes from now
    expect(formatRelativeTime(future.toISOString())).toBe("in 45m")
  })

  it("returns 'Xh ago' for past times within a day", () => {
    const past = new Date("2024-01-15T09:00:00Z") // 3 hours ago
    expect(formatRelativeTime(past.toISOString())).toBe("3h ago")
  })

  it("returns 'in Xh' for future times within a day", () => {
    const future = new Date("2024-01-15T18:00:00Z") // 6 hours from now
    expect(formatRelativeTime(future.toISOString())).toBe("in 6h")
  })

  it("returns 'Xd ago' for past times >= 1 day", () => {
    const past = new Date("2024-01-13T12:00:00Z") // 2 days ago
    expect(formatRelativeTime(past.toISOString())).toBe("2d ago")
  })

  it("returns 'in Xd' for future times >= 1 day", () => {
    const future = new Date("2024-01-18T12:00:00Z") // 3 days from now
    expect(formatRelativeTime(future.toISOString())).toBe("in 3d")
  })
})

// ---------------------------------------------------------------------------
// getExpiryInfo
// ---------------------------------------------------------------------------
describe("getExpiryInfo", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date("2024-01-15T12:00:00Z"))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it("returns infinity symbol and 'default' variant when no expiresAt", () => {
    const result = getExpiryInfo()
    expect(result.text).toBe("\u221E")
    expect(result.variant).toBe("default")
  })

  it("returns 'Expired' with 'danger' variant when already expired", () => {
    const result = getExpiryInfo("2024-01-14T12:00:00Z")
    expect(result.text).toBe("Expired")
    expect(result.variant).toBe("danger")
  })

  it("returns 'Xd left' with 'danger' variant for 1-3 days left", () => {
    // 2 days in the future: Jan 17
    const result = getExpiryInfo("2024-01-17T12:00:00Z")
    expect(result.text).toBe("2d left")
    expect(result.variant).toBe("danger")
  })

  it("returns 'Xd left' with 'danger' variant for exactly 3 days left", () => {
    const result = getExpiryInfo("2024-01-18T12:00:00Z")
    expect(result.text).toBe("3d left")
    expect(result.variant).toBe("danger")
  })

  it("returns 'Xd left' with 'warning' variant for 4-7 days left", () => {
    // 5 days in the future: Jan 20
    const result = getExpiryInfo("2024-01-20T12:00:00Z")
    expect(result.text).toBe("5d left")
    expect(result.variant).toBe("warning")
  })

  it("returns 'Xd left' with 'warning' variant for exactly 7 days left", () => {
    const result = getExpiryInfo("2024-01-22T12:00:00Z")
    expect(result.text).toBe("7d left")
    expect(result.variant).toBe("warning")
  })

  it("returns 'Xd left' with 'default' variant for more than 7 days left", () => {
    const result = getExpiryInfo("2024-02-15T12:00:00Z")
    expect(result.text).toBe("31d left")
    expect(result.variant).toBe("default")
  })
})

// ---------------------------------------------------------------------------
// formatDate
// ---------------------------------------------------------------------------
describe("formatDate", () => {
  it("formats a date in short style (default)", () => {
    const result = formatDate("2024-06-15")
    // en-US short: "Jun 15"
    expect(result).toBe("Jun 15")
  })

  it("formats a date in long style", () => {
    const result = formatDate("2024-06-15", "long")
    // en-US long: "June 15, 2024"
    expect(result).toBe("June 15, 2024")
  })

  it("formats January correctly in short style", () => {
    const result = formatDate("2024-01-01", "short")
    expect(result).toBe("Jan 1")
  })

  it("formats December correctly in long style", () => {
    const result = formatDate("2024-12-31", "long")
    expect(result).toBe("December 31, 2024")
  })
})

// ---------------------------------------------------------------------------
// formatCurrency
// ---------------------------------------------------------------------------
describe("formatCurrency", () => {
  it("formats values under 1000 with no decimals", () => {
    expect(formatCurrency(500)).toBe("$500")
  })

  it("formats exactly 999 with no decimals", () => {
    expect(formatCurrency(999)).toBe("$999")
  })

  it("formats 1000 as 1.0k", () => {
    expect(formatCurrency(1000)).toBe("$1.0k")
  })

  it("formats values >= 1000 in k notation", () => {
    expect(formatCurrency(2500)).toBe("$2.5k")
  })

  it("formats large values in k notation", () => {
    expect(formatCurrency(10000)).toBe("$10.0k")
  })

  it("formats 0 as $0", () => {
    expect(formatCurrency(0)).toBe("$0")
  })
})

// ---------------------------------------------------------------------------
// formatDateTime
// ---------------------------------------------------------------------------
describe("formatDateTime", () => {
  it("formats a date-time string in en-US locale", () => {
    // Use a fixed date to test the output shape without locale uncertainty
    const result = formatDateTime("2024-06-15T14:30:00Z")
    // Should contain the date parts
    expect(result).toMatch(/Jun/)
    expect(result).toMatch(/15/)
    expect(result).toMatch(/2024/)
  })

  it("includes time portion", () => {
    const result = formatDateTime("2024-06-15T14:30:00Z")
    // Should contain hour and minute
    expect(result).toMatch(/\d{1,2}:\d{2}/)
  })
})

// ---------------------------------------------------------------------------
// generateUUID
// ---------------------------------------------------------------------------
describe("generateUUID", () => {
  it("returns a string in UUID v4 format", () => {
    const uuid = generateUUID()
    expect(uuid).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
  })

  it("returns unique values on each call", () => {
    const uuids = new Set(Array.from({ length: 20 }, () => generateUUID()))
    expect(uuids.size).toBe(20)
  })

  it("returns a string of length 36", () => {
    expect(generateUUID()).toHaveLength(36)
  })
})
