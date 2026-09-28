import { Globe, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { UserDetails } from "@/lib/types"
import { displayName } from "./format"

/** Subscriptions tab for a customer who has none. */
export function NoSubscriptionPanel({ user, onCreate }: {
    user: UserDetails
    onCreate: () => void
}) {
    const name = displayName(user)
    return (
        <section aria-label="No subscription" className="flex flex-col gap-6 rounded-xl border border-border bg-card p-5 sm:p-7">
            <div className="flex items-center gap-4">
                <span className="flex size-11 shrink-0 items-center justify-center rounded-xl bg-muted text-muted-foreground"><Globe className="size-5" /></span>
                <div className="flex flex-col gap-0.5">
                    <h2 className="text-lg font-semibold">No subscription yet</h2>
                    <p className="text-sm text-muted-foreground">{name.startsWith("User #") ? "This customer" : name} has no subscription.</p>
                </div>
            </div>

            <div className="flex flex-wrap gap-2 sm:pl-[60px]">
                <Button className="h-10 gap-1.5 sm:h-9" onClick={onCreate}><Plus className="size-4" />Create subscription</Button>
            </div>
        </section>
    )
}
