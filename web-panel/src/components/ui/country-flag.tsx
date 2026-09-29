import { useEffect, useState } from "react"
import { cn } from "@/lib/utils"

// One small SVG per country, each its own lazy chunk, fetched only for the
// flag on screen. Emoji flags would be lighter but Windows renders them as
// two letters.
const flagLoaders = import.meta.glob("/node_modules/country-flag-icons/3x2/*.svg", {
    query: "?url",
    import: "default",
}) as Record<string, () => Promise<string>>

const loaderByCode: Record<string, () => Promise<string>> = {}
for (const [path, load] of Object.entries(flagLoaders)) {
    loaderByCode[path.slice(path.lastIndexOf("/") + 1, -4)] = load
}

let regionNames: Intl.DisplayNames | null = null
/** "IR" → "Iran"; falls back to the code. */
export function countryName(code: string): string {
    try {
        regionNames ??= new Intl.DisplayNames(["en"], { type: "region" })
        return regionNames.of(code.toUpperCase()) ?? code
    } catch {
        return code
    }
}

export function CountryFlag({ code, className }: { code?: string; className?: string }) {
    const upper = code?.toUpperCase()
    const [url, setUrl] = useState<string | null>(null)

    useEffect(() => {
        setUrl(null)
        const load = upper ? loaderByCode[upper] : undefined
        if (!load) return
        let alive = true
        load().then(u => { if (alive) setUrl(u) }).catch(() => {})
        return () => { alive = false }
    }, [upper])

    if (!upper || !loaderByCode[upper]) return null
    return url ? (
        <img
            src={url}
            alt={`Flag of ${countryName(upper)}`}
            className={cn("h-3.5 w-5 shrink-0 rounded-[2px] object-cover ring-1 ring-white/10", className)}
        />
    ) : (
        <span className={cn("inline-block h-3.5 w-5 shrink-0 rounded-[2px] bg-muted", className)} aria-hidden />
    )
}
