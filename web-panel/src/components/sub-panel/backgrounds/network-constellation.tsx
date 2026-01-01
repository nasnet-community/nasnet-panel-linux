import { useChartPalette } from "@/lib/design/palette"

export function NetworkConstellationBg() {
    const c = useChartPalette()
    return (
        <div className="fixed inset-0 z-0 pointer-events-none overflow-hidden">
            {/* Soft emerald + sky accent gradients (subtle in both themes) */}
            <div
                className="absolute inset-0"
                style={{
                    background:
                        `radial-gradient(ellipse at 20% 0%, oklch(from ${c.success} l c h / 0.07), transparent 55%), radial-gradient(ellipse at 85% 100%, oklch(from ${c.info} l c h / 0.05), transparent 55%)`,
                }}
            />
            {/* Static square grid — foreground color w/ alpha so it flips with theme */}
            <div
                className="absolute inset-0"
                style={{
                    backgroundImage:
                        "linear-gradient(oklch(from var(--foreground) l c h / 0.055) 1px, transparent 1px), linear-gradient(90deg, oklch(from var(--foreground) l c h / 0.055) 1px, transparent 1px)",
                    backgroundSize: "56px 56px",
                    maskImage:
                        "radial-gradient(ellipse at center, black 45%, transparent 80%)",
                    WebkitMaskImage:
                        "radial-gradient(ellipse at center, black 45%, transparent 80%)",
                }}
            />
            {/* Vignette — derives from background color so it darkens in dark mode and lightens in light mode */}
            <div
                className="absolute inset-0"
                style={{
                    background:
                        "radial-gradient(ellipse at center, transparent 40%, oklch(from var(--background) l c h / 0.7) 100%)",
                }}
            />
        </div>
    )
}
