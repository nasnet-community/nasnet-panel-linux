/* =============================================================================
 * Chart, canvas palette. resolves design-token CSS vars to concrete strings
 * ========================================================================== */

import { useMemo, useSyncExternalStore } from "react"

function cssVar(name: string): string {
  if (typeof window === "undefined" || !document?.documentElement) return ""
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

export interface ChartPalette {
  /* Categorical series — stable slots, theme-tuned */
  chart1: string // emerald — rx / download / accepted / success-line
  chart2: string // blue    — tx / upload / info-line
  chart3: string // amber   — latency / warning-line
  chart4: string // red     — errors / rejected / drop-rate
  chart5: string // teal    — throughput / secondary
  chart6: string // violet  — tertiary

  /* Semantic intent */
  success: string
  danger: string
  warning: string
  info: string
  neutral: string

  /* Chart chrome grid, axis ticks, value labels */
  grid: string
  axis: string
  label: string

  /* Text and surface (for inline styles, gauges, tooltips) */
  foreground: string
  mutedForeground: string
  border: string

  /* Tooltip surface */
  tooltipBg: string
  tooltipBorder: string
  tooltipText: string
  tooltipLabel: string
}

export function getChartPalette(): ChartPalette {
  return {
    chart1: cssVar("--chart-1"),
    chart2: cssVar("--chart-2"),
    chart3: cssVar("--chart-3"),
    chart4: cssVar("--chart-4"),
    chart5: cssVar("--chart-5"),
    chart6: cssVar("--chart-6"),

    success: cssVar("--status-success"),
    danger: cssVar("--status-danger"),
    warning: cssVar("--status-warning"),
    info: cssVar("--status-info"),
    neutral: cssVar("--status-neutral"),

    grid: cssVar("--border"),
    axis: cssVar("--text-tertiary"),
    label: cssVar("--text-secondary"),

    foreground: cssVar("--foreground"),
    mutedForeground: cssVar("--muted-foreground"),
    border: cssVar("--border"),

    tooltipBg: cssVar("--popover"),
    tooltipBorder: cssVar("--border"),
    tooltipText: cssVar("--popover-foreground"),
    tooltipLabel: cssVar("--muted-foreground"),
  }
}

function subscribeToTheme(onChange: () => void): () => void {
  if (typeof MutationObserver === "undefined") return () => {}
  const observer = new MutationObserver(onChange)
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] })
  const mql = window.matchMedia("(prefers-color-scheme: dark)")
  mql.addEventListener("change", onChange)
  return () => {
    observer.disconnect()
    mql.removeEventListener("change", onChange)
  }
}

function themeSnapshot(): string {
  if (typeof document === "undefined") return "light"
  return document.documentElement.classList.contains("dark") ? "dark" : "light"
}

export function useChartPalette(): ChartPalette {
  const theme = useSyncExternalStore(subscribeToTheme, themeSnapshot, () => "light")
  return useMemo(() => getChartPalette(), [theme])
}

export function tooltipContentStyle(p: ChartPalette): React.CSSProperties {
  return {
    backgroundColor: p.tooltipBg,
    border: `1px solid ${p.tooltipBorder}`,
    borderRadius: "12px",
    boxShadow: "var(--shadow-lg)",
    padding: "10px 12px",
    color: p.tooltipText,
  }
}
