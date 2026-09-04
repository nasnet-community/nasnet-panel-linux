import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// parseDateLocal parses a "YYYY-MM-DD" string as a local-TZ date. The Date
// constructor would treat a bare ISO date as UTC midnight, which shifts the
// displayed day back by one for users west of UTC.
export function parseDateLocal(s: string): Date {
  const [y, m, d] = s.split("-").map(Number)
  return new Date(y, m - 1, d)
}

export function formatBytes(bytes: number, decimals = 2) {
  if (!+bytes) return '0 Bytes'

  const k = 1024
  const dm = decimals < 0 ? 0 : decimals
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB']

  const i = Math.floor(Math.log(bytes) / Math.log(k))

  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`
}

// Format bytes for plan display (shows "Unlimited" for 0)
export function formatDataLimit(bytes: number): string {
  if (bytes === 0) return "Unlimited"
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1) return `${gb.toFixed(0)} GB`
  const mb = bytes / (1024 * 1024)
  return `${mb.toFixed(0)} MB`
}

export function formatSpeed(bytesPerSecond: number): string {
  if (!+bytesPerSecond) return '0 B/s'
  const k = 1024
  const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k))
  return `${parseFloat((bytesPerSecond / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`
}

export function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  if (seconds < 86400) {
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    return `${h}h ${m}m`
  }
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  return `${d}d ${h}h`
}

export function formatCompact(bytes: number): string {
  if (!bytes) return "0"
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1) return `${gb < 10 ? gb.toFixed(1) : Math.round(gb)}GB`
  const mb = bytes / (1024 * 1024)
  if (mb >= 1) return `${Math.round(mb)}MB`
  const kb = bytes / 1024
  return `${Math.round(kb)}KB`
}

// Get relative time label
export function getRelativeTime(timestamp: number): string {
  const seconds = Math.floor((Date.now() - timestamp) / 1000)
  if (seconds < 5) return "just now"
  if (seconds < 60) return `${seconds}s ago`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`
  return `${Math.floor(seconds / 86400)}d ago`
}

// Expiry info helper
export function getExpiryInfo(expiresAt?: string): { text: string; variant: "default" | "secondary" | "warning" | "danger" } {
  if (!expiresAt) return { text: "\u221E", variant: "default" }
  const days = Math.ceil((new Date(expiresAt).getTime() - Date.now()) / (1000 * 60 * 60 * 24))
  if (days <= 0) return { text: "Expired", variant: "danger" }
  if (days <= 3) return { text: `${days}d left`, variant: "danger" }
  if (days <= 7) return { text: `${days}d left`, variant: "warning" }
  return { text: `${days}d left`, variant: "default" }
}

export function formatDate(dateStr: string, style: "short" | "long" = "short"): string {
  const date = new Date(dateStr)
  if (style === "long") {
    return date.toLocaleDateString("en-US", { year: "numeric", month: "long", day: "numeric" })
  }
  return date.toLocaleDateString("en-US", { month: "short", day: "numeric" })
}

export function formatCurrency(value: number): string {
  if (value >= 1000) return `$${(value / 1000).toFixed(1)}k`
  return `$${value.toFixed(0)}`
}

export function formatDateTime(dateStr: string): string {
  const date = new Date(dateStr)
  return date.toLocaleString("en-US", {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  })
}

export async function copyToClipboard(text: string): Promise<boolean> {
  // Haptic feedback for mobile devices
  if ("vibrate" in navigator) {
    navigator.vibrate(10)
  }

  // Use Clipboard API only in secure contexts (HTTPS/localhost)
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // fall through to the legacy path
    }
  }

  // Synchronous fallback for HTTP — must run in the user-gesture call stack.
  // Append inside any open dialog to avoid Radix focus-trap blocking focus.
  const container = document.activeElement?.closest('[role="dialog"]') || document.body
  const textarea = document.createElement("textarea")
  textarea.value = text
  textarea.style.position = "fixed"
  textarea.style.left = "-9999px"
  textarea.style.top = "-9999px"
  textarea.style.opacity = "0"
  container.appendChild(textarea)
  textarea.focus()
  textarea.select()
  try {
    return document.execCommand("copy")
  } catch {
    return false
  } finally {
    container.removeChild(textarea)
  }
}

/** crypto.randomUUID() fallback for non-secure (HTTP) contexts */
export function generateUUID(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID()
  }
  // Manual v4 UUID using crypto.getRandomValues (available in all browsers)
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  bytes[6] = (bytes[6] & 0x0f) | 0x40 // version 4
  bytes[8] = (bytes[8] & 0x3f) | 0x80 // variant 1
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

/** Two-letter country code to its flag emoji. Empty string when the code is
 * not a usable ISO pair, so callers can render nothing instead of a tofu box. */
export function countryFlag(code?: string | null): string {
  if (!code || !/^[A-Za-z]{2}$/.test(code)) return ""
  const up = code.toUpperCase()
  return String.fromCodePoint(0x1f1e6 + up.charCodeAt(0) - 65, 0x1f1e6 + up.charCodeAt(1) - 65)
}

export function formatRelativeTime(dateStr: string): string {
  const now = new Date()
  const date = new Date(dateStr)
  const diffMs = date.getTime() - now.getTime()
  const absDiffMs = Math.abs(diffMs)
  const isPast = diffMs < 0

  if (absDiffMs < 60000) return "just now"
  if (absDiffMs < 3600000) {
    const mins = Math.floor(absDiffMs / 60000)
    return isPast ? `${mins}m ago` : `in ${mins}m`
  }
  if (absDiffMs < 86400000) {
    const hours = Math.floor(absDiffMs / 3600000)
    return isPast ? `${hours}h ago` : `in ${hours}h`
  }
  const days = Math.floor(absDiffMs / 86400000)
  return isPast ? `${days}d ago` : `in ${days}d`
}
