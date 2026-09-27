import type { CSSProperties } from "react"
import { Toaster } from "sonner"

// Toasts are rendered by @/lib/toast (ToastCard on sonner.custom); the card
// surface and layout overrides live in globals.css.
export function ToastProvider() {
    return (
        <Toaster
            position="top-center"
            dir="auto"
            visibleToasts={3}
            gap={8}
            offset={{ top: 16 }}
            mobileOffset={{ top: "calc(env(safe-area-inset-top) + 12px)", left: 16, right: 16 }}
            swipeDirections={["top", "left", "right"]}
            containerAriaLabel="Notifications"
            toastOptions={{ unstyled: true }}
            style={{ "--width": "380px" } as CSSProperties}
        />
    )
}
