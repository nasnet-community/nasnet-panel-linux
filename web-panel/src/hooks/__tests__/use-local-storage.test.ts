import { describe, it, expect, beforeEach } from "vitest"
import { renderHook, act } from "@testing-library/react"
import { useLocalStorage } from "@/hooks/use-local-storage"

beforeEach(() => {
  localStorage.clear()
})

describe("useLocalStorage", () => {
  it("returns initial value when localStorage is empty", () => {
    const { result } = renderHook(() => useLocalStorage("test-key", "default"))
    expect(result.current[0]).toBe("default")
  })

  it("returns stored value when localStorage has data", () => {
    localStorage.setItem("test-key", JSON.stringify("stored"))
    const { result } = renderHook(() => useLocalStorage("test-key", "default"))
    expect(result.current[0]).toBe("stored")
  })

  it("updates localStorage when setValue is called", () => {
    const { result } = renderHook(() => useLocalStorage("test-key", "default"))
    act(() => {
      result.current[1]("updated")
    })
    expect(result.current[0]).toBe("updated")
    expect(JSON.parse(localStorage.getItem("test-key")!)).toBe("updated")
  })

  it("supports function updater", () => {
    const { result } = renderHook(() => useLocalStorage("counter", 0))
    act(() => {
      result.current[1]((prev) => prev + 1)
    })
    expect(result.current[0]).toBe(1)
  })

  it("handles objects", () => {
    const { result } = renderHook(() => useLocalStorage("obj", { a: 1 }))
    act(() => {
      result.current[1]({ a: 2 })
    })
    expect(result.current[0]).toEqual({ a: 2 })
  })

  it("removes value from localStorage", () => {
    const { result } = renderHook(() => useLocalStorage("test-key", "default"))
    act(() => {
      result.current[1]("value")
    })
    act(() => {
      result.current[2]()
    })
    expect(result.current[0]).toBe("default")
    expect(localStorage.getItem("test-key")).toBeNull()
  })

  it("handles corrupted localStorage data gracefully", () => {
    localStorage.setItem("test-key", "not-valid-json{{{")
    const { result } = renderHook(() => useLocalStorage("test-key", "default"))
    expect(result.current[0]).toBe("default")
  })
})
