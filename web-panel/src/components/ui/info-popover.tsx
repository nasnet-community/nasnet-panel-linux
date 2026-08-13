import { HiOutlineInformationCircle } from "react-icons/hi"
import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from "@/components/ui/popover"
import { cn } from "@/lib/utils"

interface InfoPopoverProps {
    children: React.ReactNode
    className?: string
    iconClassName?: string
    /** Names the trigger for screen readers; it is an icon with no text. */
    label?: string
}

export function InfoPopover({ children, className, iconClassName, label }: InfoPopoverProps) {
    return (
        <Popover>
            <PopoverTrigger asChild>
                <button
                    type="button"
                    aria-label={label ?? "More information"}
                    className={cn(
                        "inline-flex items-center justify-center text-muted-foreground hover:text-foreground transition-colors",
                        iconClassName
                    )}
                >
                    <HiOutlineInformationCircle className="w-4 h-4" />
                </button>
            </PopoverTrigger>
            <PopoverContent
                className={cn("text-sm text-muted-foreground max-w-xs", className)}
                side="top"
                align="center"
            >
                {children}
            </PopoverContent>
        </Popover>
    )
}
