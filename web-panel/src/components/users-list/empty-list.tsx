import type { ReactNode } from "react"
import { Clock, Search, SlidersHorizontal, Users, type LucideIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { MOD_KEY } from "@/lib/platform"
import { groupDef } from "./groups"
import { filterCount, type UsersView } from "./use-users-view"

function Empty({ icon: Icon, title, body, action }: { icon: LucideIcon; title: string; body: ReactNode; action?: ReactNode }) {
    return (
        <div className="mx-auto flex max-w-[420px] flex-col items-center gap-2.5 text-center">
            <span className="flex size-10 items-center justify-center rounded-full bg-muted text-text-secondary"><Icon className="size-[18px]" /></span>
            <p className="text-[15px] font-semibold">{title}</p>
            <p className="text-[13px] leading-relaxed text-muted-foreground">{body}</p>
            {action}
        </div>
    )
}

export function EmptyList({ view, onClearSearch, onClearFilters }: { view: UsersView; onClearSearch: () => void; onClearFilters: () => void }) {
    if (view.q.trim()) {
        return (
            <Empty
                icon={Search}
                title={`No user matches “${view.q.trim()}”`}
                body={<>Search covers names, @usernames, Telegram IDs, subscription labels and config links. Press <kbd className="rounded border border-border px-1 font-mono text-[11px]">{MOD_KEY}K</kbd> to search subscriptions and pages too.</>}
                action={<Button variant="outline" size="sm" onClick={onClearSearch}>Clear search</Button>}
            />
        )
    }
    if (filterCount(view) > 0) {
        return <Empty icon={SlidersHorizontal} title="No users match these filters" body="Loosen or clear the filters to see more people." action={<Button variant="outline" size="sm" onClick={onClearFilters}>Clear filters</Button>} />
    }
    const def = groupDef(view.group)
    return <Empty icon={view.group === "ending" ? Clock : Users} title={def.empty.title} body={def.empty.body} />
}
