import { useEffect, useState, useRef, useCallback } from "react"
import { useSettingsStore } from "@/store/settings-store"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
    DropdownMenuRadioGroup,
    DropdownMenuRadioItem,
} from "@/components/ui/dropdown-menu"
import { Button } from "@/components/ui/button"
import { HiOutlineRefresh, HiOutlineClock } from "react-icons/hi"
import { cn, getRelativeTime } from "@/lib/utils"

interface AutoRefreshControlProps {
    onRefresh: () => void
    isRefreshing: boolean
    dataUpdatedAt?: number
}

export function AutoRefreshControl({ onRefresh, isRefreshing, dataUpdatedAt }: AutoRefreshControlProps) {
    const { refreshInterval, setRefreshInterval } = useSettingsStore()
    const [lastRefreshedLabel, setLastRefreshedLabel] = useState("")
    const timeoutRef = useRef<NodeJS.Timeout | null>(null)
    const [animationKey, setAnimationKey] = useState(0)

    // Stable ref for onRefresh to avoid restarting the timer when callback identity changes
    const onRefreshRef = useRef(onRefresh)
    onRefreshRef.current = onRefresh

    // Schedule refresh with a single setTimeout — no interval ticking
    useEffect(() => {
        if (refreshInterval === 0) {
            if (timeoutRef.current) clearTimeout(timeoutRef.current)
            return
        }

        const scheduleNext = () => {
            setAnimationKey(k => k + 1)
            timeoutRef.current = setTimeout(() => {
                onRefreshRef.current()
                scheduleNext()
            }, refreshInterval * 1000)
        }

        scheduleNext()

        return () => {
            if (timeoutRef.current) clearTimeout(timeoutRef.current)
        }
    }, [refreshInterval])

    // Update "last refreshed" label
    useEffect(() => {
        if (!dataUpdatedAt) return

        const update = () => setLastRefreshedLabel(getRelativeTime(dataUpdatedAt))
        update()
        const id = setInterval(update, 1000)
        return () => clearInterval(id)
    }, [dataUpdatedAt])

    return (
        <div className="flex items-center gap-2">
            {lastRefreshedLabel && (
                <span className="hidden sm:inline text-[10px] text-muted-foreground/60 whitespace-nowrap">
                    {lastRefreshedLabel}
                </span>
            )}
            <DropdownMenu>
                <DropdownMenuTrigger asChild>
                    <Button
                        variant="outline"
                        size="sm"
                        className={cn(
                            "gap-2 min-w-[100px] justify-between relative overflow-hidden transition-all",
                            isRefreshing && "text-primary border-primary"
                        )}
                    >
                        {refreshInterval > 0 && !isRefreshing && (
                            <div className="absolute bottom-0 left-0 h-0.5 bg-primary/20 w-full">
                                <div
                                    key={animationKey}
                                    className="h-full bg-primary"
                                    style={{
                                        width: '100%',
                                        animation: `auto-refresh-progress ${refreshInterval}s linear`,
                                    }}
                                />
                            </div>
                        )}

                        <div className="flex items-center gap-2 z-10">
                            <HiOutlineRefresh
                                className={cn("w-3.5 h-3.5", isRefreshing && "animate-spin text-primary")}
                            />
                            <span className="text-xs font-medium">
                                {refreshInterval === 0 ? "Off" : `${refreshInterval}s`}
                            </span>
                        </div>
                    </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-40">
                    <DropdownMenuLabel className="text-xs">Auto Refresh</DropdownMenuLabel>
                    <DropdownMenuSeparator />
                    <DropdownMenuRadioGroup value={refreshInterval.toString()} onValueChange={(v) => setRefreshInterval(Number(v))}>
                        <DropdownMenuRadioItem value="3">3 seconds</DropdownMenuRadioItem>
                        <DropdownMenuRadioItem value="5">5 seconds</DropdownMenuRadioItem>
                        <DropdownMenuRadioItem value="10">10 seconds</DropdownMenuRadioItem>
                        <DropdownMenuRadioItem value="15">15 seconds</DropdownMenuRadioItem>
                        <DropdownMenuRadioItem value="30">30 seconds</DropdownMenuRadioItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuRadioItem value="0">Off</DropdownMenuRadioItem>
                    </DropdownMenuRadioGroup>
                </DropdownMenuContent>
            </DropdownMenu>
        </div>
    )
}
