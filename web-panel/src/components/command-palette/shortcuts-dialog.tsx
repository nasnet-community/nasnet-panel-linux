import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { GO_KEYS, PALETTE_PAGES } from "@/components/layouts/nav-config"
import { IS_MAC, MOD_KEY } from "@/lib/platform"
import { useCommandPalette } from "@/store/command-palette-store"

const ROWS: { label: string; keys: string[] }[] = [
    { label: "search or jump to", keys: [MOD_KEY.trim(), "K"] },
    { label: "collapse or expand the sidebar", keys: ["["] },
    ...Object.entries(GO_KEYS).map(([key, href]) => ({
        label: `go to ${(PALETTE_PAGES.find((p) => p.href === href)?.label ?? href).toLowerCase()}`,
        keys: ["G", key.toUpperCase()],
    })),
    { label: "focus notifications", keys: [IS_MAC ? "⌥" : "Alt", "T"] },
    { label: "show this list", keys: ["?"] },
]

export function ShortcutsDialog() {
    const open = useCommandPalette((s) => s.shortcutsOpen)
    const setOpen = useCommandPalette((s) => s.setShortcutsOpen)
    return (
        <Dialog open={open} onOpenChange={setOpen}>
            <DialogContent className="max-w-sm">
                <DialogHeader>
                    <DialogTitle>Keyboard shortcuts</DialogTitle>
                    <DialogDescription>Ignored while you type in a field, the config editor or the terminal.</DialogDescription>
                </DialogHeader>
                <ul className="space-y-2 text-sm">
                    {ROWS.map((row) => (
                        <li key={row.label} className="flex items-center justify-between gap-4">
                            <span>{row.label}</span>
                            <span className="flex items-center gap-1">
                                {row.keys.map((k) => (
                                    <kbd key={k} className="min-w-6 rounded border px-1.5 text-center font-mono text-[11px] leading-5 text-muted-foreground">{k}</kbd>
                                ))}
                            </span>
                        </li>
                    ))}
                </ul>
            </DialogContent>
        </Dialog>
    )
}
