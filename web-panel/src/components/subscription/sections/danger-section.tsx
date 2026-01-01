import { useState, useRef, useEffect } from "react"
import { Pause, Play, RefreshCw, Trash2, Eye, EyeOff, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import { cn } from "@/lib/utils"
import type { Subscription } from "@/lib/types"
import type { SubscriptionDerived } from "@/lib/subscription-derived"
import { SectionHeader } from "./section-header"

type PanelPwMode = "default" | "custom" | "disabled"

interface DangerSectionProps {
    subscription: Subscription
    derived: SubscriptionDerived
    onPauseResume: () => void
    onReset: () => void
    onRevoke: () => void
    onDelete: () => void
    onSetPanelPassword: (mode: PanelPwMode, password?: string) => void
    mutationsPending: {
        pauseResume: boolean
        reset: boolean
        revoke: boolean
        delete: boolean
        panelPassword: boolean
    }
}

export function DangerSection({
    subscription,
    derived,
    onPauseResume,
    onReset,
    onRevoke,
    onDelete,
    onSetPanelPassword,
    mutationsPending,
}: DangerSectionProps) {
    const initialMode: PanelPwMode = subscription.panel_password_mode || "default"
    const [panelPwMode, setPanelPwMode] = useState<PanelPwMode>(initialMode)
    const [panelPwValue, setPanelPwValue] = useState("")
    const [showPanelPw, setShowPanelPw] = useState(false)
    const radioRefs = useRef<Array<HTMLButtonElement | null>>([])

    useEffect(() => {
        setPanelPwMode(subscription.panel_password_mode || "default")
        setPanelPwValue("")
        setShowPanelPw(false)
    }, [subscription.id, subscription.panel_password_mode])

    const modes: { value: PanelPwMode; label: string; hint: string }[] = [
        { value: "default", label: "Default", hint: "Uses the global panel password from settings." },
        { value: "custom", label: "Custom", hint: "This subscription uses its own password." },
        { value: "disabled", label: "Disabled", hint: "No password required for this subscription." },
    ]

    const handleModeChange = (mode: PanelPwMode) => {
        setPanelPwMode(mode)
        if (mode !== "custom") {
            setPanelPwValue("")
            onSetPanelPassword(mode)
        }
    }

    const handleRadioKeyDown = (e: React.KeyboardEvent, idx: number) => {
        if (e.key === "ArrowRight" || e.key === "ArrowDown") {
            e.preventDefault()
            const next = (idx + 1) % modes.length
            radioRefs.current[next]?.focus()
            handleModeChange(modes[next].value)
        } else if (e.key === "ArrowLeft" || e.key === "ArrowUp") {
            e.preventDefault()
            const prev = (idx - 1 + modes.length) % modes.length
            radioRefs.current[prev]?.focus()
            handleModeChange(modes[prev].value)
        }
    }

    return (
        <div className="space-y-4">
            {/* Panel Security */}
            <div className="space-y-1.5">
                <SectionHeader>Panel Security</SectionHeader>
                <div
                    className="flex gap-1 p-0.5 rounded-md bg-muted/50 border"
                    role="radiogroup"
                    aria-label="Panel password mode"
                >
                    {modes.map((mode, idx) => (
                        <button
                            key={mode.value}
                            ref={(el) => { radioRefs.current[idx] = el }}
                            type="button"
                            role="radio"
                            aria-checked={panelPwMode === mode.value}
                            tabIndex={panelPwMode === mode.value ? 0 : -1}
                            className={cn(
                                "flex-1 px-2 py-1 rounded text-xs font-medium transition-all outline-none focus-visible:ring-2 focus-visible:ring-ring",
                                panelPwMode === mode.value
                                    ? "bg-background shadow-sm text-foreground"
                                    : "text-muted-foreground hover:text-foreground",
                            )}
                            onClick={() => handleModeChange(mode.value)}
                            onKeyDown={(e) => handleRadioKeyDown(e, idx)}
                        >
                            {mode.label}
                        </button>
                    ))}
                </div>
                {panelPwMode === "custom" && (
                    <div className="flex gap-1.5 items-center mt-1.5">
                        <div className="relative flex-1">
                            <Input
                                type={showPanelPw ? "text" : "password"}
                                value={panelPwValue}
                                onChange={(e) => setPanelPwValue(e.target.value)}
                                placeholder="Enter password…"
                                className="h-7 text-sm pr-8"
                                aria-label="Custom panel password"
                            />
                            <button
                                type="button"
                                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
                                onClick={() => setShowPanelPw(!showPanelPw)}
                                tabIndex={-1}
                                aria-label={showPanelPw ? "Hide password" : "Show password"}
                            >
                                {showPanelPw ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                            </button>
                        </div>
                        <Button
                            size="sm"
                            className="h-7 text-xs"
                            onClick={() => {
                                if (panelPwValue.trim()) {
                                    onSetPanelPassword("custom", panelPwValue.trim())
                                    setPanelPwValue("")
                                }
                            }}
                            disabled={mutationsPending.panelPassword || !panelPwValue.trim()}
                        >
                            {mutationsPending.panelPassword ? (
                                <Loader2 className="w-3 h-3 animate-spin" />
                            ) : (
                                "Save"
                            )}
                        </Button>
                    </div>
                )}
                <p className="text-[10px] text-muted-foreground">
                    {modes.find((m) => m.value === panelPwMode)?.hint}
                </p>
            </div>

            <Separator />

            {/* Actions */}
            <div className="space-y-2">
                <SectionHeader tone="danger">Danger Zone</SectionHeader>

                <div className="flex gap-2">
                    <Button
                        variant={derived.isActive ? "outline" : "default"}
                        size="sm"
                        className="h-7 text-xs flex-1"
                        onClick={onPauseResume}
                        disabled={
                            mutationsPending.pauseResume ||
                            (subscription.status !== "active" && subscription.status !== "paused")
                        }
                    >
                        {derived.isActive ? (
                            <><Pause className="w-3.5 h-3.5 mr-1" /> Pause</>
                        ) : (
                            <><Play className="w-3.5 h-3.5 mr-1" /> Resume</>
                        )}
                    </Button>
                    <Button
                        variant="outline"
                        size="sm"
                        className="h-7 text-xs flex-1"
                        onClick={onReset}
                        disabled={mutationsPending.reset}
                    >
                        <RefreshCw className="w-3.5 h-3.5 mr-1" />
                        Reset data
                    </Button>
                </div>

                <div className="flex gap-2">
                    <Button
                        variant="outline"
                        size="sm"
                        className={cn(
                            "flex-1 h-8 text-xs",
                            "text-amber-600 border-amber-500/40 hover:bg-amber-500/10",
                            "dark:text-amber-400 dark:border-amber-500/30 dark:hover:bg-amber-950/30",
                        )}
                        onClick={onRevoke}
                        disabled={mutationsPending.revoke}
                    >
                        <Trash2 className="w-3.5 h-3.5 mr-1.5" />
                        Revoke access
                    </Button>
                    <Button
                        variant="destructive"
                        size="sm"
                        className="flex-1 h-8 text-xs"
                        onClick={onDelete}
                        disabled={mutationsPending.delete}
                    >
                        <Trash2 className="w-3.5 h-3.5 mr-1.5" />
                        Delete
                    </Button>
                </div>
                <p className="text-[10px] text-muted-foreground">
                    Revoke disables access; the record stays. Delete removes everything permanently.
                </p>
            </div>
        </div>
    )
}
