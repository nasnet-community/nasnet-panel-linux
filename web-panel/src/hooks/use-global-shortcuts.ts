import { useEffect, useRef } from "react"
import { useNavigate } from "react-router"
import { GO_KEYS } from "@/components/layouts/nav-config"
import { useCommandPalette } from "@/store/command-palette-store"

export function isTypingTarget(target: EventTarget | null): boolean {
    if (!(target instanceof HTMLElement)) return false
    if (target.isContentEditable) return true
    if (["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName)) return true
    return !!target.closest(".xterm, .monaco-editor")
}

export function useGlobalShortcuts(onToggleSidebar: () => void) {
    const navigate = useNavigate()
    // Keep the listener stable so a render between `g` and its letter does not drop the sequence.
    const toggleRef = useRef(onToggleSidebar)
    useEffect(() => {
        toggleRef.current = onToggleSidebar
    })

    useEffect(() => {
        let gPressedAt = 0
        const onKeyDown = (e: KeyboardEvent) => {
            if (e.defaultPrevented) return
            const target = e.target instanceof HTMLElement ? e.target : null
            if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === "k") {
                if (target?.closest(".xterm")) return
                e.preventDefault()
                useCommandPalette.getState().toggle()
                return
            }
            if (e.metaKey || e.ctrlKey || e.altKey || isTypingTarget(e.target)) return
            if (e.key === "[") {
                e.preventDefault()
                toggleRef.current()
            } else if (e.key === "?") {
                e.preventDefault()
                useCommandPalette.getState().setShortcutsOpen(true)
            } else if (e.key === "g") {
                gPressedAt = Date.now()
            } else if (gPressedAt && Date.now() - gPressedAt < 1000 && GO_KEYS[e.key]) {
                e.preventDefault()
                gPressedAt = 0
                navigate(GO_KEYS[e.key])
            } else {
                gPressedAt = 0
            }
        }
        window.addEventListener("keydown", onKeyDown)
        return () => window.removeEventListener("keydown", onKeyDown)
    }, [navigate])
}
