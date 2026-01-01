import { describe, it, expect } from "vitest"
import { renderHook } from "@testing-library/react"
import { usePrevious } from "@/hooks/use-previous"

describe("usePrevious", () => {
  it("returns undefined on first render", () => {
    const { result } = renderHook(() => usePrevious("first"))
    expect(result.current).toBeUndefined()
  })

  it("returns previous value after rerender", () => {
    const { result, rerender } = renderHook(({ value }) => usePrevious(value), {
      initialProps: { value: "first" },
    })
    expect(result.current).toBeUndefined()

    rerender({ value: "second" })
    expect(result.current).toBe("first")

    rerender({ value: "third" })
    expect(result.current).toBe("second")
  })

  it("works with numbers", () => {
    const { result, rerender } = renderHook(({ value }) => usePrevious(value), {
      initialProps: { value: 1 },
    })
    rerender({ value: 2 })
    expect(result.current).toBe(1)
  })

  it("works with objects (reference comparison)", () => {
    const obj1 = { a: 1 }
    const obj2 = { a: 2 }
    const { result, rerender } = renderHook(({ value }) => usePrevious(value), {
      initialProps: { value: obj1 },
    })
    rerender({ value: obj2 })
    expect(result.current).toBe(obj1)
  })
})
