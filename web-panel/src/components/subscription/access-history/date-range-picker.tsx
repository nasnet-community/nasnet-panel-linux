import { useMemo } from "react"
import { CalendarIcon } from "lucide-react"
import type { DateRange } from "react-day-picker"
import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { cn } from "@/lib/utils"

export interface DateRangePickerProps {
    value: DateRange | undefined
    onChange: (range: DateRange | undefined) => void
    // disable future dates beyond this point
    maxDate?: Date
    // earliest selectable day (e.g. now - retentionDays)
    minDate?: Date
    placeholder?: string
    className?: string
}

function fmt(d: Date): string {
    return d.toLocaleDateString([], { year: "numeric", month: "short", day: "numeric" })
}

export function DateRangePicker({
    value,
    onChange,
    maxDate,
    minDate,
    placeholder = "Select date range",
    className,
}: DateRangePickerProps) {
    const label = useMemo(() => {
        if (!value?.from) return placeholder
        if (!value.to) return fmt(value.from)
        return `${fmt(value.from)} – ${fmt(value.to)}`
    }, [value, placeholder])

    return (
        <Popover>
            <PopoverTrigger asChild>
                <Button
                    variant="outline"
                    size="sm"
                    className={cn("h-8 justify-start gap-2 text-xs font-normal", !value?.from && "text-muted-foreground", className)}
                >
                    <CalendarIcon className="h-3.5 w-3.5" />
                    {label}
                </Button>
            </PopoverTrigger>
            <PopoverContent className="w-auto p-0" align="start">
                <Calendar
                    mode="range"
                    selected={value}
                    onSelect={onChange}
                    numberOfMonths={2}
                    disabled={(d) => {
                        if (maxDate && d > maxDate) return true
                        if (minDate && d < minDate) return true
                        return false
                    }}
                    defaultMonth={value?.from ?? new Date()}
                />
            </PopoverContent>
        </Popover>
    )
}
