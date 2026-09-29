import { cn } from "@/lib/utils"
import type { CoreUser } from "@/lib/types/cores"

function initials(email: string): string {
    const name = email.split("@")[0] || email
    return name.slice(0, 2).toUpperCase()
}

// A small, deterministic palette so the same email always gets the same tint.
const TINTS = [
    "bg-teal-500/15 text-teal-600 dark:text-teal-300",
    "bg-blue-500/15 text-blue-600 dark:text-blue-300",
    "bg-violet-500/15 text-violet-600 dark:text-violet-300",
    "bg-amber-500/15 text-amber-600 dark:text-amber-300",
    "bg-emerald-500/15 text-emerald-600 dark:text-emerald-300",
    "bg-rose-500/15 text-rose-600 dark:text-rose-300",
]

function tintFor(email: string): string {
    let h = 0
    for (let i = 0; i < email.length; i++) h = (h * 31 + email.charCodeAt(i)) >>> 0
    return TINTS[h % TINTS.length]
}

/** Stacked email-initial avatars with a +N overflow chip. */
export function CoreMemberAvatars({
    users,
    max = 4,
    className,
}: {
    users: Pick<CoreUser, "id" | "email">[]
    max?: number
    className?: string
}) {
    if (users.length === 0) {
        return <span className="text-xs text-muted-foreground">No users</span>
    }
    const shown = users.slice(0, max)
    const overflow = users.length - shown.length
    return (
        <div className={cn("flex items-center", className)}>
            <div className="flex -space-x-1.5">
                {shown.map((u) => (
                    <div
                        key={u.id}
                        title={u.email}
                        className={cn(
                            "flex size-6 items-center justify-center rounded-full border border-background text-[11px] font-medium ring-1 ring-background",
                            tintFor(u.email),
                        )}
                    >
                        {initials(u.email)}
                    </div>
                ))}
                {overflow > 0 && (
                    <div className="flex size-6 items-center justify-center rounded-full border border-background bg-muted text-[11px] font-medium text-muted-foreground ring-1 ring-background">
                        +{overflow}
                    </div>
                )}
            </div>
        </div>
    )
}
