import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"

const badgeVariants = cva(
    "inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
    {
        variants: {
            variant: {
                default: "bg-primary/10 text-primary border border-primary/20",
                secondary: "bg-secondary text-secondary-foreground",
                // emerald-500 only clears AA on a dark surface — light mode needs a
                // darker ink against the same tinted background.
                success: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400 border border-emerald-500/20",
                warning: "bg-amber-500/10 text-amber-500 border border-amber-500/20",
                danger: "bg-red-500/10 text-red-500 border border-red-500/20",
                outline: "text-foreground border border-border",
            },
        },
        defaultVariants: {
            variant: "default",
        },
    }
)

export interface BadgeProps
    extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> { }

function Badge({ className, variant, ...props }: BadgeProps) {
    return (
        <div className={cn(badgeVariants({ variant }), className)} {...props} />
    )
}

export { Badge, badgeVariants }
