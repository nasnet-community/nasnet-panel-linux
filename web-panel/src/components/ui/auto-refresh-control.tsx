import { useEffect, useState } from "react"
import { useSettingsStore } from "@/store/settings-store"
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
    DropdownMenuRadioGroup,
    DropdownMenuRadioItem,
} from "@/components/ui/dropdown-menu"
import { Button } from "@/components/ui/button"
import { HiOutlineRefresh } from "react-icons/hi"
import { cn, getRelativeTime } from "@/lib/utils"

interface AutoRefreshControlProps {
    isRefreshing: boolean
    dataUpdatedAt?: number
}

// Interval picker only — react-query owns the timer. The hooks behind each page
// read the same setting via useRefreshInterval().
export function AutoRefreshControl({ isRefreshing, dataUpdatedAt }: AutoRefreshControlProps) {
    const { refreshInterval, setRefreshInterval } = useSettingsStore()
    const [lastRefreshedLabel, setLastRefreshedLabel] = useState("")

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
                                {/* Re-keyed on each landing, so the bar tracks real fetches. */}
                                <div
                                    key={dataUpdatedAt ?? 0}
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
