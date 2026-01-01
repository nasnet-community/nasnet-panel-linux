import { Toaster } from "sonner"

export function ToastProvider() {
    return (
        <Toaster
            position="bottom-center"
            theme="dark"
            dir="auto"
            expand={false}
            richColors={true}
            closeButton={false}
            duration={2500}
            gap={6}
            offset={24}
            mobileOffset={{ bottom: 100 }}
            swipeDirections={["left", "right"]}
            className="toaster group"
            toastOptions={{
                classNames: {
                    toast: "group toast group-[.toaster]:bg-background/90 group-[.toaster]:text-foreground group-[.toaster]:border-white/10 group-[.toaster]:shadow-2xl group-[.toaster]:shadow-black/40 group-[.toaster]:backdrop-blur-xl group-[.toaster]:rounded-full group-[.toaster]:py-2.5 group-[.toaster]:px-4 group-[.toaster]:gap-2 group-[.toaster]:text-sm",
                    description: "group-[.toast]:text-muted-foreground group-[.toast]:font-normal group-[.toast]:text-xs",
                    actionButton: "group-[.toast]:bg-foreground group-[.toast]:text-background font-medium",
                    cancelButton: "group-[.toast]:bg-muted group-[.toast]:text-muted-foreground font-medium",
                    success: "!border-emerald-500/20 !bg-emerald-950/30 !text-emerald-100",
                    error: "!border-rose-500/20 !bg-rose-950/30 !text-red-100",
                    warning: "!border-amber-500/20 !bg-amber-950/30 !text-amber-100",
                    info: "!border-blue-500/20 !bg-blue-950/30 !text-blue-100",
                },
                style: {
                    // Resetting default inline styles to allow Tailwind classes to take precedence
                }
            }}
        />
    )
}
