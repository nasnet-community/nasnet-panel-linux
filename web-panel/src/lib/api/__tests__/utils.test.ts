// @vitest-environment node
import { describe, it, expect } from "vitest"
import { buildQueryString } from "@/lib/api/utils"

describe("buildQueryString", () => {
  it("returns empty string for empty params", () => {
    expect(buildQueryString({})).toBe("")
  })

  it("returns empty string when all values are undefined", () => {
    expect(buildQueryString({ a: undefined, b: undefined })).toBe("")
  })

  it("returns empty string when all values are null", () => {
    expect(buildQueryString({ a: null, b: null })).toBe("")
  })

  it("returns empty string when all values are empty strings", () => {
    expect(buildQueryString({ a: "", b: "" })).toBe("")
  })

  it("returns empty string when all values are empty arrays", () => {
    expect(buildQueryString({ a: [], b: [] })).toBe("")
  })

  it("converts numbers to strings", () => {
    expect(buildQueryString({ limit: 10 })).toBe("?limit=10")
  })

  it("passes strings through", () => {
    expect(buildQueryString({ search: "hello" })).toBe("?search=hello")
  })

  it("converts booleans to strings", () => {
    expect(buildQueryString({ active: true })).toBe("?active=true")
    expect(buildQueryString({ active: false })).toBe("?active=false")
  })

  it("joins string arrays with commas", () => {
    expect(buildQueryString({ ids: ["a", "b", "c"] })).toBe("?ids=a%2Cb%2Cc")
  })

  it("joins number arrays with commas", () => {
    expect(buildQueryString({ ids: [1, 2, 3] })).toBe("?ids=1%2C2%2C3")
  })

  it("skips empty arrays", () => {
    expect(buildQueryString({ ids: [], name: "test" })).toBe("?name=test")
  })

  it("skips undefined values while keeping valid ones", () => {
    expect(buildQueryString({ a: undefined, b: "hello" })).toBe("?b=hello")
  })

  it("skips null values while keeping valid ones", () => {
    expect(buildQueryString({ a: null, b: 42 })).toBe("?b=42")
  })

  it("skips empty strings while keeping valid ones", () => {
    expect(buildQueryString({ a: "", b: "world" })).toBe("?b=world")
  })

  it("handles mixed params correctly", () => {
    const qs = buildQueryString({
      search: "proxy",
      page: 2,
      active: true,
      ids: [1, 2],
      empty: "",
      nope: null,
    })
    const params = new URLSearchParams(qs.slice(1))
    expect(params.get("search")).toBe("proxy")
    expect(params.get("page")).toBe("2")
    expect(params.get("active")).toBe("true")
    expect(params.get("ids")).toBe("1,2")
    expect(params.has("empty")).toBe(false)
    expect(params.has("nope")).toBe(false)
  })

  it("handles page/perPage to offset/limit conversion", () => {
    const qs = buildQueryString({}, { page: 1, perPage: 20 })
    const params = new URLSearchParams(qs.slice(1))
    expect(params.get("offset")).toBe("0")
    expect(params.get("limit")).toBe("20")
  })

  it("computes offset correctly for page > 1", () => {
    const qs = buildQueryString({}, { page: 3, perPage: 25 })
    const params = new URLSearchParams(qs.slice(1))
    expect(params.get("offset")).toBe("50")
    expect(params.get("limit")).toBe("25")
  })

  it("skips pagination when page is missing", () => {
    const qs = buildQueryString({ name: "test" }, { perPage: 20 })
    const params = new URLSearchParams(qs.slice(1))
    expect(params.has("offset")).toBe(false)
    expect(params.has("limit")).toBe(false)
    expect(params.get("name")).toBe("test")
  })

  it("skips pagination when perPage is missing", () => {
    const qs = buildQueryString({ name: "test" }, { page: 2 })
    const params = new URLSearchParams(qs.slice(1))
    expect(params.has("offset")).toBe(false)
    expect(params.has("limit")).toBe(false)
  })

  it("skips pagination when pagination object is not provided", () => {
    const qs = buildQueryString({ name: "test" })
    const params = new URLSearchParams(qs.slice(1))
    expect(params.has("offset")).toBe(false)
    expect(params.has("limit")).toBe(false)
    expect(params.get("name")).toBe("test")
  })

  it("combines pagination and extra params", () => {
    const qs = buildQueryString({ search: "foo", active: true }, { page: 2, perPage: 10 })
    const params = new URLSearchParams(qs.slice(1))
    expect(params.get("offset")).toBe("10")
    expect(params.get("limit")).toBe("10")
    expect(params.get("search")).toBe("foo")
    expect(params.get("active")).toBe("true")
  })
})
