import { useCallback, useEffect, useState } from "react"

/**
 * Browser-notification permission + sound mute toggle, persisted under
 * `chat:muted:<scope>`. Notifications fire only when the page is hidden
 * and mute is off and permission is granted.
 */
export function useChatNotifications(scope: string) {
    const muteKey = `chat:muted:${scope}`
    const [muted, setMutedState] = useState<boolean>(() => {
        if (typeof window === "undefined") return false
        try {
            return window.localStorage.getItem(muteKey) === "1"
        } catch {
            return false
        }
    })
    const [permission, setPermission] = useState<NotificationPermission>(() =>
        typeof Notification !== "undefined" ? Notification.permission : "denied",
    )

    const setMuted = useCallback(
        (next: boolean | ((prev: boolean) => boolean)) => {
            setMutedState((prev) => {
                const v = typeof next === "function" ? (next as (p: boolean) => boolean)(prev) : next
                try {
                    window.localStorage.setItem(muteKey, v ? "1" : "0")
                } catch {
                    /* */
                }
                return v
            })
        },
        [muteKey],
    )

    useEffect(() => {
        // No-op: we persist on every set; this effect just keeps a stable ref.
    }, [muted])

    const requestPermission = useCallback(async () => {
        if (typeof Notification === "undefined") return "denied" as NotificationPermission
        const p = await Notification.requestPermission()
        setPermission(p)
        return p
    }, [])

    const notify = useCallback(
        (title: string, body: string) => {
            if (muted) return
            if (
                typeof Notification !== "undefined" &&
                permission === "granted" &&
                typeof document !== "undefined" &&
                document.visibilityState !== "visible"
            ) {
                try {
                    new Notification(title, { body, silent: false })
                } catch {
                    /* */
                }
            }
        },
        [muted, permission],
    )

    return { muted, setMuted, permission, requestPermission, notify }
}
