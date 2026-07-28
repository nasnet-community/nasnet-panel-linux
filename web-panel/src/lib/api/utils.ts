type ParamValue = string | number | boolean | number[] | string[] | undefined | null

export function buildQueryString<T extends { [K in keyof T]: ParamValue }>(
  params: T,
  pagination?: { page?: number; perPage?: number }
): string {
  const sp = new URLSearchParams()

  if (pagination?.page && pagination?.perPage) {
    const offset = (pagination.page - 1) * pagination.perPage
    sp.set("offset", String(offset))
    sp.set("limit", String(pagination.perPage))
  }

  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null) continue
    if (typeof value === "string" && value === "") continue
    if (Array.isArray(value)) {
      if (value.length === 0) continue
      sp.set(key, value.join(","))
    } else {
      sp.set(key, String(value))
    }
  }

  const qs = sp.toString()
  return qs ? `?${qs}` : ""
}
