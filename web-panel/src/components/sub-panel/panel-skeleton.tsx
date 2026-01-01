import { Card, CardContent } from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"

export function PanelSkeleton() {
    return (
        <div className="space-y-3 md:space-y-4 animate-in fade-in duration-300">
            {/* Header: label + online dot | status badge */}
            <div className="flex items-start justify-between gap-3">
                <div className="min-w-0 space-y-1.5">
                    <div className="flex items-center gap-2">
                        <Skeleton className="h-6 md:h-7 w-40 md:w-48" />
                        <Skeleton className="h-2.5 w-2.5 rounded-full" />
                    </div>
                    <Skeleton className="h-4 w-28" />
                </div>
                <Skeleton className="h-6 md:h-7 w-16 md:w-20 rounded-full shrink-0" />
            </div>

            {/* Stats grid: Data + Time cards */}
            <div className="grid grid-cols-2 gap-2 sm:gap-3 md:gap-4">
                {/* Data card */}
                <Card className="border-border/50">
                    <CardContent className="p-3 sm:p-4 md:p-5">
                        <div className="flex items-center gap-1.5 md:gap-2 mb-3 md:mb-4">
                            <Skeleton className="h-3.5 w-3.5 md:h-4 md:w-4 rounded" />
                            <Skeleton className="h-3 md:h-4 w-10" />
                        </div>
                        <div className="flex flex-col items-center gap-3 sm:flex-row sm:gap-4 md:gap-5">
                            <Skeleton className="h-[76px] w-[76px] md:h-24 md:w-24 rounded-full shrink-0" />
                            <div className="w-full space-y-1.5 md:space-y-2.5">
                                {["Used", "Limit", "Left"].map((label) => (
                                    <div key={label} className="flex justify-between sm:justify-start sm:flex-col sm:items-start gap-0.5">
                                        <Skeleton className="h-2.5 md:h-3 w-10" />
                                        <Skeleton className="h-3.5 sm:h-4 md:h-5 w-16 md:w-20" />
                                    </div>
                                ))}
                            </div>
                        </div>
                    </CardContent>
                </Card>

                {/* Time card */}
                <Card className="border-border/50">
                    <CardContent className="p-3 sm:p-4 md:p-5">
                        <div className="flex items-center gap-1.5 md:gap-2 mb-3 md:mb-4">
                            <Skeleton className="h-3.5 w-3.5 md:h-4 md:w-4 rounded" />
                            <Skeleton className="h-3 md:h-4 w-10" />
                        </div>
                        <div className="flex flex-col items-center gap-3 sm:flex-row sm:gap-4 md:gap-5">
                            <Skeleton className="h-[76px] w-[76px] md:h-24 md:w-24 rounded-full shrink-0" />
                            <div className="w-full space-y-1.5 md:space-y-2.5">
                                {["Remaining", "Start", "End"].map((label) => (
                                    <div key={label} className="flex justify-between sm:justify-start sm:flex-col sm:items-start gap-0.5">
                                        <Skeleton className="h-2.5 md:h-3 w-14" />
                                        <Skeleton className="h-3.5 sm:h-4 md:h-5 w-16 md:w-20" />
                                    </div>
                                ))}
                            </div>
                        </div>
                    </CardContent>
                </Card>
            </div>

            {/* Server list */}
            <div className="space-y-2.5 md:space-y-3">
                <div className="flex items-center justify-between px-1">
                    <Skeleton className="h-3.5 md:h-4 w-24" />
                    <Skeleton className="h-7 md:h-8 w-28 md:w-32 rounded-md" />
                </div>
                {[1, 2, 3].map((i) => (
                    <Card key={i} className="border-border/50 overflow-hidden">
                        <CardContent className="p-0">
                            <div className="p-3 md:p-4 space-y-3">
                                {/* Header: flag + name + badges | status */}
                                <div className="flex items-start justify-between gap-2">
                                    <div className="flex items-start gap-2.5 min-w-0">
                                        <Skeleton className="h-6 w-6 md:h-7 md:w-7 rounded shrink-0 mt-0.5" />
                                        <div className="min-w-0 space-y-1.5">
                                            <Skeleton className="h-4 md:h-5 w-28 md:w-36" />
                                            <div className="flex gap-1">
                                                <Skeleton className="h-4 w-12 rounded-full" />
                                                <Skeleton className="h-4 w-10 rounded-full" />
                                            </div>
                                        </div>
                                    </div>
                                    <Skeleton className="h-5 w-14 rounded-full shrink-0 mt-0.5" />
                                </div>
                                {/* Data usage bar */}
                                <div className="flex items-center gap-3 bg-muted/40 rounded-lg px-3 py-2.5">
                                    <Skeleton className="h-4 w-4 rounded shrink-0" />
                                    <Skeleton className="h-5 md:h-6 w-16 md:w-20" />
                                    <Skeleton className="h-3 w-8 ml-1" />
                                    <Skeleton className="h-3 w-10 ml-auto" />
                                </div>
                            </div>
                            {/* Action buttons */}
                            <div className="flex border-t border-border/50">
                                <div className="flex-1 flex items-center justify-center gap-1.5 py-2.5 md:py-3">
                                    <Skeleton className="h-3.5 w-3.5 rounded" />
                                    <Skeleton className="h-3.5 w-16" />
                                </div>
                                <div className="w-px bg-border/50" />
                                <div className="flex-1 flex items-center justify-center gap-1.5 py-2.5 md:py-3">
                                    <Skeleton className="h-3.5 w-3.5 rounded" />
                                    <Skeleton className="h-3.5 w-14" />
                                </div>
                            </div>
                        </CardContent>
                    </Card>
                ))}
            </div>

            {/* Separator */}
            <Separator className="opacity-50" />

            {/* Details card */}
            <Card className="border-border/50">
                <CardContent className="p-3 sm:p-4 md:p-5 space-y-3">
                    <Skeleton className="h-3.5 md:h-4 w-16 md:w-20" />
                    <div className="grid grid-cols-2 gap-y-2 md:gap-y-2.5">
                        {[1, 2, 3, 4].map((i) => (
                            <div key={i} className="contents">
                                <Skeleton className="h-4 md:h-5 w-16 md:w-20" />
                                <Skeleton className="h-4 md:h-5 w-24 md:w-32" />
                            </div>
                        ))}
                    </div>
                </CardContent>
            </Card>

            {/* Footer */}
            <div className="flex items-center justify-center gap-1.5 py-3">
                <Skeleton className="h-1.5 w-1.5 rounded-full" />
                <Skeleton className="h-3 w-28" />
            </div>
        </div>
    )
}
