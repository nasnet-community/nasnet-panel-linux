import { useCallback, useRef } from "react"

interface Options {
    delay?: number
    moveTolerance?: number
    onLongPress: (target: EventTarget) => void
}

export function useLongPress({ delay = 450, moveTolerance = 6, onLongPress }: Options) {
    const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
    const startRef = useRef<{ x: number; y: number } | null>(null)

    const start = useCallback((e: React.PointerEvent) => {
        if (e.pointerType === "mouse" && e.button !== 0) return
        startRef.current = { x: e.clientX, y: e.clientY }
        const target = e.currentTarget as HTMLElement
        timerRef.current = setTimeout(() => {
            timerRef.current = null
            onLongPress(target)
        }, delay)
    }, [delay, onLongPress])

    const cancel = useCallback(() => {
        if (timerRef.current) {
            clearTimeout(timerRef.current)
            timerRef.current = null
        }
    }, [])

    const move = useCallback((e: React.PointerEvent) => {
        if (!startRef.current || !timerRef.current) return
        const dx = e.clientX - startRef.current.x
        const dy = e.clientY - startRef.current.y
        if (Math.hypot(dx, dy) > moveTolerance) cancel()
    }, [cancel, moveTolerance])

    return {
        onPointerDown: start,
        onPointerMove: move,
        onPointerUp: cancel,
        onPointerCancel: cancel,
        onPointerLeave: cancel,
    }
}
