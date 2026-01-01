import { useEffect, useState, useCallback } from "react"

/**
 * Persists a draft string under `chat:draft:<key>` in localStorage with a
 * 200ms debounce on writes. Returns [value, setValue, clear].
 */
export function useDraft(key: string, initial = "") {
    const storageKey = `chat:draft:${key}`
    const [value, setValue] = useState<string>(() => {
        if (typeof window === "undefined") return initial
        try {
            return window.localStorage.getItem(storageKey) ?? initial
        } catch {
            return initial
        }
    })

    useEffect(() => {
        if (typeof window === "undefined") return
        const t = setTimeout(() => {
            try {
                window.localStorage.setItem(storageKey, value)
            } catch {
                /* quota or unavailable */
            }
        }, 200)
        return () => clearTimeout(t)
    }, [value, storageKey])

    const clear = useCallback(() => {
        try {
            window.localStorage.removeItem(storageKey)
        } catch {
            /* */
        }
        setValue("")
    }, [storageKey])

    return [value, setValue, clear] as const
}
