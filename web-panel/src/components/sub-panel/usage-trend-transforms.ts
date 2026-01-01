import type { UsageTrendPoint } from "@/lib/types/sub-panel"

// Binary (1024-based) units to match the backend (domain.FormatBytes) and the
// rest of the panel (stats grid, data-forecast), so "GB" means the same thing
// everywhere on the page.
export const UNIT_FACTOR = {
  KB: 1024,
  MB: 1024 * 1024,
  GB: 1024 * 1024 * 1024,
} as const

export type ChartPoint = {
  date: string        // YYYY-MM-DD (UTC)
  upload: number      // bytes (0 for legacy / gap)
  download: number    // bytes (0 for legacy / gap)
  legacyTotal: number // bytes (0 for non-legacy / gap)
  total: number       // bytes
  isLegacy: boolean
}

function toISODate(d: Date): string {
  return d.toISOString().slice(0, 10)
}

export function buildChartPoints(
  points: UsageTrendPoint[],
  rangeDays: number,
  todayUTC: Date = new Date(),
): ChartPoint[] {
  const today = new Date(Date.UTC(
    todayUTC.getUTCFullYear(),
    todayUTC.getUTCMonth(),
    todayUTC.getUTCDate(),
  ))

  const byDate = new Map(points.map(p => [p.date, p]))

  const out: ChartPoint[] = []
  for (let i = rangeDays - 1; i >= 0; i--) {
    const d = new Date(today)
    d.setUTCDate(today.getUTCDate() - i)
    const iso = toISODate(d)
    const src = byDate.get(iso)

    if (!src) {
      out.push({ date: iso, upload: 0, download: 0, legacyTotal: 0, total: 0, isLegacy: false })
      continue
    }

    const isLegacy = src.upload === null && src.download === null
    out.push({
      date: iso,
      upload: isLegacy ? 0 : (src.upload ?? 0),
      download: isLegacy ? 0 : (src.download ?? 0),
      legacyTotal: isLegacy ? src.total : 0,
      total: src.total,
      isLegacy,
    })
  }
  return out
}
