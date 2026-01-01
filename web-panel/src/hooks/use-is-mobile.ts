import { useState, useEffect } from "react"

const MOBILE_BREAKPOINT = 767

export function useIsMobile() {
    const [isMobile, setIsMobile] = useState(false)

    useEffect(() => {
        const mql = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT}px)`)
        setIsMobile(mql.matches)

        const handler = (e: MediaQueryListEvent) => setIsMobile(e.matches)
        mql.addEventListener("change", handler)
        return () => mql.removeEventListener("change", handler)
    }, [])

    return isMobile
}
