import { format, isToday, isYesterday } from "date-fns"
export function DateSeparator({ iso }: { iso: string }) {
    const d = new Date(iso)
    const label = isToday(d) ? "Today" : isYesterday(d) ? "Yesterday" : format(d, "EEE, MMM d")
    return (
        <div className="my-2 flex justify-center">
            <span className="rounded-full bg-muted px-3 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
                {label}
            </span>
        </div>
    )
}
