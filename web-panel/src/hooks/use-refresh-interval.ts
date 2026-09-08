import { useSettingsStore } from "@/store/settings-store"

// Auto-refresh cadence the AutoRefreshControl dropdown writes, in ms.
// false = off, which is what react-query wants for "don't poll".
export function useRefreshInterval(): number | false {
    const seconds = useSettingsStore((s) => s.refreshInterval)
    return seconds > 0 ? seconds * 1000 : false
}
