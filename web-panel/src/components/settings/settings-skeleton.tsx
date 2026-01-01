import { Skeleton } from "@/components/ui/skeleton"

export function SettingsSkeleton() {
    return (
        <div className="space-y-6 animate-in fade-in duration-500">
            {/* Header skeleton */}
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div className="space-y-2">
                    <Skeleton className="h-8 w-32" />
                    <Skeleton className="h-4 w-64" />
                </div>
                <div className="flex gap-2 self-end sm:self-auto">
                    <Skeleton className="h-9 w-28" />
                    <Skeleton className="h-9 w-32" />
                    <Skeleton className="h-9 w-24" />
                </div>
            </div>

            {/* Search skeleton */}
            <Skeleton className="h-10 w-full max-w-md" />

            {/* Main content skeleton */}
            <div className="flex gap-6">
                {/* Sidebar skeleton (desktop) */}
                <div className="hidden md:flex w-[220px] shrink-0">
                    <div className="space-y-1 w-full">
                        {Array.from({ length: 8 }).map((_, i) => (
                            <Skeleton key={i} className="h-9 w-full rounded-lg" />
                        ))}
                    </div>
                </div>

                {/* Content skeleton */}
                <div className="flex-1 min-w-0 rounded-xl border bg-card p-6 space-y-4">
                    {/* Category header */}
                    <div className="flex items-center gap-3">
                        <Skeleton className="h-9 w-9 rounded-lg" />
                        <div className="space-y-1.5">
                            <Skeleton className="h-5 w-40" />
                            <Skeleton className="h-3.5 w-64" />
                        </div>
                    </div>

                    <Skeleton className="h-px w-full" />

                    {/* Field skeletons */}
                    <div className="space-y-2">
                        {Array.from({ length: 6 }).map((_, i) => (
                            <Skeleton key={i} className="h-12 w-full rounded-lg" />
                        ))}
                    </div>

                    <Skeleton className="h-px w-full" />

                    {/* Footer skeleton */}
                    <div className="flex justify-end gap-2">
                        <Skeleton className="h-8 w-16" />
                        <Skeleton className="h-8 w-28" />
                    </div>
                </div>
            </div>
        </div>
    )
}
