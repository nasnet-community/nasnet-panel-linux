import { useEffect, useState, useCallback } from "react"
import { useHref, useNavigate } from "react-router"

export function useUnsavedChanges(isDirty: boolean) {
    const navigate = useNavigate()
    // a rendered <a href> already carries the router's basename; navigate() adds it again
    const base = useHref("/").replace(/\/$/, "")
    const [showDialog, setShowDialog] = useState(false)
    const [pendingHref, setPendingHref] = useState<string | null>(null)

    // Browser beforeunload handler
    useEffect(() => {
        if (!isDirty) return
        const handler = (e: BeforeUnloadEvent) => {
            e.preventDefault()
        }
        window.addEventListener("beforeunload", handler)
        return () => window.removeEventListener("beforeunload", handler)
    }, [isDirty])

    // Intercept Next.js client-side navigation
    useEffect(() => {
        if (!isDirty) return

        const handleClick = (e: MouseEvent) => {
            const target = (e.target as HTMLElement).closest("a")
            if (!target) return
            const href = target.getAttribute("href")
            if (!href || href.startsWith("#") || href.startsWith("javascript:")) return
            // Only intercept internal links
            if (target.origin !== window.location.origin) return
            // Don't intercept if same page
            if (href === window.location.pathname) return

            e.preventDefault()
            e.stopPropagation()
            let path = target.pathname + target.search + target.hash
            if (base && (path === base || path.startsWith(base + "/"))) path = path.slice(base.length) || "/"
            setPendingHref(path)
            setShowDialog(true)
        }

        document.addEventListener("click", handleClick, true)
        return () => document.removeEventListener("click", handleClick, true)
    }, [isDirty, base])

    const confirmNavigation = useCallback(() => {
        setShowDialog(false)
        if (pendingHref) {
            navigate(pendingHref)
            setPendingHref(null)
        }
    }, [pendingHref, navigate])

    const cancelNavigation = useCallback(() => {
        setShowDialog(false)
        setPendingHref(null)
    }, [])

    return {
        showDialog,
        confirmNavigation,
        cancelNavigation,
    }
}
